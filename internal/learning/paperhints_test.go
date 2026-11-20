package learning_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	paperv1 "github.com/altnovatrail/spec/contracts/novatrail/paper/v1"
	"github.com/altnovatrail/tutor"
	"github.com/google/uuid"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/tutorstub"
)

// opening asks for the opening question of the attempt under key.
func (w *world) opening(t *testing.T, l learner, profile, attempt, key string) (*v1.GetOpeningWordsResponse, error) {
	return w.attempts.GetOpeningWords(l.ctx, &v1.GetOpeningWordsRequest{ProfileId: profile, AttemptId: attempt, IdempotencyKey: key,
		Statement: photoOf(t, formulaStatement[0], formulaStatement[1])})
}

// paperRungWords asks for the words of the rung of wrong on a task from paper,
// with the crop of the work.
func (w *world) paperRungWords(t *testing.T, l learner, profile, attempt string, wrong int32) (*v1.GetRungWordsResponse, error) {
	return w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: profile, AttemptId: attempt, IdempotencyKey: uuid.NewString(),
		WrongAnswers: &wrong, Work: paperWork(t, formulaStatement, pngOf(t, 40, 30))})
}

// A task from paper has no techniques for "Hint" before its first wrong
// answer, and the opening question instead (R82 spec): one call, whose words
// the attempt keeps, so a later request and a repeat get them with no AI. After
// a wrong answer the opening question is not current, the words of the rung
// come with the wavy line under the first mistake in the mode "Show where",
// "Hint" gives them, and "explain differently" words the rung anew; none of the
// tutor's questions is asked twice.
func TestATaskFromPaperGetsTheTutorsWords(t *testing.T) {
	t.Parallel()

	w, g := newStubWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	if h := w.hint(t, l, l.adult, a); len(h.GetTechniques().GetTechniqueIds()) != 0 || h.GetLeadingQuestion() != "" || h.GetRung() != nil {
		t.Errorf("Hint before a wrong answer = %v, want no techniques and no question", h)
	}
	key := uuid.NewString()
	first, err := w.opening(t, l, l.adult, a, key)
	if err != nil || first.GetWords() == "" || first.GetOutcome() != nil || first.GetRepeated() {
		t.Fatalf("the opening question = %v, %v; want the model's words", first, err)
	}
	for name, k := range map[string]string{"a request": uuid.NewString(), "a repeat": key} {
		if again, err := w.opening(t, l, l.adult, a, k); err != nil || again.GetWords() != first.GetWords() || !again.GetRepeated() {
			t.Errorf("%s after the words = %v, %v; want them kept, repeated", name, again, err)
		}
	}
	opening := reserveOf(g, tutor.CallPaperHint)
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != opening || profile.Reserved != 0 {
		t.Errorf("the counter = %+v, want one opening question spent", profile)
	}

	if _, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 40, 30))}); err != nil {
		t.Fatal(err)
	}
	_, err = w.opening(t, l, l.adult, a, uuid.NewString())
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_CURRENT); r.GetWrongAnswers() != 1 {
		t.Errorf("the opening question after a wrong answer: %v, want not current at one wrong answer", r)
	}
	rung, err := w.paperRungWords(t, l, l.adult, a, 1)
	if err != nil {
		t.Fatal(err)
	}
	whole := &observationv1.CropBox{Ymax: 1000, Xmax: 1000}
	if m := rung.GetMarks(); rung.GetWords() == "" || rung.GetWords() == first.GetWords() || rung.GetOutcome() != nil || len(m) != 1 ||
		m[0].GetKind() != v1.MarkKind_MARK_KIND_WAVY_LINE || m[0].GetCropBox().String() != whole.String() {
		t.Errorf("the words of rung 1 = %v, want new words with the wavy line under the task", rung)
	}
	if h := w.hint(t, l, l.adult, a); h.GetRung().GetWords() != rung.GetWords() || h.GetRung().GetRung() != 1 {
		t.Errorf("Hint after a wrong answer = %v, want the rung's words", h)
	}
	other, err := w.explain(l, l.adult, a, 1)
	if err != nil || other.GetWords() == "" || other.GetWords() == rung.GetWords() || other.GetWords() == first.GetWords() || other.GetOutcome() != nil {
		t.Errorf("explain differently = %v, %v; want other words", other, err)
	}
	words := reserveOf(g, tutor.CallPaperWords)
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != 2*opening+words+reserveOf(g, tutor.CallPaper)+reserveOf(g, tutor.CallStatement) {
		t.Errorf("the counter = %+v, want the opening question, the check, the rung and explain differently spent", profile)
	}
}

// A task from paper with no wrong answer has no rung, a task of a module no
// opening question, and the opening question takes the task's crop, a profile
// of the caller's and an own key with its value; after a wrong answer the
// words of a rung take the crop of paper.
func TestTheOpeningQuestionRefusesWhatTheContractRefuses(t *testing.T) {
	t.Parallel()

	w, _ := newStubWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	_, err := w.paperRungWords(t, l, l.adult, a, 0)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_RUNG)
	_, err = w.attempts.GetOpeningWords(l.ctx, &v1.GetOpeningWordsRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString()})
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("the opening question with no crop: %v, want invalid_argument", err)
	}
	m := w.begin(t, l, l.adult, "apples").GetAttemptId()
	if _, err := w.opening(t, l, l.adult, m, uuid.NewString()); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("the opening question on a task of a module: %v, want invalid_argument", err)
	}
	_, err = w.attempts.GetOpeningWords(l.ctx, &v1.GetOpeningWordsRequest{ProfileId: uuid.NewString(), AttemptId: a, IdempotencyKey: uuid.NewString(),
		Statement: photoOf(t, formulaStatement[0], formulaStatement[1])})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
	_, err = w.attempts.GetOpeningWords(l.ctx, &v1.GetOpeningWordsRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Statement: photoOf(t, formulaStatement[0], formulaStatement[1]), OwnKey: &v1.OwnKey{Provider: "anthropic"}})
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("the opening question on an own key with no value: %v, want invalid_argument", err)
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != 0 || profile.Reserved != 0 {
		t.Errorf("the counter = %+v, want nothing spent", profile)
	}
	if _, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 40, 30))}); err != nil {
		t.Fatal(err)
	}
	wrong := int32(1)
	_, err = w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		WrongAnswers: &wrong, Work: sheet(pngOf(t, 40, 30))})
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("the words of a rung of paper with the work of a sheet: %v, want invalid_argument", err)
	}
}

// Where the deployment admits no words of a task from paper, the opening
// question is the platform's fallback text in the language of the profile's
// interface, with the outcome "not admitted", no call and nothing spent; the
// attempt keeps none, and a repeat under the key gets the fallback text again.
// A repeat on an attempt that lapsed is refused "closed", the close left to the
// next request, which writes it; and once it is closed a repeat is refused too.
func TestTheOpeningQuestionNotAdmittedIsTheFallbackText(t *testing.T) {
	t.Parallel()

	// The reading is admitted, so that the page begins; at the stage full no words are.
	w := worldOf(t, gate(t, adultFacts, config.RungFull), []learning.Option{learning.WithAdmits(func(tutor.Call) bool { return true })})
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	w.exec(t, `UPDATE profiles SET interface_language = 'ru-RU' WHERE id = $1`, l.adult)
	key := uuid.NewString()
	for i, k := range []string{key, key, uuid.NewString()} {
		res, err := w.opening(t, l, l.adult, a, k)
		if err != nil || res.GetWords() != tutor.PaperFallback(0, "ru-RU") || res.GetRepeated() || res.GetOutcome().GetCause() != v1.Cause_CAUSE_NOT_ADMITTED {
			t.Errorf("request %d = %v, %v; want the fallback text in Russian, not admitted", i+1, res, err)
		}
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != 0 || profile.Reserved != 0 {
		t.Errorf("the counter = %+v, want nothing spent", profile)
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

// The model's opening question that is empty, breaks the form of a hint or
// does not come is the fallback text, which the attempt does not keep: the
// next request calls again. The checks go to the request's line of the log.
func TestTheOpeningQuestionTurnedDownIsTheFallbackText(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var next tutor.WordsAnswer
	tell := func(context.Context, tutor.PaperWordsRequest) tutor.WordsAnswer {
		mu.Lock()
		defer mu.Unlock()
		return next
	}
	g := stubGate(t)
	w := worldOf(t, g, []learning.Option{learning.WithTutor(tutorstub.New(g)), learning.WithPaperWords(tell)})
	l := w.learner(t)
	for _, tc := range []struct {
		name  string
		got   tutor.WordsAnswer
		cause v1.Cause
		check string
	}{
		{"no words", tutor.WordsAnswer{Outcome: tutor.Outcome{Kind: tutor.Answered}}, v1.Cause_CAUSE_UNSPECIFIED, "empty"},
		{"no question", tutor.WordsAnswer{Question: "Look at the numbers.", Outcome: tutor.Outcome{Kind: tutor.Answered}}, v1.Cause_CAUSE_UNSPECIFIED, "form"},
		{"AI failed", tutor.WordsAnswer{Outcome: tutor.Outcome{Kind: tutor.Failed}}, v1.Cause_CAUSE_AI_FAILURE, ""},
	} {
		mu.Lock()
		next = tc.got
		mu.Unlock()
		a := w.beginPage(t, l, l.adult, 1)[0]
		before := len(w.log.String())
		res, err := w.opening(t, l, l.adult, a, uuid.NewString())
		if err != nil || res.GetWords() != tutor.PaperFallback(0, "en") || res.GetOutcome().GetCause() != tc.cause || res.GetRepeated() {
			t.Errorf("%s = %v, %v; want the fallback text with %v", tc.name, res, err, tc.cause)
		}
		if line := w.log.String()[before:]; tc.check != "" && !strings.Contains(line, `"words_check":"`+tc.check+`"`) ||
			tc.check == "" && strings.Contains(line, "words_check") {
			t.Errorf("%s: the request's line = %s, want the check %q", tc.name, line, tc.check)
		}
		mu.Lock()
		next = tutor.WordsAnswer{Question: "What do you add first?", Outcome: tutor.Outcome{Kind: tutor.Answered}}
		mu.Unlock()
		if again, err := w.opening(t, l, l.adult, a, uuid.NewString()); err != nil || again.GetWords() != "What do you add first?" || again.GetRepeated() {
			t.Errorf("%s: the next request = %v, %v; want the model's words, called again", tc.name, again, err)
		}
	}
}

// Words another request kept while the model worded the opening question are
// the attempt's: the request gets them, repeated, and keeps none of its own.
// An attempt that closed meanwhile refuses it "closed", and keeps no words.
func TestTheOpeningQuestionKeptMeanwhileIsTheAttempts(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var during func()
	calls := 0
	tell := func(context.Context, tutor.PaperWordsRequest) tutor.WordsAnswer {
		mu.Lock()
		d := during
		during = nil
		calls++
		n := calls
		mu.Unlock()
		if d != nil {
			d()
		}
		q := "What do you add first?"
		if n%2 == 0 {
			q = "Which sum do you see?"
		}
		return tutor.WordsAnswer{Question: q, Outcome: tutor.Outcome{Kind: tutor.Answered}}
	}
	g := stubGate(t)
	w := worldOf(t, g, []learning.Option{learning.WithTutor(tutorstub.New(g)), learning.WithPaperWords(tell)})
	l := w.learner(t)
	page := w.beginPage(t, l, l.adult, 2)
	statement := photoOf(t, formulaStatement[0], formulaStatement[1])
	var meanwhile string
	mu.Lock()
	during = func() {
		res, err := w.attempts.GetOpeningWords(l.ctx, &v1.GetOpeningWordsRequest{ProfileId: l.adult, AttemptId: page[0], IdempotencyKey: uuid.NewString(),
			Statement: statement})
		if err != nil {
			t.Errorf("the request meanwhile: %v", err)
		}
		meanwhile = res.GetWords()
	}
	mu.Unlock()
	res, err := w.opening(t, l, l.adult, page[0], uuid.NewString())
	if err != nil || meanwhile != "Which sum do you see?" || res.GetWords() != meanwhile || !res.GetRepeated() || res.GetOutcome() != nil {
		t.Errorf("the opening question = %v, %v; want the words kept meanwhile, %q, repeated", res, err, meanwhile)
	}

	mu.Lock()
	during = func() {
		if _, err := w.pool.Exec(context.Background(), `UPDATE attempts SET close_reason = 'left', closed_at = now() WHERE id = $1`, page[1]); err != nil {
			t.Errorf("the close meanwhile: %v", err)
		}
	}
	mu.Unlock()
	_, err = w.opening(t, l, l.adult, page[1], uuid.NewString())
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED); r.GetCloseReason() != v1.CloseReason_CLOSE_REASON_LEFT {
		t.Errorf("the refusal = %v, want closed left", r)
	}
	var rungs int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM attempt_rungs WHERE attempt_id = $1`, page[1]).Scan(&rungs); err != nil || rungs != 0 {
		t.Errorf("the closed attempt keeps %d rungs (%v), want none", rungs, err)
	}
}

// The words of a rung of a task from paper read the values of the submission
// that gave the rung, not of one before it; until they are asked for, "Hint"
// gives the platform's fallback text of the rung in the language of the
// profile's interface.
func TestTheWordsOfARungReadTheSubmissionThatGaveIt(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var asked []tutor.PaperWordsRequest
	tell := func(_ context.Context, r tutor.PaperWordsRequest) tutor.WordsAnswer {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, r)
		return tutor.WordsAnswer{Question: "Which step would you check first?", Outcome: tutor.Outcome{Kind: tutor.Answered}}
	}
	g := stubGate(t)
	w := worldOf(t, g, []learning.Option{learning.WithTutor(tutorstub.New(g)), learning.WithPaperWords(tell)})
	l := w.learner(t)
	w.exec(t, `UPDATE profiles SET interface_language = 'ru-RU' WHERE id = $1`, l.adult)
	a := w.beginPage(t, l, l.adult, 1)[0]
	// A square crop is illegible, no wrong answer; one wider than tall is a wrong value.
	for _, crop := range [][2]int{{30, 30}, {40, 30}} {
		if _, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
			Work: paperWork(t, formulaStatement, pngOf(t, crop[0], crop[1]))}); err != nil {
			t.Fatal(err)
		}
	}
	if h := w.hint(t, l, l.adult, a); h.GetRung().GetWords() != tutor.PaperFallback(1, "ru-RU") || h.GetRung().GetRung() != 1 {
		t.Errorf("Hint before the words = %v, want the fallback text of rung 1 in Russian", h)
	}
	if _, err := w.paperRungWords(t, l, l.adult, a, 1); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 1 || asked[0].Ask != tutor.PaperRung || len(asked[0].Tasks) != 1 || asked[0].Tasks[0].Read != "851" || asked[0].Task != 1 {
		t.Errorf("the calls = %+v, want one of rung 1 with the value 851 of the wrong answer", asked)
	}
}

// The words of a rung of a task from paper are asked about the first task of a
// frame that is "not yet", given the tasks of the crop with their keys and the
// values read, in the language of the profile's interface; in the mode
// "Questions only" they come with no mark. Words that give a key away give way
// to the platform's fallback text of the rung, and explain differently sends no
// work.
func TestTheWordsOfATaskFromPaperSpeakOfItsFirstMistake(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var asked []tutor.PaperWordsRequest
	var next tutor.WordsAnswer
	tell := func(_ context.Context, r tutor.PaperWordsRequest) tutor.WordsAnswer {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, r)
		return next
	}
	say := func(q string) {
		mu.Lock()
		defer mu.Unlock()
		next = tutor.WordsAnswer{Question: q, Box: &observationv1.CropBox{Ymin: 600, Ymax: 700, Xmax: 400},
			Outcome: tutor.Outcome{Kind: tutor.Answered}}
	}
	g := stubGate(t)
	w := worldOf(t, g, []learning.Option{learning.WithTutor(tutorstub.New(g)), learning.WithPaperWords(tell)})
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	if _, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, severalStatement, pngOf(t, 40, 30))}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.pool.Exec(t.Context(), `UPDATE profiles SET hint_mode = 'questions_only', interface_language = 'ru-RU' WHERE id = $1`,
		l.adult); err != nil {
		t.Fatal(err)
	}
	const question = "Какой шаг решения ты проверишь первым?"
	say(question)
	rung, err := w.paperRungWords(t, l, l.adult, a, 1)
	if err != nil || rung.GetWords() != question || len(rung.GetMarks()) != 0 {
		t.Errorf("the words of rung 1 = %v, %v; want the question with no mark", rung, err)
	}
	r := asked[0]
	if r.Ask != tutor.PaperRung || r.Task != 3 || r.Rung != 1 || r.Language != "ru-RU" || len(r.Crop) == 0 || len(r.Tasks) != 3 ||
		r.Tasks[0].Read != "85" || r.Tasks[2].Statement != "50 + 36 =" || r.Tasks[2].Key.GetNumber().GetValue() != "86" {
		t.Errorf("the call = %+v, want the third task of three, read and keyed, in Russian with the work", r)
	}
	say("Сколько будет, если получится 86?")
	other, err := w.explain(l, l.adult, a, 1)
	if err != nil || other.GetWords() != tutor.PaperFallback(1, "ru-RU") || other.GetOutcome() != nil {
		t.Errorf("explain differently with a key = %v, %v; want the fallback text of rung 1", other, err)
	}
	if r := asked[1]; r.Ask != tutor.PaperAgain || len(r.Crop) != 0 || len(r.Asked) != 1 || r.Asked[0] != question {
		t.Errorf("the call of explain differently = %+v, want no work and the question asked", r)
	}
}

// The opening question kept from before a Check had only the model's own
// answers to check it by: a key derived since checks it again, and words that
// give the key away are the fallback text in their place.
func TestTheKeptOpeningQuestionIsCheckedByAKeyDerivedSince(t *testing.T) {
	t.Parallel()

	key := &paperv1.Answer{Kind: &paperv1.Answer_Number{Number: &paperv1.Number{Value: "86"}}}
	tasks := []tutor.PaperWordsTask{{Statement: "50 + 36 =", Key: key}}
	if words, kept := learning.OpeningChecked("Is it 86?", "en", tasks); kept || words != tutor.PaperFallback(0, "en") {
		t.Errorf("words that give the key away = %q, %v; want the fallback text", words, kept)
	}
	const fine = "What do you add first?"
	if words, kept := learning.OpeningChecked(fine, "en", tasks); !kept || words != fine {
		t.Errorf("words that keep the key = %q, %v; want them kept", words, kept)
	}
	if words, kept := learning.OpeningChecked("Is it 86?", "en", []tutor.PaperWordsTask{{Statement: "50 + 36 ="}}); !kept || words != "Is it 86?" {
		t.Errorf("words with no key yet = %q, %v; want them kept", words, kept)
	}
}
