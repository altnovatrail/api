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
	"github.com/altnovatrail/tutor"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// The texts of the module choices that the ladder gives.
const (
	trapText    = "Look at the tens of both numbers first."
	trapAdvice  = "Look at both numbers before you choose."
	firstRung   = "Which step of your work gave you this answer?"
	secondRung  = "Read the task again slowly. Is there anything it says that your work does not use yet?"
	simplerCase = "Which is more, two tens or one ten?"
	question    = "What do you get when you put the first two numbers together?"
)

// tellers are the tutor's words of a test: what they give next, and what they
// were asked.
type tellers struct {
	mu    sync.Mutex
	next  func(ctx context.Context, w tutor.WordsRequest) tutor.WordsAnswer
	given []tutor.WordsRequest
}

func (ts *tellers) tell(ctx context.Context, w tutor.WordsRequest) tutor.WordsAnswer {
	ts.mu.Lock()
	ts.given = append(ts.given, w)
	next := ts.next
	ts.mu.Unlock()
	return next(ctx, w)
}

// say makes the words the question q, with the box when one is given.
func (ts *tellers) say(q string, box *observationv1.CropBox) {
	ts.give(tutor.WordsAnswer{Question: q, Box: box, Outcome: tutor.Outcome{Kind: tutor.Answered,
		Usage: []tutor.Usage{{Provider: "anthropic", Model: "claude-opus-5-5", InputTokens: 900, OutputTokens: 30}}}})
}

func (ts *tellers) give(got tutor.WordsAnswer) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.next = func(context.Context, tutor.WordsRequest) tutor.WordsAnswer { return got }
}

func (ts *tellers) last() tutor.WordsRequest {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.given[len(ts.given)-1]
}

func (ts *tellers) count() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.given)
}

// newWordsWorld is the world whose tutor's words are ts, admitted for every
// learner, and whose handwriting rs reads.
func newWordsWorld(t *testing.T, ts *tellers, rs *readers) *world {
	t.Helper()
	opts := []learning.Option{learning.WithWords(ts.tell)}
	if rs != nil {
		opts = append(opts, learning.WithReader(rs.read))
	}
	return worldOf(t, gate(t, adultFacts, config.RungAuthor), opts)
}

// wrongs sends the taps of each submission in turn on the attempt.
func (w *world) wrongs(t *testing.T, l learner, profile, attempt string, submissions ...[]*v1.TappedChoice) {
	t.Helper()
	for _, taps := range submissions {
		if _, err := w.submit(l, profile, attempt, taps...); err != nil {
			t.Fatal(err)
		}
	}
}

// hint is the answer of "hint" on the attempt.
func (w *world) hint(t *testing.T, l learner, profile, attempt string) *v1.GetHintResponse {
	t.Helper()
	res, err := w.attempts.GetHint(l.ctx, &v1.GetHintRequest{ProfileId: profile, AttemptId: attempt})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// rungWords asks for the words of the rung of the attempt's wrong answers.
func (w *world) rungWords(l learner, profile, attempt string, wrong int32) (*v1.GetRungWordsResponse, error) {
	return w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: profile, AttemptId: attempt,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong})
}

// explain asks to explain the rung of the attempt's wrong answers differently.
func (w *world) explain(l learner, profile, attempt string, wrong int32) (*v1.ExplainDifferentlyResponse, error) {
	return w.attempts.ExplainDifferently(l.ctx, &v1.ExplainDifferentlyRequest{ProfileId: profile, AttemptId: attempt,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong})
}

// A hint before the first wrong answer gives the task's techniques and its
// leading question, and the attempt records both, the question only when the
// task has one; the open attempt holds what was given. After a wrong answer it
// gives the current rung, with no question.
func TestAHintBeforeTheFirstWrongAnswerGivesTheTechniquesAndTheQuestion(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	pick := w.begin(t, l, l.child, "pick").GetAttemptId()
	w.clock.move(time.Minute)
	res := w.hint(t, l, l.child, pick)
	if got := res.GetTechniques().GetTechniqueIds(); len(got) != 1 || got[0] != "count-on" || res.GetLeadingQuestion() != question {
		t.Errorf("the hint = %v, want the technique and the leading question", res)
	}
	var techniques, leading, moved bool
	if err := w.pool.QueryRow(t.Context(), `SELECT requested_techniques_at IS NOT NULL, leading_question_at IS NOT NULL,
			last_action_at > started_at FROM attempts WHERE id = $1`, pick).Scan(&techniques, &leading, &moved); err != nil {
		t.Fatal(err)
	}
	if !techniques || !leading || !moved {
		t.Errorf("recorded: techniques %v, question %v, last action moved %v; want all", techniques, leading, moved)
	}
	open, err := w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: l.child, AttemptId: pick})
	if err != nil {
		t.Fatal(err)
	}
	if a := open.GetAttempt(); len(a.GetRequestedTechniqueIds()) != 1 || a.GetRequestedLeadingQuestion() != question ||
		a.GetRung() != nil || len(a.GetStepTechniqueIds()) != 0 {
		t.Errorf("the open attempt = %v, want what the hint gave, and no rung", a)
	}

	// A task with no leading question records the techniques alone.
	pairs := w.begin(t, l, l.child, "pairs").GetAttemptId()
	if res := w.hint(t, l, l.child, pairs); len(res.GetTechniques().GetTechniqueIds()) != 2 || res.GetLeadingQuestion() != "" {
		t.Errorf("the hint of pairs = %v", res)
	}
	if err := w.pool.QueryRow(t.Context(), `SELECT requested_techniques_at IS NOT NULL, leading_question_at IS NOT NULL
			FROM attempts WHERE id = $1`, pairs).Scan(&techniques, &leading); err != nil || !techniques || leading {
		t.Errorf("recorded: techniques %v, question %v (%v); want the techniques alone", techniques, leading, err)
	}
	open, err = w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: l.child, AttemptId: pairs})
	if err != nil || open.GetAttempt().GetRequestedLeadingQuestion() != "" {
		t.Errorf("the open attempt = %v (%v), want no question", open.GetAttempt(), err)
	}

	// After a wrong answer: the rung, and no question.
	w.wrongs(t, l, l.child, pairs, []*v1.TappedChoice{tap(1, 1, "12"), tap(2, 1, "30")})
	res = w.hint(t, l, l.child, pairs)
	if r := res.GetRung(); r.GetRung() != 1 || r.GetWords() != trapText || res.GetLeadingQuestion() != "" {
		t.Errorf("the hint after a wrong answer = %v, want the first rung's fallback text", res)
	}

	// A task with no techniques and no question records neither.
	greater := w.begin(t, l, l.child, "greater").GetAttemptId()
	if res := w.hint(t, l, l.child, greater); len(res.GetTechniques().GetTechniqueIds()) != 0 || res.GetLeadingQuestion() != "" {
		t.Errorf("the hint of greater = %v, want nothing to hint", res)
	}
	if err := w.pool.QueryRow(t.Context(), `SELECT requested_techniques_at IS NOT NULL, leading_question_at IS NOT NULL
			FROM attempts WHERE id = $1`, greater).Scan(&techniques, &leading); err != nil || techniques || leading {
		t.Errorf("recorded: techniques %v, question %v (%v); want neither", techniques, leading, err)
	}

	// A task with a rubric gives the techniques, whatever.
	story := w.attempt(t, l.adult, string(w.current.Digest), "story")
	if res := w.hint(t, l, l.adult, story); res.GetTechniques() == nil || res.GetLeadingQuestion() != "" {
		t.Errorf("the hint of a task with a rubric = %v", res)
	}
	w.exec(t, `UPDATE attempts SET close_reason = 'left', closed_at = now() WHERE id = $1`, story)
	_, err = w.attempts.GetHint(l.ctx, &v1.GetHintRequest{ProfileId: l.adult, AttemptId: story})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED)
}

// The fallback text of a rung follows the chain of the format, on the first
// answer slot "not_yet" of the submission that gave the rung: the text of the
// trap value, the advice of its trap, the template of the rung, and at the
// third rung the simpler case; from the fourth wrong answer on, the third.
func TestTheFallbackTextFollowsTheChainOfTheFormat(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	trapped := []*v1.TappedChoice{tap(1, 1, "12"), tap(2, 1, "30")}
	plain := []*v1.TappedChoice{tap(1, 1, "21"), tap(2, 1, "13")}
	a := w.begin(t, l, l.child, "pairs").GetAttemptId()
	for _, step := range []struct {
		taps  []*v1.TappedChoice
		rung  int32
		words string
	}{
		{trapped, 1, trapText},
		{plain, 2, secondRung},
		{trapped, 3, simplerCase},
		// The fourth wrong answer gives no rung: the third speaks of its slot still.
		{plain, 3, simplerCase},
	} {
		w.wrongs(t, l, l.child, a, step.taps)
		if r := w.hint(t, l, l.child, a).GetRung(); r.GetRung() != step.rung || r.GetWords() != step.words {
			t.Errorf("rung %d = %v, want %q", step.rung, r, step.words)
		}
	}
	b := w.begin(t, l, l.child, "pairs").GetAttemptId()
	w.wrongs(t, l, l.child, b, plain, plain, plain)
	if r := w.hint(t, l, l.child, b).GetRung(); r.GetWords() != secondRung {
		t.Errorf("the third rung of an item with no simpler case = %v, want the second's text", r)
	}
	b = w.begin(t, l, l.child, "pairs").GetAttemptId()
	w.wrongs(t, l, l.child, b, plain)
	if r := w.hint(t, l, l.child, b).GetRung(); r.GetWords() != firstRung {
		t.Errorf("the first rung with no trap = %v, want its template", r)
	}
	pick := w.begin(t, l, l.child, "pick").GetAttemptId()
	w.wrongs(t, l, l.child, pick, []*v1.TappedChoice{tap(1, 1, "five")})
	if r := w.hint(t, l, l.child, pick).GetRung(); r.GetWords() != trapAdvice {
		t.Errorf("the first rung of a trap value with no text = %v, want the trap's advice", r)
	}
	// No submission gave the rung: it speaks as its template, and the open attempt stays readable.
	none := w.begin(t, l, l.child, "pairs").GetAttemptId()
	for wrong, words := range map[int]string{1: firstRung, 3: secondRung} {
		w.exec(t, `UPDATE attempts SET wrong_answers = $2 WHERE id = $1`, none, wrong)
		if r := w.hint(t, l, l.child, none).GetRung(); r.GetWords() != words {
			t.Errorf("rung %d with no submission = %v, want %q", wrong, r, words)
		}
	}
	if _, err := w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: l.child, AttemptId: none}); err != nil {
		t.Errorf("the open attempt with no submission = %v", err)
	}
}

// With no model admitted for the words, the words of a rung are its fallback
// text with the outcome "not admitted", and the attempt keeps them: the same
// wrong answer asked again and a repeat get them, marked a repeat. A wrong answer
// that is not the last is "not current", with how many there are.
func TestTheWordsOfARungWithNoModelAdmittedAreItsFallbackText(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	a := w.begin(t, l, l.child, "pairs").GetAttemptId()
	_, err := w.rungWords(l, l.child, a, 0)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_RUNG)
	w.wrongs(t, l, l.child, a, []*v1.TappedChoice{tap(1, 1, "12"), tap(2, 1, "30")})
	wrong := int32(1)
	req := &v1.GetRungWordsRequest{ProfileId: l.child, AttemptId: a, IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong}
	res, err := w.attempts.GetRungWords(l.ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if res.GetRung() != 1 || res.GetWords() != trapText || res.GetOutcome().GetCause() != v1.Cause_CAUSE_NOT_ADMITTED ||
		res.GetRepeated() || len(res.GetMarks()) != 0 {
		t.Errorf("the words = %v, want the fallback text, not admitted", res)
	}
	var source, words string
	if err := w.pool.QueryRow(t.Context(), `SELECT source, words FROM attempt_rungs WHERE attempt_id = $1 AND rung = 1`, a).
		Scan(&source, &words); err != nil || source != "fallback" || words != trapText {
		t.Errorf("the rung kept = %s %q (%v)", source, words, err)
	}
	var response []byte
	if err := w.pool.QueryRow(t.Context(), `SELECT response FROM idempotency_keys WHERE key = $1`, req.GetIdempotencyKey()).
		Scan(&response); err != nil || strings.Contains(string(response), trapText) {
		t.Errorf("the key's record holds %q (%v), want no words", response, err)
	}
	// A repeat keeps its outcome; the same wrong answer asked again reads the words alone.
	for name, again := range map[string]struct {
		req   *v1.GetRungWordsRequest
		cause v1.Cause
	}{
		"a repeat":              {req, v1.Cause_CAUSE_NOT_ADMITTED},
		"the same wrong answer": {&v1.GetRungWordsRequest{ProfileId: l.child, AttemptId: a, IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong}, v1.Cause_CAUSE_UNSPECIFIED},
	} {
		got, err := w.attempts.GetRungWords(l.ctx, again.req)
		if err != nil || got.GetWords() != trapText || !got.GetRepeated() || got.GetOutcome().GetCause() != again.cause {
			t.Errorf("%s = %v (%v), want the words kept, a repeat, %v", name, got, err, again.cause)
		}
	}
	_, err = w.rungWords(l, l.child, a, 2)
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_CURRENT); r.GetWrongAnswers() != 1 {
		t.Errorf("the refusal = %v, want one wrong answer", r)
	}
	_, err = w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.child, AttemptId: a, IdempotencyKey: uuid.NewString()})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	_, err = w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.child, AttemptId: a, IdempotencyKey: uuid.NewString(),
		WrongAnswers: &wrong, Work: sheet(crop(t))})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	_, err = w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.child, AttemptId: a, IdempotencyKey: uuid.NewString(),
		WrongAnswers: &wrong, OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "sk-test"}})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE)
	// An own key short of its provider or its key is refused before the gate (R54).
	for _, own := range []*v1.OwnKey{{Key: "sk-test"}, {Provider: "anthropic"}} {
		_, err = w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.child, AttemptId: a,
			IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong, OwnKey: own})
		refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	}

	// From the fourth wrong answer no rung is new.
	plain := []*v1.TappedChoice{tap(1, 1, "21"), tap(2, 1, "13")}
	w.wrongs(t, l, l.child, a, plain, plain, plain)
	_, err = w.rungWords(l, l.child, a, 4)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_MORE_WORDS)

	// A task with a rubric has no rung, and a closed attempt is closed.
	story := w.attempt(t, l.adult, string(w.current.Digest), "story")
	_, err = w.rungWords(l, l.adult, story, 0)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_RUNG)
	w.begin(t, l, l.child, "pick")
	_, err = w.rungWords(l, l.child, a, 4)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED)
	_, err = w.attempts.GetRungWords(l.ctx, req)
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED); r.GetCloseReason() != v1.CloseReason_CLOSE_REASON_LEFT {
		t.Errorf("a repeat on a closed attempt = %v, want closed left", r)
	}
}

// The tutor's words of a rung reach the learner once they pass the leak check
// and the form check, with the box of the first mistake as a mark; words that
// fail either give the fallback text with no outcome, and the request's line
// of the log names the check that turned them down; a call that gives none
// gives the fallback text with its outcome. The call gets the task's key of the
// rung's slot, its solution, its traps with the one found, its techniques, the
// style of the learner and the questions asked so far.
func TestTheWordsOfARungComeFromTheTutor(t *testing.T) {
	t.Parallel()

	ts := &tellers{}
	w := newWordsWorld(t, ts, nil)
	l := w.learner(t)
	box := &observationv1.CropBox{Ymin: 10, Xmin: 20, Ymax: 300, Xmax: 400}
	trapped := []*v1.TappedChoice{tap(1, 1, "12"), tap(2, 1, "30")}
	for _, tc := range []struct {
		name  string
		got   tutor.WordsAnswer
		words string
		cause v1.Cause
		marks int
		// check is the check the request's line names, if any.
		check string
	}{
		{"words that pass", tutor.WordsAnswer{Question: "Which of the two numbers has more tens?", Box: box,
			Outcome: tutor.Outcome{Kind: tutor.Answered}}, "Which of the two numbers has more tens?", v1.Cause_CAUSE_UNSPECIFIED, 1, ""},
		{"too many sentences", tutor.WordsAnswer{Question: "Look. Look again. Which number has more tens?", Box: box,
			Outcome: tutor.Outcome{Kind: tutor.Answered}}, trapText, v1.Cause_CAUSE_UNSPECIFIED, 0, "form"},
		{"no question", tutor.WordsAnswer{Question: "Look at the tens.", Outcome: tutor.Outcome{Kind: tutor.Answered}},
			trapText, v1.Cause_CAUSE_UNSPECIFIED, 0, "form"},
		{"no words", tutor.WordsAnswer{Outcome: tutor.Outcome{Kind: tutor.Answered}}, trapText, v1.Cause_CAUSE_UNSPECIFIED, 0, "empty"},
		{"AI failed", tutor.WordsAnswer{Outcome: tutor.Outcome{Kind: tutor.Failed}}, trapText, v1.Cause_CAUSE_AI_FAILURE, 0, ""},
		{"a limit", tutor.WordsAnswer{Outcome: tutor.Outcome{Kind: tutor.LimitReached}}, trapText, v1.Cause_CAUSE_PLATFORM_LIMIT, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts.give(tc.got)
			a := w.begin(t, l, l.child, "pairs").GetAttemptId()
			w.wrongs(t, l, l.child, a, trapped)
			before := len(w.log.String())
			res, err := w.rungWords(l, l.child, a, 1)
			if err != nil {
				t.Fatal(err)
			}
			if res.GetWords() != tc.words || res.GetOutcome().GetCause() != tc.cause || len(res.GetMarks()) != tc.marks {
				t.Errorf("the words = %v, want %q, %v, %d marks", res, tc.words, tc.cause, tc.marks)
			}
			if line := w.log.String()[before:]; tc.check != "" && !strings.Contains(line, `"words_check":"`+tc.check+`"`) ||
				tc.check == "" && strings.Contains(line, "words_check") {
				t.Errorf("the request's line = %s, want the check %q", line, tc.check)
			}
			if tc.marks > 0 && res.GetMarks()[0].GetCropBox().GetYmax() != 300 {
				t.Errorf("the mark = %v, want the box", res.GetMarks()[0])
			}
			if r := w.hint(t, l, l.child, a).GetRung(); r.GetWords() != tc.words {
				t.Errorf("the hint after = %v, want the words kept", r)
			}
		})
	}
	asked := ts.last()
	if asked.Rung != 1 || asked.Key != "21" || asked.Found != "first-seen" || len(asked.Traps) != 1 ||
		asked.Traps[0].Description == "" || asked.Language != "en" || !strings.Contains(asked.Style, "child") ||
		len(asked.Techniques) != 2 || asked.Techniques[0] != "Compare the tens" || len(asked.Solution) != 1 ||
		asked.Crop != nil || asked.Task == nil || len(asked.Asked) != 0 || asked.Route.Own != nil {
		t.Errorf("the call = %+v", asked)
	}
	if log := w.log.String(); !strings.Contains(log, `"ai_outcome":"limit_reached"`) || strings.Contains(log, trapText) ||
		strings.Contains(log, "more tens") {
		t.Errorf("the log = %s, want the calls and no words", log)
	}
}

// The words of a handwriting sheet's rung go with the crop sent again, which
// no store keeps; an attempt that another device closed or answered again while
// the words were called is refused, and nothing is written.
func TestTheWordsOfAHandwritingRungGoWithItsCrop(t *testing.T) {
	t.Parallel()

	ts, rs := &tellers{}, &readers{}
	w := newWordsWorld(t, ts, rs)
	l := w.learner(t)
	png := crop(t)
	rs.give(answered(read("6"), read("6")))
	a := w.begin(t, l, l.adult, "apples").GetAttemptId()
	if _, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), Work: sheet(png)}); err != nil {
		t.Fatal(err)
	}
	wrong := int32(1)
	_, err := w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	ts.say("Which numbers did you add?", &observationv1.CropBox{Ymin: 1, Xmin: 2, Ymax: 3, Xmax: 4})
	res, err := w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong, Work: sheet(png)})
	if err != nil {
		t.Fatal(err)
	}
	if res.GetWords() != "Which numbers did you add?" || len(res.GetMarks()) != 1 {
		t.Errorf("the words = %v", res)
	}
	if asked := ts.last(); string(asked.Crop) != string(png) || asked.Key != "7" || asked.Found != "" || asked.Style != "an adult" {
		t.Errorf("the call = %+v, want the crop and the key", asked)
	}
	var kept int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM idempotency_keys WHERE position($1::bytea IN response) > 0`, png).
		Scan(&kept); err != nil || kept != 0 {
		t.Errorf("the crop is kept %d times (%v), want none", kept, err)
	}

	// Closed, or answered again, while the words were called: nothing is written.
	for name, meanwhile := range map[string]string{
		"closed":         `UPDATE attempts SET close_reason = 'left', closed_at = now() WHERE id = $1`,
		"answered again": `UPDATE attempts SET wrong_answers = wrong_answers + 1 WHERE id = $1`,
	} {
		t.Run(name, func(t *testing.T) {
			b := w.begin(t, l, l.adult, "apples").GetAttemptId()
			if _, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: b,
				IdempotencyKey: uuid.NewString(), Work: sheet(png)}); err != nil {
				t.Fatal(err)
			}
			ts.mu.Lock()
			ts.next = func(context.Context, tutor.WordsRequest) tutor.WordsAnswer {
				w.exec(t, meanwhile, b)
				return tutor.WordsAnswer{Question: "Which numbers did you add?", Outcome: tutor.Outcome{Kind: tutor.Answered}}
			}
			ts.mu.Unlock()
			key := uuid.NewString()
			_, err := w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: b,
				IdempotencyKey: key, WrongAnswers: &wrong, Work: sheet(png)})
			if err == nil {
				t.Fatal("the words were given")
			}
			var rungs, records int
			if err := w.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM attempt_rungs WHERE attempt_id = $1),
					(SELECT count(*) FROM idempotency_keys WHERE key = $2)`, b, key).Scan(&rungs, &records); err != nil || rungs+records != 0 {
				t.Errorf("wrote %d rungs and %d records (%v), want none", rungs, records, err)
			}
		})
	}
}

// "Explain differently" words the rung anew, once a rung, with the questions
// asked so far: the learner reads its words from then on, and a second time on
// the rung is "already asked". Words that are the ones the learner reads are the
// outcome "no other words". With no model admitted, platform AI is refused "not
// admitted" and the right stays; an own key gets it as an outcome.
func TestExplainDifferentlyWordsTheRungAnewOnce(t *testing.T) {
	t.Parallel()

	ts := &tellers{}
	w := newWordsWorld(t, ts, nil)
	l := w.learner(t)
	trapped := []*v1.TappedChoice{tap(1, 1, "12"), tap(2, 1, "30")}
	a := w.begin(t, l, l.child, "pairs").GetAttemptId()
	_, err := w.explain(l, l.child, a, 0)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_RUNG)
	w.wrongs(t, l, l.child, a, trapped)
	ts.say("Which of the two numbers has more tens?", nil)
	if _, err := w.rungWords(l, l.child, a, 1); err != nil {
		t.Fatal(err)
	}
	ts.say("How many tens has each number?", nil)
	wrong := int32(1)
	req := &v1.ExplainDifferentlyRequest{ProfileId: l.child, AttemptId: a, IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong}
	res, err := w.attempts.ExplainDifferently(l.ctx, req)
	if err != nil || res.GetWords() != "How many tens has each number?" || res.GetOutcome() != nil {
		t.Fatalf("explained = %v (%v)", res, err)
	}
	if asked := ts.last(); len(asked.Asked) != 1 || asked.Asked[0] != "Which of the two numbers has more tens?" || asked.Crop != nil {
		t.Errorf("the call = %+v, want the question asked and no work", asked)
	}
	open, err := w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: l.child, AttemptId: a})
	if err != nil || open.GetAttempt().GetRung().GetWords() != "How many tens has each number?" || !open.GetAttempt().GetExplainedDifferently() {
		t.Errorf("the open attempt = %v (%v), want the other words, explained", open.GetAttempt(), err)
	}
	calls := ts.count()
	if again, err := w.attempts.ExplainDifferently(l.ctx, req); err != nil || again.GetWords() != "How many tens has each number?" || ts.count() != calls {
		t.Errorf("a repeat = %v (%v), %d calls; want the words kept and no call", again, err, ts.count()-calls)
	}
	_, err = w.explain(l, l.child, a, 1)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ALREADY_ASKED)
	_, err = w.explain(l, l.child, a, 2)
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_CURRENT); r.GetWrongAnswers() != 1 {
		t.Errorf("the refusal = %v", r)
	}

	// The fallback text the learner reads already: no other words, and the right is spent.
	b := w.begin(t, l, l.child, "pairs").GetAttemptId()
	w.wrongs(t, l, l.child, b, trapped)
	ts.give(tutor.WordsAnswer{Outcome: tutor.Outcome{Kind: tutor.Answered}, Question: "Look at the tens."})
	res, err = w.explain(l, l.child, b, 1)
	if err != nil || res.GetWords() != trapText || res.GetOutcome().GetCause() != v1.Cause_CAUSE_NO_OTHER_WORDS {
		t.Errorf("explained to the same words = %v (%v), want no other words", res, err)
	}
	_, err = w.explain(l, l.child, b, 1)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ALREADY_ASKED)
	// From the fourth wrong answer on, the third rung, once.
	plain := []*v1.TappedChoice{tap(1, 1, "21"), tap(2, 1, "13")}
	w.wrongs(t, l, l.child, b, plain, plain, plain)
	ts.give(tutor.WordsAnswer{Outcome: tutor.Outcome{Kind: tutor.Failed}})
	// The learner reads the third rung's words still, which came at the third wrong answer.
	res, err = w.explain(l, l.child, b, 3)
	if err != nil || res.GetWords() != secondRung || res.GetOutcome().GetCause() != v1.Cause_CAUSE_AI_FAILURE || ts.last().Rung != 3 {
		t.Errorf("explained at the fourth wrong answer = %v (%v), rung %d", res, err, ts.last().Rung)
	}
	_, err = w.explain(l, l.child, b, 4)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ALREADY_ASKED)
	_, err = w.explain(l, l.child, b, 2)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_CURRENT)

	// No model admitted: platform AI is refused, an own key gets the outcome.
	n := newWorld(t)
	nl := n.learner(t)
	c := n.begin(t, nl, nl.child, "pairs").GetAttemptId()
	n.wrongs(t, nl, nl.child, c, trapped)
	_, err = n.explain(nl, nl.child, c, 1)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED)
	if open, err := n.learning.GetOpenAttempt(nl.ctx, &v1.GetOpenAttemptRequest{ProfileId: nl.child, AttemptId: c}); err != nil ||
		open.GetAttempt().GetExplainedDifferently() {
		t.Errorf("the open attempt = %v (%v), want the right kept", open.GetAttempt(), err)
	}
	d := n.begin(t, nl, nl.adult, "pick").GetAttemptId()
	n.wrongs(t, nl, nl.adult, d, []*v1.TappedChoice{tap(1, 1, "five")})
	res, err = n.attempts.ExplainDifferently(nl.ctx, &v1.ExplainDifferentlyRequest{ProfileId: nl.adult, AttemptId: d,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong, OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "sk-test"}})
	if err != nil || res.GetWords() != trapAdvice || res.GetOutcome().GetCause() != v1.Cause_CAUSE_NOT_ADMITTED {
		t.Errorf("explained on an own key = %v (%v), want the words read, not admitted", res, err)
	}
	_, err = n.attempts.ExplainDifferently(nl.ctx, &v1.ExplainDifferentlyRequest{ProfileId: nl.child, AttemptId: c,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong, OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "sk-test"}})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE)
}

// The words after a correct answer are asked once, on the attempt the answer
// has just closed, while the profile has begun no other: tutor has no call for
// them yet, so the answer holds none, with the outcome "not admitted".
func TestTheWordsAfterACorrectAnswerAreNotAdmittedYet(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	a := w.begin(t, l, l.child, "pick").GetAttemptId()
	ask := func(attempt string, work *v1.Work) (*v1.GetCorrectAnswerWordsResponse, error) {
		return w.attempts.GetCorrectAnswerWords(l.ctx, &v1.GetCorrectAnswerWordsRequest{ProfileId: l.child, AttemptId: attempt,
			IdempotencyKey: uuid.NewString(), Work: work})
	}
	_, err := ask(a, nil)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_MORE_WORDS)
	w.wrongs(t, l, l.child, a, []*v1.TappedChoice{tap(1, 1, "three and four")})
	_, err = ask(a, sheet(crop(t)))
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	req := &v1.GetCorrectAnswerWordsRequest{ProfileId: l.child, AttemptId: a, IdempotencyKey: uuid.NewString()}
	res, err := w.attempts.GetCorrectAnswerWords(l.ctx, req)
	if err != nil || res.Words != nil || res.GetOutcome().GetCause() != v1.Cause_CAUSE_NOT_ADMITTED || res.GetRepeated() {
		t.Errorf("the words = %v (%v), want none, not admitted", res, err)
	}
	if again, err := w.attempts.GetCorrectAnswerWords(l.ctx, req); err != nil || !again.GetRepeated() || again.Words != nil {
		t.Errorf("a repeat = %v (%v), want a repeat without words", again, err)
	}
	w.begin(t, l, l.child, "pairs")
	_, err = ask(a, nil)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_MORE_WORDS)
	_, err = ask(uuid.NewString(), nil)
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	_, err = w.attempts.GetCorrectAnswerWords(l.ctx, &v1.GetCorrectAnswerWordsRequest{ProfileId: l.child, AttemptId: a,
		IdempotencyKey: uuid.NewString(), OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "sk-test"}})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE)
}

// The solution is shown to an adult learner from the beginning, and to a child
// from the fourth wrong answer, by an adult's action while "ask before showing"
// is on; the show closes the attempt "solution shown" and deletes the words of
// its rungs. A repeat gets the solution again.
func TestTheSolutionIsShownAfterTheLadderAndByTheAdult(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	show := func(profile, attempt string, action v1.Action) (*v1.ShowSolutionResponse, error) {
		return w.attempts.ShowSolution(l.ctx, &v1.ShowSolutionRequest{ProfileId: profile, AttemptId: attempt,
			IdempotencyKey: uuid.NewString(), Action: action})
	}
	adult := w.begin(t, l, l.adult, "grown-ups").GetAttemptId()
	req := &v1.ShowSolutionRequest{ProfileId: l.adult, AttemptId: adult, IdempotencyKey: uuid.NewString(), Action: v1.Action_ACTION_LEARNING}
	res, err := w.attempts.ShowSolution(l.ctx, req)
	if err != nil || res.GetSolved() == nil || res.GetSolved().GetModuleFormatVersion() != "0.4" {
		t.Fatalf("shown = %v (%v)", res, err)
	}
	if got := w.closeOf(t, adult); got != "solution_shown" {
		t.Errorf("the attempt is closed %q, want solution_shown", got)
	}
	if again, err := w.attempts.ShowSolution(l.ctx, req); err != nil || again.GetSolved() == nil {
		t.Errorf("a repeat = %v (%v), want the solution again", again, err)
	}

	child := w.begin(t, l, l.child, "pairs").GetAttemptId()
	trapped := []*v1.TappedChoice{tap(1, 1, "12"), tap(2, 1, "30")}
	w.wrongs(t, l, l.child, child, trapped, trapped, trapped)
	if _, err := w.rungWords(l, l.child, child, 3); err != nil {
		t.Fatal(err)
	}
	for _, action := range []v1.Action{v1.Action_ACTION_LEARNING, v1.Action_ACTION_ADULT} {
		_, err := show(l.child, child, action)
		refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_LADDER_NOT_PASSED)
	}
	w.wrongs(t, l, l.child, child, trapped)
	_, err = show(l.child, child, v1.Action_ACTION_LEARNING)
	refusedAs(t, err, connect.CodePermissionDenied, v1.Cause_CAUSE_ADULT_NEEDED)
	_, err = show(l.child, child, v1.Action_ACTION_UNSPECIFIED)
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	w.denyManage.Store(true)
	_, err = show(l.child, child, v1.Action_ACTION_ADULT)
	refusedAs(t, err, connect.CodePermissionDenied, v1.Cause_CAUSE_UNSPECIFIED)
	w.denyManage.Store(false)
	if res, err := show(l.child, child, v1.Action_ACTION_ADULT); err != nil || res.GetSolved() == nil {
		t.Errorf("shown by an adult = %v (%v)", res, err)
	}
	var words, sources int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(words), count(source) FROM attempt_rungs WHERE attempt_id = $1`, child).
		Scan(&words, &sources); err != nil || words != 0 || sources != 1 {
		t.Errorf("after the show: %d words and %d sources (%v), want the source alone", words, sources, err)
	}

	// The setting off: a learning action shows it.
	w.exec(t, `UPDATE profiles SET ask_before_showing = false WHERE id = $1`, l.child)
	other := w.begin(t, l, l.child, "pairs").GetAttemptId()
	w.wrongs(t, l, l.child, other, trapped, trapped, trapped, trapped)
	if res, err := show(l.child, other, v1.Action_ACTION_LEARNING); err != nil || res.GetSolved() == nil {
		t.Errorf("shown by the child with the setting off = %v (%v)", res, err)
	}
	story := w.attempt(t, l.adult, string(w.current.Digest), "story")
	_, err = show(l.adult, story, v1.Action_ACTION_LEARNING)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_SHOWING)
}

// Words that hold a key of the task that its texts do not show leak, and give
// the fallback text with no outcome.
func TestTheWordsThatLeakAKeyGiveTheFallbackText(t *testing.T) {
	t.Parallel()

	ts, rs := &tellers{}, &readers{}
	w := newWordsWorld(t, ts, rs)
	l := w.learner(t)
	rs.give(answered(read("6"), read("6")))
	a := w.begin(t, l, l.adult, "apples").GetAttemptId()
	if _, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), Work: sheet(crop(t))}); err != nil {
		t.Fatal(err)
	}
	ts.say("Did you get 7 apples?", &observationv1.CropBox{Ymin: 1, Xmin: 2, Ymax: 3, Xmax: 4})
	wrong := int32(1)
	before := len(w.log.String())
	res, err := w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong, Work: sheet(crop(t))})
	if err != nil || res.GetWords() != firstRung || res.GetOutcome() != nil || len(res.GetMarks()) != 0 {
		t.Errorf("the words that leak = %v (%v), want the first rung's template, no outcome, no mark", res, err)
	}
	if line := w.log.String()[before:]; !strings.Contains(line, `"words_check":"leak"`) || strings.Contains(line, "7 apples") {
		t.Errorf("the request's line = %s, want the check leak and no words", line)
	}
}

// A database that fails is unavailable, at each step of the ladder.
func TestALadderOnADatabaseThatFailsIsUnavailable(t *testing.T) {
	t.Parallel()

	for _, failOn := range []string{
		"FROM attempt_rungs WHERE", "INSERT INTO attempt_rungs (", "ON CONFLICT", "UNION ALL", "requested_techniques_at =",
		"ORDER BY number", "SELECT ask_before_showing", "NOT EXISTS", "SET close_reason",
	} {
		t.Run(failOn, func(t *testing.T) {
			t.Parallel()

			ts := &tellers{}
			ts.say("Which of the two numbers has more tens?", nil)
			w := worldOf(t, gate(t, adultFacts, config.RungAuthor), []learning.Option{learning.WithWords(ts.tell)},
				func(p *pgxpool.Pool) rpc.DB { return &failing{Pool: p, failOn: failOn} })
			l := w.learner(t)
			trapped := []*v1.TappedChoice{tap(1, 1, "12"), tap(2, 1, "30")}
			a := w.begin(t, l, l.child, "pairs").GetAttemptId()
			var errs []error
			note := func(_ any, err error) { errs = append(errs, err) }
			note(w.attempts.GetHint(l.ctx, &v1.GetHintRequest{ProfileId: l.child, AttemptId: a}))
			note(w.submit(l, l.child, a, trapped...))
			note(w.attempts.GetHint(l.ctx, &v1.GetHintRequest{ProfileId: l.child, AttemptId: a}))
			note(w.rungWords(l, l.child, a, 1))
			note(w.explain(l, l.child, a, 1))
			note(w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: l.child, AttemptId: a}))
			for range 3 {
				note(w.submit(l, l.child, a, trapped...))
			}
			note(w.attempts.ShowSolution(l.ctx, &v1.ShowSolutionRequest{ProfileId: l.child, AttemptId: a,
				IdempotencyKey: uuid.NewString(), Action: v1.Action_ACTION_ADULT}))
			b := w.begin(t, l, l.adult, "pick").GetAttemptId()
			note(w.submit(l, l.adult, b, tap(1, 1, "three and four")))
			note(w.attempts.GetCorrectAnswerWords(l.ctx, &v1.GetCorrectAnswerWordsRequest{ProfileId: l.adult, AttemptId: b,
				IdempotencyKey: uuid.NewString()}))
			unavailable := false
			for _, err := range errs {
				if connect.CodeOf(err) == connect.CodeUnavailable {
					unavailable = true
				}
			}
			if !unavailable {
				t.Errorf("no request was unavailable: %v", errs)
			}
		})
	}
}

// A repeat of the words of a rung is refused once the wrong answer is no longer
// the last, or the attempt lapsed; the words after a correct answer of an
// attempt on a package the index does not know wait for the deployment.
func TestARepeatOfTheWordsChecksTheAttemptAgain(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	trapped := []*v1.TappedChoice{tap(1, 1, "12"), tap(2, 1, "30")}
	a := w.begin(t, l, l.child, "pairs").GetAttemptId()
	w.wrongs(t, l, l.child, a, trapped)
	wrong := int32(1)
	req := &v1.GetRungWordsRequest{ProfileId: l.child, AttemptId: a, IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong}
	if _, err := w.attempts.GetRungWords(l.ctx, req); err != nil {
		t.Fatal(err)
	}
	w.wrongs(t, l, l.child, a, trapped)
	_, err := w.attempts.GetRungWords(l.ctx, req)
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_CURRENT); r.GetWrongAnswers() != 2 {
		t.Errorf("the repeat's refusal = %v, want two wrong answers", r)
	}
	w.clock.move(store.AbandonedAfter + time.Minute)
	_, err = w.attempts.GetRungWords(l.ctx, req)
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED); r.GetCloseReason() != v1.CloseReason_CLOSE_REASON_ABANDONED {
		t.Errorf("the repeat on a lapsed attempt = %v, want abandoned", r)
	}

	gone := w.attempt(t, l.adult, "sha256:"+strings.Repeat("0", 64), "pick")
	w.exec(t, `UPDATE attempts SET close_reason = 'correct', closed_at = now() WHERE id = $1`, gone)
	_, err = w.attempts.GetCorrectAnswerWords(l.ctx, &v1.GetCorrectAnswerWordsRequest{ProfileId: l.adult, AttemptId: gone,
		IdempotencyKey: uuid.NewString()})
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_PACKAGE_UNKNOWN)
}

// Two requests beside each other on one rung keep one set of words: the words
// of a rung worded meanwhile are those the attempt keeps, and "explain
// differently" asked for meanwhile takes the right, so this one is "already
// asked".
func TestRequestsBesideEachOtherOnARungKeepOneWords(t *testing.T) {
	t.Parallel()

	ts := &tellers{}
	w := newWordsWorld(t, ts, nil)
	l := w.learner(t)
	a := w.begin(t, l, l.child, "pairs").GetAttemptId()
	w.wrongs(t, l, l.child, a, []*v1.TappedChoice{tap(1, 1, "12"), tap(2, 1, "30")})
	beside := func(sql string) {
		ts.mu.Lock()
		defer ts.mu.Unlock()
		ts.next = func(context.Context, tutor.WordsRequest) tutor.WordsAnswer {
			w.exec(t, sql, l.child, a)
			return tutor.WordsAnswer{Question: "Which of the two numbers has more tens?", Outcome: tutor.Outcome{Kind: tutor.Answered}}
		}
	}
	beside(`INSERT INTO attempt_rungs (profile_id, attempt_id, rung, source, words) VALUES ($1, $2, 1, 'fallback', 'Beside.')`)
	res, err := w.rungWords(l, l.child, a, 1)
	if err != nil || res.GetWords() != "Beside." || !res.GetRepeated() {
		t.Errorf("the words of a rung worded meanwhile = %v (%v), want those kept", res, err)
	}
	beside(`UPDATE attempt_rungs SET explained_at = now(), explained_source = 'fallback', explained_words = 'Other.'
		WHERE profile_id = $1 AND attempt_id = $2`)
	_, err = w.explain(l, l.child, a, 1)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ALREADY_ASKED)
	if r := w.hint(t, l, l.child, a).GetRung(); r.GetWords() != "Other." {
		t.Errorf("the rung after = %v, want the other words kept", r)
	}
}
