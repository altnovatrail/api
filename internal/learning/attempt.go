package learning

import (
	"context"
	"slices"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/module-kit/observation"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// SubmitWork checks a submission of a task with a verdict, under its idempotency
// key: the verdict of every item and answer slot, by module-kit, on the options
// tapped. A submission with an answer slot "not_yet" is a wrong answer (R09);
// one whose every item is correct closes the attempt "correct", and its answer
// holds the task solved, its keys and its solution (R05 spec). The answer slots
// that AI reads get "unchecked", with the outcome "not admitted": no AI is
// admitted in this code yet, and their work is read by no one (R44). The
// submission is recorded with its answer, and the key's record names it, since
// the answer holds the options tapped (architecture 3.2). What comes next comes
// with the engine's choice and the lessons' code.
func (a *AttemptService) SubmitWork(ctx context.Context, req *v1.SubmitWorkRequest) (*v1.SubmitWorkResponse, error) {
	s := a.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	if req.GetOwnKey() != nil && category != formatv0.AgeCategoryAdult {
		return nil, rpc.Refuse(ctx, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE)
	}
	profileID := req.GetProfileId()
	key := rpc.Key{
		Method: apiv1connect.AttemptServiceSubmitWorkProcedure,
		Repeat: func(ctx context.Context, tx pgx.Tx, record store.KeyRecord) (proto.Message, error) {
			written, err := store.SubmissionAnswer(ctx, tx, profileID, record.SubmissionID)
			if err != nil {
				return nil, err
			}
			answer := &v1.SubmitWorkResponse{}
			return answer, proto.Unmarshal(written, answer)
		},
	}
	answer, err := rpc.Once(ctx, s.db, key, req, func(tx pgx.Tx, _ string) (*v1.SubmitWorkResponse, store.KeyTarget, error) {
		now := s.clock()
		h, err := s.hold(ctx, tx, profileID, req.GetAttemptId(), now)
		if err != nil {
			return nil, store.KeyTarget{}, err
		}
		answer, err := s.submit(ctx, h, req)
		if err != nil {
			return nil, store.KeyTarget{}, err
		}
		if answer.GetSolved() != nil {
			if err := store.CloseAttempt(ctx, tx, profileID, h.row.ID, closeCorrect, now); err != nil {
				return nil, store.KeyTarget{}, err
			}
		}
		// An answer of the contract always marshals.
		written, _ := proto.Marshal(answer)
		wrong := answer.GetWrongAnswers() > int32(h.row.WrongAnswers)
		id, err := store.Submit(ctx, tx, profileID, h.row.ID, written, wrong, now)
		if err != nil {
			return nil, store.KeyTarget{}, err
		}
		return answer, store.KeyTarget{ProfileID: profileID, AttemptID: h.row.ID, SubmissionID: id}, nil
	})
	if err != nil {
		return nil, refused(ctx, err)
	}
	return answer, nil
}

// submit is the verdict of the submission on the attempt h, in the contract's
// words.
func (s *Service) submit(ctx context.Context, h held, req *v1.SubmitWorkRequest) (*v1.SubmitWorkResponse, error) {
	if rubric(h.task) {
		// A task with a rubric is sent as an essay.
		return nil, rpc.Malformed("attempt_id")
	}
	tap := byTap(h.task)
	if tap != (req.GetWork() == nil) {
		// The work goes when AI reads a slot, and only then.
		return nil, rpc.Malformed("work")
	}
	tapped := make([]observation.Tapped, len(req.GetTappedChoices()))
	values := map[string]string{}
	for i, c := range req.GetTappedChoices() {
		tapped[i] = observation.Tapped{Place: c.GetAnswerSlot(), Value: c.GetValue()}
		values[placeKey(c.GetAnswerSlot())] = c.GetValue()
	}
	verdicts, err := observation.Verdicts(ctx, h.task.Items, nil, tapped)
	switch {
	case err != nil && ctx.Err() != nil:
		return nil, err
	case err != nil:
		// The package is checked whole as it is built: what is left is a tap
		// that the task does not take — of no slot bound choice, out of order or
		// twice.
		return nil, rpc.Malformed("tapped_choices")
	}
	items, wrong, correct := itemVerdicts(verdicts, values)
	answer := &v1.SubmitWorkResponse{Items: items, WrongAnswers: int32(h.row.WrongAnswers)}
	if wrong {
		answer.WrongAnswers++
	}
	if !tap {
		answer.Outcome = &v1.Outcome{Cause: v1.Cause_CAUSE_NOT_ADMITTED}
	}
	if correct {
		if answer.Solved, err = solved(h.pkg, h.task); err != nil {
			return nil, err
		}
	}
	return answer, nil
}

// BeginStep begins a solving step whose gives gives techniques, by its place:
// the answer is the IDs of the task's techniques, and the attempt records that
// they were given at a step, the first time (R82 spec). A repeat gets the same.
func (a *AttemptService) BeginStep(ctx context.Context, req *v1.BeginStepRequest) (*v1.BeginStepResponse, error) {
	s := a.s
	if _, err := s.learner(ctx, req.GetProfileId()); err != nil {
		return nil, err
	}
	var answer *v1.BeginStepResponse
	err := s.act(ctx, req.GetProfileId(), req.GetAttemptId(), func(tx pgx.Tx, h held, now time.Time) error {
		if !givesTechniques(h.pkg.View.SolvingSteps, req.GetStep()) {
			return rpc.Malformed("step")
		}
		if err := store.GiveStepTechniques(ctx, tx, req.GetProfileId(), h.row.ID, now); err != nil {
			return err
		}
		answer = &v1.BeginStepResponse{Techniques: &v1.Techniques{TechniqueIds: techniques(h.task)}}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return answer, nil
}

// givesTechniques says whether the step at place, from 1, is one of the module's
// solving steps whose gives gives techniques.
func givesTechniques(steps *formatv0.SolvingSteps, place int32) bool {
	if steps == nil || place < 1 || int(place) > len(steps.Steps) {
		return false
	}
	return slices.Contains(steps.Steps[place-1].Gives, formatv0.StepGivesTechniques)
}

// Done ends an attempt of a task with a rubric (R14). A task with a verdict
// takes no "Done", and an attempt of a task with a rubric has no submission
// before the rubric's code sends one: it is "no submission".
func (a *AttemptService) Done(ctx context.Context, req *v1.DoneRequest) (*v1.DoneResponse, error) {
	s := a.s
	if _, err := s.learner(ctx, req.GetProfileId()); err != nil {
		return nil, err
	}
	err := s.act(ctx, req.GetProfileId(), req.GetAttemptId(), func(_ pgx.Tx, h held, _ time.Time) error {
		if !rubric(h.task) {
			return rpc.Malformed("attempt_id")
		}
		return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_SUBMISSION)
	})
	return nil, err
}
