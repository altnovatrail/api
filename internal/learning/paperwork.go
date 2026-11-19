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

	"github.com/altnovatrail/api/internal/debug"
	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// paperChecker reads the statement of a task from paper into its tasks, solves
// those no formula keys, and reads the work on them, as tutor does; own is as
// sheetReader has it (R162 spec).
type paperChecker interface {
	ReadStatement(ctx context.Context, p tutor.PaperTask, own string) tutor.StatementRead
	SolveTask(ctx context.Context, p tutor.PaperTask, tasks []tutor.TaskToSolve, own string) tutor.TasksSolved
	ReadPaper(ctx context.Context, r tutor.PaperReading, own string) tutor.SheetRead
}

func (g gateTutor) ReadStatement(ctx context.Context, p tutor.PaperTask, _ string) tutor.StatementRead {
	return g.Gate.ReadStatement(ctx, p)
}

func (g gateTutor) SolveTask(ctx context.Context, p tutor.PaperTask, tasks []tutor.TaskToSolve, _ string) tutor.TasksSolved {
	return g.Gate.SolveTask(ctx, p, tasks)
}

func (g gateTutor) ReadPaper(ctx context.Context, r tutor.PaperReading, _ string) tutor.SheetRead {
	return g.Gate.ReadPaper(ctx, r)
}

// taskPlace is the place of the answer slot of the task n of a crop from paper,
// from 1: the slot 1 of the item n.
func taskPlace(n int) *observationv1.AnswerSlotPlace {
	return &observationv1.AnswerSlotPlace{Item: int32(n), Slot: 1}
}

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
	// debug is the action of the debug mode, nil without it (R97).
	debug *debug.Action
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
// (R162 spec). The first submission derives the key from the task's crop: the
// statement read into its tasks, one or each of several framed together, and
// the key of each, the solver's on its formula, or, for the tasks with no
// formula the solver takes, one solution of AI of them all, whose answer of a
// task is its key once it holds to the rules of one (paper.Key); the work is
// read beside the solution, given the tasks' statements. The attempt keeps the
// key, with the statements, until it closes. A task with no answer is
// unchecked, and with none in any task every submission gets CAUSE_NO_KEY and
// calls no AI. Calls that derive no key are an outcome, the slots unchecked,
// and the next submission derives it again. The work is read as a sheet is,
// each value brought to its task's key, joined and judged by module-kit; every
// task with a key correct closes the attempt "correct" with an empty solved, no
// next and no evidence. Taps are none: the task has no slot bound choice.
func (a *AttemptService) submitPaper(ctx context.Context, req *v1.SubmitWorkRequest, category formatv0.AgeCategory, key rpc.Key) (*v1.SubmitWorkResponse, error) {
	s := a.s
	act := s.debugged(ctx, v1.DebugActionKind_DEBUG_ACTION_KIND_CHECK, req.GetAttemptId())
	defer act.End()
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
			out := paperSending{attemptID: row.ID, route: act.Route(routeOf(category, req.GetOwnKey())), statement: work.GetStatement(), crop: work.GetCrop(), debug: act}
			if out.key, err = keptKey(row); err != nil {
				return paperSending{}, target, err
			}
			if out.key != nil && !keyed(out.key) {
				// No key to judge by: no call goes, and nothing is reserved.
				return out, target, nil
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
			saved, err := keptKey(row)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			k, keep := judgedKey(saved, out)
			if keep {
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
// deadline: the reading of the work, given the statements of the tasks, which
// a key derived now has only once the statement is read, and then the reading
// goes beside the solution. A reserve the quota refused, or a key kept with no
// answer, calls nothing. The crops go no further than the calls.
func (s *Service) checkPaper(ctx context.Context, out paperSending, own string) (done paperSending) {
	defer func() { done.statement, done.crop = nil, nil }()
	if out.reading.refused != nil || out.key != nil && !keyed(out.key) {
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
	var wg sync.WaitGroup
	reading := false
	readWork := func(tasks []string) {
		reading = true
		wg.Go(func() {
			start := time.Now()
			read = s.paper.ReadPaper(ctx, tutor.PaperReading{Route: out.route, Crop: out.crop, Tasks: tasks}, own)
			settled(out.reading, read.Outcome, time.Since(start))
		})
	}
	if out.derive {
		out.derived, out.noKey = s.deriveKey(ctx, out, own, settled, readWork)
	} else {
		readWork(statementsOf(out.key))
	}
	wg.Wait()
	if !reading {
		settled(out.reading, tutor.Outcome{Kind: tutor.NotAdmitted}, 0)
	}
	out.read = read
	return out
}

// keyed is whether some task of a key has an answer to judge by.
func keyed(k *paperv1.Key) bool {
	for _, t := range k.GetTasks() {
		if t.GetAnswer() != nil {
			return true
		}
	}
	return false
}

// statementsOf are the statements of a key's tasks, in order, as the reading of
// the work is given them.
func statementsOf(k *paperv1.Key) []string {
	out := make([]string, len(k.GetTasks()))
	for i, t := range k.GetTasks() {
		out[i] = t.GetStatement()
	}
	return out
}

// deriveKey is the key of a task from paper from its crop (R162 spec): the
// statement read into its tasks, and the key of each, the solver's on its
// formula or else that of one solution of AI of all the tasks no formula keys.
// read starts the reading of the work once the statements are known, beside
// the solution. A reading of the statement that gave no tasks — a failure, a
// limit, no admission — stops the chain, and its outcome is the key's: the next
// submission asks again. A crop on which the statement found no task gets a key
// of none, CAUSE_NO_KEY. settled notes and settles a call, and with no time
// gives a reserve back as release does.
func (s *Service) deriveKey(ctx context.Context, out paperSending, own string, settled func(reserve, tutor.Outcome, time.Duration), read func([]string)) (*paperv1.Key, *v1.Outcome) {
	task := tutor.PaperTask{Route: out.route, Statement: out.statement}
	unused := tutor.Outcome{Kind: tutor.NotAdmitted}
	start := time.Now()
	st := s.paper.ReadStatement(ctx, task, own)
	settled(out.stating, st.Outcome, time.Since(start))
	if st.Outcome.Kind != tutor.Answered {
		settled(out.solving, unused, 0)
		return nil, outcomeOf(st.Outcome, out.route.AI)
	}
	k := &paperv1.Key{}
	if len(st.Tasks) == 0 {
		settled(out.solving, unused, 0)
		return k, nil
	}
	statements := make([]string, len(st.Tasks))
	for i, t := range st.Tasks {
		statements[i] = t.Statement
	}
	read(statements)
	var unsolved []tutor.TaskToSolve
	start = time.Now()
	for i, t := range st.Tasks {
		tk := &paperv1.TaskKey{Statement: t.Statement}
		if t.Formula != "" {
			if n, _ := paper.Solve(t.Formula); n != nil {
				tk = paper.Key(n, nil)
				tk.Statement = t.Statement
			}
		}
		if tk.GetAnswer() == nil {
			unsolved = append(unsolved, tutor.TaskToSolve{Number: i + 1, Statement: t.Statement})
		}
		k.Tasks = append(k.Tasks, tk)
	}
	if len(unsolved) < len(st.Tasks) {
		out.debug.Solved(k, time.Since(start))
	}
	if len(unsolved) == 0 {
		settled(out.solving, unused, 0)
		return k, nil
	}
	start = time.Now()
	so := s.paper.SolveTask(ctx, task, unsolved, own)
	settled(out.solving, so.Outcome, time.Since(start))
	if so.Outcome.Kind != tutor.Answered {
		return nil, outcomeOf(so.Outcome, out.route.AI)
	}
	for _, t := range unsolved {
		tk := paper.Key(nil, so.Answers[t.Number])
		tk.Statement = t.Statement
		k.Tasks[t.Number-1] = tk
	}
	return k, nil
}

// keptKey is the key the attempt keeps, or none. A key of a shape this build
// knows not reads as no task and fields unknown: it is none, derived again, not
// a key of no task, which every submission would take for CAUSE_NO_KEY.
func keptKey(row store.Attempt) (*paperv1.Key, error) {
	if row.PaperKey == nil {
		return nil, nil
	}
	k := &paperv1.Key{}
	if err := proto.Unmarshal(row.PaperKey, k); err != nil {
		return nil, err
	}
	if len(k.GetTasks()) == 0 && len(k.ProtoReflect().GetUnknown()) > 0 {
		return nil, nil
	}
	return k, nil
}

// judgedKey is the key a submission is judged by, given the one the attempt
// keeps now, and whether to keep it: a submission that derived a key read the
// work against its statements, and is judged by it alone, kept unless a Check
// that went meanwhile kept its own, which may list other tasks. Any other is
// judged by the key kept, none when the derivation gave none.
func judgedKey(kept *paperv1.Key, out paperSending) (*paperv1.Key, bool) {
	if !out.derive {
		return kept, false
	}
	return out.derived, kept == nil && out.derived != nil
}

// paperVerdict is the verdict of a submission of a task from paper by its key,
// in the contract's words, and the observation the submission keeps: an item
// for each task of the key, the task n the item n, each judged by module-kit
// against its own key (paper.Items) on the readings of its slot brought to it
// and joined, or the one reader's; a task with no key is unchecked. With no key,
// or none in any task, every slot is unchecked with the outcome of why: the
// quota, the calls that derived none, or CAUSE_NO_KEY. The value the learner
// reads is the one a reader wrote, before it was brought to the key, with the
// box where the reader saw it.
func (s *Service) paperVerdict(ctx context.Context, row store.Attempt, k *paperv1.Key, out paperSending) (*v1.SubmitWorkResponse, *observationv1.Observation, error) {
	answer := &v1.SubmitWorkResponse{WrongAnswers: int32(row.WrongAnswers)}
	unchecked := func(o *v1.Outcome) (*v1.SubmitWorkResponse, *observationv1.Observation, error) {
		for n := range max(1, len(k.GetTasks())) {
			answer.Items = append(answer.Items, &v1.ItemVerdict{Verdict: string(verdict.Unchecked), AnswerSlots: []*v1.AnswerSlotVerdict{{AnswerSlot: taskPlace(n + 1), Verdict: string(verdict.Unchecked)}}})
		}
		answer.Outcome = o
		return answer, nil, nil
	}
	switch {
	case out.reading.refused != nil:
		return unchecked(out.reading.refused)
	case k == nil:
		return unchecked(out.noKey)
	case !keyed(k):
		return unchecked(&v1.Outcome{Cause: v1.Cause_CAUSE_NO_KEY})
	}
	if act := out.debug; act.Pair() && len(out.read.Observations) == 2 {
		act.Differ(readDiffer(out.read.Observations[0], out.read.Observations[1]))
	}
	kept := &observationv1.Observation{}
	solved := true
	for i, t := range k.GetTasks() {
		place := taskPlace(i + 1)
		read := tutor.SheetRead{Outcome: out.read.Outcome}
		for _, o := range out.read.Observations {
			read.Observations = append(read.Observations, slotOf(o, place))
		}
		if t.GetAnswer() == nil {
			answer.Items = append(answer.Items, &v1.ItemVerdict{Verdict: string(verdict.Unchecked), AnswerSlots: []*v1.AnswerSlotVerdict{{AnswerSlot: place, Verdict: string(verdict.Unchecked)}}})
			continue
		}
		items := paper.Items(t.GetAnswer())
		joined, outcome := s.joinedPaper(items, t.GetAnswer(), read, out.route.AI, out.debug)
		verdicts, err := observation.Verdicts(ctx, items, joined, nil)
		if err != nil {
			return nil, nil, err
		}
		shown := joined
		if joined != nil {
			shown = proto.Clone(joined).(*observationv1.Observation)
			if v, ok := shown.GetAnswerSlots()[0].GetRead().(*observationv1.AnswerSlotObservation_Value); ok {
				v.Value = writtenAs(t.GetAnswer(), v.Value, read.Observations)
			}
			slot := proto.Clone(joined.GetAnswerSlots()[0]).(*observationv1.AnswerSlotObservation)
			slot.AnswerSlot = place
			kept.AnswerSlots = append(kept.AnswerSlots, slot)
		}
		one, wrong, correct := itemVerdicts(verdicts, nil, shown)
		slot := one[0].GetAnswerSlots()[0]
		slot.AnswerSlot = place
		if slot.Value != nil {
			slot.CropBox = boxOf(read.Observations)
		}
		answer.Items = append(answer.Items, one[0])
		if outcome != nil {
			answer.Outcome = outcome
		}
		if wrong {
			answer.WrongAnswers = int32(row.WrongAnswers) + 1
		}
		solved = solved && correct
	}
	if solved {
		answer.Solved = &v1.SolvedTask{}
	}
	if len(kept.GetAnswerSlots()) == 0 {
		kept = nil
	}
	return answer, kept, nil
}

// slotOf is the observation of the slot at place of a reader's observation,
// put at the place 1, 1 of a task's one item, or one with no slot when the
// reader gave none there.
func slotOf(o *observationv1.Observation, place *observationv1.AnswerSlotPlace) *observationv1.Observation {
	for _, slot := range o.GetAnswerSlots() {
		if proto.Equal(slot.GetAnswerSlot(), place) {
			one := proto.Clone(slot).(*observationv1.AnswerSlotObservation)
			one.AnswerSlot = taskPlace(1)
			return &observationv1.Observation{AnswerSlots: []*observationv1.AnswerSlotObservation{one}}
		}
	}
	return &observationv1.Observation{}
}

// boxOf is where the first reader that gave the slot a box saw it, or none.
func boxOf(read []*observationv1.Observation) *observationv1.CropBox {
	for _, o := range read {
		if b := o.GetAnswerSlots(); len(b) > 0 && b[0].GetCropBox() != nil {
			return b[0].GetCropBox()
		}
	}
	return nil
}

// joinedPaper is the observation of a reading of the work on a task from
// paper, by its one reader in any mode of reading (R96), or by the pair of the
// debug mode's two readers (R97), each value brought to the key first, so that
// x = 4 is 4; or none, with the outcome that left the slot unchecked.
func (s *Service) joinedPaper(items []formatv0.Item, key *paperv1.Answer, r tutor.SheetRead, route admissionv0.AiRoute, act *debug.Action) (*observationv1.Observation, *v1.Outcome) {
	brought := tutor.SheetRead{Outcome: r.Outcome, Observations: make([]*observationv1.Observation, len(r.Observations))}
	for i, o := range r.Observations {
		brought.Observations[i] = paper.Brought(key, o)
	}
	return s.joinOf(items, brought, route, !act.Pair())
}

// readDiffer is whether two readers read some answer slot differently: its
// value or why it is unreadable, as the join of R68 tells them apart; where
// each saw the slot does not count.
func readDiffer(a, b *observationv1.Observation) bool {
	read := func(o *observationv1.Observation) map[[2]int32]string {
		m := map[[2]int32]string{}
		for _, slot := range o.GetAnswerSlots() {
			place := [2]int32{slot.GetAnswerSlot().GetItem(), slot.GetAnswerSlot().GetSlot()}
			if _, ok := slot.GetRead().(*observationv1.AnswerSlotObservation_Value); ok {
				m[place] = "value:" + slot.GetValue()
			} else {
				m[place] = "unreadable:" + slot.GetUnreadable().String()
			}
		}
		return m
	}
	ra, rb := read(a), read(b)
	if len(ra) != len(rb) {
		return true
	}
	for place, v := range ra {
		if rb[place] != v {
			return true
		}
	}
	return false
}

// debugChosen is whether the caller's account has a first model of the debug
// mode, which takes a call past the admission list (R97).
func (s *Service) debugChosen(ctx context.Context) bool {
	caller, _ := rpc.CallerFrom(ctx)
	return s.debug.Chosen(caller.AccountID)
}

// debugged is the action of the debug mode for a request of the caller's
// account, nil without the mode (R97).
func (s *Service) debugged(ctx context.Context, kind v1.DebugActionKind, attemptID string) *debug.Action {
	if s.debug == nil {
		return nil
	}
	caller, _ := rpc.CallerFrom(ctx)
	return s.debug.Begin(caller.AccountID, kind, attemptID, s.clock())
}

// writtenAs is the value a reader wrote that is brought to value, the first
// reader's first, or value itself when none is.
func writtenAs(key *paperv1.Answer, value string, read []*observationv1.Observation) string {
	for _, o := range read {
		if len(o.GetAnswerSlots()) == 0 {
			continue
		}
		if v, ok := o.GetAnswerSlots()[0].GetRead().(*observationv1.AnswerSlotObservation_Value); ok && paper.BroughtValue(key, v.Value) == value {
			return v.Value
		}
	}
	return value
}
