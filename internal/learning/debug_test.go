package learning_test

import (
	"context"
	"reflect"
	"slices"
	"sync"
	"testing"

	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	"github.com/altnovatrail/tutor/admission"
	"github.com/google/uuid"

	"github.com/altnovatrail/api/internal/debug"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/tutorstub"
)

// debugStub is the stub of tutor that answers as tutor does in the debug mode:
// it records each call of a model by the route's record, a call of a task
// from paper by the first model and any other by a model of the list, each
// reader of a sheet with its place, and keeps which of its methods were
// called. With none set, a call must come with no debug mode, and none is
// recorded.
type debugStub struct {
	*tutorstub.Stub
	t      *testing.T
	mu     sync.Mutex
	none   bool
	called map[string]bool
}

// paperCalls are the calls whose models the debug mode chooses.
var paperCalls = []tutor.Call{tutor.CallFinding, tutor.CallStatement, tutor.CallChecking, tutor.CallPaperHint, tutor.CallDrawing}

// listModel is the model of the list the stub records a call of a module by.
var listModel = admission.Model{Provider: "anthropic", ID: "claude-haiku-5-5", Effort: "medium"}

// recorded records the call c of the stub's method on the route r, by readers
// models, each with its place, or by one with none.
func (d *debugStub) recorded(method string, r tutor.Route, c tutor.Call, answer string, readers int) {
	d.mu.Lock()
	none := d.none
	if d.called == nil {
		d.called = map[string]bool{}
	}
	d.called[method] = true
	d.mu.Unlock()
	switch {
	case none && r.Debug != nil:
		d.t.Errorf("a call of %v had the debug mode of an account without its right", c)
		return
	case none:
		return
	case r.Debug == nil || r.Debug.Record == nil:
		d.t.Errorf("a call of %v by %s went past the record of the debug mode", c, method)
		return
	}
	m := listModel
	if slices.Contains(paperCalls, c) {
		if r.Debug.First == nil {
			d.t.Errorf("a call of %v had no models of the debug mode", c)
			return
		}
		m = *r.Debug.First
	}
	for reader := range max(readers, 1) {
		place := 0
		if readers > 0 {
			place = reader + 1
		}
		r.Debug.Record(tutor.DebugCall{Call: c, Attempt: 1, Reader: place, Model: m, Answer: answer,
			Usage: &tutor.Usage{Model: m.ID, InputTokens: 1000, OutputTokens: 100, USD: 0.0123, Billed: true}})
	}
}

func (d *debugStub) ReadSheet(ctx context.Context, r tutor.SheetReading, task formatv0.Task, own string) tutor.SheetRead {
	read := d.Stub.ReadSheet(ctx, r, task, own)
	d.recorded("ReadSheet", r.Route, tutor.CallSheet, `{"parts": []}`, 2)
	return read
}

func (d *debugStub) TellWords(ctx context.Context, w tutor.WordsRequest, own string) tutor.WordsAnswer {
	told := d.Stub.TellWords(ctx, w, own)
	d.recorded("TellWords", w.Route, map[bool]tutor.Call{true: tutor.CallWords, false: tutor.CallHint}[len(w.Crop) > 0], `{"hint": "?"}`, 0)
	return told
}

func (d *debugStub) TellCorrectWords(ctx context.Context, c tutor.CorrectRequest, own string) tutor.CorrectAnswer {
	told := d.Stub.TellCorrectWords(ctx, c, own)
	d.recorded("TellCorrectWords", c.Route, tutor.CallCorrect, `{"words": "?"}`, 0)
	return told
}

func (d *debugStub) ReadEssay(ctx context.Context, r tutor.EssayReading, task formatv0.Task, own string) tutor.EssayRead {
	read := d.Stub.ReadEssay(ctx, r, task, own)
	d.recorded("ReadEssay", r.Route, tutor.CallEssay, `{"text": "?"}`, 0)
	return read
}

func (d *debugStub) AssessEssay(ctx context.Context, r tutor.RubricAssessment, own string) tutor.RubricAssessed {
	assessed := d.Stub.AssessEssay(ctx, r, own)
	d.recorded("AssessEssay", r.Route, tutor.CallRubric, `{"criteria": []}`, 0)
	return assessed
}

func (d *debugStub) FindTasks(ctx context.Context, f tutor.TaskFinding, own string) tutor.TasksFound {
	found := d.Stub.FindTasks(ctx, f, own)
	d.recorded("FindTasks", f.Route, tutor.CallFinding, `{"tasks": []}`, 0)
	return found
}

func (d *debugStub) ReadStatement(ctx context.Context, p tutor.PaperTask, own string) tutor.StatementRead {
	read := d.Stub.ReadStatement(ctx, p, own)
	d.recorded("ReadStatement", p.Route, tutor.CallStatement, `{"tasks": []}`, 0)
	return read
}

func (d *debugStub) CheckPaper(ctx context.Context, c tutor.PaperCheck, own string) tutor.PaperChecked {
	checked := d.Stub.CheckPaper(ctx, c, own)
	d.recorded("CheckPaper", c.Route, tutor.CallChecking, `{"slots": []}`, 0)
	return checked
}

func (d *debugStub) TellPaperWords(ctx context.Context, w tutor.PaperWordsRequest, own string) tutor.WordsAnswer {
	told := d.Stub.TellPaperWords(ctx, w, own)
	d.recorded("TellPaperWords", w.Route, tutor.CallPaperHint, `{"hint": "?"}`, 0)
	return told
}

func (d *debugStub) GrownUp(ctx context.Context, g tutor.GrownUpRequest, own string) tutor.GrownUpAnswer {
	drawn := d.Stub.GrownUp(ctx, g, own)
	d.recorded("GrownUp", g.Route, tutor.CallDrawing, `{"say": "?"}`, 0)
	return drawn
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

// In the debug mode grown-up mode goes by the account's first model too, past
// the list, as an action of words with one call of the drawing.
func TestTheDebugModeRecordsGrownUpMode(t *testing.T) {
	t.Parallel()

	g := stubGate(t)
	store := debug.New(true)
	w := worldOf(t, g, []learning.Option{learning.WithTutor(&debugStub{Stub: tutorstub.New(g), t: t}), learning.WithDebug(store),
		learning.WithAdmits(func(tutor.Call) bool { return false })})
	l := w.learner(t)
	if _, err := store.SetChoice(l.accountID, &v1.DebugChoice{
		First: &v1.DebugModelChoice{Model: "claude-sonnet-5-5", Effort: v1.DebugEffort_DEBUG_EFFORT_LOW},
	}); err != nil {
		t.Fatal(err)
	}
	a := w.beginPage(t, l, l.adult, 1)[0]
	if res, err := w.grownUp(t, l, l.adult, a, uuid.NewString(), "Draw"); err != nil || res.GetOutcome() != nil || len(res.GetStrokes()) != 1 {
		t.Fatalf("grown-up mode = %v, %v; want the stub's act past the list", res, err)
	}
	taken := store.Take(l.accountID)
	if len(taken) != 1 || taken[0].GetKind() != v1.DebugActionKind_DEBUG_ACTION_KIND_WORDS || taken[0].GetAttemptId() != a ||
		len(taken[0].GetCalls()) != 1 || taken[0].GetCalls()[0].GetKind() != v1.DebugCallKind_DEBUG_CALL_KIND_DRAWING {
		t.Errorf("the actions = %v, want one of words with one call of the drawing", taken)
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

// In the debug mode every call of a model the account's requests make is
// recorded with its cost, wherever it was made — a task from paper, a task of a
// module, an essay, grown-up mode —, each request as its action; the models of
// the choice serve a task from paper alone. Every method of tutor that calls a
// model is driven here, so that a request that called one past the record
// fails the test.
func TestTheDebugModeRecordsEveryCallOfAModel(t *testing.T) {
	t.Parallel()

	g := stubGate(t)
	store := debug.New(true)
	ds := &debugStub{Stub: tutorstub.New(g), t: t}
	w := worldOf(t, g, []learning.Option{learning.WithTutor(ds), learning.WithDebug(store)})
	l := w.learner(t)
	if _, err := store.SetChoice(l.accountID, &v1.DebugChoice{
		First: &v1.DebugModelChoice{Model: "claude-sonnet-5-5", Effort: v1.DebugEffort_DEBUG_EFFORT_LOW},
	}); err != nil {
		t.Fatal(err)
	}
	// A profile holds one attempt of a module open: each begins before its requests.
	page := w.beginPage(t, l, l.adult, 1)[0]
	var none, sum, story, pick string
	submit := func(width, height int) error {
		_, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: sum,
			IdempotencyKey: uuid.NewString(), Work: sheet(pngOf(t, width, height))})
		return err
	}
	wrong := int32(1)
	var (
		check = v1.DebugActionKind_DEBUG_ACTION_KIND_CHECK
		words = v1.DebugActionKind_DEBUG_ACTION_KIND_WORDS
		read  = v1.DebugCallKind_DEBUG_CALL_KIND_WORK_READING
		told  = v1.DebugCallKind_DEBUG_CALL_KIND_TUTOR_WORDS
		drawn = v1.DebugCallKind_DEBUG_CALL_KIND_DRAWING
	)
	for _, tc := range []struct {
		name    string
		request func() error
		action  v1.DebugActionKind
		attempt *string
		calls   []v1.DebugCallKind
		readers []int32
	}{
		{"FindTasks", func() error {
			_, err := w.findTasks(l, l.adult, photoOf(t, 300, 400), uuid.NewString())
			return err
		}, v1.DebugActionKind_DEBUG_ACTION_KIND_FIND_TASKS, &none, []v1.DebugCallKind{v1.DebugCallKind_DEBUG_CALL_KIND_TASK_FINDING}, nil},
		{"ReadStatement", func() error {
			_, err := w.recognise(t, l, page, formulaStatement, uuid.NewString())
			return err
		}, check, &page, []v1.DebugCallKind{v1.DebugCallKind_DEBUG_CALL_KIND_STATEMENT_READING}, nil},
		{"SubmitWork of paper", func() error {
			_, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: page, IdempotencyKey: uuid.NewString(),
				Work: paperWork(t, formulaStatement, pngOf(t, 40, 30))})
			return err
		}, check, &page, []v1.DebugCallKind{v1.DebugCallKind_DEBUG_CALL_KIND_TASK_CHECKING}, nil},
		{"AskTutor", func() error {
			_, err := w.ask(t, l, page, uuid.NewString(), "Where do I start?")
			return err
		}, words, &page, []v1.DebugCallKind{told}, nil},
		{"AskGrownUp of paper", func() error {
			_, err := w.grownUp(t, l, l.adult, page, uuid.NewString(), "Draw")
			return err
		}, words, &page, []v1.DebugCallKind{drawn}, nil},
		{"SubmitWork of a module", func() error {
			sum = w.begin(t, l, l.adult, "apples").GetAttemptId()
			return submit(10, 40)
		}, check, &sum, []v1.DebugCallKind{read, read}, []int32{1, 2}},
		{"GetRungWords", func() error {
			_, err := w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: sum,
				IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong, Work: sheet(pngOf(t, 10, 40))})
			return err
		}, words, &sum, []v1.DebugCallKind{told}, nil},
		{"ExplainDifferently", func() error {
			_, err := w.explain(l, l.adult, sum, 1)
			return err
		}, words, &sum, []v1.DebugCallKind{told}, nil},
		{"GetCorrectAnswerWords", func() error {
			if err := submit(40, 10); err != nil {
				return err
			}
			// The reading that solved the task is an action of its own, taken here.
			store.Take(l.accountID)
			_, err := w.praised(l, l.adult, sum, sheet(pngOf(t, 40, 10)))
			return err
		}, words, &sum, []v1.DebugCallKind{told}, nil},
		{"SubmitEssay", func() error {
			story = w.begin(t, l, l.adult, "story").GetAttemptId()
			_, err := w.attempts.SubmitEssay(l.ctx, &v1.SubmitEssayRequest{ProfileId: l.adult, AttemptId: story,
				IdempotencyKey: uuid.NewString(), Crop: pngOf(t, 40, 10)})
			return err
		}, check, &story, []v1.DebugCallKind{read, v1.DebugCallKind_DEBUG_CALL_KIND_ESSAY_ASSESSMENT}, nil},
		{"AskGrownUp of a module", func() error {
			pick = w.begin(t, l, l.adult, "pick").GetAttemptId()
			_, err := w.grownUp(t, l, l.adult, pick, uuid.NewString(), "Solve it on the sheet")
			return err
		}, words, &pick, []v1.DebugCallKind{drawn}, nil},
	} {
		if err := tc.request(); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		taken := store.Take(l.accountID)
		if len(taken) != 1 || taken[0].GetKind() != tc.action || taken[0].GetAttemptId() != *tc.attempt {
			t.Errorf("%s: the actions = %v, want one %v on %q", tc.name, taken, tc.action, *tc.attempt)
			continue
		}
		calls := taken[0].GetCalls()
		if len(calls) != len(tc.calls) {
			t.Errorf("%s: the calls = %v, want %v", tc.name, calls, tc.calls)
			continue
		}
		for i, c := range calls {
			reader := int32(0)
			if tc.readers != nil {
				reader = tc.readers[i]
			}
			if c.GetKind() != tc.calls[i] || c.GetReader() != reader || c.GetCostMicroUsd() != 12300 || c.GetInputTokens() != 1000 {
				t.Errorf("%s: call %d = %v, want %v of reader %d, its tokens and its cost", tc.name, i+1, c, tc.calls[i], reader)
			}
			// The choice serves a task from paper alone.
			if paper := *tc.attempt == page || tc.action == v1.DebugActionKind_DEBUG_ACTION_KIND_FIND_TASKS ||
				c.GetKind() == drawn; (c.GetModel() == "claude-sonnet-5-5") != paper {
				t.Errorf("%s: call %d is by %s", tc.name, i+1, c.GetModel())
			}
		}
	}
	// Each method of tutor that calls a model, the stub's, by the route of its
	// request, was driven by a request above.
	stub := reflect.TypeFor[*tutorstub.Stub]()
	route := reflect.TypeFor[tutor.Route]()
	for i := range stub.NumMethod() {
		m := stub.Method(i)
		if m.Type.NumIn() < 3 || m.Type.In(2).Kind() != reflect.Struct {
			continue
		}
		if f, ok := m.Type.In(2).FieldByName("Route"); ok && f.Type == route && !ds.called[m.Name] {
			t.Errorf("no request above calls %s: the record of the debug mode is not tested on it", m.Name)
		}
	}
}
