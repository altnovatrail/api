package learning

import (
	"bytes"
	"context"
	"image/png"
	"slices"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/module-kit/observation"
	"github.com/altnovatrail/module-kit/openview"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// finishReserve is the part of a request's time kept, after its call of AI,
// for the transaction that writes what came of it: the reading ends that much
// before the request's deadline.
const finishReserve = 3 * time.Second

// SubmitWork checks a submission of a task with a verdict, under its idempotency
// key: the verdict of every item and answer slot, by module-kit, on the options
// tapped and on what tutor's two readers read of the crop of a handwriting
// sheet (R46). A submission with an answer slot "not_yet" is a wrong answer;
// "unreadable" and "unchecked" are none (R09). One whose every item is correct
// closes the attempt "correct", and its answer holds the task solved, its keys
// and its solution (R05 spec). A call of AI that gives no reading is no
// refusal: the slots AI reads get "unchecked", and the answer names the
// outcome. The submission is recorded with its answer and, when AI read, the
// observation of what it read; the key's record names it (architecture 3.2),
// and the crop lives in the request's memory alone (R15).
//
// A task tapped alone is checked in one transaction (rpc.Once). One that AI
// reads runs in three parts (rpc.Across): the checks of the attempt and the
// work, and the key's record going, in one transaction; the reading, in none;
// and the attempt held again with the verdict and the record done, in a
// second. An attempt closed meanwhile — by another device that began another —
// is "closed", and nothing is written. What comes next comes with the engine's
// choice and the lessons' code.
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
		Method:   apiv1connect.AttemptServiceSubmitWorkProcedure,
		GoingFor: s.goingFor,
		Repeat: func(ctx context.Context, tx pgx.Tx, record store.KeyRecord) (proto.Message, error) {
			written, err := store.SubmissionAnswer(ctx, tx, profileID, record.SubmissionID)
			if err != nil {
				return nil, err
			}
			answer := &v1.SubmitWorkResponse{}
			return answer, proto.Unmarshal(written, answer)
		},
	}
	prepare := func(tx pgx.Tx) (held, sending, error) {
		h, err := s.hold(ctx, tx, profileID, req.GetAttemptId(), s.clock())
		if err != nil {
			return held{}, sending{}, err
		}
		out, err := s.sent(ctx, h, req, category)
		return h, out, err
	}
	var answer *v1.SubmitWorkResponse
	if s.readsAI(ctx, profileID, req.GetAttemptId()) {
		answer, err = rpc.Across(ctx, s.db, key, req, rpc.Phases[sending, *v1.SubmitWorkResponse]{
			Prepare: func(tx pgx.Tx, _ string) (sending, store.KeyTarget, error) {
				h, out, err := prepare(tx)
				return out, store.KeyTarget{ProfileID: profileID, AttemptID: h.row.ID}, err
			},
			Call: func(ctx context.Context, out sending) sending { return s.read(ctx, out) },
			Finish: func(tx pgx.Tx, out sending) (*v1.SubmitWorkResponse, store.KeyTarget, error) {
				// Held again: another device may have closed it while AI read.
				h, err := s.hold(ctx, tx, profileID, out.attemptID, s.clock())
				if err != nil {
					return nil, store.KeyTarget{}, err
				}
				return s.record(ctx, tx, profileID, h, out)
			},
		})
	} else {
		answer, err = rpc.Once(ctx, s.db, key, req, func(tx pgx.Tx, _ string) (*v1.SubmitWorkResponse, store.KeyTarget, error) {
			h, out, err := prepare(tx)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			return s.record(ctx, tx, profileID, h, out)
		})
	}
	if err != nil {
		return nil, refused(ctx, err)
	}
	if c := answer.GetOutcome().GetCause(); c != v1.Cause_CAUSE_UNSPECIFIED {
		rpc.NoteOutcome(ctx, c)
	}
	return answer, nil
}

// readsAI says whether AI reads a slot of the task of the profile's attempt,
// read with no lock: what an attempt is of never changes. An attempt this
// cannot tell of — none, or on a package the index does not know — goes the
// way of a task tapped, whose checks refuse it.
func (s *Service) readsAI(ctx context.Context, profileID, attemptID string) bool {
	if !store.CanonicalID(attemptID) {
		return false
	}
	digest, taskID, err := store.AttemptTask(ctx, s.db, profileID, attemptID)
	if err != nil {
		return false
	}
	pkg, ok := s.set.Packages[formatv0.Digest(digest)]
	if !ok {
		return false
	}
	task, ok := pkg.View.Tasks[formatv0.TaskID(taskID)]
	return ok && !byTap(task)
}

// record writes the submission out on the profile's attempt h: its verdict, the close of
// a task solved, and the submission with its answer and observation, which the
// key's record names.
func (s *Service) record(ctx context.Context, tx pgx.Tx, profileID string, h held, out sending) (*v1.SubmitWorkResponse, store.KeyTarget, error) {
	now := s.clock()
	answer, observed, err := s.verdict(ctx, h, out)
	if err != nil {
		return nil, store.KeyTarget{}, err
	}
	if answer.GetSolved() != nil {
		if err := store.CloseAttempt(ctx, tx, profileID, h.row.ID, closeCorrect, now); err != nil {
			return nil, store.KeyTarget{}, err
		}
	}
	// An answer and an observation of the contract always marshal.
	written, _ := proto.Marshal(answer)
	var kept []byte
	if observed != nil {
		kept, _ = proto.Marshal(observed)
	}
	wrong := answer.GetWrongAnswers() > int32(h.row.WrongAnswers)
	id, err := store.Submit(ctx, tx, profileID, h.row.ID, written, kept, wrong, now)
	if err != nil {
		return nil, store.KeyTarget{}, err
	}
	return answer, store.KeyTarget{ProfileID: profileID, AttemptID: h.row.ID, SubmissionID: id}, nil
}

// sending is a submission between the parts of its request: the attempt and
// the instance it was checked on, the options tapped, and, for a task that AI
// reads, the crop with its route and what came of reading it.
type sending struct {
	attemptID string
	task      formatv0.Task
	tapped    []observation.Tapped
	values    map[string]string
	// ai says whether AI reads a slot of the task.
	ai    bool
	crop  []byte
	route tutor.Route
	read  tutor.SheetRead
}

// sent checks the submission's form on the attempt h, before anything is
// called: a task with a rubric goes as an essay; the work goes when AI reads a
// slot, and only then, as the crop of a sheet in PNG; and every tap must be of a
// slot bound choice, in order and once.
func (s *Service) sent(ctx context.Context, h held, req *v1.SubmitWorkRequest, category formatv0.AgeCategory) (sending, error) {
	if rubric(h.task) {
		return sending{}, rpc.Malformed("attempt_id")
	}
	out := sending{attemptID: h.row.ID, task: h.task, ai: !byTap(h.task), values: map[string]string{}}
	if out.ai != (req.GetWork() != nil) {
		return sending{}, rpc.Malformed("work")
	}
	if out.ai {
		out.crop = req.GetWork().GetSheet().GetCrop()
		if !cropTaken(out.crop) {
			// A workbook is no sheet, and no task that tutor reads begins on one.
			return sending{}, rpc.Malformed("work")
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 2*finishReserve {
			// Too little time is left to read and write: a reading cut short would be
			// recorded as a failure of AI under the key for good.
			return sending{}, connect.NewError(connect.CodeDeadlineExceeded, "too little time to read the work")
		}
		out.route = tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategory(category)}
		if own := req.GetOwnKey(); own != nil {
			out.route.AI = admissionv0.AiRouteOwnKey
			out.route.Own = tutor.NewOwnKey(admissionv0.Provider(own.GetProvider()), own.GetKey())
		}
	}
	for _, c := range req.GetTappedChoices() {
		out.tapped = append(out.tapped, observation.Tapped{Place: c.GetAnswerSlot(), Value: c.GetValue()})
		out.values[placeKey(c.GetAnswerSlot())] = c.GetValue()
	}
	if !out.ai {
		// A task tapped alone is judged in this transaction, which checks its taps.
		return out, nil
	}
	// The taps are checked before AI is called for the rest.
	switch _, err := observation.Verdicts(ctx, h.task.Items, nil, out.tapped); {
	case err != nil && ctx.Err() != nil:
		return sending{}, err
	case err != nil:
		// The package is checked whole as it is built: what is left is a tap that
		// the task does not take — of no slot bound choice, out of order or twice.
		return sending{}, rpc.Malformed("tapped_choices")
	}
	return out, nil
}

// read reads the crop of out by tutor, when AI reads a slot of its task, until
// finishReserve before the request's deadline, and notes the call in the
// request's log.
func (s *Service) read(ctx context.Context, out sending) sending {
	if !out.ai {
		return out
	}
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline.Add(-finishReserve))
		defer cancel()
	}
	view, err := openview.Task(out.task)
	start := time.Now()
	if err != nil {
		out.read = tutor.SheetRead{Outcome: tutor.Outcome{Kind: tutor.Failed, Err: err}}
	} else {
		out.read = s.reader.ReadSheet(ctx, tutor.SheetReading{Route: out.route, Task: &view, Crop: out.crop})
	}
	// The crop goes no further than the reading.
	out.crop = nil
	call := rpc.AICall{Outcome: aiOutcomes[out.read.Outcome.Kind], Duration: time.Since(start)}
	for _, u := range out.read.Outcome.Usage {
		call.Usage = append(call.Usage, rpc.AIUsage{Provider: string(u.Provider), Model: u.Model,
			InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, ThinkingTokens: u.ThinkingTokens})
	}
	rpc.NoteAICall(ctx, call)
	return out
}

// aiOutcomes is each kind of tutor's outcome in the log's words.
var aiOutcomes = map[tutor.Kind]string{
	tutor.Answered:     "answered",
	tutor.Failed:       "failed",
	tutor.LimitReached: "limit_reached",
	tutor.NotAdmitted:  "not_admitted",
	tutor.KeyRefused:   "key_refused",
}

// verdict is the verdict of the submission out on the attempt h, in the
// contract's words, and the observation the submission keeps: the readings of
// the two readers joined, valid as module-kit takes it, from which the same
// release gives the verdict again with the options tapped — which the answer
// holds as the values of their slots (§ 10 of spec's contracts text). None when
// AI read nothing. A reading that gave no observation, or two that module-kit
// cannot join, leaves the slots AI reads "unchecked", with the outcome's cause.
func (s *Service) verdict(ctx context.Context, h held, out sending) (*v1.SubmitWorkResponse, *observationv1.Observation, error) {
	var joined *observationv1.Observation
	var cause v1.Cause
	if out.ai {
		joined, cause = s.joined(h.task, out)
	}
	verdicts, err := observation.Verdicts(ctx, h.task.Items, joined, out.tapped)
	switch {
	case err != nil && ctx.Err() != nil:
		return nil, nil, err
	case err != nil:
		// The package is checked whole as it is built, and joined by module-kit:
		// what is left is a tap that the task does not take — of no slot bound
		// choice, out of order or twice.
		return nil, nil, rpc.Malformed("tapped_choices")
	}
	items, wrong, correct := itemVerdicts(verdicts, out.values, joined)
	answer := &v1.SubmitWorkResponse{Items: items, WrongAnswers: int32(h.row.WrongAnswers)}
	if wrong {
		answer.WrongAnswers++
	}
	if cause != v1.Cause_CAUSE_UNSPECIFIED {
		answer.Outcome = &v1.Outcome{Cause: cause}
	}
	if correct {
		if answer.Solved, err = solved(h.pkg, h.task); err != nil {
			return nil, nil, err
		}
	}
	return answer, joined, nil
}

// joined is the observation of the two readers of out joined by module-kit, or
// none with the cause of the outcome that left the slots AI reads unchecked.
func (s *Service) joined(task formatv0.Task, out sending) (*observationv1.Observation, v1.Cause) {
	r := out.read
	if r.Outcome.Kind != tutor.Answered {
		return nil, outcomeCause(r.Outcome.Kind, out.route.AI)
	}
	if len(r.Observations) != 2 {
		return nil, v1.Cause_CAUSE_AI_FAILURE
	}
	joined, err := observation.Join(task.Items, r.Observations[0], r.Observations[1])
	if err != nil {
		// An observation that is not valid is as a failure of AI.
		return nil, v1.Cause_CAUSE_AI_FAILURE
	}
	return joined, v1.Cause_CAUSE_UNSPECIFIED
}

// outcomeCause is the cause of an outcome of tutor that gave no reading, on the
// route: a limit is the platform's on platform AI and the key's own on an own key.
func outcomeCause(k tutor.Kind, route admissionv0.AiRoute) v1.Cause {
	switch k {
	case tutor.NotAdmitted:
		return v1.Cause_CAUSE_NOT_ADMITTED
	case tutor.LimitReached:
		if route == admissionv0.AiRouteOwnKey {
			return v1.Cause_CAUSE_LIMIT_REACHED
		}
		return v1.Cause_CAUSE_PLATFORM_LIMIT
	case tutor.KeyRefused:
		return v1.Cause_CAUSE_KEY_REFUSED
	}
	return v1.Cause_CAUSE_AI_FAILURE
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

// maxCropSide is the longest side of a crop that tutor reads, in pixels: a crop
// cut by the rule of the crop is far smaller.
const maxCropSide = 4096

// cropTaken says whether crop is a PNG that tutor would read: whole, and no
// larger than maxCropSide a side. Its size is read before it is decoded, so
// that a picture that would take much memory is not.
func cropTaken(crop []byte) bool {
	cfg, err := png.DecodeConfig(bytes.NewReader(crop))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxCropSide || cfg.Height > maxCropSide {
		return false
	}
	_, err = png.Decode(bytes.NewReader(crop))
	return err == nil
}
