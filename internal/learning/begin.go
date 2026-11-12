package learning

import (
	"context"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/module-kit/modulepackage"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// begun is the task a beginning names: its module and its local ID, and the
// engine's choice, the earlier attempt or the lesson run's segment it begins by —
// the run held, on whose package the attempt goes, and the open attempt of the
// segment, when it has one.
type begun struct {
	module      formatv0.ModuleID
	task        formatv0.TaskID
	choiceID    *string
	previousID  *string
	run         *heldRun
	segment     int
	openAttempt string
}

// BeginAttempt begins an attempt, under its idempotency key: by an engine's
// choice of a task, or the same task again outside a lesson, each on the current
// version of its module (R07). It closes the profile's open attempt in the same
// transaction: "left", or as the check of a request would have closed it (R10).
// The module must be connected and have a current version for the learner that
// holds the task, and the task must suit the age category of the profile (R51
// spec). A task with a rubric is not admitted: its path is not in this code
// (R14). A task of handwriting begins when tutor reads its form — a
// handwriting sheet with one answer slot bound anywhere — and the gate of
// admission chooses models to read it, by platform AI or the own key named
// (R45, R46). A task segment of a lesson run begins on the run's package, once
// the run is checked whole and every task segment before it has ended, and it
// has not; its open attempt is given as it is (R13, R48), and waits, if the
// quota ran out on it. On platform AI a task that AI checks begins only with
// room for a typical task left of the family day, and while the limit of its
// provider leaves platform AI open: else "quota", with when the next day
// begins, or "platform limit" (R11, R59). A refusal of the admission, the quota
// or the limit leaves the run open. The answer, the attempt with the open view
// of its task, is what the key's record holds: it names nothing the learner
// sent.
func (l *LearningService) BeginAttempt(ctx context.Context, req *v1.BeginAttemptRequest) (*v1.BeginAttemptResponse, error) {
	s := l.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	if req.GetStart() == nil {
		return nil, rpc.Malformed("start")
	}
	if err := ownKeyFits(ctx, category, req.GetOwnKeyProvider() != ""); err != nil {
		return nil, err
	}
	profileID := req.GetProfileId()
	answer, err := rpc.Once(ctx, s.db, rpc.Key{Method: apiv1connect.LearningServiceBeginAttemptProcedure}, req,
		func(tx pgx.Tx, _ string) (*v1.BeginAttemptResponse, store.KeyTarget, error) {
			now := s.clock()
			b, err := s.started(ctx, tx, req, category, now)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			start, err := s.begin(ctx, tx, profileID, category, req.GetOwnKeyProvider(), b, now)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			return &v1.BeginAttemptResponse{Attempt: start},
				store.KeyTarget{ProfileID: profileID, AttemptID: start.GetAttemptId()}, nil
		})
	if err != nil {
		return nil, refused(ctx, err)
	}
	return answer, nil
}

// started is the task the beginning names, by what it begins by.
func (s *Service) started(ctx context.Context, tx pgx.Tx, req *v1.BeginAttemptRequest, category formatv0.AgeCategory, now time.Time) (begun, error) {
	profileID := req.GetProfileId()
	switch start := req.GetStart().(type) {
	case *v1.BeginAttemptRequest_ChoiceId:
		id := start.ChoiceId
		if !store.CanonicalID(id) {
			return begun{}, store.ErrNotFound
		}
		c, err := store.ChoiceOf(ctx, tx, profileID, id)
		if err != nil {
			return begun{}, err
		}
		if c.Kind != "task" {
			return begun{}, rpc.Malformed("choice_id")
		}
		// A choice begins one attempt: once it has, app reads the open attempt.
		if done, err := store.ChoiceBegun(ctx, tx, profileID, id); err != nil || done {
			return begun{}, firstOf(err, store.ErrChoiceBegun)
		}
		return begun{module: formatv0.ModuleID(c.ModuleID), task: formatv0.TaskID(c.TaskID), choiceID: &id}, nil
	case *v1.BeginAttemptRequest_PreviousAttemptId:
		id := start.PreviousAttemptId
		if !store.CanonicalID(id) {
			return begun{}, store.ErrNotFound
		}
		// An earlier attempt of a segment too: the new one goes outside the lesson.
		earlier, err := store.HoldAttempt(ctx, tx, profileID, id)
		if err != nil {
			return begun{}, err
		}
		return begun{module: formatv0.ModuleID(earlier.ModuleID), task: formatv0.TaskID(earlier.TaskID), previousID: &id}, nil
	default:
		segment := req.GetSegment()
		if segment.GetSegment() < 1 {
			return begun{}, rpc.Malformed("segment")
		}
		return s.segmentBegun(ctx, tx, profileID, segment.GetLessonRunId(), int(segment.GetSegment()), category, now)
	}
}

// firstOf is err when there is one, and else refusal.
func firstOf(err, refusal error) error {
	if err != nil {
		return err
	}
	return refusal
}

// begin begins the attempt of b for a learner of category, by the own key of
// ownKey when it names a provider, at now: the checks of the module and the
// task and its admission, then the close of the open attempt and the row.
func (s *Service) begin(ctx context.Context, tx pgx.Tx, profileID string, category formatv0.AgeCategory, ownKey string, b begun, now time.Time) (*v1.AttemptStart, error) {
	if b.openAttempt != "" {
		// The segment's open attempt goes on as it is; one that lapsed is closed by
		// its check, and a new attempt begins.
		h, err := s.hold(ctx, tx, profileID, b.openAttempt, now)
		if _, lapsed := rpc.Kept(err); !lapsed {
			if err != nil {
				return nil, err
			}
			return startOf(h)
		}
	}
	pkg, err := s.packageOf(ctx, tx, profileID, category, b)
	if err != nil {
		return nil, err
	}
	task, ok := pkg.View.Tasks[b.task]
	switch {
	case !ok:
		return nil, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION)
	case !suits(pkg, task, category):
		return nil, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_SUITABLE)
	case !s.admitted(task, category, ownKey):
		return nil, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED)
	}
	if !byTap(task) && ownKey == "" {
		// A new task that platform AI checks begins only with room for a typical
		// task today, and only while the provider's limit leaves it open (R11).
		// A task with a rubric begins in no case here (R14): its typical task,
		// quota.TypicalRubric, waits for its path.
		closed, err := closedTo(ctx, tx, profileID, quota.TypicalHandwriting, now)
		if err != nil {
			return nil, err
		}
		if closed != nil {
			return nil, closedRefusal(ctx, closed)
		}
	}
	if err := s.leave(ctx, tx, profileID, now); err != nil {
		return nil, err
	}
	seed := newSeed()
	got, picked, err := instance(pkg, task, seed)
	if err != nil {
		return nil, err
	}
	view, err := openTask(pkg, got)
	if err != nil {
		return nil, err
	}
	m := pkg.View.Manifest
	a := store.NewAttempt{
		PackageDigest: string(pkg.Digest), ModuleID: string(m.ID), ModuleVersion: string(m.Version),
		FormatVersion: string(m.ModuleFormatVersion), ModuleKit: s.moduleKit, TaskID: string(task.ID),
		Seed: seed, Instance: picked, ChoiceID: b.choiceID, PreviousID: b.previousID,
	}
	start := &v1.AttemptStart{Task: view, PackageDigest: string(pkg.Digest)}
	if b.run != nil {
		a.LessonRunID, a.Segment = &b.run.row.ID, &b.segment
		start.Segment = &v1.SegmentPlace{LessonRunId: b.run.row.ID, Segment: int32(b.segment)}
	}
	if start.AttemptId, err = store.BeginAttempt(ctx, tx, profileID, a, now); err != nil {
		return nil, err
	}
	return start, nil
}

// packageOf is the package the attempt of b goes on: the run's, whose check
// held the module and the lesson's tasks already, or the current version of its
// connected module for the learner (R07).
func (s *Service) packageOf(ctx context.Context, tx pgx.Tx, profileID string, category formatv0.AgeCategory, b begun) (*modulepackage.Package, error) {
	if b.run != nil {
		return b.run.pkg, nil
	}
	connected, err := store.ModuleConnected(ctx, tx, profileID, string(b.module))
	switch {
	case err != nil:
		return nil, err
	case !connected:
		return nil, store.ErrNotFound
	}
	pkg, ok := s.set.CurrentFor(b.module, category)
	if !ok {
		return nil, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION)
	}
	return pkg, nil
}

// leave closes the profile's open attempt, when it has one, for the attempt
// that begins: "left", unless it lapsed already and is closed as the check of
// a request would close it (R07, R10).
func (s *Service) leave(ctx context.Context, tx pgx.Tx, profileID string, now time.Time) error {
	if err := store.HoldAttemptRun(ctx, tx, profileID, ""); err != nil {
		return err
	}
	open, found, err := store.HoldOpenAttempt(ctx, tx, profileID)
	if err != nil || !found {
		return err
	}
	reason := s.lapsed(open, now)
	if reason == "" {
		reason = closeLeft
	}
	return s.close(ctx, tx, profileID, open.ID, reason, now)
}

// GetOpenAttempt is the open attempt, by which the learner goes on with it on
// another device too: its beginning, the verdicts of its last submission, how
// many wrong answers it has, the current rung with the words the attempt keeps
// for it or its fallback text, and whether "explain differently" was asked for
// on it, and the techniques given at a step and on request, with the leading
// question given on request (R47). It reads, and so it is no action on the
// attempt: its last action stays.
func (l *LearningService) GetOpenAttempt(ctx context.Context, req *v1.GetOpenAttemptRequest) (*v1.GetOpenAttemptResponse, error) {
	s := l.s
	if _, err := s.learner(ctx, req.GetProfileId()); err != nil {
		return nil, err
	}
	var open *v1.OpenAttempt
	err := s.act(ctx, req.GetProfileId(), req.GetAttemptId(), func(tx pgx.Tx, h held, _ time.Time) error {
		start, err := startOf(h)
		if err != nil {
			return err
		}
		open = &v1.OpenAttempt{Start: start, WrongAnswers: int32(h.row.WrongAnswers)}
		answer, found, err := store.LastSubmission(ctx, tx, req.GetProfileId(), h.row.ID)
		if err != nil {
			return err
		}
		if found {
			last := &v1.SubmitWorkResponse{}
			if err := proto.Unmarshal(answer, last); err != nil {
				return err
			}
			open.Items = last.GetItems()
		}
		if h.row.StepTechniques {
			open.StepTechniqueIds = techniques(h.task)
		}
		if h.row.RequestedTechniques {
			open.RequestedTechniqueIds = techniques(h.task)
		}
		if h.row.LeadingQuestion {
			open.RequestedLeadingQuestion = leadingQuestion(h.task)
		}
		if h.row.WrongAnswers > 0 && !rubric(h.task) {
			open.Rung, open.ExplainedDifferently, err = rungHint(ctx, tx, req.GetProfileId(), h)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return &v1.GetOpenAttemptResponse{Attempt: open}, nil
}

// startOf is the attempt as it began.
func startOf(h held) (*v1.AttemptStart, error) {
	view, err := openTask(h.pkg, h.task)
	if err != nil {
		return nil, err
	}
	start := &v1.AttemptStart{AttemptId: h.row.ID, Task: view, PackageDigest: h.row.PackageDigest}
	if h.row.LessonRunID != nil {
		start.Segment = &v1.SegmentPlace{LessonRunId: *h.row.LessonRunID, Segment: int32(*h.row.Segment)}
	}
	return start, nil
}

// admitted says whether this code begins an attempt of the task for a learner
// of category, by the own key of ownKey when it names a provider: the same rule
// for a beginning and for the pool the engine chooses from (R50). A task with a
// rubric is not, as its path is not in this code (R14), so AI may not assess
// one; nor is one AI would read on a workbook, a zone or the whole sheet, which
// this release of tutor does not read (R46); nor one of handwriting whose
// models the table, the facts, the stage and the platform's keys of the
// deployment do not admit (R45).
func (s *Service) admitted(task formatv0.Task, category formatv0.AgeCategory, ownKey string) bool {
	switch {
	case rubric(task):
		return false
	case byTap(task):
		return true
	}
	return sheetRead(task) && s.gate.Handwriting(category, ownKey)
}
