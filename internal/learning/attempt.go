package learning

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"slices"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/module-kit/observation"
	"github.com/altnovatrail/module-kit/openview"
	"github.com/altnovatrail/module-kit/verdict"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/altnovatrail/api/internal/packages"
	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// finishReserve is the part of a request's time kept, after its call of AI,
// for the transaction that writes what came of it: the reading ends that much
// before the request's deadline.
const finishReserve = 3 * time.Second

// SubmitWork checks a submission of a task with a verdict, under its idempotency
// key: the verdict of every item and answer slot, by module-kit, on the options
// tapped and on what tutor's readers — the pair, or one reader (R88) — read of
// the crop of a handwriting sheet (R46) or of the crops of a workbook page's
// filled boxes (R81). A submission with an answer slot "not_yet" is a wrong
// answer; "unreadable" and "unchecked" are none (R09). One whose every item is
// correct closes the attempt "correct", and its answer holds the task solved,
// its keys and its solution (R05 spec). A call of AI that gives no reading is no
// refusal: the slots AI reads get "unchecked", and the answer names the
// outcome. The submission is recorded with its answer and, when AI read, the
// observation of what it read; the key's record names it, and the crop lives in
// the request's memory alone (R15).
//
// A task tapped alone is checked in one transaction (rpc.Once). One that AI
// reads runs in three parts (rpc.Across): the checks of the attempt and the
// work, and the key's record going, in one transaction; the reading, in none;
// and the attempt held again with the verdict and the record done, in a
// second. An attempt closed meanwhile — by another device that began another —
// is "closed", and nothing is written. An answer that closes the attempt says
// what comes next in a lesson: the next segment, or that none is left (R48);
// outside a lesson, the engine's choice (R50), on the route of the own key's
// provider, or of own_key_provider when the submission sends neither work nor
// own key — beside either it is malformed —, or else platform AI (R65).
func (a *AttemptService) SubmitWork(ctx context.Context, req *v1.SubmitWorkRequest) (*v1.SubmitWorkResponse, error) {
	s := a.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	if err := ownKeySent(ctx, category, req.GetOwnKey()); err != nil {
		return nil, err
	}
	if err := ownKeyFits(ctx, category, req.GetOwnKeyProvider() != ""); err != nil {
		return nil, err
	}
	if req.GetOwnKeyProvider() != "" && (req.GetWork() != nil || req.GetOwnKey() != nil) {
		// There the route is that of own_key, or platform AI's without one.
		return nil, rpc.Malformed("own_key_provider")
	}
	// The route of the engine's choice in next: the provider of the own key, or
	// the one named without the key by a submission AI does not read (R65).
	ownKey := req.GetOwnKey().GetProvider()
	if ownKey == "" {
		ownKey = req.GetOwnKeyProvider()
	}
	profileID := req.GetProfileId()
	key := rpc.Key{
		Method:   apiv1connect.AttemptServiceSubmitWorkProcedure,
		GoingFor: s.paperGoingFor,
		Repeat: func(ctx context.Context, tx pgx.Tx, record store.KeyRecord) (proto.Message, error) {
			written, err := store.SubmissionAnswer(ctx, tx, profileID, record.SubmissionID)
			if err != nil {
				return nil, err
			}
			answer := &v1.SubmitWorkResponse{}
			if err := proto.Unmarshal(written, answer); err != nil || answer.GetSolved() == nil {
				return answer, err
			}
			// The answer is kept without what comes next, which the attempt gives again.
			row, err := store.HoldAttempt(ctx, tx, profileID, req.GetAttemptId())
			if err != nil {
				return nil, err
			}
			answer.Next, err = s.nextAfter(ctx, tx, profileID, row, category, ownKey, s.clock(), true)
			return answer, err
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
	// An attempt this cannot tell of — none, or on a package the index does not
	// know — goes the way of a task tapped, whose checks refuse it.
	task, paper, known := s.attemptOf(ctx, profileID, req.GetAttemptId())
	if paper {
		answer, err = a.submitPaper(ctx, req, category, key)
	} else if known && !byTap(task) {
		answer, err = rpc.Across(ctx, s.db, key, req, rpc.Phases[sending, *v1.SubmitWorkResponse]{
			Prepare: func(tx pgx.Tx, _ string) (sending, store.KeyTarget, error) {
				h, out, err := prepare(tx)
				if err == nil && out.ai {
					// The reading takes its reserve here, beside the record going, and
					// settles it after the call (R58): a workbook page by its crops, and a
					// page left blank, which goes to no model, none (R81).
					if out.day, err = s.familyDay(ctx, tx, out.route, req.ClockOffsetMinutes); err == nil && !out.blank() {
						out.reserve, err = s.reserveFor(ctx, tx, profileID, out.route, out.call, out.crops(), out.day)
					}
				}
				return out, store.KeyTarget{ProfileID: profileID, AttemptID: h.row.ID}, err
			},
			Call: func(ctx context.Context, out sending) sending { return s.read(ctx, out, req.GetOwnKey().GetKey()) },
			Finish: func(tx pgx.Tx, out sending) (*v1.SubmitWorkResponse, store.KeyTarget, error) {
				// Held again: another device may have closed it while AI read.
				h, err := s.hold(ctx, tx, profileID, out.attemptID, s.clock())
				if err != nil {
					return nil, store.KeyTarget{}, err
				}
				return s.record(ctx, tx, profileID, category, ownKey, h, out)
			},
		})
	} else {
		answer, err = rpc.Once(ctx, s.db, key, req, func(tx pgx.Tx, _ string) (*v1.SubmitWorkResponse, store.KeyTarget, error) {
			h, out, err := prepare(tx)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			return s.record(ctx, tx, profileID, category, ownKey, h, out)
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

// attemptOf is what the profile's attempt is of, read with no lock, since
// that never changes: paper for a task from paper, or else the task as the
// package holds it — a template, when the attempt got an instance of one, of
// the same canvas and answer slots — and whether this can tell, false for none
// or one on a package the index does not know.
func (s *Service) attemptOf(ctx context.Context, profileID, attemptID string) (task formatv0.Task, paper, known bool) {
	if !store.CanonicalID(attemptID) {
		return formatv0.Task{}, false, false
	}
	digest, taskID, err := store.AttemptTask(ctx, s.db, profileID, attemptID)
	switch {
	case errors.Is(err, store.ErrPaper):
		return formatv0.Task{}, true, false
	case err != nil:
		return formatv0.Task{}, false, false
	}
	pkg, ok := s.set.Packages[formatv0.Digest(digest)]
	if !ok {
		return formatv0.Task{}, false, false
	}
	task, known = pkg.View.Tasks[formatv0.TaskID(taskID)]
	return task, false, known
}

// record writes the submission out on the profile's attempt h — its verdict,
// the submission with its answer and observation, and, for a task solved, the
// close with the engine's choice on the route of ownKey —, and the target the
// key's record names: the attempt and the submission.
func (s *Service) record(ctx context.Context, tx pgx.Tx, profileID string, category formatv0.AgeCategory, ownKey string, h held, out sending) (*v1.SubmitWorkResponse, store.KeyTarget, error) {
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
	// An answer and an observation of the contract always marshal; the answer is
	// kept without what comes next.
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
	if answer.GetSolved() != nil {
		// The evidence of the attempt is owed once its last submission is written.
		if err := s.settle(ctx, tx, profileID, now); err != nil {
			return nil, store.KeyTarget{}, err
		}
		if answer.Next, err = s.nextAfter(ctx, tx, profileID, h.row, category, ownKey, now, false); err != nil {
			return nil, store.KeyTarget{}, err
		}
	}
	return answer, store.KeyTarget{ProfileID: profileID, AttemptID: h.row.ID, SubmissionID: id}, nil
}

// sending is a submission between the parts of its request: the attempt and
// the instance it was checked on, the options tapped, and, for a task that AI
// reads, its kind of call, the crop of a sheet or the crops of a workbook page's
// filled boxes, with its route and what came of reading it.
type sending struct {
	attemptID string
	task      formatv0.Task
	tapped    []observation.Tapped
	values    map[string]string
	// ai says whether AI reads a slot of the task.
	ai    bool
	call  tutor.Call
	crop  []byte
	boxes []tutor.BoxCrop
	route tutor.Route
	// day is the family day of the account on platform AI (R11), and reserve
	// what the reading took of its quota (R58).
	day     quota.Day
	reserve reserve
	// read is what the reading came to: of a sheet, or of a workbook page, whose
	// reading has the same two fields.
	read tutor.SheetRead
}

// blank says whether the submission is a workbook page with no box filled,
// which goes to no model and spends nothing (tutor's ReadWorkbook).
func (out sending) blank() bool { return out.call == tutor.CallWorkbook && len(out.boxes) == 0 }

// crops is how many crops the reading shows: a sheet's one, or a workbook
// page's filled boxes, by which the quota reserves (R58).
func (out sending) crops() int {
	if out.call == tutor.CallWorkbook {
		return len(out.boxes)
	}
	return 1
}

// sent checks the submission's form on the attempt h, before anything is
// called: a task with a rubric goes as an essay; the work goes when AI reads a
// slot, and only then, in the form of the task's kind of call — the crop of a
// sheet in PNG, or the crops of a workbook page's filled boxes (boxesTaken) —;
// and every tap must be of a slot bound choice, in order and once.
func (s *Service) sent(ctx context.Context, h held, req *v1.SubmitWorkRequest, category formatv0.AgeCategory) (sending, error) {
	if rubric(h.task) {
		return sending{}, rpc.Malformed("attempt_id")
	}
	out := sending{attemptID: h.row.ID, task: h.task, ai: !byTap(h.task), values: map[string]string{}}
	if out.ai != (req.GetWork() != nil) {
		return sending{}, rpc.Malformed("work")
	}
	if out.ai {
		var ok bool
		out.call, ok = callOf(h.task)
		switch work := req.GetWork(); {
		case out.call == tutor.CallSheet:
			out.crop = work.GetSheet().GetCrop()
			ok = cropTaken(out.crop)
		case ok && work.GetWorkbook() != nil:
			out.boxes, ok = boxesTaken(h.task, work.GetWorkbook().GetBoxes())
		default:
			// A sheet's work on a workbook, or a task no kind of call reads, which
			// does not begin.
			ok = false
		}
		if !ok {
			return sending{}, rpc.Malformed("work")
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 2*finishReserve {
			// Too little time is left to read and write: a reading cut short would be
			// recorded as a failure of AI under the key for good.
			return sending{}, connect.NewError(connect.CodeDeadlineExceeded, "too little time to read the work")
		}
		out.route = routeOf(category, req.GetOwnKey())
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

// read reads the crop of out, or the crops of its workbook page, by tutor, when
// AI reads a slot of its task, until finishReserve before the request's
// deadline; own is the value of the request's own key, for the stub. A reading
// the quota does not hold is not called, and one called settles its reserve by
// what it cost.
func (s *Service) read(ctx context.Context, out sending, own string) sending {
	if !out.ai {
		return out
	}
	if out.reserve.refused != nil {
		out.crop, out.boxes = nil, nil
		return out
	}
	ctx, cancel := finishing(ctx)
	defer cancel()
	view, err := openview.Task(out.task)
	start := time.Now()
	switch {
	case err != nil:
		out.read = tutor.SheetRead{Outcome: tutor.Outcome{Kind: tutor.Failed, Err: err}}
	case out.call == tutor.CallWorkbook:
		page := tutor.WorkbookReading{Route: out.route, Task: &view, Boxes: out.boxes}
		out.read = tutor.SheetRead(s.workbook.ReadWorkbook(ctx, page, out.task, own))
	default:
		out.read = s.reader.ReadSheet(ctx, tutor.SheetReading{Route: out.route, Task: &view, Crop: out.crop}, out.task, own)
	}
	// The crops go no further than the reading.
	out.crop, out.boxes = nil, nil
	noteCall(ctx, out.read.Outcome, time.Since(start))
	s.afterCall(ctx, out.reserve, out.read.Outcome)
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
// the two readers joined, or the one reader's (R88), valid as module-kit takes
// it, from which the same release gives the verdict again with the options
// tapped — which the answer holds as the values of their slots. None when AI
// read nothing. A reading that gave no observation, or two that module-kit
// cannot join, or one it does not take, leaves the slots AI reads "unchecked",
// with the outcome's cause.
func (s *Service) verdict(ctx context.Context, h held, out sending) (*v1.SubmitWorkResponse, *observationv1.Observation, error) {
	var joined *observationv1.Observation
	var outcome *v1.Outcome
	if out.ai {
		joined, outcome = s.joined(h.task, out)
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
	answer.Outcome = outcome
	if correct {
		if answer.Solved, err = solved(h.pkg, h.task); err != nil {
			return nil, nil, err
		}
	}
	return answer, joined, nil
}

// joined is the observation of the reading of out, in the mode of reading of
// s (R88): by the pair, the observations of the two readers joined by
// module-kit; by one reader, its one observation as it is, valid as module-kit
// takes it, which module-kit gives the verdict on and api keeps as it keeps a
// joined one (R86 spec). Or none, with the outcome that left the slots AI reads
// unchecked: the quota that did not hold the reading, what the call came to, or
// a failure of AI for a reading of another number of observations, or of one
// that module-kit does not take.
func (s *Service) joined(task formatv0.Task, out sending) (*observationv1.Observation, *v1.Outcome) {
	if out.reserve.refused != nil {
		return nil, out.reserve.refused
	}
	return s.joinOf(task.Items, out.read, out.route.AI, s.reading == tutor.ReadingSingle)
}

// joinOf is the observation of the reading r of items, by one reader when single
// and else by the pair, as joined has it, or none with the outcome of why, on
// the route.
func (s *Service) joinOf(items []formatv0.Item, r tutor.SheetRead, route admissionv0.AiRoute, single bool) (*observationv1.Observation, *v1.Outcome) {
	if r.Outcome.Kind != tutor.Answered {
		return nil, outcomeOf(r.Outcome, route)
	}
	failure := &v1.Outcome{Cause: v1.Cause_CAUSE_AI_FAILURE}
	if single {
		if len(r.Observations) != 1 || observation.Validate(items, r.Observations[0]) != nil {
			return nil, failure
		}
		return r.Observations[0], nil
	}
	if len(r.Observations) != 2 {
		return nil, failure
	}
	joined, err := observation.Join(items, r.Observations[0], r.Observations[1])
	if err != nil {
		// An observation that is not valid is as a failure of AI.
		return nil, failure
	}
	return joined, nil
}

// outcomeOf is the outcome of a call of tutor that gave no reading or no words,
// on the route: a limit is the platform's on platform AI and the key's own on an
// own key, which names which limit by the reason tutor gives, and when access
// returns where tutor gives the time, none where it gives zero (R80).
func outcomeOf(o tutor.Outcome, route admissionv0.AiRoute) *v1.Outcome {
	switch o.Kind {
	case tutor.NotAdmitted:
		return &v1.Outcome{Cause: v1.Cause_CAUSE_NOT_ADMITTED}
	case tutor.LimitReached:
		if route == admissionv0.AiRouteOwnKey {
			limit := &v1.OwnKeyLimit{Cause: limitCauses[o.Limit]}
			if !o.AccessReturns.IsZero() {
				limit.AccessReturns = timestamppb.New(o.AccessReturns)
			}
			return &v1.Outcome{Cause: v1.Cause_CAUSE_LIMIT_REACHED, OwnKeyLimit: limit}
		}
		return &v1.Outcome{Cause: v1.Cause_CAUSE_PLATFORM_LIMIT}
	case tutor.KeyRefused:
		return &v1.Outcome{Cause: v1.Cause_CAUSE_KEY_REFUSED}
	}
	return &v1.Outcome{Cause: v1.Cause_CAUSE_AI_FAILURE}
}

// limitCauses are the causes of the contract by the reasons of tutor's limits;
// no reason, or one not here, is LIMIT_CAUSE_UNSPECIFIED.
var limitCauses = map[tutor.LimitReason]v1.LimitCause{
	tutor.LimitOwner:   v1.LimitCause_LIMIT_CAUSE_LIMIT,
	tutor.LimitTier:    v1.LimitCause_LIMIT_CAUSE_TIER,
	tutor.LimitBalance: v1.LimitCause_LIMIT_CAUSE_BALANCE,
	tutor.LimitBilling: v1.LimitCause_LIMIT_CAUSE_PAYMENT,
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

// Done ends an attempt of a task with a rubric by the learner's "Done" (R14):
// once it has a submission, however that ended, the attempt is closed "done" in
// one transaction, with no evidence, and the answer is the task solved — the
// criteria of its rubric and its solution, if it has one (R87 spec) — and what
// comes next: in a lesson the segment after its own, which "done" ends (R13),
// and outside a lesson the engine's choice (R50). A task with a verdict takes no
// "Done", and an attempt with no submission yet is "no submission". It carries
// no key: a repeat on an attempt closed "done" gets the same answer, by the
// state of the attempt, with the choice the close gave; one closed otherwise is
// "closed". A provider named for a learner who is not an adult is refused, as
// at every request whose answer may give an engine's choice (R65).
func (a *AttemptService) Done(ctx context.Context, req *v1.DoneRequest) (*v1.DoneResponse, error) {
	s := a.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	if err := ownKeyFits(ctx, category, req.GetOwnKeyProvider() != ""); err != nil {
		return nil, err
	}
	profileID, ownKey := req.GetProfileId(), req.GetOwnKeyProvider()
	var answer *v1.DoneResponse
	err = s.write(ctx, func(tx pgx.Tx, now time.Time) error {
		h, err := s.hold(ctx, tx, profileID, req.GetAttemptId(), now)
		var closed *closedError
		if errors.As(err, &closed) && closed.reason == closeDone {
			answer, err = s.doneAgain(ctx, tx, profileID, closed.row, category, ownKey, now)
			return err
		}
		if err != nil {
			return err
		}
		if !rubric(h.task) {
			return rpc.Malformed("attempt_id")
		}
		switch submitted, err := store.Submitted(ctx, tx, profileID, h.row.ID); {
		case err != nil:
			return err
		case !submitted:
			return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_SUBMISSION)
		}
		out, err := solved(h.pkg, h.task)
		if err != nil {
			return err
		}
		// The model gives an attempt with a rubric no evidence (О-17): the close
		// settles only what the profile's other closed attempts owe.
		if err := s.close(ctx, tx, profileID, h.row.ID, closeDone, now); err != nil {
			return err
		}
		next, err := s.nextAfter(ctx, tx, profileID, h.row, category, ownKey, now, false)
		answer = &v1.DoneResponse{Solved: out, Next: next}
		return err
	})
	if err != nil {
		return nil, err
	}
	return answer, nil
}

// doneAgain is the answer of "Done" again on the profile's attempt row, held
// and closed "done": the task solved, of the package and the instance it went
// on, and what came next, which the close gave and the engine is not asked
// again (R14). A package the index does not know is errPackageUnknown, as for
// a request on an open attempt.
func (s *Service) doneAgain(ctx context.Context, tx pgx.Tx, profileID string, row store.Attempt, category formatv0.AgeCategory, ownKey string, now time.Time) (*v1.DoneResponse, error) {
	digest := formatv0.Digest(row.PackageDigest)
	if s.set.Status(digest) == packages.Unknown {
		return nil, errPackageUnknown
	}
	pkg := s.set.Packages[digest]
	task, err := instanceAgain(pkg, formatv0.TaskID(row.TaskID), row.Instance)
	if err != nil {
		return nil, err
	}
	out, err := solved(pkg, task)
	if err != nil {
		return nil, err
	}
	next, err := s.nextAfter(ctx, tx, profileID, row, category, ownKey, now, true)
	if err != nil {
		return nil, err
	}
	return &v1.DoneResponse{Solved: out, Next: next}, nil
}

// maxCropSide is the longest side of a crop that tutor reads, in pixels: a crop
// cut by the rule of the crop is far smaller.
const maxCropSide = 4096

// cropTaken says whether crop is a PNG that tutor would read as a sheet's:
// whole, and no larger than maxCropSide a side.
func cropTaken(crop []byte) bool { return pngTaken(crop, maxCropSide) }

// pngTaken says whether crop is a PNG whole and no larger than side a side. Its
// size is read before it is decoded, so that a picture that would take much
// memory is not.
func pngTaken(crop []byte, side int) bool {
	cfg, err := png.DecodeConfig(bytes.NewReader(crop))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > side || cfg.Height > side {
		return false
	}
	_, err = png.Decode(bytes.NewReader(crop))
	return err == nil
}

// The bounds of a workbook page that tutor reads (tutor's ReadWorkbook): the
// most filled boxes a reading shows, and the longest side of a box's crop, in
// pixels. A box holds a number or two, far smaller.
const (
	maxBoxes   = 20
	maxBoxSide = 512
)

// boxesTaken is the crops of a workbook page's filled boxes as tutor reads them,
// when they are such: each of an answer slot of the task bound box, by the
// places of its item and of the slot among the item's, from 1, which are those
// of the box among the item's boxes; no box twice; no more than maxBoxes; and
// each crop a PNG that pngTaken takes at maxBoxSide. Their order is the
// request's: tutor reads them in the task's. No box at all is a page left
// blank, which is no error: its every box is read blank (R81).
func boxesTaken(task formatv0.Task, boxes []*v1.BoxCrop) ([]tutor.BoxCrop, bool) {
	if len(boxes) > maxBoxes {
		return nil, false
	}
	out := make([]tutor.BoxCrop, 0, len(boxes))
	seen := map[string]bool{}
	for _, b := range boxes {
		place := b.GetAnswerSlot()
		i, j := int(place.GetItem())-1, int(place.GetSlot())-1
		if i < 0 || i >= len(task.Items) {
			return nil, false
		}
		slots := verdict.AnswerSlots(task.Items[i])
		if j < 0 || j >= len(slots) || slots[j].Binding != string(formatv0.LinePartBindingBox) || seen[placeKey(place)] ||
			!pngTaken(b.GetCrop(), maxBoxSide) {
			return nil, false
		}
		seen[placeKey(place)] = true
		out = append(out, tutor.BoxCrop{Item: i + 1, Slot: j + 1, Crop: b.GetCrop()})
	}
	return out, true
}
