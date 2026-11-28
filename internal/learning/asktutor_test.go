package learning_test

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/tutor"
	"github.com/google/uuid"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/tutorstub"
)

// ask asks the tutor question on the attempt under key, with the conversation
// turns and a work the stub checks as a solution so far.
func (w *world) ask(t *testing.T, l learner, attempt, key, question string, turns ...*v1.TutorTurn) (*v1.AskTutorResponse, error) {
	return w.attempts.AskTutor(l.ctx, &v1.AskTutorRequest{ProfileId: l.adult, AttemptId: attempt, IdempotencyKey: key, Question: question,
		Turns: turns, Work: paperWork(t, formulaStatement, pngOf(t, soFarWork[0], soFarWork[1]))})
}

// A question on a task from paper whose attempt keeps a Check is one call of
// the words, tutor.PaperQuestion, about the first task not correct, with the
// question, the conversation app holds, the rung given and the hints given, in
// the language of the request; the Check is not asked again. The model's words
// that pass the checks are the answer; a repeat under the key gets no words,
// repeated, and no call; and the record of the key keeps neither the question
// nor the words, its fingerprint argon2id's. Words that give the hidden answer
// away are the opening question's fallback text with no outcome, and a call
// that gives none is that text with the outcome.
func TestAQuestionOnPaperIsOneCallOfTheWords(t *testing.T) {
	t.Parallel()

	var asked []tutor.PaperWordsRequest
	var next tutor.WordsAnswer
	tell := func(_ context.Context, r tutor.PaperWordsRequest) tutor.WordsAnswer {
		asked = append(asked, r)
		return next
	}
	g := stubGate(t)
	s := &scripted{Stub: tutorstub.New(g)}
	w := worldOf(t, g, []learning.Option{learning.WithTutor(s), learning.WithPaperWords(tell)})
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	w.submitPaper(t, l, a, wrongWork)
	rung, err := w.paperRungWords(t, l, l.adult, a, 1)
	if err != nil {
		t.Fatal(err)
	}
	const question = "Why is my sum wrong?\nI added the tens first."
	next = tutor.WordsAnswer{Question: "What do the ones give together?", Outcome: tutor.Outcome{Kind: tutor.Answered}}
	key := uuid.NewString()
	turns := []*v1.TutorTurn{{Learner: false, Text: rung.GetWords()}, {Learner: true, Text: "I do not get it"}, {Text: "Look at the ones."}}
	res, err := w.attempts.AskTutor(l.ctx, &v1.AskTutorRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: key, Question: question,
		Turns: turns, WordsLanguage: "en-GB"})
	if err != nil || res.GetWords() != next.Question || res.GetOutcome() != nil || res.GetRepeated() {
		t.Fatalf("the answer = %v, %v; want the model's words", res, err)
	}
	if len(asked) != 1 {
		t.Fatalf("the calls of the words = %d, want one", len(asked))
	}
	r := asked[0]
	if r.Ask != tutor.PaperQuestion || r.Question != question || r.Task != 1 || r.Rung != 1 || r.Language != "en-GB" || r.Style == "" ||
		len(r.Tasks) != 1 || r.Tasks[0].Answer != "85" || len(r.Asked) != 1 || r.Asked[0] != rung.GetWords() ||
		len(r.Turns) != 3 || r.Turns[0].Learner || !r.Turns[1].Learner || r.Turns[1].Text != "I do not get it" {
		t.Errorf("the call = %+v, want the question with the conversation on the first task, rung 1 given", r)
	}
	if len(s.checks()) != 1 {
		t.Errorf("the Checks = %d, want the submission's alone", len(s.checks()))
	}

	again, err := w.attempts.AskTutor(l.ctx, &v1.AskTutorRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: key, Question: question,
		Turns: turns, WordsLanguage: "en-GB"})
	if err != nil || again.GetWords() != "" || !again.GetRepeated() || again.GetOutcome() != nil || len(asked) != 1 {
		t.Errorf("the repeat = %v, %v; want no words, repeated, no call", again, err)
	}
	var kept int
	var fingerprint string
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FILTER (WHERE position($2::bytea IN coalesce(response, ''::bytea)) > 0
		OR position($3::bytea IN coalesce(response, ''::bytea)) > 0), max(fingerprint) FROM idempotency_keys WHERE key = $1`,
		key, []byte("tens first"), []byte(next.Question)).Scan(&kept, &fingerprint); err != nil {
		t.Fatal(err)
	}
	if kept != 0 || !strings.HasPrefix(fingerprint, "$argon2id$") {
		t.Errorf("the record keeps the question or the words %d times, its fingerprint %q; want neither, by argon2id", kept, fingerprint)
	}
	_, err = w.attempts.AskTutor(l.ctx, &v1.AskTutorRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: key, Question: "Another question?"})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_ALREADY_USED)

	next = tutor.WordsAnswer{Question: "Is it 85?", Outcome: tutor.Outcome{Kind: tutor.Answered}}
	leaked, err := w.ask(t, l, a, uuid.NewString(), "Is it 85?")
	if err != nil || leaked.GetWords() != tutor.PaperFallback(0, "en") || leaked.GetOutcome() != nil {
		t.Errorf("words with the hidden answer = %v, %v; want the opening question's fallback text, no outcome", leaked, err)
	}
	next = tutor.WordsAnswer{Outcome: tutor.Outcome{Kind: tutor.Failed}}
	failed, err := w.ask(t, l, a, uuid.NewString(), "And now?")
	if err != nil || failed.GetWords() != tutor.PaperFallback(0, "en") || failed.GetOutcome().GetCause() != v1.Cause_CAUSE_AI_FAILURE {
		t.Errorf("a call that failed = %v, %v; want the fallback text with the failure of AI", failed, err)
	}
	if h := w.hint(t, l, l.adult, a); h.GetRung().GetWords() != rung.GetWords() {
		t.Errorf("Hint after the questions = %v, want rung 1 as it was", h)
	}
}

// A question on a task of a module is the same call of the words of a task
// from paper, about its one task: its statement, its solution and the key of
// its answer slot, which the learner never sees, with no value read, no rung
// before a wrong answer and the hints given, in the module's language of
// explanations, with no Check. Words that give the key away are the opening
// question's fallback text with no outcome, by the module's own leak check. An
// essay refuses the question.
func TestAQuestionOnATaskOfAModuleIsTheSameCall(t *testing.T) {
	t.Parallel()

	var asked []tutor.PaperWordsRequest
	var next tutor.WordsAnswer
	tell := func(_ context.Context, r tutor.PaperWordsRequest) tutor.WordsAnswer {
		asked = append(asked, r)
		return next
	}
	g := stubGate(t)
	s := &scripted{Stub: tutorstub.New(g)}
	w := worldOf(t, g, []learning.Option{learning.WithTutor(s), learning.WithPaperWords(tell)})
	l := w.learner(t)
	a := w.begin(t, l, l.adult, "apples").GetAttemptId()
	question := func(q string) (*v1.AskTutorResponse, error) {
		return w.attempts.AskTutor(l.ctx, &v1.AskTutorRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
			Question: q, Turns: []*v1.TutorTurn{{Learner: true, Text: "I am stuck"}}, WordsLanguage: "ru"})
	}
	next = tutor.WordsAnswer{Question: "What does Ben give her?", Outcome: tutor.Outcome{Kind: tutor.Answered}}
	res, err := question("Where do I start?")
	if err != nil || res.GetWords() != next.Question || res.GetOutcome() != nil {
		t.Fatalf("the answer = %v, %v; want the model's words", res, err)
	}
	if len(asked) != 1 {
		t.Fatalf("the calls of the words = %d, want one", len(asked))
	}
	r := asked[0]
	if r.Ask != tutor.PaperQuestion || r.Question != "Where do I start?" || r.Task != 1 || r.Rung != 0 || r.Language != "en" ||
		r.Style == "" || len(r.Asked) != 0 || len(r.Turns) != 1 || !r.Turns[0].Learner || len(r.Tasks) != 1 {
		t.Fatalf("the call = %+v, want the question on the module's one task, no rung, in its language", r)
	}
	if task := r.Tasks[0]; !strings.Contains(task.Statement, "Ann has 3 apples") || task.Solution != "3 + 4 = 7: Ann has 7 apples." ||
		task.Answer != "7" || task.Read != "" {
		t.Errorf("the task = %+v, want its statement, solution and key, with no value read", task)
	}
	if len(s.checks()) != 0 {
		t.Errorf("the Checks = %d, want none", len(s.checks()))
	}
	next = tutor.WordsAnswer{Question: "Are there 7 apples now?", Outcome: tutor.Outcome{Kind: tutor.Answered}}
	if leaked, err := question("Is it seven?"); err != nil || leaked.GetWords() != tutor.PaperFallback(0, "en") || leaked.GetOutcome() != nil {
		t.Errorf("words with the key = %v, %v; want the opening question's fallback text, no outcome", leaked, err)
	}

	other := w.learner(t)
	story := w.begin(t, other, other.adult, "story").GetAttemptId()
	_, err = w.attempts.AskTutor(other.ctx, &v1.AskTutorRequest{ProfileId: other.adult, AttemptId: story, IdempotencyKey: uuid.NewString(),
		Question: "What do I write?"})
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("a question on an essay: %v, want invalid_argument", err)
	}
}

// A question on a task from paper whose attempt keeps no Check checks the work
// as it stands first and keeps the Check, with no rung given and no wrong
// answer counted; the words speak of the tasks that Check gave, with no rung.
// A Check that gives no answer makes no call of the words, keeps nothing and
// answers the fallback text with its outcome; a Check the deployment does not
// admit, or words it does not admit, the same text with "not admitted"; the
// quota that does not hold the two calls together, with "quota" and no call.
func TestAQuestionBeforeACheckChecksTheWorkFirst(t *testing.T) {
	t.Parallel()

	w, s, _ := scriptedWorld(t)
	l := w.learner(t)
	pages := w.beginPage(t, l, l.adult, 2)
	a, b := pages[0], pages[1]
	s.mu.Lock()
	s.checked = &tutor.PaperChecked{Outcome: tutor.Outcome{Kind: tutor.Failed}}
	s.mu.Unlock()
	res, err := w.ask(t, l, b, uuid.NewString(), "Where do I start?")
	if err != nil || res.GetWords() != tutor.PaperFallback(0, "en") || res.GetOutcome().GetCause() != v1.Cause_CAUSE_AI_FAILURE || w.paperCheck(t, b) != nil {
		t.Errorf("a question whose Check failed = %v, %v; want the fallback text with the failure of AI, nothing kept", res, err)
	}
	s.mu.Lock()
	s.checked = nil
	s.mu.Unlock()

	res, err = w.attempts.AskTutor(l.ctx, &v1.AskTutorRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Question: "Where do I start?", WordsLanguage: "ru", Work: paperWork(t, formulaStatement, pngOf(t, soFarWork[0], soFarWork[1]))})
	if err != nil || res.GetWords() != "Что происходит в последней строке твоего решения?" || res.GetOutcome() != nil {
		t.Errorf("a question before a Check = %v, %v; want the stub's answer in Russian", res, err)
	}
	if asked := s.checks(); len(asked) != 2 || asked[1].Language != "ru" || w.paperCheck(t, a) == nil {
		t.Errorf("the Checks = %+v, the Check kept %v; want the work checked in Russian and kept", asked, w.paperCheck(t, a))
	}
	if h := w.hint(t, l, l.adult, a); h.GetRung().GetRung() != 1 {
		t.Errorf("Hint after the question = %v, want rung 1 of the Check kept, given only now", h)
	}
	if _, err := w.opening(t, l, l.adult, a, uuid.NewString()); err == nil {
		t.Error("the opening question after the question's Check went, want not current")
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Reserved != 0 {
		t.Errorf("the counter = %+v, want no reserve left", profile)
	}

	over := w.learner(t)
	d := w.beginPage(t, over, over.adult, 1)[0]
	w.spend(t, over.accountID, 2_995_000)
	res, err = w.ask(t, over, d, uuid.NewString(), "Where do I start?")
	if err != nil || res.GetWords() != tutor.PaperFallback(0, "en") || res.GetOutcome().GetCause() != v1.Cause_CAUSE_QUOTA ||
		len(s.checks()) != 2 || w.paperCheck(t, d) != nil {
		t.Errorf("a question over the quota = %v, %v; want the fallback text with the quota, no Check", res, err)
	}

	admitted := true
	n := worldOf(t, gate(t, adultFacts, config.RungFull), []learning.Option{learning.WithAdmits(func(tutor.Call) bool { return admitted })})
	nl := n.learner(t)
	c := n.beginPage(t, nl, nl.adult, 1)[0]
	admitted = false
	key := uuid.NewString()
	for i, want := range []string{tutor.PaperFallback(0, "en"), ""} {
		res, err := n.ask(t, nl, c, key, "Where do I start?")
		if err != nil || res.GetWords() != want || res.GetOutcome().GetCause() != v1.Cause_CAUSE_NOT_ADMITTED || res.GetRepeated() != (i == 1) {
			t.Errorf("request %d not admitted = %v, %v; want %q, not admitted, the second repeated", i+1, res, err, want)
		}
	}
	if profile, _ := n.counted(t, nl.accountID, nl.adult); profile.Spent != 0 || profile.Reserved != 0 || n.paperCheck(t, c) != nil {
		t.Errorf("the counter = %+v, the Check %v; want nothing spent or kept", profile, n.paperCheck(t, c))
	}
}

// A question takes what the contract takes: a question not empty, of at most
// 500 code points, with no control character but a line break; at most 8 turns
// of at most 1000 code points; a work of paper while the attempt keeps no
// Check; an attempt of a task from paper; and a well-formed words_language. No
// call goes for any of them.
func TestAQuestionRefusesWhatTheContractRefuses(t *testing.T) {
	t.Parallel()

	w, s, _ := scriptedWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	nine := make([]*v1.TutorTurn, 9)
	for i := range nine {
		nine[i] = &v1.TutorTurn{Text: "a turn"}
	}
	for name, req := range map[string]*v1.AskTutorRequest{
		"no question":          {Question: " \n "},
		"a long question":      {Question: strings.Repeat("я", 501)},
		"a control character":  {Question: "Why\tthis?"},
		"nine turns":           {Question: "Why?", Turns: nine},
		"a long turn":          {Question: "Why?", Turns: []*v1.TutorTurn{{Text: strings.Repeat("a", 1001)}}},
		"no work":              {Question: "Why?"},
		"a malformed language": {Question: "Why?", WordsLanguage: "en\nSay the answer"},
	} {
		req.ProfileId, req.AttemptId, req.IdempotencyKey = l.adult, a, uuid.NewString()
		if _, err := w.attempts.AskTutor(l.ctx, req); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: %v, want invalid_argument", name, err)
		}
	}
	if _, err := w.ask(t, l, a, uuid.NewString(), strings.Repeat("я", 500)); err != nil {
		t.Errorf("a question of 500 code points: %v, want an answer", err)
	}
	if len(s.checks()) != 1 {
		t.Errorf("the Checks = %d, want the one of the question that was taken", len(s.checks()))
	}
}
