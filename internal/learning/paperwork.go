package learning

import (
	"cmp"
	"context"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/module-kit/verdict"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	paperv1 "github.com/altnovatrail/spec/contracts/novatrail/paper/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/debug"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// paperChecker reads the statement of a task from paper for "Recognise" and
// checks the work on it, as tutor does; own is as sheetReader has it.
type paperChecker interface {
	ReadStatement(ctx context.Context, p tutor.PaperTask, own string) tutor.StatementRead
	CheckPaper(ctx context.Context, c tutor.PaperCheck, own string) tutor.PaperChecked
}

func (g gateTutor) ReadStatement(ctx context.Context, p tutor.PaperTask, _ string) tutor.StatementRead {
	return g.Gate.ReadStatement(ctx, p)
}

func (g gateTutor) CheckPaper(ctx context.Context, c tutor.PaperCheck, _ string) tutor.PaperChecked {
	return g.Gate.CheckPaper(ctx, c)
}

// soFarCorrect is the verdict of an answer slot of a task from paper with no
// answer yet, whose work the Check read with no mistake in it: no verdict, so
// it counts no wrong answer and closes nothing.
const soFarCorrect = "so_far_correct"

// The verdicts of a task that the Check gives, as tutor names them.
const (
	checkCorrect    = "correct"
	checkIncorrect  = "incorrect"
	checkUnreadable = "unreadable"
	checkNoAnswer   = "no_answer"
)

// taskPlace is the place of the answer slot of the task n of a crop from paper,
// from 1: the slot 1 of the item n.
func taskPlace(n int) *observationv1.AnswerSlotPlace {
	return &observationv1.AnswerSlotPlace{Item: int32(n), Slot: 1}
}

// paperChecking is a Check of a task from paper between the parts of its
// request: the attempt, the call, the reserve of its quota and what came of it,
// or the answer of a request that calls nothing.
type paperChecking struct {
	attemptID string
	check     tutor.PaperCheck
	reserve   reserve
	checked   tutor.PaperChecked
	ready     *v1.GetOpeningWordsResponse
}

// outcome is why the Check of c gave no verdict: the quota that refused its
// reserve, or what its call came to; none when it answered.
func (c paperChecking) outcome() *v1.Outcome {
	if c.reserve.refused != nil {
		return c.reserve.refused
	}
	if c.checked.Outcome.Kind != tutor.Answered {
		return outcomeOf(c.checked.Outcome, c.check.Route.AI)
	}
	return nil
}

// paperAttempt says whether the profile's attempt is of a task from paper,
// read with no lock: what an attempt is of never changes.
func (s *Service) paperAttempt(ctx context.Context, profileID, attemptID string) bool {
	_, paper, _ := s.attemptOf(ctx, profileID, attemptID)
	return paper
}

// holdPaper is the profile's open attempt of a task from paper, held in tx and
// checked as hold checks an attempt of a module: one closed is a closedError,
// and one that lapsed past 7 days with no action is closed "abandoned" by this
// check, the refusal keeping the close.
func (s *Service) holdPaper(ctx context.Context, tx pgx.Tx, profileID, attemptID string, now time.Time) (store.Attempt, error) {
	row, err := store.HoldAttempt(ctx, tx, profileID, attemptID)
	if err != nil {
		return store.Attempt{}, err
	}
	return s.paperOpen(ctx, tx, profileID, row, now)
}

// paperOpen is row, held in tx, checked as holdPaper checks it.
func (s *Service) paperOpen(ctx context.Context, tx pgx.Tx, profileID string, row store.Attempt, now time.Time) (store.Attempt, error) {
	switch {
	case row.CloseReason != nil:
		return store.Attempt{}, &closedError{reason: *row.CloseReason, row: row}
	case !row.Paper:
		return store.Attempt{}, store.ErrNotFound
	case now.After(row.LastActionAt.Add(store.AbandonedAfter)):
		if err := store.CloseAttempt(ctx, tx, profileID, row.ID, closeAbandoned, now); err != nil {
			return store.Attempt{}, err
		}
		return store.Attempt{}, rpc.Keep(refused(ctx, &closedError{reason: closeAbandoned}))
	}
	return row, nil
}

// submitPaper checks a submission of a task from paper under its idempotency
// key, in three parts (rpc.Across): the checks of the attempt and the work, the
// reserve and the key's record going; the Check, one call of AI, in none; and
// the attempt held again with the verdict, in a second. Each slot of the Check
// becomes the verdict of the attempt's answer slot (slotVerdict), and the
// attempt keeps the Check in place of the one before, which goes to its
// history, until it closes. A Check that read nothing of a solution in any
// slot is CAUSE_NO_KEY, its slots unchecked, and the next submission checks
// again, as after one that gave no Check — the quota, a limit, a failure.
// Every task correct closes the attempt "correct" with an empty solved, no
// next and no evidence; any task not yet counts one wrong answer. Taps are
// none: the task has no slot bound choice.
func (a *AttemptService) submitPaper(ctx context.Context, req *v1.SubmitWorkRequest, category formatv0.AgeCategory, key rpc.Key) (*v1.SubmitWorkResponse, error) {
	s := a.s
	act := s.debugged(ctx, v1.DebugActionKind_DEBUG_ACTION_KIND_CHECK, req.GetAttemptId())
	defer act.End()
	profileID := req.GetProfileId()
	// A repeat gives the answer kept: a task from paper has no next. That of the
	// Check that solved the attempt is the record's own, the solved task alone,
	// since the close erased its submission.
	key.Repeat = func(ctx context.Context, tx pgx.Tx, record store.KeyRecord) (proto.Message, error) {
		written := record.Response
		if record.SubmissionID != "" {
			var err error
			if written, err = store.SubmissionAnswer(ctx, tx, profileID, record.SubmissionID); err != nil {
				return nil, err
			}
		}
		answer := &v1.SubmitWorkResponse{}
		return answer, proto.Unmarshal(written, answer)
	}
	return rpc.Across(ctx, s.db, key, req, rpc.Phases[paperChecking, *v1.SubmitWorkResponse]{
		Prepare: func(tx pgx.Tx, _ string) (paperChecking, store.KeyTarget, error) {
			row, err := s.holdPaper(ctx, tx, profileID, req.GetAttemptId(), s.clock())
			if err != nil {
				return paperChecking{}, store.KeyTarget{}, err
			}
			target := store.KeyTarget{ProfileID: profileID, AttemptID: row.ID}
			if err := paperWorkTaken(req.GetWork().GetPaper()); err != nil {
				return paperChecking{}, target, err
			}
			if len(req.GetTappedChoices()) > 0 {
				return paperChecking{}, target, rpc.Malformed("tapped_choices")
			}
			route := act.Route(routeOf(category, req.GetOwnKey()))
			c, err := s.checkAsked(ctx, tx, profileID, row, req.GetWork().GetPaper(), route, category, req.ClockOffsetMinutes)
			return c, target, err
		},
		Call: func(ctx context.Context, c paperChecking) paperChecking {
			return s.callCheck(ctx, c, req.GetOwnKey().GetKey())
		},
		Finish: func(tx pgx.Tx, c paperChecking) (*v1.SubmitWorkResponse, store.KeyTarget, error) {
			now := s.clock()
			row, err := s.holdPaper(ctx, tx, profileID, c.attemptID, now)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			answer := paperVerdict(row, c)
			if answer.GetSolved() != nil {
				// No evidence: an attempt of paper gives none, and no engine chooses after
				// it. Its close keeps none of its submissions and no Check, so this one is
				// not written, and a repeat gets the solved task alone, which the close
				// keeps anyway, and no verdict of a task.
				if err := store.CloseAttempt(ctx, tx, profileID, row.ID, closeCorrect, now); err != nil {
					return nil, store.KeyTarget{}, err
				}
				again, _ := proto.Marshal(&v1.SubmitWorkResponse{Solved: answer.GetSolved()})
				return answer, store.KeyTarget{ProfileID: profileID, AttemptID: row.ID, Response: again}, nil
			}
			if c.outcome() == nil {
				if err := keepCheck(ctx, tx, profileID, row, c.checked); err != nil {
					return nil, store.KeyTarget{}, err
				}
			}
			// An answer of the contract always marshals.
			written, _ := proto.Marshal(answer)
			id, err := store.Submit(ctx, tx, profileID, row.ID, written, nil, answer.GetWrongAnswers() > int32(row.WrongAnswers), now)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			return answer, store.KeyTarget{ProfileID: profileID, AttemptID: row.ID, SubmissionID: id}, nil
		},
	})
}

// paperWorkTaken refuses the work of a task from paper that the contract does
// not take: the task's crop, a JPEG or a PNG, the PNG of the work, and the
// instruction's crop, a JPEG or a PNG when there is one.
func paperWorkTaken(work *v1.PaperWork) error {
	if work == nil || !photoTaken(work.GetStatement()) || !cropTaken(work.GetCrop()) ||
		len(work.GetInstruction()) > 0 && !photoTaken(work.GetInstruction()) {
		return rpc.Malformed("work")
	}
	return nil
}

// checkAsked is the Check of work on the attempt row of a task from paper,
// held in tx, by route, before anything is called: the frame's tasks
// (tasksOf), the language of the profile's interface, the learner's age
// category, the history the attempt keeps, and on platform AI the reserve of
// its quota on the family day of offset. The request's deadline leaves time for
// the call and the transaction after it.
func (s *Service) checkAsked(ctx context.Context, tx pgx.Tx, profileID string, row store.Attempt, work *v1.PaperWork, route tutor.Route, category formatv0.AgeCategory, offset *int32) (paperChecking, error) {
	c := paperChecking{attemptID: row.ID}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 2*finishReserve {
		return c, connect.NewError(connect.CodeDeadlineExceeded, "too little time to check the work")
	}
	language, _, err := store.PaperWordsOf(ctx, tx, profileID)
	if err != nil {
		return c, err
	}
	tasks, err := tasksOf(row)
	if err != nil {
		return c, err
	}
	history, err := historyOf(ctx, tx, profileID, row)
	if err != nil {
		return c, err
	}
	c.check = tutor.PaperCheck{Route: route, Statement: work.GetStatement(), Work: work.GetCrop(), Instruction: work.GetInstruction(),
		Tasks: tasks, Language: cmp.Or(language, wordsLanguage), AgeCategory: string(category), History: history}
	day, err := s.familyDay(ctx, tx, route, offset)
	if err != nil {
		return c, err
	}
	c.reserve, err = s.reserveFor(ctx, tx, profileID, route, tutor.CallChecking, 1, day)
	return c, err
}

// callCheck makes the Check of c until finishReserve before the request's
// deadline, and settles its reserve; own is as sheetReader has it. A request
// answered already, or whose reserve the quota refused, calls nothing. The
// crops go no further than the call.
func (s *Service) callCheck(ctx context.Context, c paperChecking, own string) paperChecking {
	if c.ready == nil && c.reserve.refused == nil {
		called, cancel := finishing(ctx)
		start := time.Now()
		c.checked = s.paper.CheckPaper(called, c.check, own)
		cancel()
		noteCall(ctx, c.checked.Outcome, time.Since(start))
		s.afterCall(ctx, c.reserve, c.checked.Outcome)
	}
	c.check.Statement, c.check.Work, c.check.Instruction = nil, nil, nil
	return c
}

// tasksOf is how many tasks the frame of the attempt row holds, as the Check is
// told: as many as its last Check gave, else 0, for the model to find them
// itself. "Recognise" may read a frame round a column of sums as fewer tasks
// than it holds, so its count is not given.
func tasksOf(row store.Attempt) (int, error) {
	check, err := keptCheck(row)
	if err != nil {
		return 0, err
	}
	return min(len(check.GetSlots()), maxPaperTasks), nil
}

// keptCheck is the Check the attempt keeps, or none before its first.
func keptCheck(row store.Attempt) (*paperv1.Check, error) {
	if row.PaperCheck == nil {
		return nil, nil
	}
	c := &paperv1.Check{}
	if err := proto.Unmarshal(row.PaperCheck, c); err != nil {
		return nil, err
	}
	return c, nil
}

// historyOf is the attempt's Checks before the next one, as the Check is given
// them, in order: those its history keeps, and its last (lastTurn).
func historyOf(ctx context.Context, tx pgx.Tx, profileID string, row store.Attempt) ([]tutor.CheckTurn, error) {
	var history []tutor.CheckTurn
	for _, written := range row.PaperHistory {
		turn := &paperv1.Check{}
		if err := proto.Unmarshal(written, turn); err != nil {
			return nil, err
		}
		history = append(history, turnOf(turn))
	}
	last, err := lastTurn(ctx, tx, profileID, row)
	if err != nil || last == nil {
		return history, err
	}
	return append(history, turnOf(last)), nil
}

// lastTurn is what the history keeps of the attempt's last Check, once a Check
// comes after it: its verdicts, its tutor's words and, as its hints, the rungs
// of its ladder the learner was shown. None before the first Check.
func lastTurn(ctx context.Context, tx pgx.Tx, profileID string, row store.Attempt) (*paperv1.Check, error) {
	check, err := keptCheck(row)
	if err != nil || check == nil {
		return nil, err
	}
	shown, err := store.ShownRungs(ctx, tx, profileID, row.ID)
	if err != nil {
		return nil, err
	}
	turn := &paperv1.Check{TutorWords: check.GetTutorWords(), Hints: shown}
	for _, slot := range check.GetSlots() {
		turn.Slots = append(turn.Slots, &paperv1.SlotCheck{Task: slot.GetTask(), Verdict: slot.GetVerdict()})
	}
	return turn, nil
}

// turnOf is a turn of the history as tutor takes it.
func turnOf(c *paperv1.Check) tutor.CheckTurn {
	turn := tutor.CheckTurn{Words: c.GetTutorWords(), Rungs: c.GetHints()}
	for _, slot := range c.GetSlots() {
		turn.Verdicts = append(turn.Verdicts, slot.GetVerdict())
	}
	return turn
}

// keepCheck keeps checked on the attempt row, held in tx, in place of the Check
// before, which goes to the end of its history (lastTurn).
func keepCheck(ctx context.Context, tx pgx.Tx, profileID string, row store.Attempt, checked tutor.PaperChecked) error {
	last, err := lastTurn(ctx, tx, profileID, row)
	if err != nil {
		return err
	}
	var turn []byte
	if last != nil {
		// A Check of the contract always marshals.
		turn, _ = proto.Marshal(last)
	}
	kept, _ := proto.Marshal(checkOf(checked))
	return store.KeepPaperCheck(ctx, tx, profileID, row.ID, kept, turn)
}

// checkOf is the Check tutor gave, as the attempt keeps it.
func checkOf(checked tutor.PaperChecked) *paperv1.Check {
	c := &paperv1.Check{TutorWords: checked.TutorWords, Hints: checked.Hints}
	for _, s := range checked.Slots {
		slot := &paperv1.SlotCheck{Task: int32(s.Task), Verdict: s.Verdict, Confidence: s.Confidence, Read: s.Read, ReadBox: s.ReadBox,
			Assumptions: s.Assumptions, Solution: s.Solution, Answer: s.Answer}
		for _, e := range s.Errors {
			slot.Errors = append(slot.Errors, &paperv1.CheckError{Box: e.Box, Where: e.Where, Kind: e.Kind, Note: e.Note})
		}
		c.Slots = append(c.Slots, slot)
	}
	return c
}

// paperVerdict is the verdict of the Check c on the attempt row of a task from
// paper, in the contract's words: an item for each task the Check gave, the
// task n the item n, each with the verdict of its slot (slotVerdict), however
// many the Checks before gave; the Check's tutor's words; a wrong answer
// counted when any task is not yet; and the solved task, empty, when every
// task is correct. A Check that read nothing of a solution in any slot, and
// one that did not answer, leave unchecked, with the outcome of why, an item
// for each task the Check gave, else for each it was told of, else one.
func paperVerdict(row store.Attempt, c paperChecking) *v1.SubmitWorkResponse {
	answer := &v1.SubmitWorkResponse{WrongAnswers: int32(row.WrongAnswers)}
	outcome := c.outcome()
	if outcome == nil && blank(c.checked.Slots) {
		outcome = &v1.Outcome{Cause: v1.Cause_CAUSE_NO_KEY}
	}
	if outcome != nil {
		tasks := len(c.checked.Slots)
		if tasks == 0 {
			tasks = max(1, c.check.Tasks)
		}
		for n := range tasks {
			answer.Items = append(answer.Items, &v1.ItemVerdict{Verdict: string(verdict.Unchecked),
				AnswerSlots: []*v1.AnswerSlotVerdict{{AnswerSlot: taskPlace(n + 1), Verdict: string(verdict.Unchecked)}}})
		}
		answer.Outcome = outcome
		return answer
	}
	solved, wrong := true, false
	for _, s := range c.checked.Slots {
		slot := slotVerdict(s)
		answer.Items = append(answer.Items, &v1.ItemVerdict{Verdict: slot.GetVerdict(), AnswerSlots: []*v1.AnswerSlotVerdict{slot}})
		solved = solved && slot.GetVerdict() == string(verdict.Correct)
		wrong = wrong || slot.GetVerdict() == string(verdict.NotYet)
	}
	if wrong {
		answer.WrongAnswers++
	}
	answer.TutorWords = c.checked.TutorWords
	if solved {
		answer.Solved = &v1.SolvedTask{}
	}
	return answer
}

// blank says whether a Check read nothing of a solution in any slot: each with
// no answer, nothing read and no mistake.
func blank(slots []tutor.SlotChecked) bool {
	for _, s := range slots {
		if s.Verdict != checkNoAnswer || s.Read != "" || len(s.Errors) > 0 {
			return false
		}
	}
	return true
}

// slotVerdict is the verdict of the answer slot of the task of s, by the
// Check's verdict of it: "correct" is correct and "incorrect" not yet, each with
// the value read; "unreadable" is unreadable, illegible; and with no answer yet
// a mistake is not yet, at the first mistake, a solution read with none is so
// far correct, and nothing read is unreadable, blank, with no box. The slot
// stands at the box of what was read.
func slotVerdict(s tutor.SlotChecked) *v1.AnswerSlotVerdict {
	v := &v1.AnswerSlotVerdict{AnswerSlot: taskPlace(s.Task), CropBox: s.ReadBox}
	switch {
	case s.Verdict == checkCorrect:
		v.Verdict = string(verdict.Correct)
	case s.Verdict == checkIncorrect:
		v.Verdict = string(verdict.NotYet)
	case s.Verdict == checkUnreadable:
		v.Verdict, v.Reason = string(verdict.Unreadable), string(verdict.Illegible)
	case len(s.Errors) > 0:
		v.Verdict = string(verdict.NotYet)
		if box := s.Errors[0].Box; box != nil {
			v.CropBox = box
		}
	case s.Read != "":
		v.Verdict = soFarCorrect
	default:
		v.Verdict, v.Reason, v.CropBox = string(verdict.Unreadable), string(verdict.Blank), nil
	}
	if (s.Verdict == checkCorrect || s.Verdict == checkIncorrect) && s.Read != "" {
		v.Value = proto.String(s.Read)
	}
	return v
}

// firstMistake is the box of the first mistake of the first task the Check
// did not judge correct, where the wavy line of "Show where" goes; none when
// that task has no mistake with a place.
func firstMistake(c *paperv1.Check) *observationv1.CropBox {
	for _, slot := range c.GetSlots() {
		if slot.GetVerdict() == checkCorrect {
			continue
		}
		if errs := slot.GetErrors(); len(errs) > 0 {
			return errs[0].GetBox()
		}
		return nil
	}
	return nil
}

// debugChosen is whether the caller's account has the right of the debug mode
// and a first model of it, which takes a call past the admission list: a choice
// kept from before the right was taken chooses nothing.
func (s *Service) debugChosen(ctx context.Context) bool {
	caller, _ := rpc.CallerFrom(ctx)
	return s.debug.Open(caller) && s.debug.Chosen(caller.AccountID)
}

// debugged is the action of the debug mode for a request of the caller's
// account, nil without the mode or without the account's right.
func (s *Service) debugged(ctx context.Context, kind v1.DebugActionKind, attemptID string) *debug.Action {
	caller, _ := rpc.CallerFrom(ctx)
	if !s.debug.Open(caller) {
		return nil
	}
	return s.debug.Begin(caller.AccountID, kind, attemptID, s.clock())
}
