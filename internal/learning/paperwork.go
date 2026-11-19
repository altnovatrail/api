package learning

import (
	"context"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/module-kit/observation"
	"github.com/altnovatrail/module-kit/paper"
	"github.com/altnovatrail/module-kit/verdict"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	paperv1 "github.com/altnovatrail/spec/contracts/novatrail/paper/v1"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// paperChecker reads the statement of a task from paper into a formula, solves
// it, and reads the work on it, as tutor does; own is as sheetReader has it
// (R162 spec).
type paperChecker interface {
	ReadStatement(ctx context.Context, p tutor.PaperTask, own string) tutor.StatementRead
	SolveTask(ctx context.Context, p tutor.PaperTask, own string) tutor.TaskSolved
	ReadPaper(ctx context.Context, r tutor.PaperReading, own string) tutor.SheetRead
}

func (g gateTutor) ReadStatement(ctx context.Context, p tutor.PaperTask, _ string) tutor.StatementRead {
	return g.Gate.ReadStatement(ctx, p)
}

func (g gateTutor) SolveTask(ctx context.Context, p tutor.PaperTask, _ string) tutor.TaskSolved {
	return g.Gate.SolveTask(ctx, p)
}

func (g gateTutor) ReadPaper(ctx context.Context, r tutor.PaperReading, _ string) tutor.SheetRead {
	return g.Gate.ReadPaper(ctx, r)
}

// paperPlace is the place of the one answer slot of a task from paper.
var paperPlace = &observationv1.AnswerSlotPlace{Item: 1, Slot: 1}

// paperSending is a submission of a task from paper between the parts of its
// request: the attempt, the route, the work — the task's crop and the crop of
// what the learner wrote —, the key the attempt keeps, if any, and whether the
// key is derived now; the reserves of the reading, the statement and the
// solution; and what came of the calls: the reading, and the key derived, or
// the outcome of the calls that derived none.
type paperSending struct {
	attemptID string
	route     tutor.Route
	statement []byte
	crop      []byte
	key       *paperv1.Key
	derive    bool
	day       quota.Day
	// reading, stating and solving are the reserves of the three calls (R58).
	reading, stating, solving reserve
	read                      tutor.SheetRead
	derived                   *paperv1.Key
	noKey                     *v1.Outcome
}

// paperAttempt says whether the profile's attempt is of a task from paper,
// read with no lock: what an attempt is of never changes.
func (s *Service) paperAttempt(ctx context.Context, profileID, attemptID string) bool {
	if !store.CanonicalID(attemptID) {
		return false
	}
	_, _, err := store.AttemptTask(ctx, s.db, profileID, attemptID)
	return err == store.ErrPaper
}

// holdPaper is the profile's open attempt of a task from paper, held in tx and
// checked as hold checks an attempt of a module: one closed is a closedError,
// and one that lapsed past 7 days with no action is closed "abandoned" by this
// check, the refusal keeping the close (R10).
func (s *Service) holdPaper(ctx context.Context, tx pgx.Tx, profileID, attemptID string, now time.Time) (store.Attempt, error) {
	row, err := store.HoldAttempt(ctx, tx, profileID, attemptID)
	switch {
	case err != nil:
		return store.Attempt{}, err
	case row.CloseReason != nil:
		return store.Attempt{}, &closedError{reason: *row.CloseReason, row: row}
	case !row.Paper:
		return store.Attempt{}, store.ErrNotFound
	case now.After(row.LastActionAt.Add(store.AbandonedAfter)):
		if err := store.CloseAttempt(ctx, tx, profileID, attemptID, closeAbandoned, now); err != nil {
			return store.Attempt{}, err
		}
		return store.Attempt{}, rpc.Keep(refused(ctx, &closedError{reason: closeAbandoned}))
	}
	return row, nil
}

// submitPaper checks a submission of a task from paper under its idempotency
// key, in three parts (rpc.Across), as a task of a module that AI reads: the
// checks of the attempt and the work, the reserve and the key's record going;
// the calls, in none; and the attempt held again with the verdict, in a second
// (R162 spec). The first submission derives the key from the task's crop while
// the work is read beside it: the statement read into a formula, whose answer
// the solver of module-kit computes, or, when the statement is no formula or
// the solver does not take it, one solution of AI, whose answer is the key
// once it holds to the rules of one (paper.Key). The attempt keeps the key until
// it closes. A key with no answer — the solution gave none a program can
// compare — leaves the slot unchecked with CAUSE_NO_KEY, now and on every later
// submission, which calls no AI. Calls that derive no key are an outcome, the
// slot unchecked, and the next submission derives it again. The work is read as
// a sheet is, its values brought to the key, joined and judged by module-kit;
// a correct answer closes the attempt "correct" with an empty solved, no next
// and no evidence. Taps are none: the task has no slot bound choice.
func (a *AttemptService) submitPaper(ctx context.Context, req *v1.SubmitWorkRequest, category formatv0.AgeCategory, key rpc.Key) (*v1.SubmitWorkResponse, error) {
	s := a.s
	profileID := req.GetProfileId()
	work := req.GetWork().GetPaper()
	// A repeat gives the answer kept: a task from paper has no next.
	key.Repeat = func(ctx context.Context, tx pgx.Tx, record store.KeyRecord) (proto.Message, error) {
		written, err := store.SubmissionAnswer(ctx, tx, profileID, record.SubmissionID)
		if err != nil {
			return nil, err
		}
		answer := &v1.SubmitWorkResponse{}
		return answer, proto.Unmarshal(written, answer)
	}
	return rpc.Across(ctx, s.db, key, req, rpc.Phases[paperSending, *v1.SubmitWorkResponse]{
		Prepare: func(tx pgx.Tx, _ string) (paperSending, store.KeyTarget, error) {
			row, err := s.holdPaper(ctx, tx, profileID, req.GetAttemptId(), s.clock())
			if err != nil {
				return paperSending{}, store.KeyTarget{}, err
			}
			target := store.KeyTarget{ProfileID: profileID, AttemptID: row.ID}
			switch {
			case work == nil || !photoTaken(work.GetStatement()) || !cropTaken(work.GetCrop()):
				return paperSending{}, target, rpc.Malformed("work")
			case len(req.GetTappedChoices()) > 0:
				return paperSending{}, target, rpc.Malformed("tapped_choices")
			}
			if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 2*finishReserve {
				return paperSending{}, target, connect.NewError(connect.CodeDeadlineExceeded, "too little time to read the work")
			}
			out := paperSending{attemptID: row.ID, route: routeOf(category, req.GetOwnKey()), statement: work.GetStatement(), crop: work.GetCrop()}
			if out.key, err = keptKey(row); err != nil {
				return paperSending{}, target, err
			}
			if out.key != nil {
				if out.key.GetAnswer() == nil {
					// No key to judge by: no call goes, and nothing is reserved.
					return out, target, nil
				}
			}
			out.derive = out.key == nil
			if out.day, err = s.familyDay(ctx, tx, out.route, req.ClockOffsetMinutes); err != nil {
				return paperSending{}, target, err
			}
			out.reading, out.stating, out.solving, err = s.reservePaper(ctx, tx, profileID, out.route, out.derive, out.day)
			return out, target, err
		},
		Call: func(ctx context.Context, out paperSending) paperSending {
			return s.checkPaper(ctx, out, req.GetOwnKey().GetKey())
		},
		Finish: func(tx pgx.Tx, out paperSending) (*v1.SubmitWorkResponse, store.KeyTarget, error) {
			now := s.clock()
			row, err := s.holdPaper(ctx, tx, profileID, out.attemptID, now)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			// A key kept is the attempt's, the one a Check that went meanwhile derived too.
			k, err := keptKey(row)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			if k == nil && out.derived != nil {
				k = out.derived
				// A key of the contract always marshals.
				kept, _ := proto.Marshal(k)
				if err := store.SetPaperKey(ctx, tx, profileID, row.ID, kept); err != nil {
					return nil, store.KeyTarget{}, err
				}
			}
			answer, observed, err := s.paperVerdict(ctx, row, k, out)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			if answer.GetSolved() != nil {
				// No evidence: an attempt of paper gives none, and no engine chooses after it.
				if err := store.CloseAttempt(ctx, tx, profileID, row.ID, closeCorrect, now); err != nil {
					return nil, store.KeyTarget{}, err
				}
			}
			written, _ := proto.Marshal(answer)
			var kept []byte
			if observed != nil {
				kept, _ = proto.Marshal(observed)
			}
			id, err := store.Submit(ctx, tx, profileID, row.ID, written, kept, answer.GetWrongAnswers() > int32(row.WrongAnswers), now)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			return answer, store.KeyTarget{ProfileID: profileID, AttemptID: row.ID, SubmissionID: id}, nil
		},
	})
}

// reservePaper reserves the calls of a submission of a task from paper on
// platform AI by route, as reserveEssay reserves two (R58): the reading of the
// work and, when the key is derived, the reading of the statement and the
// solution, one charge which the quota holds whole or not at all, given as the
// part each call settles by itself. A solution the solver makes needless gives
// its part back whole. A refusal is the reading's.
func (s *Service) reservePaper(ctx context.Context, tx pgx.Tx, profileID string, route tutor.Route, derive bool, day quota.Day) (reading, stating, solving reserve, err error) {
	if route.AI != admissionv0.AiRoutePlatformAi {
		return reserve{}, reserve{}, reserve{}, nil
	}
	read := quota.Reserve(tutor.CallPaper, s.price(tutor.CallPaper, route), 1)
	var state, solve quota.Micro
	if derive {
		state = quota.Reserve(tutor.CallStatement, s.price(tutor.CallStatement, route), 1)
		solve = quota.Reserve(tutor.CallSolving, s.price(tutor.CallSolving, route), 1)
	}
	all, err := s.reserveOf(ctx, tx, profileID, read+state+solve, day)
	if err != nil || !all.taken {
		return all, reserve{}, reserve{}, err
	}
	reading, stating, solving = all, all, all
	reading.charge.Amount, stating.charge.Amount, solving.charge.Amount = read, state, solve
	stating.taken, solving.taken = derive, derive
	return reading, stating, solving, nil
}

// checkPaper makes the calls of out until finishReserve before the request's
// deadline: the reading of the work, and beside it, when the key is derived,
// the chain of the key. A reserve the quota refused, or a key kept with no
// answer, calls nothing. The crops go no further than the calls.
func (s *Service) checkPaper(ctx context.Context, out paperSending, own string) (done paperSending) {
	defer func() { done.statement, done.crop = nil, nil }()
	if out.reading.refused != nil || out.key != nil && out.key.GetAnswer() == nil {
		return out
	}
	ctx, cancel := finishing(ctx)
	defer cancel()
	// The calls read out alone and write what they give apart; what each came to
	// goes to the request's log and the quota one at a time, which share no lock.
	var mu sync.Mutex
	settled := func(r reserve, o tutor.Outcome, took time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		if took > 0 {
			noteCall(ctx, o, took)
		}
		s.afterCall(ctx, r, o)
	}
	var read tutor.SheetRead
	var derived *paperv1.Key
	var noKey *v1.Outcome
	var wg sync.WaitGroup
	wg.Go(func() {
		start := time.Now()
		read = s.paper.ReadPaper(ctx, tutor.PaperReading{Route: out.route, Crop: out.crop}, own)
		settled(out.reading, read.Outcome, time.Since(start))
	})
	if out.derive {
		wg.Go(func() { derived, noKey = s.deriveKey(ctx, out, own, settled) })
	}
	wg.Wait()
	out.read, out.derived, out.noKey = read, derived, noKey
	return out
}

// deriveKey is the key of a task from paper from its crop: the solver's on the
// formula the statement reads into, or else that of one solution of AI (R162
// spec). A reading of the statement that failed or was not admitted goes on to
// the solution, which may serve; a limit or an own key refused stops the chain.
// No key is the outcome of the call that gave none, which the next submission
// asks again. settled notes and settles a call, and with no time gives a
// reserve back as release does.
func (s *Service) deriveKey(ctx context.Context, out paperSending, own string, settled func(reserve, tutor.Outcome, time.Duration)) (*paperv1.Key, *v1.Outcome) {
	task := tutor.PaperTask{Route: out.route, Statement: out.statement}
	unused := tutor.Outcome{Kind: tutor.NotAdmitted}
	start := time.Now()
	st := s.paper.ReadStatement(ctx, task, own)
	settled(out.stating, st.Outcome, time.Since(start))
	switch st.Outcome.Kind {
	case tutor.Answered:
		if st.Formula != "" {
			if n, _ := paper.Solve(st.Formula); n != nil {
				settled(out.solving, unused, 0)
				return paper.Key(n, nil), nil
			}
		}
	case tutor.LimitReached, tutor.KeyRefused:
		settled(out.solving, unused, 0)
		return nil, outcomeOf(st.Outcome, out.route.AI)
	}
	start = time.Now()
	so := s.paper.SolveTask(ctx, task, own)
	settled(out.solving, so.Outcome, time.Since(start))
	if so.Outcome.Kind != tutor.Answered {
		return nil, outcomeOf(so.Outcome, out.route.AI)
	}
	return paper.Key(nil, so.Answer), nil
}

// keptKey is the key the attempt keeps, or none.
func keptKey(row store.Attempt) (*paperv1.Key, error) {
	if row.PaperKey == nil {
		return nil, nil
	}
	k := &paperv1.Key{}
	return k, proto.Unmarshal(row.PaperKey, k)
}

// paperVerdict is the verdict of a submission of a task from paper by its key,
// in the contract's words, and the observation the submission keeps: the
// readings brought to the key and joined, or the one reader's, which module-kit
// judges against the task's one item (paper.Items). With no key, or one with no
// answer, the slot is unchecked with the outcome of why: the quota, the calls
// that derived none, or CAUSE_NO_KEY. The value the learner reads is the one a
// reader wrote, before it was brought to the key.
func (s *Service) paperVerdict(ctx context.Context, row store.Attempt, k *paperv1.Key, out paperSending) (*v1.SubmitWorkResponse, *observationv1.Observation, error) {
	answer := &v1.SubmitWorkResponse{WrongAnswers: int32(row.WrongAnswers)}
	unchecked := func(o *v1.Outcome) (*v1.SubmitWorkResponse, *observationv1.Observation, error) {
		answer.Items = []*v1.ItemVerdict{{Verdict: string(verdict.Unchecked), AnswerSlots: []*v1.AnswerSlotVerdict{{AnswerSlot: paperPlace, Verdict: string(verdict.Unchecked)}}}}
		answer.Outcome = o
		return answer, nil, nil
	}
	switch {
	case out.reading.refused != nil:
		return unchecked(out.reading.refused)
	case k == nil:
		return unchecked(out.noKey)
	case k.GetAnswer() == nil:
		return unchecked(&v1.Outcome{Cause: v1.Cause_CAUSE_NO_KEY})
	}
	items := paper.Items(k.GetAnswer())
	joined, outcome := s.joinedPaper(items, k.GetAnswer(), out.read, out.route.AI)
	verdicts, err := observation.Verdicts(ctx, items, joined, nil)
	if err != nil {
		return nil, nil, err
	}
	shown := joined
	if joined != nil {
		shown = proto.Clone(joined).(*observationv1.Observation)
		if v, ok := shown.GetAnswerSlots()[0].GetRead().(*observationv1.AnswerSlotObservation_Value); ok {
			v.Value = writtenAs(k.GetAnswer(), v.Value, out.read.Observations)
		}
	}
	itemsOut, wrong, correct := itemVerdicts(verdicts, nil, shown)
	answer.Items, answer.Outcome = itemsOut, outcome
	if wrong {
		answer.WrongAnswers++
	}
	if correct {
		answer.Solved = &v1.SolvedTask{}
	}
	return answer, joined, nil
}

// joinedPaper is the observation of a reading of the work on a task from
// paper, as joinOf has it for a sheet, each reader's values brought to the key
// first, so that x = 4 and 4 are one value; or none, with the outcome that left
// the slot unchecked.
func (s *Service) joinedPaper(items []formatv0.Item, key *paperv1.Answer, r tutor.SheetRead, route admissionv0.AiRoute) (*observationv1.Observation, *v1.Outcome) {
	brought := tutor.SheetRead{Outcome: r.Outcome, Observations: make([]*observationv1.Observation, len(r.Observations))}
	for i, o := range r.Observations {
		brought.Observations[i] = paper.Brought(key, o)
	}
	return s.joinOf(items, brought, route)
}

// writtenAs is the value a reader wrote that is brought to value, the first
// reader's first, or value itself when none is.
func writtenAs(key *paperv1.Answer, value string, read []*observationv1.Observation) string {
	for _, o := range read {
		if v, ok := o.GetAnswerSlots()[0].GetRead().(*observationv1.AnswerSlotObservation_Value); ok && paper.BroughtValue(key, v.Value) == value {
			return v.Value
		}
	}
	return value
}
