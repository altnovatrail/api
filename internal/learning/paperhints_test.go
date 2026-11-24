package learning_test

import (
	"context"
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/tutor"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/tutorstub"
)

// opening asks for the opening question of the attempt under key, with a work
// the stub checks as a solution so far.
func (w *world) opening(t *testing.T, l learner, profile, attempt, key string) (*v1.GetOpeningWordsResponse, error) {
	return w.attempts.GetOpeningWords(l.ctx, &v1.GetOpeningWordsRequest{ProfileId: profile, AttemptId: attempt, IdempotencyKey: key,
		Work: paperWork(t, formulaStatement, pngOf(t, soFarWork[0], soFarWork[1]))})
}

// paperRungWords asks for the words of the rung of wrong on a task from paper.
func (w *world) paperRungWords(_ *testing.T, l learner, profile, attempt string, wrong int32) (*v1.GetRungWordsResponse, error) {
	return w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: profile, AttemptId: attempt, IdempotencyKey: uuid.NewString(),
		WrongAnswers: &wrong})
}

// The stub's ladder of hints in English, as its Check gives it.
const (
	stubSecondQuestion = "What does the task ask you to find?"
	stubThirdQuestion  = "What is given in the task, in your own words?"
)

// A task from paper has no techniques for "Hint" before its first Check, and
// the opening question instead: one Check of the work as it stands, which the
// attempt keeps with its ladder, showing no verdict and counting no wrong
// answer, whose first rung is the words. "Hint" then gives that rung, a repeat
// gets it, and a new request is not current. After each wrong answer the next
// rung of the ladder of the last Check comes with no call of AI, with the wavy
// line under the first mistake in the mode "Show where", and "Hint" gives it;
// "explain differently" alone calls AI for words.
func TestATaskFromPaperClimbsTheLadderOfItsCheck(t *testing.T) {
	t.Parallel()

	w, s, g := scriptedWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	if h := w.hint(t, l, l.adult, a); len(h.GetTechniques().GetTechniqueIds()) != 0 || h.GetLeadingQuestion() != "" || h.GetRung() != nil {
		t.Errorf("Hint before a Check = %v, want no techniques and no question", h)
	}
	key := uuid.NewString()
	first, err := w.opening(t, l, l.adult, a, key)
	if err != nil || first.GetWords() != stubFirstQuestion || first.GetOutcome() != nil || first.GetRepeated() {
		t.Fatalf("the opening question = %v, %v; want the ladder's first rung", first, err)
	}
	if again, err := w.opening(t, l, l.adult, a, key); err != nil || again.GetWords() != first.GetWords() || !again.GetRepeated() {
		t.Errorf("the repeat = %v, %v; want the words kept, repeated", again, err)
	}
	_, err = w.opening(t, l, l.adult, a, uuid.NewString())
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_CURRENT)
	if h := w.hint(t, l, l.adult, a); h.GetRung().GetWords() != stubFirstQuestion || h.GetRung().GetRung() != 1 {
		t.Errorf("Hint after the opening question = %v, want rung 1", h)
	}
	if c := w.paperCheck(t, a); len(c.GetHints()) != 3 || w.closeOf(t, a) != "" {
		t.Errorf("the Check kept = %v, want its ladder, the attempt open", c)
	}
	checking := reserveOf(g, tutor.CallChecking)
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != checking || profile.Reserved != 0 {
		t.Errorf("the counter = %+v, want one Check spent", profile)
	}

	if res := w.submitPaper(t, l, a, wrongWork); res.GetWrongAnswers() != 1 {
		t.Fatalf("the Check = %v, want one wrong answer", res)
	}
	if h := s.checks()[1].History; len(h) != 1 || h[0].Verdicts[0] != "no_answer" || len(h[0].Rungs) != 1 || h[0].Rungs[0] != stubFirstQuestion {
		t.Errorf("the history after the opening question = %+v, want its Check and the rung shown", h)
	}
	whole := &observationv1.CropBox{Ymax: 1000, Xmax: 1000}
	rung, err := w.paperRungWords(t, l, l.adult, a, 1)
	if m := rung.GetMarks(); err != nil || rung.GetWords() != stubFirstQuestion || rung.GetRung() != 1 || len(m) != 1 ||
		m[0].GetKind() != v1.MarkKind_MARK_KIND_WAVY_LINE || !proto.Equal(m[0].GetCropBox(), whole) {
		t.Errorf("the words of rung 1 = %v, %v; want the first rung with the wavy line under the mistake", rung, err)
	}
	if again, err := w.paperRungWords(t, l, l.adult, a, 1); err != nil || again.GetWords() != stubFirstQuestion || len(again.GetMarks()) != 0 || !again.GetRepeated() {
		t.Errorf("the words of rung 1 again = %v, %v; want them kept, with no mark", again, err)
	}
	if h := w.hint(t, l, l.adult, a); h.GetRung().GetWords() != stubFirstQuestion || h.GetRung().GetRung() != 1 {
		t.Errorf("Hint after a wrong answer = %v, want the rung's words", h)
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != 2*checking {
		t.Errorf("the counter = %+v, want the two Checks alone", profile)
	}
	one := int32(1)
	explain := &v1.ExplainDifferentlyRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(), WrongAnswers: &one}
	other, err := w.attempts.ExplainDifferently(l.ctx, explain)
	if err != nil || other.GetWords() == "" || other.GetWords() == stubFirstQuestion || other.GetOutcome() != nil {
		t.Errorf("explain differently = %v, %v; want other words", other, err)
	}
	if again, err := w.attempts.ExplainDifferently(l.ctx, explain); err != nil || again.GetWords() != other.GetWords() {
		t.Errorf("the repeat of explain differently = %v, %v; want the words kept", again, err)
	}
	if h := w.hint(t, l, l.adult, a); h.GetRung().GetWords() != other.GetWords() {
		t.Errorf("Hint after explain differently = %v, want the words put another way", h)
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != 2*checking+reserveOf(g, tutor.CallPaperHint) {
		t.Errorf("the counter = %+v, want explain differently spent", profile)
	}

	w.submitPaper(t, l, a, wrongWork)
	if rung, err := w.paperRungWords(t, l, l.adult, a, 2); err != nil || rung.GetWords() != stubSecondQuestion || rung.GetRung() != 2 {
		t.Errorf("the words of rung 2 = %v, %v; want the second rung of the new ladder", rung, err)
	}
	w.submitPaper(t, l, a, wrongWork)
	if rung, err := w.paperRungWords(t, l, l.adult, a, 3); err != nil || rung.GetWords() != stubThirdQuestion {
		t.Errorf("the words of rung 3 = %v, %v; want the third", rung, err)
	}
	w.submitPaper(t, l, a, wrongWork)
	_, err = w.paperRungWords(t, l, l.adult, a, 4)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_MORE_WORDS)
}

// A task from paper with no wrong answer has no rung, a task of a module no
// opening question, and the opening question takes the work of paper, a
// profile of the caller's and an own key with its value.
func TestTheOpeningQuestionRefusesWhatTheContractRefuses(t *testing.T) {
	t.Parallel()

	w, _ := newStubWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	_, err := w.paperRungWords(t, l, l.adult, a, 0)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_RUNG)
	for name, work := range map[string]*v1.Work{
		"no work":           nil,
		"a sheet":           sheet(pngOf(t, 30, 30)),
		"a statement alone": {Surface: &v1.Work_Paper{Paper: &v1.PaperWork{Statement: photoOf(t, 40, 30)}}},
	} {
		_, err = w.attempts.GetOpeningWords(l.ctx, &v1.GetOpeningWordsRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
			Work: work})
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("the opening question with %s: %v, want invalid_argument", name, err)
		}
	}
	m := w.begin(t, l, l.adult, "apples").GetAttemptId()
	if _, err := w.opening(t, l, l.adult, m, uuid.NewString()); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("the opening question on a task of a module: %v, want invalid_argument", err)
	}
	_, err = w.opening(t, l, uuid.NewString(), a, uuid.NewString())
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
	_, err = w.attempts.GetOpeningWords(l.ctx, &v1.GetOpeningWordsRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 30, 30)), OwnKey: &v1.OwnKey{Provider: "anthropic"}})
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("the opening question on an own key with no value: %v, want invalid_argument", err)
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != 0 || profile.Reserved != 0 {
		t.Errorf("the counter = %+v, want nothing spent", profile)
	}
}

// Where the deployment admits no Check, the opening question is the platform's
// fallback text in the language of the profile's interface, with the outcome
// "not admitted", no call and nothing spent; the attempt keeps nothing, and a
// repeat under the key gets the fallback text again. A repeat on an attempt
// that lapsed is refused "closed", the close left to the next request, which
// writes it; and once it is closed a repeat is refused too.
func TestTheOpeningQuestionNotAdmittedIsTheFallbackText(t *testing.T) {
	t.Parallel()

	// The Check is admitted while the page begins, and then no more.
	admitted := true
	w := worldOf(t, gate(t, adultFacts, config.RungFull), []learning.Option{learning.WithAdmits(func(tutor.Call) bool { return admitted })})
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	admitted = false
	w.exec(t, `UPDATE profiles SET interface_language = 'ru-RU' WHERE id = $1`, l.adult)
	key := uuid.NewString()
	for i, k := range []string{key, key, uuid.NewString()} {
		res, err := w.opening(t, l, l.adult, a, k)
		if err != nil || res.GetWords() != tutor.PaperFallback(0, "ru-RU") || res.GetRepeated() || res.GetOutcome().GetCause() != v1.Cause_CAUSE_NOT_ADMITTED {
			t.Errorf("request %d = %v, %v; want the fallback text in Russian, not admitted", i+1, res, err)
		}
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != 0 || profile.Reserved != 0 || w.paperCheck(t, a) != nil {
		t.Errorf("the counter = %+v, the Check %v; want nothing spent or kept", profile, w.paperCheck(t, a))
	}

	w.exec(t, `UPDATE attempts SET last_action_at = last_action_at - interval '8 days' WHERE id = $1`, a)
	_, err := w.opening(t, l, l.adult, a, key)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED)
	_, err = w.opening(t, l, l.adult, a, uuid.NewString())
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED)
	if w.closeOf(t, a) != "abandoned" {
		t.Errorf("the attempt closed %q, want abandoned", w.closeOf(t, a))
	}
	_, err = w.opening(t, l, l.adult, a, key)
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED); r.GetCloseReason() != v1.CloseReason_CLOSE_REASON_ABANDONED {
		t.Errorf("the repeat on the closed attempt = %v, want closed abandoned", r)
	}
}

// A Check of the opening question that gives no answer leaves the fallback
// text with its outcome and keeps nothing, so the next request checks again;
// one whose first rung is a fallback text gives the tutor's words in its
// place.
func TestTheOpeningQuestionOfACheckWithNoFirstRung(t *testing.T) {
	t.Parallel()

	w, s, _ := scriptedWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	s.mu.Lock()
	s.checked = &tutor.PaperChecked{Outcome: tutor.Outcome{Kind: tutor.Failed}}
	s.mu.Unlock()
	res, err := w.opening(t, l, l.adult, a, uuid.NewString())
	if err != nil || res.GetWords() != tutor.PaperFallback(0, "en") || res.GetOutcome().GetCause() != v1.Cause_CAUSE_AI_FAILURE || w.paperCheck(t, a) != nil {
		t.Errorf("a Check that failed = %v, %v; want the fallback text with the failure of AI, nothing kept", res, err)
	}
	s.mu.Lock()
	s.checked = &tutor.PaperChecked{Outcome: tutor.Outcome{Kind: tutor.Answered}, TutorWords: "Let us look at the first line.",
		Hints: []string{tutor.PaperFallback(1, "en"), "Which number comes next?"},
		Slots: []tutor.SlotChecked{{Task: 1, Verdict: "no_answer", Read: "37 +"}}}
	s.mu.Unlock()
	res, err = w.opening(t, l, l.adult, a, uuid.NewString())
	if err != nil || res.GetWords() != "Let us look at the first line." || res.GetOutcome() != nil || res.GetRepeated() {
		t.Errorf("a first rung of fallback = %v, %v; want the tutor's words", res, err)
	}
	if h := w.hint(t, l, l.adult, a); h.GetRung().GetWords() != "Let us look at the first line." {
		t.Errorf("Hint = %v, want the opening question kept as rung 1", h)
	}
}

// A Check another request kept while the opening question was checked is the
// attempt's: the request gets its words, repeated, and keeps none of its own.
// An attempt that closed meanwhile refuses it "closed", and keeps nothing.
func TestTheOpeningQuestionKeptMeanwhileIsTheAttempts(t *testing.T) {
	t.Parallel()

	w, s, _ := scriptedWorld(t)
	l := w.learner(t)
	page := w.beginPage(t, l, l.adult, 2)
	var meanwhile string
	s.mu.Lock()
	s.during = func() {
		s.mu.Lock()
		s.during = nil
		s.mu.Unlock()
		res, err := w.opening(t, l, l.adult, page[0], uuid.NewString())
		if err != nil {
			t.Errorf("the request meanwhile: %v", err)
		}
		meanwhile = res.GetWords()
	}
	s.mu.Unlock()
	res, err := w.opening(t, l, l.adult, page[0], uuid.NewString())
	if err != nil || meanwhile != stubFirstQuestion || res.GetWords() != meanwhile || !res.GetRepeated() || res.GetOutcome() != nil {
		t.Errorf("the opening question = %v, %v; want the words kept meanwhile, %q, repeated", res, err, meanwhile)
	}
	if n := w.paperHistory(t, page[0]); n != 0 {
		t.Errorf("the history keeps %d Checks, want the one kept meanwhile alone", n)
	}

	s.mu.Lock()
	s.during = func() {
		s.mu.Lock()
		s.during = nil
		s.mu.Unlock()
		if _, err := w.pool.Exec(context.Background(), `UPDATE attempts SET close_reason = 'left', closed_at = now() WHERE id = $1`, page[1]); err != nil {
			t.Errorf("the close meanwhile: %v", err)
		}
	}
	s.mu.Unlock()
	_, err = w.opening(t, l, l.adult, page[1], uuid.NewString())
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED); r.GetCloseReason() != v1.CloseReason_CLOSE_REASON_LEFT {
		t.Errorf("the refusal = %v, want closed left", r)
	}
	var rungs int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM attempt_rungs WHERE attempt_id = $1`, page[1]).Scan(&rungs); err != nil || rungs != 0 {
		t.Errorf("the closed attempt keeps %d rungs (%v), want none", rungs, err)
	}
}

// "Explain differently" on a task from paper speaks of the first task the
// Check did not judge correct, given each task of the frame with its statement
// as "Recognise" read it, the hidden solution and answer of a task not yet,
// and what was read, in the language of the profile's interface, with the
// rung's words among those asked. In the mode "Questions only" the rung comes
// with no mark. Words that give a hidden answer away give way to the
// platform's fallback text of the rung.
func TestExplainDifferentlyOnPaperSpeaksOfTheFirstMistake(t *testing.T) {
	t.Parallel()

	var asked []tutor.PaperWordsRequest
	var next tutor.WordsAnswer
	tell := func(_ context.Context, r tutor.PaperWordsRequest) tutor.WordsAnswer {
		asked = append(asked, r)
		return next
	}
	g := stubGate(t)
	w := worldOf(t, g, []learning.Option{learning.WithTutor(tutorstub.New(g)), learning.WithPaperWords(tell)})
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	w.exec(t, `UPDATE profiles SET hint_mode = 'questions_only', interface_language = 'ru-RU' WHERE id = $1`, l.adult)
	if _, err := w.recognise(t, l, a, severalStatement, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	w.submitPaper(t, l, a, wrongWork)
	const first = "Какой шаг своего решения ты проверишь первым?"
	rung, err := w.paperRungWords(t, l, l.adult, a, 1)
	if err != nil || rung.GetWords() != first || len(rung.GetMarks()) != 0 {
		t.Errorf("the words of rung 1 = %v, %v; want the first rung in Russian with no mark", rung, err)
	}
	next = tutor.WordsAnswer{Question: "Что получится, если сложить единицы?", Outcome: tutor.Outcome{Kind: tutor.Answered}}
	other, err := w.explain(l, l.adult, a, 1)
	if err != nil || other.GetWords() != next.Question || other.GetOutcome() != nil {
		t.Errorf("explain differently = %v, %v; want the model's words", other, err)
	}
	r := asked[0]
	if r.Ask != tutor.PaperAgain || r.Task != 1 || r.Rung != 1 || r.Language != "ru-RU" || len(r.Tasks) != 3 ||
		r.Tasks[0].Statement != "37 + 48 =" || r.Tasks[0].Answer != "85" || r.Tasks[0].Solution == "" || r.Tasks[0].Read != "851" ||
		r.Tasks[1].Answer != "" || r.Tasks[1].Read != "85" || len(r.Asked) != 1 || r.Asked[0] != first {
		t.Errorf("the call = %+v, want the first task of three not yet, with its hidden answer, in Russian, the rung asked", r)
	}
	// On a frame of one task, whose learner wrote no 85, the words that name it
	// give it away.
	one := w.beginPage(t, l, l.adult, 1)[0]
	w.submitPaper(t, l, one, wrongWork)
	if _, err := w.paperRungWords(t, l, l.adult, one, 1); err != nil {
		t.Fatal(err)
	}
	next = tutor.WordsAnswer{Question: "Может, получится 85?", Outcome: tutor.Outcome{Kind: tutor.Answered}}
	leaked, err := w.explain(l, l.adult, one, 1)
	if err != nil || leaked.GetWords() != tutor.PaperFallback(1, "ru-RU") || leaked.GetOutcome() != nil {
		t.Errorf("explain differently with the hidden answer = %v, %v; want the fallback text of rung 1", leaked, err)
	}
}
