package learning_test

import (
	"context"
	"sync"
	"testing"

	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/tutor"
	"github.com/google/uuid"

	"github.com/altnovatrail/api/internal/debug"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/tutorstub"
)

// debugStub is the stub of tutor that answers as tutor does in the debug mode:
// it records each call by the route's record, by the first model. With none
// set, a call must come with no debug mode, and none is recorded.
type debugStub struct {
	*tutorstub.Stub
	t    *testing.T
	mu   sync.Mutex
	none bool
}

func (d *debugStub) recorded(r tutor.Route, c tutor.Call, answer string) {
	d.mu.Lock()
	none := d.none
	d.mu.Unlock()
	switch {
	case none && r.Debug != nil:
		d.t.Errorf("a call of %v had the debug mode of an account without its right", c)
		return
	case none:
		return
	case r.Debug == nil || r.Debug.First == nil:
		d.t.Errorf("a call of %v had no models of the debug mode", c)
		return
	}
	m := *r.Debug.First
	r.Debug.Record(tutor.DebugCall{Call: c, Attempt: 1, Model: m, Answer: answer,
		Usage: &tutor.Usage{Model: m.ID, InputTokens: 1000, OutputTokens: 100, USD: 0.0123, Billed: true}})
}

func (d *debugStub) ReadStatement(ctx context.Context, p tutor.PaperTask, own string) tutor.StatementRead {
	read := d.Stub.ReadStatement(ctx, p, own)
	d.recorded(p.Route, tutor.CallStatement, `{"tasks": []}`)
	return read
}

func (d *debugStub) CheckPaper(ctx context.Context, c tutor.PaperCheck, own string) tutor.PaperChecked {
	checked := d.Stub.CheckPaper(ctx, c, own)
	d.recorded(c.Route, tutor.CallChecking, `{"slots": []}`)
	return checked
}

func (d *debugStub) TellPaperWords(ctx context.Context, w tutor.PaperWordsRequest, own string) tutor.WordsAnswer {
	told := d.Stub.TellPaperWords(ctx, w, own)
	d.recorded(w.Route, tutor.CallPaperHint, `{"hint": "?"}`)
	return told
}

// In the debug mode the calls of a task from paper go by the account's first
// model, past the list, and the store keeps each with its cost until it is
// taken: the opening question as an action of words of one Check, a
// submission as a Check of one, and "explain differently" as an action of
// words of the tutor's words alone.
func TestTheDebugModeRecordsATaskFromPaper(t *testing.T) {
	t.Parallel()

	g := stubGate(t)
	store := debug.New(true)
	w := worldOf(t, g, []learning.Option{learning.WithTutor(&debugStub{Stub: tutorstub.New(g), t: t}), learning.WithDebug(store)})
	l := w.learner(t)
	if _, err := store.SetChoice(l.accountID, &v1.DebugChoice{
		First: &v1.DebugModelChoice{Model: "claude-sonnet-5-5", Effort: v1.DebugEffort_DEBUG_EFFORT_LOW},
	}); err != nil {
		t.Fatal(err)
	}
	page := w.beginPage(t, l, l.adult, 2)
	if res, err := w.opening(t, l, l.adult, page[0], uuid.NewString()); err != nil || res.GetWords() == "" {
		t.Fatalf("the opening question = %v, %v", res, err)
	}
	res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: page[1], IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 40, 30))})
	if err != nil || res.GetWrongAnswers() != 1 {
		t.Fatalf("the Check = %v, %v; want a wrong answer", res, err)
	}
	if _, err := w.explain(l, l.adult, page[1], 1); err != nil {
		t.Fatal(err)
	}
	taken := store.Take(l.accountID)
	want := []struct {
		action  v1.DebugActionKind
		attempt string
		call    v1.DebugCallKind
	}{
		{v1.DebugActionKind_DEBUG_ACTION_KIND_WORDS, page[0], v1.DebugCallKind_DEBUG_CALL_KIND_TASK_CHECKING},
		{v1.DebugActionKind_DEBUG_ACTION_KIND_CHECK, page[1], v1.DebugCallKind_DEBUG_CALL_KIND_TASK_CHECKING},
		{v1.DebugActionKind_DEBUG_ACTION_KIND_WORDS, page[1], v1.DebugCallKind_DEBUG_CALL_KIND_TUTOR_WORDS},
	}
	if len(taken) != len(want) {
		t.Fatalf("the actions = %v, want %d", taken, len(want))
	}
	for i, a := range taken {
		calls := a.GetCalls()
		if a.GetKind() != want[i].action || a.GetAttemptId() != want[i].attempt || len(calls) != 1 || calls[0].GetKind() != want[i].call {
			t.Errorf("action %d = %v, want %v on %s with one call of %v", i+1, a, want[i].action, want[i].attempt, want[i].call)
			continue
		}
		if c := calls[0]; c.GetCostMicroUsd() != 12300 || c.GetInputTokens() != 1000 || c.GetModel() != "claude-sonnet-5-5" ||
			c.GetEffort() != v1.DebugEffort_DEBUG_EFFORT_LOW || c.GetAttempt() != 1 {
			t.Errorf("the call of action %d = %v, want Sonnet at low, its tokens and 12300 micro-dollars", i+1, c)
		}
	}
	if again := store.Take(l.accountID); len(again) != 0 {
		t.Errorf("a second take = %v, want none", again)
	}
}

// Without the flag the debug mode is the account's by the right of its row: an
// account without it has no action and no model of its choice, which takes no
// call past the list, and one given it has them from its next request; one
// whose right is taken again has neither, its choice kept or not.
func TestTheDebugModeIsAnAccountsByItsRight(t *testing.T) {
	t.Parallel()

	g := stubGate(t)
	store := debug.New(false)
	ds := &debugStub{Stub: tutorstub.New(g), t: t, none: true}
	w := worldOf(t, g, []learning.Option{learning.WithTutor(ds), learning.WithDebug(store)})
	l := w.learner(t)
	if _, err := store.SetChoice(l.accountID, &v1.DebugChoice{
		First: &v1.DebugModelChoice{Model: "claude-sonnet-5-5", Effort: v1.DebugEffort_DEBUG_EFFORT_LOW},
	}); err != nil {
		t.Fatal(err)
	}
	words := func() {
		t.Helper()
		a := w.beginPage(t, l, l.adult, 1)[0]
		if res, err := w.opening(t, l, l.adult, a, uuid.NewString()); err != nil || res.GetWords() == "" {
			t.Fatalf("the opening question = %v, %v", res, err)
		}
	}
	words()
	if taken := store.Take(l.accountID); len(taken) != 0 {
		t.Errorf("the actions of an account without the right = %v, want none", taken)
	}

	w.exec(t, `UPDATE accounts SET debug_models = true WHERE id = $1`, l.accountID)
	ds.mu.Lock()
	ds.none = false
	ds.mu.Unlock()
	words()
	if taken := store.Take(l.accountID); len(taken) != 1 || taken[0].GetKind() != v1.DebugActionKind_DEBUG_ACTION_KIND_WORDS {
		t.Errorf("the actions of the account given the right = %v, want the words", taken)
	}

	w.exec(t, `UPDATE accounts SET debug_models = false WHERE id = $1`, l.accountID)
	ds.mu.Lock()
	ds.none = true
	ds.mu.Unlock()
	words()
	if taken := store.Take(l.accountID); len(taken) != 0 {
		t.Errorf("the actions of the account whose right was taken = %v, want none", taken)
	}
}
