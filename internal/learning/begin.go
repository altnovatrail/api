package learning

import (
	"context"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// begun is the task a beginning names: its module and its local ID, and the
// engine's choice or the earlier attempt it begins by.
type begun struct {
	module     formatv0.ModuleID
	task       formatv0.TaskID
	choiceID   *string
	previousID *string
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
// (R45, R46). A task segment of a lesson run comes with the lessons' code;
// with no such run it is "not found". The answer, the attempt with the open
// view of its task, is what the key's record holds: it names nothing the
// learner sent.
func (l *LearningService) BeginAttempt(ctx context.Context, req *v1.BeginAttemptRequest) (*v1.BeginAttemptResponse, error) {
	s := l.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	if req.GetStart() == nil {
		return nil, rpc.Malformed("start")
	}
	if req.GetOwnKeyProvider() != "" && category != formatv0.AgeCategoryAdult {
		return nil, rpc.Refuse(ctx, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE)
	}
	profileID := req.GetProfileId()
	answer, err := rpc.Once(ctx, s.db, rpc.Key{Method: apiv1connect.LearningServiceBeginAttemptProcedure}, req,
		func(tx pgx.Tx, _ string) (*v1.BeginAttemptResponse, store.KeyTarget, error) {
			b, err := s.started(ctx, tx, req)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			start, err := s.begin(ctx, tx, profileID, category, req.GetOwnKeyProvider(), b, s.clock())
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
func (s *Service) started(ctx context.Context, tx pgx.Tx, req *v1.BeginAttemptRequest) (begun, error) {
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
		if !store.CanonicalID(segment.GetLessonRunId()) {
			return begun{}, store.ErrNotFound
		}
		there, err := store.LessonRunThere(ctx, tx, profileID, segment.GetLessonRunId())
		switch {
		case err != nil:
			return begun{}, err
		case !there:
			return begun{}, store.ErrNotFound
		}
		// Lesson runs begin with the lessons' code, and with them the attempts of
		// their segments.
		return begun{}, connect.NewError(connect.CodeUnimplemented, "segment")
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
	task, ok := pkg.View.Tasks[b.task]
	switch {
	case !ok:
		return nil, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION)
	case !suits(pkg, task, category):
		return nil, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_SUITABLE)
	case rubric(task):
		// The path of a task with a rubric is not in this code (R14), so AI may
		// not assess one.
		return nil, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED)
	case !byTap(task) && !sheetRead(task):
		// AI would read it on a workbook, a zone or the whole sheet, which this
		// release of tutor does not read (R46).
		return nil, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED)
	case !byTap(task) && !s.gate.Handwriting(category, ownKey):
		// AI would read the handwriting, and the table, the facts, the stage and
		// the platform's keys of the deployment admit no models for it (R45).
		return nil, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED)
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
	id, err := store.BeginAttempt(ctx, tx, profileID, store.NewAttempt{
		PackageDigest: string(pkg.Digest), ModuleID: string(m.ID), ModuleVersion: string(m.Version),
		FormatVersion: string(m.ModuleFormatVersion), ModuleKit: s.moduleKit, TaskID: string(task.ID),
		Seed: seed, Instance: picked, ChoiceID: b.choiceID, PreviousID: b.previousID,
	}, now)
	if err != nil {
		return nil, err
	}
	return &v1.AttemptStart{AttemptId: id, Task: view, PackageDigest: string(pkg.Digest)}, nil
}

// leave closes the profile's open attempt, when it has one, for the attempt
// that begins: "left", unless it lapsed already and is closed as the check of
// a request would close it (R07, R10).
func (s *Service) leave(ctx context.Context, tx pgx.Tx, profileID string, now time.Time) error {
	open, found, err := store.HoldOpenAttempt(ctx, tx, profileID)
	if err != nil || !found {
		return err
	}
	reason := s.lapsed(open, now)
	if reason == "" {
		reason = closeLeft
	}
	return store.CloseAttempt(ctx, tx, profileID, open.ID, reason, now)
}

// GetOpenAttempt is the open attempt, by which the learner goes on with it on
// another device too: its beginning, the verdicts of its last submission, how
// many wrong answers it has, and the techniques given at a step. The rung and
// what a hint gives come with the ladder's code. It reads, and so it is no
// action on the attempt: its last action stays.
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
		return nil
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
