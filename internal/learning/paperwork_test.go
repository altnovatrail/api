package learning_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	paperv1 "github.com/altnovatrail/spec/contracts/novatrail/paper/v1"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/tutor"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/admission"
	"github.com/altnovatrail/api/internal/idem"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/tutorstub"
)

// paperWork is the work of a task from paper: the statement's crop, a JPEG of
// width by height, which the stub reads into a formula when wider than tall,
// and the crop of the work, whose shape says what the stub reads in it.
func paperWork(t *testing.T, statement [2]int, crop []byte) *v1.Work {
	return &v1.Work{Surface: &v1.Work_Paper{Paper: &v1.PaperWork{Statement: photoOf(t, statement[0], statement[1]), Crop: crop}}}
}

var (
	formulaStatement = [2]int{400, 300}
	wordStatement    = [2]int{300, 400}
	noKeyStatement   = [2]int{300, 300}
	severalStatement = [2]int{150, 400}
)

// reserveOf is what a call of a task from paper reserves on platform AI for an
// adult: the expected price of every attempt of its cascade, by tutor through
// the gate (R137 tutor).
func reserveOf(g *admission.Gate, c tutor.Call) quota.Micro {
	return quota.Reserve(c, g.Price(c, tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategoryAdult}), 1)
}

// paperKey is the key the attempt keeps, or nil when it keeps none.
func (w *world) paperKey(t *testing.T, attempt string) *paperv1.Key {
	t.Helper()
	var kept []byte
	if err := w.pool.QueryRow(t.Context(), `SELECT paper_key FROM attempts WHERE id = $1`, attempt).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept == nil {
		return nil
	}
	k := &paperv1.Key{}
	if err := proto.Unmarshal(kept, k); err != nil {
		t.Fatal(err)
	}
	return k
}

// firstTask is the first task of a key, or nil.
func firstTask(k *paperv1.Key) *paperv1.TaskKey {
	if len(k.GetTasks()) == 0 {
		return nil
	}
	return k.GetTasks()[0]
}

// slotOf is the verdict of the one answer slot of a task from paper.
func slotOf(t *testing.T, res *v1.SubmitWorkResponse) *v1.AnswerSlotVerdict {
	t.Helper()
	if len(res.GetItems()) != 1 || len(res.GetItems()[0].GetAnswerSlots()) != 1 {
		t.Fatalf("the verdict = %v, want one item of one slot", res)
	}
	return res.GetItems()[0].GetAnswerSlots()[0]
}

// The first Check of a task from paper derives its key while the work is read
// beside it (R162 spec): a statement the stub reads into a formula gets the
// solver's key, and its solution is not asked, its part of the reserve given
// back. A wrong value is a wrong answer, and a later Check reads the work alone
// by the key kept; the right one closes the attempt "correct" with an empty
// solved and no next, the key and the submissions erased with the close. A
// repeat under the key gives the task solved alone, with no call and no verdict
// of a task, which the close erased (R101).
func TestATaskFromPaperIsCheckedByTheSolversKey(t *testing.T) {
	t.Parallel()

	w, g := newStubWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	first := &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 40, 30))}
	wrong, err := w.attempts.SubmitWork(l.ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if s := slotOf(t, wrong); s.GetVerdict() != "not_yet" || s.GetValue() != "851" || wrong.GetWrongAnswers() != 1 || wrong.GetSolved() != nil {
		t.Errorf("a wrong value = %v, want not_yet, 851 read and one wrong answer", wrong)
	}
	if again, err := w.attempts.SubmitWork(l.ctx, first); err != nil || !proto.Equal(again, wrong) {
		t.Errorf("the repeat of the wrong Check = %v (%v), want its answer, which its submission keeps", again, err)
	}
	// The key kept, a Check on an own key the provider refuses reads the work
	// alone, and gets the refusal: unchecked, and no wrong answer more.
	refused, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 90, 30)), OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "sk-own" + tutorstub.KeyRefused}})
	if err != nil {
		t.Fatal(err)
	}
	if s := slotOf(t, refused); s.GetVerdict() != "unchecked" || refused.GetOutcome().GetCause() != v1.Cause_CAUSE_KEY_REFUSED || refused.GetWrongAnswers() != 1 {
		t.Errorf("a refused own key by the key kept = %v, want unchecked with CAUSE_KEY_REFUSED and one wrong answer still", refused)
	}
	k := w.paperKey(t, a)
	if firstTask(k).GetProvenance() != paperv1.Provenance_PROVENANCE_SOLVER || firstTask(k).GetAnswer().GetNumber().GetValue() != "85" {
		t.Errorf("the key kept = %v, want the solver's 85", k)
	}
	reading, stating := reserveOf(g, tutor.CallPaper), reserveOf(g, tutor.CallStatement)
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != reading+stating || profile.Reserved != 0 {
		t.Errorf("the counter = %+v, want the reading and the statement spent, the solution given back", profile)
	}

	key := uuid.NewString()
	req := &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: key, Work: paperWork(t, formulaStatement, pngOf(t, 90, 30))}
	right, err := w.attempts.SubmitWork(l.ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if s := slotOf(t, right); s.GetVerdict() != "correct" || s.GetValue() != "85" || right.GetSolved() == nil || right.GetNext() != nil || right.GetWrongAnswers() != 1 {
		t.Errorf("the right value = %v, want correct, solved and no next", right)
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != 2*reading+stating {
		t.Errorf("the counter = %+v, want a second reading alone", profile)
	}
	var kept int
	if err := w.pool.QueryRow(l.ctx, `SELECT count(*) FROM submissions WHERE attempt_id = $1`, a).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if w.closeOf(t, a) != "correct" || w.paperKey(t, a) != nil || kept != 0 {
		t.Errorf("the attempt closed %q, its key %v, %d submissions kept; want correct, and none", w.closeOf(t, a), w.paperKey(t, a), kept)
	}
	again, err := w.attempts.SubmitWork(l.ctx, req)
	if err != nil || !proto.Equal(again, &v1.SubmitWorkResponse{Solved: right.GetSolved()}) {
		t.Errorf("the repeat = %v (%v), want the task solved alone", again, err)
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != 2*reading+stating {
		t.Errorf("the counter after the repeat = %+v, want no call", profile)
	}
	_, err = w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 90, 30))})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED)
}

// A statement that is no formula goes to one solution of AI, whose answer is
// the key; a square crop of the work is illegible, no wrong answer.
func TestATaskFromPaperIsCheckedByAIsKey(t *testing.T) {
	t.Parallel()

	w, g := newStubWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, wordStatement, pngOf(t, 30, 30))})
	if err != nil {
		t.Fatal(err)
	}
	if s := slotOf(t, res); s.GetVerdict() != "unreadable" || res.GetWrongAnswers() != 0 || res.GetOutcome() != nil {
		t.Errorf("an illegible work = %v, want unreadable and no wrong answer", res)
	}
	if k := w.paperKey(t, a); firstTask(k).GetProvenance() != paperv1.Provenance_PROVENANCE_AI || firstTask(k).GetAnswer().GetNumber().GetValue() != "85" {
		t.Errorf("the key kept = %v, want AI's 85", k)
	}
	all := reserveOf(g, tutor.CallPaper) + reserveOf(g, tutor.CallStatement) + reserveOf(g, tutor.CallSolving)
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != all {
		t.Errorf("the counter = %+v, want the three calls spent, %d", profile, all)
	}
}

// A task whose solution gives no one answer has no key: the slot is unchecked
// with CAUSE_NO_KEY, and a later Check calls no AI.
func TestATaskFromPaperWithNoKeyIsNotChecked(t *testing.T) {
	t.Parallel()

	w, g := newStubWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	calls := 0
	for i := range 2 {
		res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
			Work: paperWork(t, noKeyStatement, pngOf(t, 90, 30))})
		if err != nil {
			t.Fatal(err)
		}
		if s := slotOf(t, res); s.GetVerdict() != "unchecked" || res.GetOutcome().GetCause() != v1.Cause_CAUSE_NO_KEY {
			t.Errorf("Check %d = %v, want unchecked with CAUSE_NO_KEY", i+1, res)
		}
		if n := strings.Count(w.log.String(), `"model":"stub"`); i == 0 {
			calls = n
		} else if n != calls || calls == 0 {
			t.Errorf("the log has %d calls of the stub after the second Check, %d after the first; want none more", n, calls)
		}
	}
	if k := w.paperKey(t, a); k == nil || firstTask(k).GetAnswer() != nil {
		t.Errorf("the key kept = %v, want one with no answer", k)
	}
	all := reserveOf(g, tutor.CallPaper) + reserveOf(g, tutor.CallStatement) + reserveOf(g, tutor.CallSolving)
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != all {
		t.Errorf("the counter = %+v, want the first Check's calls alone, %d", profile, all)
	}
}

// A crop of several tasks is checked task by task (R162 spec): the statement
// reads into three, each keyed by the solver, so no solution is asked and its
// reserve goes back; the work is read given their statements, and each task's
// item gets its verdict, its value and the box where it was read. One wrong
// task makes the Check a wrong answer, and the attempt stays open; a later
// Check reads the work alone, by the key kept with its statements.
func TestATaskFromPaperOfSeveralTasksIsCheckedTaskByTask(t *testing.T) {
	t.Parallel()

	w, g := newStubWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	calls := 0
	for i := range 2 {
		res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
			Work: paperWork(t, severalStatement, pngOf(t, 30, 90))})
		if err != nil {
			t.Fatal(err)
		}
		items := res.GetItems()
		if len(items) != 3 || res.GetOutcome() != nil || res.GetSolved() != nil || res.GetWrongAnswers() != int32(i+1) {
			t.Fatalf("Check %d = %v, want three items, two right", i+1, res)
		}
		for n, want := range []string{"correct", "correct", "not_yet"} {
			s := items[n].GetAnswerSlots()[0]
			if items[n].GetVerdict() != want || s.GetAnswerSlot().GetItem() != int32(n+1) || s.GetValue() != "85" || s.GetCropBox() == nil {
				t.Errorf("Check %d, task %d = %v, want %s, read 85, with its box", i+1, n+1, items[n], want)
			}
		}
		if n := strings.Count(w.log.String(), `"model":"stub"`); i == 0 {
			calls = n
		} else if n != calls+1 {
			t.Errorf("the log has %d calls of the stub after the second Check, %d after the first; want the reading alone", n, calls)
		}
	}
	k := w.paperKey(t, a)
	if len(k.GetTasks()) != 3 || k.GetTasks()[2].GetStatement() != "50 + 36 =" || k.GetTasks()[2].GetAnswer().GetNumber().GetValue() != "86" ||
		k.GetTasks()[0].GetProvenance() != paperv1.Provenance_PROVENANCE_SOLVER {
		t.Errorf("the key kept = %v, want the solver's three, with their statements", k)
	}
	spent := 2*reserveOf(g, tutor.CallPaper) + reserveOf(g, tutor.CallStatement)
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != spent {
		t.Errorf("the counter = %+v, want two readings and the statement, %d", profile, spent)
	}
}

// The quota that does not hold the calls leaves the slot unchecked with its
// outcome and keeps no key, so the next Check derives it.
func TestATaskFromPaperOverTheQuota(t *testing.T) {
	t.Parallel()

	w, _ := newStubWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	w.spend(t, l.accountID, 1_995_000)
	res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 90, 30))})
	if err != nil {
		t.Fatal(err)
	}
	if s := slotOf(t, res); s.GetVerdict() != "unchecked" || res.GetOutcome().GetCause() != v1.Cause_CAUSE_QUOTA {
		t.Errorf("over the quota = %v, want unchecked with CAUSE_QUOTA", res)
	}
	if k := w.paperKey(t, a); k != nil {
		t.Errorf("the key kept = %v, want none", k)
	}
}

// What a Check of a task from paper refuses: no work of paper, a statement or a
// crop the contract does not take, and taps; and a task's work on an attempt
// of paper. Nothing is called.
func TestATaskFromPaperRefusesWhatTheContractRefuses(t *testing.T) {
	t.Parallel()

	w, _ := newStubWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	for name, req := range map[string]*v1.SubmitWorkRequest{
		"no work":         {},
		"a sheet":         {Work: sheet(pngOf(t, 90, 30))},
		"no statement":    {Work: &v1.Work{Surface: &v1.Work_Paper{Paper: &v1.PaperWork{Crop: pngOf(t, 90, 30)}}}},
		"no crop":         {Work: &v1.Work{Surface: &v1.Work_Paper{Paper: &v1.PaperWork{Statement: photoOf(t, 40, 30)}}}},
		"a JPEG for crop": {Work: &v1.Work{Surface: &v1.Work_Paper{Paper: &v1.PaperWork{Statement: photoOf(t, 40, 30), Crop: photoOf(t, 40, 30)}}}},
		"taps":            {Work: paperWork(t, formulaStatement, pngOf(t, 90, 30)), TappedChoices: []*v1.TappedChoice{tap(1, 1, "85")}},
	} {
		req.ProfileId, req.AttemptId, req.IdempotencyKey = l.adult, a, uuid.NewString()
		if _, err := w.attempts.SubmitWork(l.ctx, req); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: %v, want invalid_argument", name, err)
		}
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != 0 || profile.Reserved != 0 {
		t.Errorf("the counter = %+v, want nothing spent", profile)
	}
	m := w.begin(t, l, l.adult, "apples").GetAttemptId()
	_, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: m, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 90, 30))})
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("the work of paper on a task of a module: %v, want invalid_argument", err)
	}
}

// One reader reads the work in the reading mode single, and the own key that a
// provider refuses gives the outcome and keeps no key.
func TestATaskFromPaperByOneReaderAndAnOwnKey(t *testing.T) {
	t.Parallel()

	g := stubGateOf(t, adultFacts, "API_READING_MODE=single")
	w := worldOf(t, g, []learning.Option{learning.WithTutor(tutorstub.New(g))})
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 90, 30)), OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "sk-own" + tutorstub.KeyRefused}})
	if err != nil {
		t.Fatal(err)
	}
	if s := slotOf(t, res); s.GetVerdict() != "unchecked" || res.GetOutcome().GetCause() != v1.Cause_CAUSE_KEY_REFUSED || w.paperKey(t, a) != nil {
		t.Errorf("a refused own key = %v, the key %v; want unchecked with CAUSE_KEY_REFUSED, and no key", res, w.paperKey(t, a))
	}
	res, err = w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 90, 30))})
	if err != nil {
		t.Fatal(err)
	}
	if s := slotOf(t, res); s.GetVerdict() != "correct" || s.GetValue() != "85" {
		t.Errorf("one reader = %v, want correct", res)
	}
}

// The gate's tutor on the stub's adapter calls no provider: each call fails as
// AI does. The slot of a Check is unchecked with no key kept, and by a key kept
// too; the finding gives no frame, and the opening question is the fallback
// text, each with the failure of AI.
func TestATaskFromPaperOnTheGatesTutor(t *testing.T) {
	t.Parallel()

	w := worldOf(t, stubGate(t), nil)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, wordStatement, pngOf(t, 90, 30))})
	if err != nil {
		t.Fatal(err)
	}
	if s := slotOf(t, res); s.GetVerdict() != "unchecked" || res.GetOutcome().GetCause() != v1.Cause_CAUSE_AI_FAILURE || w.paperKey(t, a) != nil {
		t.Errorf("the gate's tutor = %v, the key %v; want unchecked with the failure of AI, and no key", res, w.paperKey(t, a))
	}
	// A key kept, the work alone is read, and fails as AI does.
	key, err := proto.Marshal(&paperv1.Key{Tasks: []*paperv1.TaskKey{{Statement: "37 + 48 =",
		Answer: &paperv1.Answer{Kind: &paperv1.Answer_Number{Number: &paperv1.Number{Value: "85"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	w.exec(t, `UPDATE attempts SET paper_key = $2 WHERE id = $1`, a, key)
	res, err = w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 90, 30))})
	if err != nil {
		t.Fatal(err)
	}
	if s := slotOf(t, res); s.GetVerdict() != "unchecked" || res.GetOutcome().GetCause() != v1.Cause_CAUSE_AI_FAILURE || res.GetWrongAnswers() != 0 {
		t.Errorf("the reading by the key kept = %v, want unchecked with the failure of AI", res)
	}
	found, err := w.findTasks(l, l.adult, photoOf(t, 300, 400), uuid.NewString())
	if err != nil || len(found.GetFrames()) != 0 || found.GetOutcome().GetCause() != v1.Cause_CAUSE_AI_FAILURE {
		t.Errorf("the finding = %v (%v), want the failure of AI", found, err)
	}
	opening, err := w.opening(t, l, l.adult, a, uuid.NewString())
	if err != nil || opening.GetWords() != tutor.PaperFallback(0, "en") || opening.GetOutcome().GetCause() != v1.Cause_CAUSE_AI_FAILURE {
		t.Errorf("the opening question = %v (%v), want the fallback text with the failure of AI", opening, err)
	}
}

// scripted is the stub of tutor whose calls of a task from paper a test answers
// where it sets them, the stub the others; during runs as the work is read,
// between the two transactions of a Check.
type scripted struct {
	*tutorstub.Stub
	mu        sync.Mutex
	statement *tutor.StatementRead
	solved    *tutor.TasksSolved
	read      *tutor.SheetRead
	during    func()
}

func (s *scripted) ReadStatement(ctx context.Context, p tutor.PaperTask, own string) tutor.StatementRead {
	s.mu.Lock()
	given := s.statement
	s.mu.Unlock()
	if given != nil {
		return *given
	}
	return s.Stub.ReadStatement(ctx, p, own)
}

func (s *scripted) SolveTask(ctx context.Context, p tutor.PaperTask, tasks []tutor.TaskToSolve, own string) tutor.TasksSolved {
	s.mu.Lock()
	given := s.solved
	s.mu.Unlock()
	if given != nil {
		return *given
	}
	return s.Stub.SolveTask(ctx, p, tasks, own)
}

func (s *scripted) ReadPaper(ctx context.Context, r tutor.PaperReading, own string) tutor.SheetRead {
	s.mu.Lock()
	given, during := s.read, s.during
	s.mu.Unlock()
	if during != nil {
		during()
	}
	if given != nil {
		return *given
	}
	return s.Stub.ReadPaper(ctx, r, own)
}

// itemVerdicts are the verdicts of the items of a Check, in order.
func itemVerdicts(res *v1.SubmitWorkResponse) []string {
	var out []string
	for _, item := range res.GetItems() {
		out = append(out, item.GetVerdict())
	}
	return out
}

// What the statement, the solution and the reading give a Check as AI gave
// them (R162 spec): a crop on which the statement found no task has a key of
// none, CAUSE_NO_KEY, with no reading; a solution that fails keeps no key; a
// task the solution gives no answer is unchecked beside one the solver keyed,
// which alone decides the attempt; and a task the reader read nothing of is
// unchecked with the failure of AI beside one it read, whose value with no
// box comes with none.
func TestAChecksKeyAndReadingAreWhatAIGave(t *testing.T) {
	t.Parallel()

	answered := tutor.Outcome{Kind: tutor.Answered}
	two := []tutor.StatementTask{{Statement: "37 + 48 =", Formula: "37+48"}, {Statement: "40 + 45 =", Formula: "40+45"}}
	// The key is the number of its tasks, -1 for none kept.
	for _, tc := range []struct {
		name      string
		read      *tutor.StatementRead
		solved    *tutor.TasksSolved
		work      *tutor.SheetRead
		statement [2]int
		verdicts  string
		cause     v1.Cause
		key       int
		closed    string
	}{
		{"no task", &tutor.StatementRead{Outcome: answered}, nil, nil, formulaStatement, "unchecked", v1.Cause_CAUSE_NO_KEY, 0, ""},
		{"a solution that fails", nil, &tutor.TasksSolved{Outcome: tutor.Outcome{Kind: tutor.Failed}}, nil, wordStatement,
			"unchecked", v1.Cause_CAUSE_AI_FAILURE, -1, ""},
		{"a task with no answer", &tutor.StatementRead{Outcome: answered, Tasks: []tutor.StatementTask{two[0], {Statement: "Draw a cat."}}},
			&tutor.TasksSolved{Outcome: answered}, nil, formulaStatement, "correct unchecked", v1.Cause_CAUSE_UNSPECIFIED, -1, "correct"},
		{"a task read of nothing", &tutor.StatementRead{Outcome: answered, Tasks: two}, nil,
			&tutor.SheetRead{Outcome: answered, Observations: []*observationv1.Observation{{AnswerSlots: []*observationv1.AnswerSlotObservation{{
				AnswerSlot: &observationv1.AnswerSlotPlace{Item: 1, Slot: 1}, Read: &observationv1.AnswerSlotObservation_Value{Value: "85"}}}}}},
			formulaStatement, "correct unchecked", v1.Cause_CAUSE_AI_FAILURE, 2, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			g := stubGate(t)
			script := &scripted{Stub: tutorstub.New(g), statement: tc.read, solved: tc.solved, read: tc.work}
			w := worldOf(t, g, []learning.Option{learning.WithTutor(script)})
			l := w.learner(t)
			a := w.beginPage(t, l, l.adult, 1)[0]
			res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
				Work: paperWork(t, tc.statement, pngOf(t, 90, 30))})
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(itemVerdicts(res), " "); got != tc.verdicts || res.GetOutcome().GetCause() != tc.cause {
				t.Errorf("the Check = %v, want %s with %v", res, tc.verdicts, tc.cause)
			}
			if s := res.GetItems()[0].GetAnswerSlots()[0]; tc.work != nil && (s.GetValue() != "85" || s.GetCropBox() != nil) {
				t.Errorf("a value read with no box = %v, want it shown with none", s)
			}
			k := w.paperKey(t, a)
			if tc.key < 0 && k != nil || tc.key >= 0 && (k == nil || len(k.GetTasks()) != tc.key) {
				t.Errorf("the key kept = %v, want %d tasks", k, tc.key)
			}
			if w.closeOf(t, a) != tc.closed {
				t.Errorf("the attempt closed %q, want %q", w.closeOf(t, a), tc.closed)
			}
			if profile, _ := w.counted(t, l.accountID, l.adult); profile.Reserved != 0 {
				t.Errorf("the counter = %+v, want nothing left in reserve", profile)
			}
		})
	}
}

// An attempt of paper that another device closed while AI read the work is
// "closed": nothing is written, and the key records nothing.
func TestAnAttemptOfPaperClosedWhileAIReadsIsClosed(t *testing.T) {
	t.Parallel()

	g := stubGate(t)
	script := &scripted{Stub: tutorstub.New(g)}
	w := worldOf(t, g, []learning.Option{learning.WithTutor(script)})
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	script.mu.Lock()
	script.during = func() {
		if _, err := w.pool.Exec(context.Background(), `UPDATE attempts SET close_reason = 'left', closed_at = now() WHERE id = $1`, a); err != nil {
			t.Errorf("the close meanwhile: %v", err)
		}
	}
	script.mu.Unlock()
	req := &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(), Work: paperWork(t, formulaStatement, pngOf(t, 90, 30))}
	_, err := w.attempts.SubmitWork(l.ctx, req)
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED); r.GetCloseReason() != v1.CloseReason_CLOSE_REASON_LEFT {
		t.Errorf("the refusal = %v, want closed left", r)
	}
	var submissions, records int
	if err := w.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM submissions WHERE attempt_id = $1),
			(SELECT count(*) FROM idempotency_keys WHERE key = $2)`, a, req.GetIdempotencyKey()).Scan(&submissions, &records); err != nil {
		t.Fatal(err)
	}
	if submissions != 0 || records != 0 {
		t.Errorf("a Check of a closed attempt wrote %d submissions and %d records, want none", submissions, records)
	}
}

// A key the attempt keeps that this build cannot read fails the requests that
// read it, a Check, the opening question and the words of a rung, as a failure
// of the service rather than a judgement or words by another key; nothing is
// called.
func TestAKeptKeyThatCannotBeReadFailsTheRequest(t *testing.T) {
	t.Parallel()

	w, _ := newStubWorld(t)
	l := w.learner(t)
	page := w.beginPage(t, l, l.adult, 2)
	if _, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: page[0], IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 40, 30))}); err != nil {
		t.Fatal(err)
	}
	w.exec(t, `UPDATE attempts SET paper_key = '\xff' WHERE id = ANY($1)`, page)
	calls := strings.Count(w.log.String(), `"model":"stub"`)
	_, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: page[0], IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 90, 30))})
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Errorf("a Check = %v, want unavailable", err)
	}
	if _, err := w.paperRungWords(t, l, l.adult, page[0], 1); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Errorf("the words of rung 1 = %v, want unavailable", err)
	}
	if _, err := w.opening(t, l, l.adult, page[1], uuid.NewString()); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Errorf("the opening question = %v, want unavailable", err)
	}
	if n := strings.Count(w.log.String(), `"model":"stub"`); n != calls {
		t.Errorf("the log has %d calls of the stub, %d before; want none more", n, calls)
	}
}

// The requests of a task from paper that call AI refuse a deadline that leaves
// too little time for the call and the transaction after it, before anything
// is called or reserved: the finding, a Check, the opening question and the
// words of a rung.
func TestTheRequestsOfPaperWithTooLittleTimeAreRefused(t *testing.T) {
	t.Parallel()

	w, _ := newStubWorld(t)
	l := w.learner(t)
	page := w.beginPage(t, l, l.adult, 2)
	if _, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: page[0], IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 40, 30))}); err != nil {
		t.Fatal(err)
	}
	before, _ := w.counted(t, l.accountID, l.adult)
	short := func() context.Context {
		ctx, cancel := context.WithTimeout(l.ctx, 5*time.Second)
		t.Cleanup(cancel)
		return ctx
	}
	wrong := int32(1)
	for name, call := range map[string]func() error{
		"FindTasks": func() error {
			_, err := w.paper.FindTasks(short(), &v1.FindTasksRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString(), Photo: photoOf(t, 300, 400)})
			return err
		},
		"SubmitWork": func() error {
			_, err := w.attempts.SubmitWork(short(), &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: page[0], IdempotencyKey: uuid.NewString(),
				Work: paperWork(t, formulaStatement, pngOf(t, 90, 30))})
			return err
		},
		"GetOpeningWords": func() error {
			_, err := w.attempts.GetOpeningWords(short(), &v1.GetOpeningWordsRequest{ProfileId: l.adult, AttemptId: page[1], IdempotencyKey: uuid.NewString(),
				Statement: photoOf(t, formulaStatement[0], formulaStatement[1])})
			return err
		},
		"GetRungWords": func() error {
			_, err := w.attempts.GetRungWords(short(), &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: page[0], IdempotencyKey: uuid.NewString(),
				WrongAnswers: &wrong, Work: paperWork(t, formulaStatement, pngOf(t, 40, 30))})
			return err
		},
	} {
		if err := call(); connect.CodeOf(err) != connect.CodeDeadlineExceeded {
			t.Errorf("%s with 5 s left = %v, want deadline_exceeded", name, err)
		}
	}
	if after, _ := w.counted(t, l.accountID, l.adult); after != before {
		t.Errorf("the counter = %+v, %+v before; want nothing spent or reserved", after, before)
	}
}

// The record of a Check of paper going is of a request that fell only past the
// time limit of paper (R98): under a limit of five minutes, one two minutes old
// is still going, and one six minutes old goes again.
func TestACheckOfPaperGoesForTheTimeLimitOfPaper(t *testing.T) {
	t.Parallel()

	g := stubGate(t)
	w := worldOf(t, g, []learning.Option{learning.WithTutor(tutorstub.New(g)), learning.WithPaperRequestTimeout(5 * time.Minute)})
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	fell := func(ago string) *v1.SubmitWorkRequest {
		req := &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(), Work: paperWork(t, formulaStatement, pngOf(t, 40, 30))}
		w.exec(t, `INSERT INTO idempotency_keys (account_id, key, method, fingerprint, state, created_at)
			VALUES ($1, $2, '/novatrail.api.v1.AttemptService/SubmitWork', $3, 'going', now() - $4::interval)`,
			l.accountID, req.GetIdempotencyKey(), idem.Fingerprint(req, false), ago)
		return req
	}
	_, err := w.attempts.SubmitWork(l.ctx, fell("2 minutes"))
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_STILL_GOING)
	if res, err := w.attempts.SubmitWork(l.ctx, fell("6 minutes")); err != nil || slotOf(t, res).GetVerdict() != "not_yet" {
		t.Errorf("the repeat of a request that fell = %v, %v; want it checked again", res, err)
	}
}

// An attempt of paper past 7 days with no action is closed "abandoned" by the
// Check, which reads nothing.
func TestATaskFromPaperLapses(t *testing.T) {
	t.Parallel()

	w, _ := newStubWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	if _, err := w.pool.Exec(t.Context(), `UPDATE attempts SET last_action_at = last_action_at - interval '8 days' WHERE id = $1`, a); err != nil {
		t.Fatal(err)
	}
	_, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 90, 30))})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED)
	if w.closeOf(t, a) != "abandoned" {
		t.Errorf("the attempt closed %q, want abandoned", w.closeOf(t, a))
	}
}
