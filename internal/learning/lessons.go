package learning

import (
	"context"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/engine"
	"github.com/altnovatrail/module-kit/modulepackage"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/packages"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// snapshot is a look at the profile that writes nothing: its reads are of one
// moment.
var snapshot = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}

// ListLessons gives the lessons of a connected module for its tile and its page
// of lessons: those of its current version for the learner that suit the
// learner whole, in the author's order, which the engine gives, each with its
// state by the profile's runs; then the lesson of each open run that the
// current version does not list, in a group of its own, on the run's package.
// It writes nothing: no run is touched or ended, and no choice is made. A
// module not connected is "not found", one with no current version for the
// learner is refused so, and one solved on the sheet is malformed.
func (l *LearningService) ListLessons(ctx context.Context, req *v1.ListLessonsRequest) (*v1.ListLessonsResponse, error) {
	s := l.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	module := formatv0.ModuleID(req.GetModuleId())
	if s.set.OnSheet(module) {
		return nil, rpc.Malformed("module_id")
	}
	profileID := req.GetProfileId()
	var out *v1.ListLessonsResponse
	err = pgx.BeginTxFunc(ctx, s.db, snapshot, func(tx pgx.Tx) error {
		connected, err := store.ModuleConnected(ctx, tx, profileID, string(module))
		switch {
		case err != nil:
			return err
		case !connected:
			return store.ErrNotFound
		}
		pkg, ok := s.set.OfferedFor(module, category)
		if !ok {
			return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION)
		}
		runs, err := store.ProfileRuns(ctx, tx, profileID)
		if err != nil {
			return err
		}
		out, err = s.lessonsOf(ctx, tx, profileID, pkg, category, runs)
		return err
	})
	if err != nil {
		return nil, refused(ctx, err)
	}
	return out, nil
}

// lessonsOf is the lessons of the current version pkg for a learner of
// category, by the profile's runs, as ListLessons gives them.
func (s *Service) lessonsOf(ctx context.Context, tx pgx.Tx, profileID string, pkg *modulepackage.Package, category formatv0.AgeCategory, runs []store.Run) (*v1.ListLessonsResponse, error) {
	module := pkg.View.Manifest.ID
	var suited []engine.Lesson
	for id, lesson := range pkg.View.Lessons {
		if lessonSuits(pkg, lesson, category) {
			suited = append(suited, engine.Lesson{ID: lessonID(module, id), Skills: skillsOf(module, lesson.Skills)})
		}
	}
	order, err := engine.LessonOrder(s.model.Graphs([]string{string(module)})[string(module)].Graph, suited)
	if err != nil {
		return nil, err
	}
	// The open run of each lesson, the one begun last among them first, and the
	// lessons passed: the runs come in the order they began.
	open := map[string]store.Run{}
	var opened []string
	passed := map[string]bool{}
	for _, r := range slices.Backward(runs) {
		switch {
		case r.ModuleID != string(module):
		case r.EndReason == nil:
			if _, seen := open[r.LessonID]; !seen {
				open[r.LessonID] = r
				opened = append(opened, r.LessonID)
			}
		case *r.EndReason == store.RunPassed:
			passed[r.LessonID] = true
		}
	}
	out := &v1.ListLessonsResponse{}
	group := int32(-1)
	for _, o := range order {
		lesson, _ := lessonOf(pkg, o.ID)
		m, err := s.moduleLesson(ctx, tx, profileID, pkg, lesson, open, passed)
		if err != nil {
			return nil, err
		}
		m.Group, group = o.Group, o.Group
		out.Lessons = append(out.Lessons, m)
	}
	for _, id := range slices.Sorted(slices.Values(opened)) {
		if slices.ContainsFunc(out.Lessons, func(m *v1.ModuleLesson) bool { return m.GetLesson() == id }) {
			continue
		}
		digest := formatv0.Digest(open[id].PackageDigest)
		runPkg := s.set.Packages[digest]
		if s.set.Status(digest) != packages.Given {
			// A run on a package the image no longer gives ends "unchecked" at its
			// next request: it does not go on.
			continue
		}
		// The run began on its package, which holds its lesson.
		lesson, _ := lessonOf(runPkg, id)
		m, err := s.moduleLesson(ctx, tx, profileID, runPkg, lesson, open, passed)
		if err != nil {
			return nil, err
		}
		group++
		m.Group = group
		out.Lessons = append(out.Lessons, m)
	}
	out.TileLesson = tileLesson(out.Lessons, opened)
	return out, nil
}

// moduleLesson is the lesson of pkg as the page of lessons lists it, with its
// state by the open runs and the lessons passed: the segments and the places
// ended of its open run, on the run's package, when it has one.
func (s *Service) moduleLesson(ctx context.Context, tx pgx.Tx, profileID string, pkg *modulepackage.Package, lesson formatv0.Lesson, open map[string]store.Run, passed map[string]bool) (*v1.ModuleLesson, error) {
	id := lessonID(pkg.View.Manifest.ID, lesson.ID)
	m := &v1.ModuleLesson{Lesson: id, Title: string(lesson.Title), Segments: int32(len(lesson.Segments)), State: v1.LessonState_LESSON_STATE_NEW}
	if passed[id] {
		m.State = v1.LessonState_LESSON_STATE_PASSED
	}
	r, ok := open[id]
	if !ok {
		return m, nil
	}
	m.State = v1.LessonState_LESSON_STATE_OPEN
	if runPkg, ok := s.set.Packages[formatv0.Digest(r.PackageDigest)]; ok {
		if runLesson, ok := lessonOf(runPkg, id); ok {
			m.Segments = int32(len(runLesson.Segments))
		}
	}
	ended, err := store.EndedSegments(ctx, tx, profileID, r.ID)
	if err != nil {
		return nil, err
	}
	m.EndedSegments = places(ended)
	return m, nil
}

// tileLesson is the lesson the tile shows, of the lessons listed: of the open
// run begun last, then the first not passed, then the first; none when there
// are no lessons. opened is the lessons of open runs, the one begun last first.
func tileLesson(lessons []*v1.ModuleLesson, opened []string) string {
	for _, id := range opened {
		if slices.ContainsFunc(lessons, func(m *v1.ModuleLesson) bool { return m.GetLesson() == id }) {
			return id
		}
	}
	for _, m := range lessons {
		if m.GetState() != v1.LessonState_LESSON_STATE_PASSED {
			return m.GetLesson()
		}
	}
	if len(lessons) > 0 {
		return lessons[0].GetLesson()
	}
	return ""
}

// openChosen opens the lesson the learner chose, by its full ID: the open run
// of the lesson when its check passes, or else a new run on the package of the
// module's current version, with a choice of its own, "lesson chosen", that
// holds what an engine's choice would at now. The choice and the run go in a
// savepoint of their own, so that a run another request began meanwhile leaves
// no choice behind: that run is read again.
func (s *Service) openChosen(ctx context.Context, tx pgx.Tx, profileID string, category formatv0.AgeCategory, ownKey, full string, now time.Time) (heldRun, error) {
	module, local, ok := cutLesson(full)
	switch {
	case !ok:
		return heldRun{}, store.ErrNotFound
	case s.set.OnSheet(module):
		return heldRun{}, rpc.Malformed("lesson")
	}
	for range 2 {
		row, found, err := store.OpenRunOf(ctx, tx, profileID, full)
		if err != nil {
			return heldRun{}, err
		}
		if found {
			h, err := s.checkRunGiving(ctx, tx, profileID, row, category, ownKey, now, true, false)
			if _, ended := rpc.Kept(err); !ended {
				return h, err
			}
			// The check ended the run, which the learner did not ask to end: the end
			// stands in tx, and the lesson begins a run of its own.
		}
		var h heldRun
		err = pgx.BeginFunc(ctx, tx, func(sp pgx.Tx) error {
			choiceID, err := s.chooseLesson(ctx, sp, profileID, category, ownKey, module, local, now)
			if err != nil {
				return err
			}
			h, err = s.newRun(ctx, sp, profileID, category, store.Choice{Kind: "lesson", ModuleID: string(module), LessonID: string(local)}, choiceID, now)
			return err
		})
		if err != store.ErrRunBegun {
			return h, err
		}
	}
	return heldRun{}, store.ErrRunBegun
}

// chooseLesson writes the choice of the lesson the learner chose, once the
// module is connected, its current version for the learner has the lesson, and
// the lesson and each of its tasks suit the learner: a choice of the module,
// "lesson chosen", with what a replay of an engine's choice would read at now,
// though no branch of the rule made it.
func (s *Service) chooseLesson(ctx context.Context, tx pgx.Tx, profileID string, category formatv0.AgeCategory, ownKey string, module formatv0.ModuleID, local formatv0.LessonID, now time.Time) (string, error) {
	pkg, err := s.packageOf(ctx, tx, profileID, category, begun{module: module})
	if err != nil {
		return "", err
	}
	lesson, ok := pkg.View.Lessons[local]
	switch {
	case !ok:
		return "", rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION)
	case !lessonSuits(pkg, lesson, category):
		return "", rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_SUITABLE)
	}
	settled, err := s.model.Settle(ctx, tx, profileID, now)
	if err != nil {
		return "", err
	}
	pkgs, err := s.engineModules(ctx, tx, profileID, category)
	if err != nil {
		return "", err
	}
	admitted, closed, err := s.aiClosed(ctx, tx, profileID, category, ownKey, now)
	if err != nil {
		return "", err
	}
	c := engine.Choice{Kind: engine.ChoiceLesson, ID: lessonID(module, local), Reason: engine.ReasonLessonChosen}
	row, err := s.choiceRow(c, pkgs, settled, category, store.Scope{Module: string(module)}, !admitted || closed != nil, after{})
	if err != nil {
		return "", err
	}
	return store.WriteChoice(ctx, tx, profileID, row, now)
}

// cutLesson is the module and the local ID of a lesson's full ID.
func cutLesson(full string) (formatv0.ModuleID, formatv0.LessonID, bool) {
	i := strings.LastIndex(full, "/lesson/")
	if i <= 0 || i+len("/lesson/") == len(full) {
		return "", "", false
	}
	return formatv0.ModuleID(full[:i]), formatv0.LessonID(full[i+len("/lesson/"):]), true
}
