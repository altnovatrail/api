package learning_test

import (
	"slices"
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/google/uuid"
)

// An attempt gives the options of its answer at its beginning and every time it
// is read again, in the same order: the key, the trap values and the whole
// numbers next to the key of a number, the options of a slot bound choice, and
// none for a task with a rubric.
func TestAnAttemptGivesTheOptionsOfItsAnswer(t *testing.T) {
	t.Parallel()

	w := newEssayWorld(t, &essays{})
	l := w.learner(t)
	for task, want := range map[string][]string{
		"apples": {"7", "3", "34", "8", "6"},
		"pick":   {"three and four", "five"},
	} {
		start := w.begin(t, l, l.adult, task)
		got := start.GetAnswerOptions()
		if !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(want))) {
			t.Errorf("%s: answer options = %q, want %q in some order", task, got, want)
		}
		open, err := w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: l.adult, AttemptId: start.GetAttemptId()})
		if err != nil || !slices.Equal(open.GetAttempt().GetStart().GetAnswerOptions(), got) {
			t.Errorf("%s: read again = %q (%v), want %q", task, open.GetAttempt().GetStart().GetAnswerOptions(), err, got)
		}
	}
	if got := w.begin(t, l, l.adult, "story").GetAnswerOptions(); got != nil {
		t.Errorf("a task with a rubric has answer options %q", got)
	}
}

// An option tapped on a task whose slot AI reads is judged by the key, with no
// work and no reading: a wrong one is a wrong answer, whose rung's words go with
// no work, and the key solves the task, whose words after it take no work
// either. A tap that is none of the options, beside the work, twice or on
// another slot is malformed.
func TestAnOptionTappedIsJudgedByTheKey(t *testing.T) {
	t.Parallel()

	ts, rs := &tellers{}, &readers{}
	w := newWordsWorld(t, ts, rs)
	l := w.learner(t)
	a := w.begin(t, l, l.adult, "apples").GetAttemptId()
	for name, req := range map[string]*v1.SubmitWorkRequest{
		"none of the options": {TappedChoices: []*v1.TappedChoice{tap(1, 1, "9")}},
		"beside the work":     {TappedChoices: []*v1.TappedChoice{tap(1, 1, "7")}, Work: sheet(crop(t))},
		"twice":               {TappedChoices: []*v1.TappedChoice{tap(1, 1, "3"), tap(1, 1, "7")}},
		"another slot":        {TappedChoices: []*v1.TappedChoice{tap(1, 2, "7")}},
	} {
		req.ProfileId, req.AttemptId, req.IdempotencyKey = l.adult, a, uuid.NewString()
		_, err := w.attempts.SubmitWork(l.ctx, req)
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: %v, want invalid_argument", name, err)
		}
	}

	res, err := w.submit(l, l.adult, a, tap(1, 1, "3"))
	if err != nil {
		t.Fatal(err)
	}
	slot := res.GetItems()[0].GetAnswerSlots()[0]
	if res.GetWrongAnswers() != 1 || slot.GetVerdict() != "not_yet" || slot.GetValue() != "3" || res.GetSolved() != nil {
		t.Errorf("a wrong option = %v, want not_yet on 3 and one wrong answer", res)
	}
	var observed bool
	if err := w.pool.QueryRow(t.Context(), `SELECT observation IS NOT NULL FROM submissions WHERE attempt_id = $1`, a).
		Scan(&observed); err != nil || observed {
		t.Errorf("the submission keeps an observation: %v (%v)", observed, err)
	}

	ts.say("Which number did you start from?", nil)
	one := int32(1)
	_, err = w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &one, Work: sheet(crop(t))})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	words, err := w.rungWords(l, l.adult, a, 1)
	if err != nil || words.GetWords() != "Which number did you start from?" {
		t.Fatalf("the words of the rung = %v (%v)", words, err)
	}
	if asked := ts.last(); asked.Crop != nil || asked.Found != "first-seen" || asked.Key != "7" {
		t.Errorf("the call = %+v, want no crop, the trap found and the key", asked)
	}

	res, err = w.submit(l, l.adult, a, tap(1, 1, "7"))
	if err != nil || res.GetSolved() == nil || res.GetWrongAnswers() != 1 ||
		res.GetItems()[0].GetAnswerSlots()[0].GetValue() != "7" {
		t.Fatalf("the key = %v (%v), want the task solved", res, err)
	}
	if rs.count() != 0 {
		t.Errorf("AI read %d times, want none", rs.count())
	}
	_, err = w.attempts.GetCorrectAnswerWords(l.ctx, &v1.GetCorrectAnswerWordsRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), Work: sheet(crop(t))})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	if _, err := w.attempts.GetCorrectAnswerWords(l.ctx, &v1.GetCorrectAnswerWordsRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString()}); err != nil {
		t.Errorf("the words after the key with no work: %v", err)
	}
}

// A slot bound choice keeps its taps as they were.
func TestAnOptionOfASlotBoundChoiceIsTappedAsBefore(t *testing.T) {
	t.Parallel()

	w := newReadingWorld(t, &readers{})
	l := w.learner(t)
	a := w.begin(t, l, l.adult, "pick").GetAttemptId()
	if res, err := w.submit(l, l.adult, a, tap(1, 1, "five")); err != nil || res.GetWrongAnswers() != 1 {
		t.Errorf("a wrong option = %v (%v), want a wrong answer", res, err)
	}
	res, err := w.submit(l, l.adult, a, tap(1, 1, "three and four"))
	if err != nil || res.GetSolved() == nil {
		t.Errorf("the key = %v (%v), want the task solved", res, err)
	}
}
