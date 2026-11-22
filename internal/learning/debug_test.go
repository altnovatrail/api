package learning_test

import (
	"context"
	"sync"
	"testing"

	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/tutor"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/debug"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/tutorstub"
)

// debugStub is the stub of tutor that answers as tutor does in the debug mode:
// it records each call by the route's record, and with a second model reads the
// work twice, the second reader's value its own when differ is set, and only
// where it saw the slot its own when it is not. With none set, a call must come
// with no debug mode, and none is recorded.
type debugStub struct {
	*tutorstub.Stub
	t      *testing.T
	mu     sync.Mutex
	differ bool
	none   bool
}

func (d *debugStub) recorded(r tutor.Route, c tutor.Call, reader int, answer string) {
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
	if reader == 2 {
		m = *r.Debug.Second
	}
	r.Debug.Record(tutor.DebugCall{Call: c, Reader: reader, Model: m, Answer: answer,
		Usage: &tutor.Usage{Model: m.ID, InputTokens: 1000, OutputTokens: 100, USD: 0.0123, Billed: true}})
}

func (d *debugStub) ReadStatement(ctx context.Context, p tutor.PaperTask, own string) tutor.StatementRead {
	read := d.Stub.ReadStatement(ctx, p, own)
	d.recorded(p.Route, tutor.CallStatement, 0, `{"tasks": []}`)
	return read
}

func (d *debugStub) ReadPaper(ctx context.Context, r tutor.PaperReading, own string) tutor.SheetRead {
	read := d.Stub.ReadPaper(ctx, r, own)
	d.recorded(r.Route, tutor.CallPaper, 1, `{"first": true}`)
	if r.Route.Debug != nil && r.Route.Debug.Second != nil && len(read.Observations) == 1 {
		second := proto.Clone(read.Observations[0]).(*observationv1.Observation)
		d.mu.Lock()
		if d.differ {
			second.GetAnswerSlots()[0].Read = &observationv1.AnswerSlotObservation_Value{Value: "86"}
		} else {
			// Where a reader saw the slot is no reading of it.
			second.GetAnswerSlots()[0].CropBox = &observationv1.CropBox{Ymin: 1, Xmin: 2, Ymax: 30, Xmax: 40}
		}
		d.mu.Unlock()
		read.Observations = append(read.Observations, second)
		d.recorded(r.Route, tutor.CallPaper, 2, `{"second": true}`)
	}
	return read
}

func (d *debugStub) TellPaperWords(ctx context.Context, w tutor.PaperWordsRequest, own string) tutor.WordsAnswer {
	told := d.Stub.TellPaperWords(ctx, w, own)
	d.recorded(w.Route, tutor.CallPaperHint, 0, `{"hint": "?"}`)
	return told
}

// In the debug mode the tutor's words of a task from paper go by the account's
// first model, past the list, and the store keeps their call as an action of
// words (R97).
func TestTheDebugModeRecordsTheTutorsWords(t *testing.T) {
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
	a := w.beginPage(t, l, l.adult, 1)[0]
	if res, err := w.opening(t, l, l.adult, a, uuid.NewString()); err != nil || res.GetWords() == "" {
		t.Fatalf("the opening question = %v, %v", res, err)
	}
	taken := store.Take(l.accountID)
	if len(taken) != 1 || taken[0].GetKind() != v1.DebugActionKind_DEBUG_ACTION_KIND_WORDS || taken[0].GetAttemptId() != a ||
		len(taken[0].GetCalls()) != 1 || taken[0].GetCalls()[0].GetKind() != v1.DebugCallKind_DEBUG_CALL_KIND_TUTOR_WORDS {
		t.Errorf("the actions = %v, want the words with their one call", taken)
	}
}

// In the debug mode the calls of a Check of a task from paper go by the
// account's models, and the store keeps each, the solver's among them, until
// they are taken, with its cost (R97). A second model reads the work beside the
// first, the two joined as the pair is: a value they read differently is
// unreadable, and the action says the readers differ; one they agree on is
// judged.
func TestTheDebugModeRecordsATaskFromPaper(t *testing.T) {
	t.Parallel()

	g := stubGate(t)
	store := debug.New(true)
	ds := &debugStub{Stub: tutorstub.New(g), t: t, differ: true}
	w := worldOf(t, g, []learning.Option{learning.WithTutor(ds), learning.WithDebug(store)})
	l := w.learner(t)
	if _, err := store.SetChoice(l.accountID, &v1.DebugChoice{
		First:  &v1.DebugModelChoice{Model: "claude-sonnet-5-5", Effort: v1.DebugEffort_DEBUG_EFFORT_LOW, Thinking: false},
		Second: &v1.DebugModelChoice{Model: "claude-haiku-5-5", Effort: v1.DebugEffort_DEBUG_EFFORT_MEDIUM, Thinking: true},
	}); err != nil {
		t.Fatal(err)
	}
	a := w.beginPage(t, l, l.adult, 1)[0]
	submit := func() *v1.SubmitWorkResponse {
		t.Helper()
		res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
			Work: paperWork(t, formulaStatement, pngOf(t, 90, 30))})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if s := slotOf(t, submit()); s.GetVerdict() == "correct" {
		t.Errorf("two readings that differ = %v, want no verdict of a value", s)
	}
	ds.mu.Lock()
	ds.differ = false
	ds.mu.Unlock()
	if s := slotOf(t, submit()); s.GetVerdict() != "correct" {
		t.Errorf("two readings of 85 = %v, want correct", s)
	}

	taken := store.Take(l.accountID)
	if len(taken) != 2 || !taken[0].GetReadersDiffer() || taken[1].GetReadersDiffer() {
		t.Fatalf("the actions = %v, want two, the first with readers that differ", taken)
	}
	kinds := map[v1.DebugCallKind]int{}
	for _, c := range taken[0].GetCalls() {
		kinds[c.GetKind()]++
		if c.GetKind() == v1.DebugCallKind_DEBUG_CALL_KIND_SOLVER {
			if c.GetAnswer() == "" || c.GetCostMicroUsd() != 0 {
				t.Errorf("the solver's call = %v, want its key and no cost", c)
			}
			continue
		}
		if c.GetCostMicroUsd() != 12300 || c.GetInputTokens() != 1000 {
			t.Errorf("a call of a model = %v, want its tokens and 12300 micro-dollars", c)
		}
		if c.GetReader() == 2 && (c.GetModel() != "claude-haiku-5-5" || !c.GetThinking()) {
			t.Errorf("the second reader = %v, want Haiku thinking", c)
		}
		if c.GetReader() == 1 && (c.GetModel() != "claude-sonnet-5-5" || c.GetThinking() || c.GetEffort() != v1.DebugEffort_DEBUG_EFFORT_LOW) {
			t.Errorf("the first reader = %v, want Sonnet at low with no thinking", c)
		}
	}
	if taken[0].GetKind() != v1.DebugActionKind_DEBUG_ACTION_KIND_CHECK || taken[0].GetAttemptId() != a ||
		kinds[v1.DebugCallKind_DEBUG_CALL_KIND_STATEMENT_READING] != 1 || kinds[v1.DebugCallKind_DEBUG_CALL_KIND_SOLVER] != 1 ||
		kinds[v1.DebugCallKind_DEBUG_CALL_KIND_WORK_READING] != 2 {
		t.Errorf("the first Check = %v, want the statement, the solver and two readers", taken[0])
	}
	// The key kept, the second Check reads the work alone.
	if len(taken[1].GetCalls()) != 2 {
		t.Errorf("the second Check = %v, want the two readers alone", taken[1])
	}
	if again := store.Take(l.accountID); len(again) != 0 {
		t.Errorf("a second take = %v, want none", again)
	}
}

// Without the flag the debug mode is the account's by the right of its row: an
// account without it has no action and no model of its choice, which takes no
// call past the list, and one given it has them from its next request; one whose
// right is taken again has neither, its choice kept or not (R97).
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
