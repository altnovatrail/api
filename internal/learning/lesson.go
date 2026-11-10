package learning

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/module-kit/openview"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/packages"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// runEnds is each end reason of a lesson run of the schema in the contract's
// words.
var runEnds = map[string]v1.RunEndReason{
	store.RunPassed:      v1.RunEndReason_RUN_END_REASON_PASSED,
	store.RunUnchecked:   v1.RunEndReason_RUN_END_REASON_UNCHECKED,
	store.RunNotSuitable: v1.RunEndReason_RUN_END_REASON_NOT_SUITABLE,
	store.RunAbandoned:   v1.RunEndReason_RUN_END_REASON_ABANDONED,
}

// runEndedError is a lesson run ended, by its reason.
type runEndedError struct{ reason string }

func (e *runEndedError) Error() string { return "learning: the lesson run has ended " + e.reason }

// runEnd is how a run ended, in the contract's words. The engine's choice that
// the end gives comes with the engine's code (T40): until then app asks
// ChooseNext.
func runEnd(reason string) *v1.RunEnd { return &v1.RunEnd{Reason: runEnds[reason]} }

// lessonID is the full ID of a module's lesson (§ 8 of the format's text).
func lessonID(module formatv0.ModuleID, lesson formatv0.LessonID) string {
	return string(module) + "/lesson/" + string(lesson)
}

// lessonOf is the lesson of pkg by its full ID, and whether pkg has it.
func lessonOf(pkg *modulepackage.Package, full string) (formatv0.Lesson, bool) {
	local, ok := strings.CutPrefix(full, lessonID(pkg.View.Manifest.ID, ""))
	if !ok {
		return formatv0.Lesson{}, false
	}
	lesson, ok := pkg.View.Lessons[formatv0.LessonID(local)]
	return lesson, ok
}

// heldRun is an open lesson run as a request acts on it: its row, held, its
// package, and its lesson.
type heldRun struct {
	row    store.LessonRun
	pkg    *modulepackage.Package
	lesson formatv0.Lesson
}

// lessonSuits says whether the lesson of pkg suits a learner of category: by
// its own age categories, or by the module's audience when it names none, and
// each of its tasks by theirs (R58 spec).
func lessonSuits(pkg *modulepackage.Package, lesson formatv0.Lesson, category formatv0.AgeCategory) bool {
	categories := lesson.AgeCategories
	if len(categories) == 0 {
		categories = pkg.View.Manifest.Audience.AgeCategories
	}
	if !slices.Contains(categories, category) {
		return false
	}
	for _, seg := range lesson.Segments {
		if seg.Task == nil {
			continue
		}
		task, ok := pkg.View.Tasks[*seg.Task]
		if !ok || !suits(pkg, task, category) {
			return false
		}
	}
	return true
}

// checkRun checks the profile's run row, held in tx, as every request on it
// does (R13; architecture 3.7): one ended is a runEndedError; one on a package
// the index does not know, errPackageUnknown, with nothing written. A run whose
// package the image no longer gives ends "unchecked", one with no action for 30
// days "abandoned", and, when full, one whose module is no longer connected or
// has no current version for the learner, or whose lesson or a task of it no
// longer suits the learner on the run's package, "not suitable", with its open
// attempt; the end commits, and the request is refused "run ended" after it.
// The checks passed, the request is an action on the run.
func (s *Service) checkRun(ctx context.Context, tx pgx.Tx, profileID string, row store.LessonRun, category formatv0.AgeCategory, now time.Time, full bool) (heldRun, error) {
	if row.EndReason != nil {
		return heldRun{}, &runEndedError{reason: *row.EndReason}
	}
	digest := formatv0.Digest(row.PackageDigest)
	switch s.set.Status(digest) {
	case packages.Unknown:
		return heldRun{}, errPackageUnknown
	case packages.NotGiven:
		return heldRun{}, s.endRun(ctx, tx, profileID, row.ID, store.RunUnchecked, now)
	}
	if now.After(row.LastActionAt.Add(store.RunAbandonedAfter)) {
		return heldRun{}, s.endRun(ctx, tx, profileID, row.ID, store.RunAbandoned, now)
	}
	pkg := s.set.Packages[digest]
	module := pkg.View.Manifest.ID
	lesson, ok := lessonOf(pkg, row.LessonID)
	if full && ok {
		connected, err := store.ModuleConnected(ctx, tx, profileID, string(module))
		if err != nil {
			return heldRun{}, err
		}
		_, current := s.set.CurrentFor(module, category)
		ok = connected && current && lessonSuits(pkg, lesson, category)
	}
	if !ok {
		return heldRun{}, s.endRun(ctx, tx, profileID, row.ID, store.RunNotSuitable, now)
	}
	if err := store.TouchLessonRun(ctx, tx, profileID, row.ID, now); err != nil {
		return heldRun{}, err
	}
	return heldRun{row: row, pkg: pkg, lesson: lesson}, nil
}

// endRun ends the run with reason at now, and refuses the request "run ended"
// after the end commits.
func (s *Service) endRun(ctx context.Context, tx pgx.Tx, profileID, runID, reason string, now time.Time) error {
	if err := store.EndLessonRun(ctx, tx, profileID, runID, reason, now); err != nil {
		return err
	}
	return rpc.Keep(refused(ctx, &runEndedError{reason: reason}))
}

// holdRun is the profile's lesson run by its ID, held in tx and checked.
func (s *Service) holdRun(ctx context.Context, tx pgx.Tx, profileID, runID string, category formatv0.AgeCategory, now time.Time, full bool) (heldRun, error) {
	if !store.CanonicalID(runID) {
		return heldRun{}, store.ErrNotFound
	}
	row, err := store.HoldLessonRun(ctx, tx, profileID, runID)
	if err != nil {
		return heldRun{}, err
	}
	return s.checkRun(ctx, tx, profileID, row, category, now, full)
}

// OpenLesson opens the lesson of an engine's choice (R13): the open run of the
// lesson, when the profile has one, or else a new run on the package of the
// module's current version, which the module connected, its current version
// for the learner with the lesson, and the lesson and each of its tasks suited
// to the learner allow. A choice opens one run: once it has ended, the choice
// gets "run ended". Two openings at once give the one run: the second reads the
// first's again. It carries no key and repeats by the state of the run. A
// segment that this code cannot begin — a task with a rubric, a workbook, or
// handwriting AI may not read for the learner — is not checked here: its
// attempt is "not admitted", and the run stays open (Д26 RUN.md).
func (l *LearningService) OpenLesson(ctx context.Context, req *v1.OpenLessonRequest) (*v1.OpenLessonResponse, error) {
	s := l.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	profileID, choiceID := req.GetProfileId(), req.GetChoiceId()
	var run *v1.LessonRun
	err = s.write(ctx, func(tx pgx.Tx, now time.Time) error {
		if !store.CanonicalID(choiceID) {
			return store.ErrNotFound
		}
		c, err := store.ChoiceOf(ctx, tx, profileID, choiceID)
		switch {
		case err != nil:
			return err
		case c.Kind != "lesson":
			return rpc.Malformed("choice_id")
		}
		h, err := s.openRun(ctx, tx, profileID, category, c, choiceID, now)
		if err != nil {
			return err
		}
		run, err = lessonRun(ctx, tx, profileID, h)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &v1.OpenLessonResponse{Run: run}, nil
}

// openRun is the run the choice c opens: its own, the profile's open run of its
// lesson, or a new one. A new one that another request began meanwhile is read
// again.
func (s *Service) openRun(ctx context.Context, tx pgx.Tx, profileID string, category formatv0.AgeCategory, c store.Choice, choiceID string, now time.Time) (heldRun, error) {
	lesson := lessonID(formatv0.ModuleID(c.ModuleID), formatv0.LessonID(c.LessonID))
	for range 2 {
		if row, found, err := store.RunOfChoice(ctx, tx, profileID, choiceID); err != nil || found {
			if err != nil {
				return heldRun{}, err
			}
			return s.checkRun(ctx, tx, profileID, row, category, now, true)
		}
		row, found, err := store.OpenRunOf(ctx, tx, profileID, lesson)
		if err != nil {
			return heldRun{}, err
		}
		if found {
			h, err := s.checkRun(ctx, tx, profileID, row, category, now, true)
			if _, ended := rpc.Kept(err); !ended {
				return h, err
			}
			// The check ended another choice's run, which this choice never named: the
			// end stands in tx, and this choice begins a run of its own.
		}
		h, err := s.newRun(ctx, tx, profileID, category, c, choiceID, now)
		if err != store.ErrRunBegun {
			return h, err
		}
	}
	return heldRun{}, store.ErrRunBegun
}

// newRun begins the run of the choice c on the package of its module's current
// version for the learner, once the module is connected, that version has the
// lesson, and the lesson and each of its tasks suit the learner.
func (s *Service) newRun(ctx context.Context, tx pgx.Tx, profileID string, category formatv0.AgeCategory, c store.Choice, choiceID string, now time.Time) (heldRun, error) {
	pkg, err := s.packageOf(ctx, tx, profileID, category, begun{module: formatv0.ModuleID(c.ModuleID)})
	if err != nil {
		return heldRun{}, err
	}
	lesson, ok := pkg.View.Lessons[formatv0.LessonID(c.LessonID)]
	switch {
	case !ok:
		return heldRun{}, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION)
	case !lessonSuits(pkg, lesson, category):
		return heldRun{}, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_SUITABLE)
	}
	m := pkg.View.Manifest
	id, err := store.BeginLessonRun(ctx, tx, profileID, store.NewRun{LessonID: lessonID(m.ID, lesson.ID),
		ModuleID: string(m.ID), ModuleVersion: string(m.Version), PackageDigest: string(pkg.Digest), ChoiceID: choiceID}, now)
	if err != nil {
		return heldRun{}, err
	}
	row := store.LessonRun{ID: id, LessonID: lessonID(m.ID, lesson.ID), ModuleID: string(m.ID),
		ModuleVersion: string(m.Version), PackageDigest: string(pkg.Digest), ChoiceID: choiceID, LastActionAt: now}
	return heldRun{row: row, pkg: pkg, lesson: lesson}, nil
}

// lessonRun is the open run h as app leads the lesson by it: its ID, the open
// view of the lesson whole, its package, the places of the task segments that
// have ended, and the open attempt of a segment of it.
func lessonRun(ctx context.Context, tx pgx.Tx, profileID string, h heldRun) (*v1.LessonRun, error) {
	view, err := openview.Lesson(h.lesson)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	ended, err := store.EndedSegments(ctx, tx, profileID, h.row.ID)
	if err != nil {
		return nil, err
	}
	run := &v1.LessonRun{LessonRunId: h.row.ID, PackageDigest: h.row.PackageDigest, EndedSegments: places(ended),
		Lesson: &v1.OpenView{Json: data, ModuleFormatVersion: string(h.pkg.View.Manifest.ModuleFormatVersion)}}
	open, found, err := store.RunAttempt(ctx, tx, profileID, h.row.ID)
	if err != nil {
		return nil, err
	}
	if found {
		run.OpenAttempt = &v1.OpenAttemptRef{AttemptId: open.ID,
			Segment: &v1.SegmentPlace{LessonRunId: h.row.ID, Segment: int32(*open.Segment)}}
	}
	return run, nil
}

// places is the places of segments in the contract's words.
func places(ps []int) []int32 {
	out := make([]int32, len(ps))
	for i, p := range ps {
		out[i] = int32(p)
	}
	return out
}

// GetLessonRun is an open lesson run again, by its ID, after a restart or on
// another device, checked as OpenLesson checks it. It repeats by the state of
// the run.
func (l *LearningService) GetLessonRun(ctx context.Context, req *v1.GetLessonRunRequest) (*v1.GetLessonRunResponse, error) {
	s := l.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	var run *v1.LessonRun
	err = s.write(ctx, func(tx pgx.Tx, now time.Time) error {
		h, err := s.holdRun(ctx, tx, req.GetProfileId(), req.GetLessonRunId(), category, now, true)
		if err != nil {
			return err
		}
		run, err = lessonRun(ctx, tx, req.GetProfileId(), h)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &v1.GetLessonRunResponse{Run: run}, nil
}

// CompleteLesson is "lesson done": it ends the run "passed" once every task
// segment of the lesson has ended, and earlier refuses "segments not ended" with
// the places of those that have. A run that ended "passed" gets the same answer
// again; one that ended otherwise, "run ended". It is no check of the lesson:
// only of its package and its term (R13). It repeats by the state of the run.
func (l *LearningService) CompleteLesson(ctx context.Context, req *v1.CompleteLessonRequest) (*v1.CompleteLessonResponse, error) {
	s := l.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	profileID, runID := req.GetProfileId(), req.GetLessonRunId()
	err = s.write(ctx, func(tx pgx.Tx, now time.Time) error {
		if !store.CanonicalID(runID) {
			return store.ErrNotFound
		}
		row, err := store.HoldLessonRun(ctx, tx, profileID, runID)
		switch {
		case err != nil:
			return err
		case row.EndReason != nil && *row.EndReason == store.RunPassed:
			return nil
		}
		h, err := s.checkRun(ctx, tx, profileID, row, category, now, false)
		if err != nil {
			return err
		}
		ended, err := store.EndedSegments(ctx, tx, profileID, h.row.ID)
		if err != nil {
			return err
		}
		for i, seg := range h.lesson.Segments {
			if seg.Task != nil && !slices.Contains(ended, i+1) {
				return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_SEGMENTS_NOT_ENDED, func(r *v1.Refusal) {
					r.Data = &v1.Refusal_EndedSegments{EndedSegments: &v1.EndedSegments{Places: places(ended)}}
				})
			}
		}
		return store.EndLessonRun(ctx, tx, profileID, h.row.ID, store.RunPassed, now)
	})
	if err != nil {
		return nil, err
	}
	return &v1.CompleteLessonResponse{End: runEnd(store.RunPassed)}, nil
}

// segmentBegun checks the beginning of an attempt on the task segment at place
// of the profile's run (R13): the run held and checked whole, the place a task
// segment of its lesson, every task segment before it ended, and it not. The
// open attempt of the segment, when it has one, is what the beginning gives.
func (s *Service) segmentBegun(ctx context.Context, tx pgx.Tx, profileID, runID string, place int, category formatv0.AgeCategory, now time.Time) (begun, error) {
	h, err := s.holdRun(ctx, tx, profileID, runID, category, now, true)
	if err != nil {
		return begun{}, err
	}
	if place > len(h.lesson.Segments) || h.lesson.Segments[place-1].Task == nil {
		return begun{}, rpc.Malformed("segment")
	}
	ended, err := store.EndedSegments(ctx, tx, profileID, h.row.ID)
	if err != nil {
		return begun{}, err
	}
	for i, seg := range h.lesson.Segments[:place] {
		before := i+1 < place
		if seg.Task != nil && slices.Contains(ended, i+1) != before {
			return begun{}, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_SEGMENT_UNAVAILABLE, func(r *v1.Refusal) {
				r.Data = &v1.Refusal_EndedSegments{EndedSegments: &v1.EndedSegments{Places: places(ended)}}
			})
		}
	}
	b := begun{module: h.pkg.View.Manifest.ID, task: *h.lesson.Segments[place-1].Task, run: &h, segment: place}
	open, found, err := store.RunAttempt(ctx, tx, profileID, h.row.ID)
	if err != nil {
		return begun{}, err
	}
	if found && *open.Segment == place {
		b.openAttempt = open.ID
	}
	return b, nil
}

// nextOf is what comes next once the attempt row is closed by a correct answer
// or by the solution shown: in a lesson, the place of the segment after its own,
// of any kind, or that no segment is left after it; outside a lesson, the
// engine's choice, which comes with the engine's code (T40).
func (s *Service) nextOf(ctx context.Context, db store.Querier, profileID string, row store.Attempt) (*v1.Next, error) {
	if row.LessonRunID == nil {
		return nil, nil
	}
	full, err := store.RunLesson(ctx, db, profileID, *row.LessonRunID)
	if err != nil {
		return nil, err
	}
	pkg, ok := s.set.Packages[formatv0.Digest(row.PackageDigest)]
	if !ok {
		// A repeat of an answer on a package the image no longer gives: the run ends
		// "unchecked" at its next request, and no segment comes next.
		return nil, nil
	}
	lesson, ok := lessonOf(pkg, full)
	if !ok {
		return nil, nil
	}
	if *row.Segment < len(lesson.Segments) {
		return &v1.Next{Next: &v1.Next_Segment{Segment: int32(*row.Segment + 1)}}, nil
	}
	return &v1.Next{Next: &v1.Next_SegmentsEnded{SegmentsEnded: true}}, nil
}
