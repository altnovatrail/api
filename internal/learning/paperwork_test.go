package learning_test

import (
	"context"
	"slices"
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
// and the crop of the work, whose shape says how the stub checks it.
func paperWork(t *testing.T, statement [2]int, crop []byte) *v1.Work {
	return &v1.Work{Surface: &v1.Work_Paper{Paper: &v1.PaperWork{Statement: photoOf(t, statement[0], statement[1]), Crop: crop}}}
}

var (
	formulaStatement  = [2]int{400, 300}
	wordStatement     = [2]int{300, 400}
	severalStatement  = [2]int{150, 400}
	rightWork         = [2]int{90, 30}
	wrongWork         = [2]int{40, 30}
	soFarWork         = [2]int{30, 30}
	illegibleWork     = [2]int{30, 40}
	nothingWrittenOn  = [2]int{30, 90}
	stubWordsEnglish  = "Let us look at your work together."
	stubFirstQuestion = "Which step of your work would you check first?"
)

// submitPaper submits the work of crop's shape on the attempt under a new key,
// on a statement of one formula.
func (w *world) submitPaper(t *testing.T, l learner, attempt string, crop [2]int) *v1.SubmitWorkResponse {
	t.Helper()
	return w.submitPaperOn(t, l, attempt, formulaStatement, crop)
}

// submitPaperOn submits the work of crop's shape on the statement of its shape.
func (w *world) submitPaperOn(t *testing.T, l learner, attempt string, statement, crop [2]int) *v1.SubmitWorkResponse {
	t.Helper()
	res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: attempt, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, statement, pngOf(t, crop[0], crop[1]))})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// reserveOf is what a call of a task from paper reserves on platform AI for an
// adult: the expected price of every attempt of its cascade, by tutor through
// the gate.
func reserveOf(g *admission.Gate, c tutor.Call) quota.Micro {
	return quota.Reserve(c, g.Price(c, tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategoryAdult}), 1)
}

// paperCheck is the Check the attempt keeps, or nil when it keeps none.
func (w *world) paperCheck(t *testing.T, attempt string) *paperv1.Check {
	t.Helper()
	var kept []byte
	if err := w.pool.QueryRow(t.Context(), `SELECT paper_check FROM attempts WHERE id = $1`, attempt).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept == nil {
		return nil
	}
	c := &paperv1.Check{}
	if err := proto.Unmarshal(kept, c); err != nil {
		t.Fatal(err)
	}
	return c
}

// paperHistory is how many Checks the attempt keeps before its last.
func (w *world) paperHistory(t *testing.T, attempt string) int {
	t.Helper()
	var n int
	if err := w.pool.QueryRow(t.Context(), `SELECT coalesce(cardinality(paper_history), 0) FROM attempts WHERE id = $1`, attempt).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// slotOf is the verdict of the one answer slot of a task from paper.
func slotOf(t *testing.T, res *v1.SubmitWorkResponse) *v1.AnswerSlotVerdict {
	t.Helper()
	if len(res.GetItems()) != 1 || len(res.GetItems()[0].GetAnswerSlots()) != 1 {
		t.Fatalf("the verdict = %v, want one item of one slot", res)
	}
	return res.GetItems()[0].GetAnswerSlots()[0]
}

// scripted is the stub of tutor whose calls of a task from paper a test answers
// where it sets them, the stub the others, and which keeps every Check it is
// asked; during runs as the work is checked, between the two transactions of a
// Check.
type scripted struct {
	*tutorstub.Stub
	mu        sync.Mutex
	statement *tutor.StatementRead
	checked   *tutor.PaperChecked
	during    func()
	asked     []tutor.PaperCheck
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

func (s *scripted) CheckPaper(ctx context.Context, c tutor.PaperCheck, own string) tutor.PaperChecked {
	s.mu.Lock()
	given, during := s.checked, s.during
	s.asked = append(s.asked, c)
	s.mu.Unlock()
	if during != nil {
		during()
	}
	if given != nil {
		return *given
	}
	return s.Stub.CheckPaper(ctx, c, own)
}

// checks are the Checks the stub was asked, in order.
func (s *scripted) checks() []tutor.PaperCheck {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.asked)
}

// scriptedWorld is a world on the scripted stub.
func scriptedWorld(t *testing.T) (*world, *scripted, *admission.Gate) {
	t.Helper()
	g := stubGate(t)
	s := &scripted{Stub: tutorstub.New(g)}
	return worldOf(t, g, []learning.Option{learning.WithTutor(s)}), s, g
}

// A submission of a task from paper is one Check, told no task count before
// the first and the last one's after it, the language of the interface, the learner's age category and the instruction.
// A task not yet is a wrong answer with the value read at its box and the
// tutor's words, and the attempt keeps the Check; a Check that came to an
// outcome keeps nothing and counts nothing. The next Check is given the one
// before as its history, with the rungs the learner was shown; the right value
// closes the attempt "correct" with an empty solved and no next, the Check,
// its history and the submissions erased with the close. A repeat under the
// key gives the task solved alone, with no call.
func TestATaskFromPaperIsOneCheck(t *testing.T) {
	t.Parallel()

	w, s, g := scriptedWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	instruction := photoOf(t, 400, 100)
	first := &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: &v1.Work{Surface: &v1.Work_Paper{Paper: &v1.PaperWork{Statement: photoOf(t, 400, 300), Crop: pngOf(t, wrongWork[0], wrongWork[1]),
			Instruction: instruction}}}}
	wrong, err := w.attempts.SubmitWork(l.ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	band := &observationv1.CropBox{Ymax: 1000, Xmax: 1000}
	if s := slotOf(t, wrong); s.GetVerdict() != "not_yet" || s.GetValue() != "851" || !proto.Equal(s.GetCropBox(), band) ||
		wrong.GetWrongAnswers() != 1 || wrong.GetSolved() != nil || wrong.GetTutorWords() != stubWordsEnglish {
		t.Errorf("a wrong value = %v, want not_yet, 851 read at its box, one wrong answer and the tutor's words", wrong)
	}
	if again, err := w.attempts.SubmitWork(l.ctx, first); err != nil || !proto.Equal(again, wrong) {
		t.Errorf("the repeat of the wrong Check = %v (%v), want its answer, which its submission keeps", again, err)
	}
	asked := s.checks()
	if len(asked) != 1 || asked[0].Tasks != 0 || asked[0].Language != "en" || asked[0].AgeCategory != "adult" ||
		len(asked[0].Instruction) == 0 || len(asked[0].History) != 0 {
		t.Fatalf("the Checks asked = %+v, want one of no task count, in English, for an adult, with the instruction and no history", asked)
	}
	if c := w.paperCheck(t, a); len(c.GetSlots()) != 1 || c.GetSlots()[0].GetVerdict() != "incorrect" || c.GetSlots()[0].GetAnswer() != "85" ||
		len(c.GetHints()) != 3 || w.paperHistory(t, a) != 0 {
		t.Errorf("the Check kept = %v, want the incorrect task with its hidden answer and the ladder, and no history", c)
	}
	checking := reserveOf(g, tutor.CallChecking)
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != checking || profile.Reserved != 0 {
		t.Errorf("the counter = %+v, want one Check spent, %d", profile, checking)
	}

	refused, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, rightWork[0], rightWork[1])), OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "sk-own" + tutorstub.KeyRefused}})
	if err != nil {
		t.Fatal(err)
	}
	if s := slotOf(t, refused); s.GetVerdict() != "unchecked" || refused.GetOutcome().GetCause() != v1.Cause_CAUSE_KEY_REFUSED ||
		refused.GetWrongAnswers() != 1 || refused.GetTutorWords() != "" {
		t.Errorf("a refused own key = %v, want unchecked with CAUSE_KEY_REFUSED and one wrong answer still", refused)
	}
	if c := w.paperCheck(t, a); c.GetSlots()[0].GetVerdict() != "incorrect" || w.paperHistory(t, a) != 0 {
		t.Errorf("the Check kept after the refusal = %v, want the one before", c)
	}
	rung, err := w.paperRungWords(t, l, l.adult, a, 1)
	if err != nil || rung.GetWords() != stubFirstQuestion {
		t.Fatalf("the words of rung 1 = %v, %v; want the ladder's first rung", rung, err)
	}

	key := uuid.NewString()
	req := &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: key, Work: paperWork(t, formulaStatement, pngOf(t, rightWork[0], rightWork[1]))}
	right, err := w.attempts.SubmitWork(l.ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if s := slotOf(t, right); s.GetVerdict() != "correct" || s.GetValue() != "85" || right.GetSolved() == nil || right.GetNext() != nil ||
		right.GetWrongAnswers() != 1 || right.GetTutorWords() != stubWordsEnglish {
		t.Errorf("the right value = %v, want correct, solved, the tutor's words and no next", right)
	}
	asked = s.checks()
	if n := asked[len(asked)-1].Tasks; n != 1 {
		t.Errorf("the last Check was told %d tasks, want the one the first gave", n)
	}
	if h := asked[len(asked)-1].History; len(h) != 1 || !slices.Equal(h[0].Verdicts, []string{"incorrect"}) || h[0].Words != stubWordsEnglish ||
		!slices.Equal(h[0].Rungs, []string{stubFirstQuestion}) {
		t.Errorf("the history of the last Check = %+v, want the first, its words and the rung shown", h)
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != 2*checking {
		t.Errorf("the counter = %+v, want a second Check spent", profile)
	}
	var kept int
	if err := w.pool.QueryRow(l.ctx, `SELECT count(*) FROM submissions WHERE attempt_id = $1`, a).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if w.closeOf(t, a) != "correct" || w.paperCheck(t, a) != nil || w.paperHistory(t, a) != 0 || kept != 0 {
		t.Errorf("the attempt closed %q, its Check %v, %d submissions kept; want correct, and none", w.closeOf(t, a), w.paperCheck(t, a), kept)
	}
	again, err := w.attempts.SubmitWork(l.ctx, req)
	if err != nil || !proto.Equal(again, &v1.SubmitWorkResponse{Solved: right.GetSolved()}) {
		t.Errorf("the repeat = %v (%v), want the task solved alone", again, err)
	}
	if n := len(s.checks()); n != len(asked) {
		t.Errorf("%d Checks after the repeat, %d before; want none more", n, len(asked))
	}
	_, err = w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, rightWork[0], rightWork[1]))})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED)
}

// Each Check replaces the one before, which goes to the history: a solution so
// far correct counts no wrong answer and closes nothing; an unreadable work is
// illegible; and a work with nothing written on it is CAUSE_NO_KEY, its slot
// unchecked, its ladder kept.
func TestEachCheckOfPaperReplacesTheOneBefore(t *testing.T) {
	t.Parallel()

	w, s, _ := scriptedWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	for i, tc := range []struct {
		crop    [2]int
		verdict string
		reason  string
		cause   v1.Cause
		words   string
	}{
		{soFarWork, "so_far_correct", "", 0, stubWordsEnglish},
		{illegibleWork, "unreadable", "illegible", 0, stubWordsEnglish},
		{nothingWrittenOn, "unchecked", "", v1.Cause_CAUSE_NO_KEY, ""},
	} {
		res := w.submitPaper(t, l, a, tc.crop)
		if s := slotOf(t, res); s.GetVerdict() != tc.verdict || s.GetReason() != tc.reason || s.GetValue() != "" ||
			res.GetOutcome().GetCause() != tc.cause || res.GetTutorWords() != tc.words || res.GetWrongAnswers() != 0 || res.GetSolved() != nil {
			t.Errorf("Check %d = %v, want %s %s with %v and words %q, no wrong answer", i+1, res, tc.verdict, tc.reason, tc.cause, tc.words)
		}
		if n := w.paperHistory(t, a); n != i || w.paperCheck(t, a) == nil {
			t.Errorf("after Check %d the history keeps %d, want %d", i+1, n, i)
		}
	}
	if h := s.checks()[2].History; len(h) != 2 || h[0].Verdicts[0] != "no_answer" || h[1].Verdicts[0] != "unreadable" || len(h[0].Rungs) != 0 {
		t.Errorf("the history of the third Check = %+v, want the two before, no rung shown", h)
	}
	if w.closeOf(t, a) != "" {
		t.Errorf("the attempt closed %q, want it open", w.closeOf(t, a))
	}
}

// The first Check of a frame is told no task count, for the model to find
// the tasks itself, whatever "Recognise" read, and each Check after it the
// count of the last: each task's item gets its verdict, its value and the box
// where it was read, and one wrong task makes the Check one wrong answer.
func TestATaskFromPaperOfSeveralTasksIsCheckedTaskByTask(t *testing.T) {
	t.Parallel()

	w, s, _ := scriptedWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	if res, err := w.recognise(t, l, a, formulaStatement, uuid.NewString()); err != nil || len(res.GetStatements()) != 1 {
		t.Fatalf("Recognise = %v, %v; want one task", res, err)
	}
	for i := range 2 {
		res := w.submitPaperOn(t, l, a, severalStatement, wrongWork)
		items := res.GetItems()
		if len(items) != 3 || res.GetOutcome() != nil || res.GetSolved() != nil || res.GetWrongAnswers() != int32(i+1) {
			t.Fatalf("Check %d = %v, want three items, one wrong", i+1, res)
		}
		for n, want := range []string{"not_yet", "correct", "correct"} {
			s := items[n].GetAnswerSlots()[0]
			if items[n].GetVerdict() != want || s.GetAnswerSlot().GetItem() != int32(n+1) || s.GetCropBox().GetYmin() != int32(n*1000/3) {
				t.Errorf("Check %d, task %d = %v, want %s in its band", i+1, n+1, items[n], want)
			}
		}
	}
	if told := tasksTold(s.checks()); !slices.Equal(told, []int{0, 3}) {
		t.Errorf("the Checks were told %v tasks, want none, then the three the first found", told)
	}
}

// tasksTold is how many tasks each Check asked was told of, in order.
func tasksTold(asked []tutor.PaperCheck) []int {
	var told []int
	for _, c := range asked {
		told = append(told, c.Tasks)
	}
	return told
}

// checkOfTasks is a Check of the scripted stub that gives the verdicts, a task
// each, the incorrect read "1" with an error and the correct "85".
func checkOfTasks(verdicts ...string) *tutor.PaperChecked {
	c := &tutor.PaperChecked{Outcome: tutor.Outcome{Kind: tutor.Answered}, TutorWords: stubWordsEnglish,
		Hints: []string{"Which line would you check?", "What did you carry?"}}
	for i, v := range verdicts {
		slot := tutor.SlotChecked{Task: i + 1, Verdict: v, Read: "85", Solution: "37 + 48 = 85", Answer: "85", Confidence: "high",
			ReadBox: &observationv1.CropBox{Ymin: int32(i * 100), Ymax: int32(i*100 + 90), Xmax: 1000}}
		if v == "incorrect" {
			slot.Read = "1"
			slot.Errors = []tutor.CheckError{{Box: slot.ReadBox, Where: "the answer", Kind: "arithmetic"}}
		}
		c.Slots = append(c.Slots, slot)
	}
	return c
}

// The items of an attempt of a task from paper are the tasks of its latest
// Check, however many the Checks before gave: a frame read as one task grows to
// three, then shrinks to two, each Check told the count of the last, the
// history keeping each turn as its own Check gave it, and the hints standing on
// the latest Check's tasks.
func TestTheTasksOfAFrameAreThoseOfTheLatestCheck(t *testing.T) {
	t.Parallel()

	w, s, _ := scriptedWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	if res := w.submitPaper(t, l, a, wrongWork); len(res.GetItems()) != 1 {
		t.Fatalf("the first Check = %v, want the one task the model found", res)
	}
	s.checked = checkOfTasks("correct", "incorrect", "correct")
	grown := w.submitPaper(t, l, a, wrongWork)
	if got := verdictsOf(grown); got != "correct not_yet correct" || grown.GetWrongAnswers() != 2 {
		t.Errorf("the Check of three tasks = %v, want its three items, the second not yet", grown)
	}
	if c := w.paperCheck(t, a); len(c.GetSlots()) != 3 || w.paperHistory(t, a) != 1 {
		t.Errorf("the Check kept = %v, want its three tasks and one before it", c)
	}
	if h := w.hint(t, l, l.adult, a); h.GetRung().GetWords() != "What did you carry?" {
		t.Errorf("the hint after two wrong answers = %v, want the rung of the latest ladder", h)
	}
	s.checked = checkOfTasks("correct", "no_answer")
	shrunk := w.submitPaper(t, l, a, wrongWork)
	if got := verdictsOf(shrunk); got != "correct so_far_correct" || shrunk.GetWrongAnswers() != 2 || shrunk.GetSolved() != nil {
		t.Errorf("the Check of two tasks = %v, want its two items, the second so far correct", shrunk)
	}
	s.checked = checkOfTasks("correct", "correct")
	solved := w.submitPaper(t, l, a, rightWork)
	if got := verdictsOf(solved); got != "correct correct" || solved.GetSolved() == nil {
		t.Errorf("the Check of two correct = %v, want the attempt solved", solved)
	}
	asked := s.checks()
	if told := tasksTold(asked); !slices.Equal(told, []int{0, 1, 3, 2}) {
		t.Errorf("the Checks were told %v tasks, want none, then each the count of the one before", told)
	}
	var turns []int
	for _, turn := range asked[3].History {
		turns = append(turns, len(turn.Verdicts))
	}
	if !slices.Equal(turns, []int{1, 3, 2}) {
		t.Errorf("the history of the last Check has turns of %v tasks, want 1, 3 and 2", turns)
	}
}

// verdictsOf are the verdicts of the items of a SubmitWork answer.
func verdictsOf(res *v1.SubmitWorkResponse) string {
	var out []string
	for _, item := range res.GetItems() {
		out = append(out, item.GetVerdict())
	}
	return strings.Join(out, " ")
}

// The quota that does not hold the Check leaves the slot unchecked with its
// outcome and keeps no Check, and nothing is called.
func TestATaskFromPaperOverTheQuota(t *testing.T) {
	t.Parallel()

	w, s, _ := scriptedWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	w.spend(t, l.accountID, 2_995_000)
	res := w.submitPaper(t, l, a, rightWork)
	if s := slotOf(t, res); s.GetVerdict() != "unchecked" || res.GetOutcome().GetCause() != v1.Cause_CAUSE_QUOTA {
		t.Errorf("over the quota = %v, want unchecked with CAUSE_QUOTA", res)
	}
	if c := w.paperCheck(t, a); c != nil || len(s.checks()) != 0 {
		t.Errorf("the Check kept = %v after %d calls, want none", c, len(s.checks()))
	}
}

// What a Check of a task from paper refuses: no work of paper, a statement, a
// crop or an instruction the contract does not take, and taps; and a task's
// work on an attempt of paper. Nothing is called.
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
		"an instruction that is no picture": {Work: &v1.Work{Surface: &v1.Work_Paper{Paper: &v1.PaperWork{Statement: photoOf(t, 40, 30),
			Crop: pngOf(t, 90, 30), Instruction: []byte("no picture")}}}},
		"taps": {Work: paperWork(t, formulaStatement, pngOf(t, 90, 30)), TappedChoices: []*v1.TappedChoice{tap(1, 1, "85")}},
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

// The gate's tutor on the stub's adapter calls no provider: each call fails as
// AI does. The slot of a Check is unchecked with no Check kept; the finding
// gives no frame, and the opening question is the fallback text, each with the
// failure of AI.
func TestATaskFromPaperOnTheGatesTutor(t *testing.T) {
	t.Parallel()

	w := worldOf(t, stubGate(t), nil)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	res := w.submitPaper(t, l, a, rightWork)
	if s := slotOf(t, res); s.GetVerdict() != "unchecked" || res.GetOutcome().GetCause() != v1.Cause_CAUSE_AI_FAILURE || w.paperCheck(t, a) != nil {
		t.Errorf("the gate's tutor = %v, the Check %v; want unchecked with the failure of AI, and no Check", res, w.paperCheck(t, a))
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

// An attempt of paper that another device closed while AI checked the work is
// "closed": nothing is written, and the key records nothing.
func TestAnAttemptOfPaperClosedWhileAIChecksIsClosed(t *testing.T) {
	t.Parallel()

	w, script, _ := scriptedWorld(t)
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
	if submissions != 0 || records != 0 || w.paperCheck(t, a) != nil {
		t.Errorf("a Check of a closed attempt wrote %d submissions and %d records, want none", submissions, records)
	}
}

// A Check, a history or a reading the attempt keeps that this build cannot
// read fails the requests that read it — a Check, "Hint", the words of a rung,
// "explain differently" and "Recognise" — as a failure of the service rather
// than a judgement or words by another Check; nothing is called.
func TestAKeptCheckThatCannotBeReadFailsTheRequest(t *testing.T) {
	t.Parallel()

	w, s, _ := scriptedWorld(t)
	l := w.learner(t)
	page := w.beginPage(t, l, l.adult, 3)
	w.submitPaper(t, l, page[0], wrongWork)
	w.submitPaper(t, l, page[1], wrongWork)
	w.exec(t, `UPDATE attempts SET paper_check = '\xff' WHERE id = $1`, page[0])
	w.exec(t, `UPDATE attempts SET paper_history = ARRAY['\xff'::bytea] WHERE id = $1`, page[1])
	w.exec(t, `UPDATE attempts SET paper_reading = '\xff' WHERE id = $1`, page[2])
	calls := len(s.checks())
	submit := func(a string) error {
		_, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
			Work: paperWork(t, formulaStatement, pngOf(t, 90, 30))})
		return err
	}
	for name, call := range map[string]func() error{
		"a Check":                func() error { return submit(page[0]) },
		"a Check by its history": func() error { return submit(page[1]) },
		"the words of rung 1":    func() error { _, err := w.paperRungWords(t, l, l.adult, page[0], 1); return err },
		"explain differently":    func() error { _, err := w.explain(l, l.adult, page[0], 1); return err },
		"Recognise by its reading": func() error {
			_, err := w.recognise(t, l, page[2], formulaStatement, uuid.NewString())
			return err
		},
		"Hint": func() error {
			_, err := w.attempts.GetHint(l.ctx, &v1.GetHintRequest{ProfileId: l.adult, AttemptId: page[0]})
			return err
		},
	} {
		if err := call(); connect.CodeOf(err) != connect.CodeUnavailable {
			t.Errorf("%s = %v, want unavailable", name, err)
		}
	}
	if n := len(s.checks()); n != calls {
		t.Errorf("%d Checks, %d before; want none more", n, calls)
	}
}

// The requests of a task from paper that call AI refuse a deadline that leaves
// too little time for the call and the transaction after it, before anything
// is called or reserved: the finding, a Check, the opening question and
// "Recognise". The words of a rung call none, and are given.
func TestTheRequestsOfPaperWithTooLittleTimeAreRefused(t *testing.T) {
	t.Parallel()

	w, _ := newStubWorld(t)
	l := w.learner(t)
	page := w.beginPage(t, l, l.adult, 2)
	w.submitPaper(t, l, page[0], wrongWork)
	before, _ := w.counted(t, l.accountID, l.adult)
	short := func() context.Context {
		ctx, cancel := context.WithTimeout(l.ctx, 5*time.Second)
		t.Cleanup(cancel)
		return ctx
	}
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
				Work: paperWork(t, formulaStatement, pngOf(t, 30, 30))})
			return err
		},
		"ReadStatement": func() error {
			_, err := w.attempts.ReadStatement(short(), &v1.ReadStatementRequest{ProfileId: l.adult, AttemptId: page[1], IdempotencyKey: uuid.NewString(),
				Statement: photoOf(t, formulaStatement[0], formulaStatement[1])})
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
	wrong := int32(1)
	if res, err := w.attempts.GetRungWords(short(), &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: page[0], IdempotencyKey: uuid.NewString(),
		WrongAnswers: &wrong}); err != nil || res.GetWords() == "" {
		t.Errorf("the words of rung 1 with 5 s left = %v, %v; want them given", res, err)
	}
}

// The record of a Check of paper going is of a request that fell only past the
// time limit of paper: under a limit of five minutes, one two minutes old is
// still going, and one six minutes old goes again.
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
	refusedAs(t, err, connect.CodeAborted, v1.Cause_CAUSE_STILL_GOING)
	if res, err := w.attempts.SubmitWork(l.ctx, fell("6 minutes")); err != nil || slotOf(t, res).GetVerdict() != "not_yet" {
		t.Errorf("the repeat of a request that fell = %v, %v; want it checked again", res, err)
	}
}

// An attempt of paper past 7 days with no action is closed "abandoned" by the
// Check, which calls nothing.
func TestATaskFromPaperLapses(t *testing.T) {
	t.Parallel()

	w, s, _ := scriptedWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	w.exec(t, `UPDATE attempts SET last_action_at = last_action_at - interval '8 days' WHERE id = $1`, a)
	_, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 90, 30))})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED)
	if w.closeOf(t, a) != "abandoned" || len(s.checks()) != 0 {
		t.Errorf("the attempt closed %q after %d Checks, want abandoned after none", w.closeOf(t, a), len(s.checks()))
	}
}
