package debug_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/tutor"
	"github.com/altnovatrail/tutor/admission"
	"github.com/altnovatrail/tutor/provider"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/debug"
)

// The models are tutor's catalog of Claude, cheapest first, by their names
// without "Claude", with their prices per million tokens in micro-dollars, and
// thinking off only where the model turns it off, at high and below.
func TestTheModels(t *testing.T) {
	t.Parallel()

	models := debug.Models()
	if len(models) != 4 || models[0].GetId() != "claude-haiku-5-5" || models[0].GetName() != "Haiku 5.5" ||
		models[0].GetInputMicroUsd() != 100000 || models[0].GetOutputMicroUsd() != 500000 || len(models[0].GetEfforts()) != 5 {
		t.Fatalf("the models = %v", models)
	}
	for _, m := range models {
		off := len(m.GetThinkingOffEfforts())
		if want := map[string]int{"claude-haiku-5-5": 3, "claude-sonnet-5-5": 3}[m.GetId()]; off != want {
			t.Errorf("%s turns thinking off at %d efforts, want %d", m.GetId(), off, want)
		}
	}
}

// A choice holds per account; an effort not named is medium; a model the catalog
// does not name, an effort out of the enum, or thinking off where it stays on is
// refused, and the choice held stays. A second model is not read.
func TestTheChoice(t *testing.T) {
	t.Parallel()

	s := debug.New(false)
	if c := s.Choice("a"); c.GetFirst() != nil || c.GetSecond() != nil {
		t.Fatalf("a new account's choice = %v, want none", c)
	}
	set, err := s.SetChoice("a", &v1.DebugChoice{First: &v1.DebugModelChoice{Model: "claude-opus-5-5", Thinking: true},
		Second: &v1.DebugModelChoice{Model: "claude-opus-9"}})
	if err != nil || set.GetFirst().GetEffort() != v1.DebugEffort_DEBUG_EFFORT_MEDIUM || set.GetSecond() != nil {
		t.Fatalf("SetChoice = %v, %v; want Opus at medium", set, err)
	}
	if c := s.Choice("b"); c.GetFirst() != nil {
		t.Errorf("another account's choice = %v, want none", c)
	}
	for name, c := range map[string]*v1.DebugModelChoice{
		"no model":          {Model: "claude-opus-9", Thinking: true},
		"no effort":         {Model: "claude-haiku-5-5", Effort: 9, Thinking: true},
		"Opus not thinking": {Model: "claude-opus-5-5", Effort: v1.DebugEffort_DEBUG_EFFORT_LOW},
		"Haiku at xhigh":    {Model: "claude-haiku-5-5", Effort: v1.DebugEffort_DEBUG_EFFORT_XHIGH},
	} {
		if _, err := s.SetChoice("a", &v1.DebugChoice{First: c}); !errors.Is(err, debug.ErrChoice) {
			t.Errorf("%s: SetChoice error = %v, want ErrChoice", name, err)
		}
	}
	if c := s.Choice("a"); c.GetFirst().GetModel() != "claude-opus-5-5" {
		t.Errorf("the choice after a refusal = %v, want Opus kept", c)
	}
}

// An action gives tutor the account's models and keeps each call it records,
// with what failed as an outcome names it, until the account takes it; an
// action with no call is kept by none, and an account keeps its last 100.
func TestAnAction(t *testing.T) {
	t.Parallel()

	var none *debug.Store
	if a := none.Begin("a", v1.DebugActionKind_DEBUG_ACTION_KIND_CHECK, "x", time.Now()); a != nil || a.Chosen() || none.Chosen("a") {
		t.Fatal("a nil store began an action")
	} else {
		a.End()
		if r := a.Route(tutor.Route{}); r.Debug != nil {
			t.Error("a nil action set the mode")
		}
	}

	s := debug.New(false)
	if _, err := s.SetChoice("a", &v1.DebugChoice{
		First: &v1.DebugModelChoice{Model: "claude-sonnet-5-5", Effort: v1.DebugEffort_DEBUG_EFFORT_HIGH},
	}); err != nil {
		t.Fatal(err)
	}
	a := s.Begin("a", v1.DebugActionKind_DEBUG_ACTION_KIND_CHECK, "attempt", time.Unix(100, 0))
	r := a.Route(tutor.Route{AI: admissionv0.AiRouteOwnKey})
	sonnet := admission.Model{Provider: "anthropic", ID: "claude-sonnet-5-5", Effort: "high", NoThinking: true}
	if !s.Chosen("a") || s.Chosen("b") || !a.Chosen() {
		t.Error("the first model chosen is not the account's alone")
	}
	if r.Debug == nil || *r.Debug.First != sonnet {
		t.Fatalf("the route = %+v", r.Debug)
	}
	r.Debug.Record(tutor.DebugCall{Call: tutor.CallChecking, Attempt: 1, Model: sonnet, Answer: "{}",
		Usage: &tutor.Usage{Model: "claude-sonnet-5-5-20261101", InputTokens: 7, CacheWrites: []provider.CacheWrite{{Tokens: 3}, {Tokens: 4}},
			OutputTokens: 5, USD: 0.0000011, Latency: 1500 * time.Millisecond, Retries: 1}})
	// The attempts of a cascade keep their places.
	for i, err := range []error{&provider.LimitError{}, fmt.Errorf("x: %w", provider.ErrKeyRefused)} {
		r.Debug.Record(tutor.DebugCall{Call: tutor.CallStatement, Attempt: i + 1, Model: sonnet, Err: err})
	}
	// A call that broke off before the provider named its model.
	r.Debug.Record(tutor.DebugCall{Call: tutor.CallStatement, Attempt: 3, Model: sonnet, Err: errors.New("down"), Usage: &tutor.Usage{OutputTokens: 2}})
	r.Debug.Record(tutor.DebugCall{Call: tutor.CallPaperHint, Attempt: 1, Model: sonnet, Answer: "{}"})
	a.End()
	s.Begin("a", v1.DebugActionKind_DEBUG_ACTION_KIND_FIND_TASKS, "", time.Now()).End()

	taken := s.Take("a")
	if len(taken) != 1 {
		t.Fatalf("taken %v, want the one action with calls", taken)
	}
	want := &v1.DebugAction{Kind: v1.DebugActionKind_DEBUG_ACTION_KIND_CHECK, AttemptId: "attempt", At: taken[0].GetAt(),
		Calls: []*v1.DebugCall{
			{Kind: v1.DebugCallKind_DEBUG_CALL_KIND_TASK_CHECKING, Attempt: 1, Model: "claude-sonnet-5-5-20261101", Effort: v1.DebugEffort_DEBUG_EFFORT_HIGH,
				Answer: "{}", InputTokens: 7, CacheWriteTokens: 7, OutputTokens: 5, DurationMs: 1500, Retries: 1, CostMicroUsd: 2},
			{Kind: v1.DebugCallKind_DEBUG_CALL_KIND_STATEMENT_READING, Attempt: 1, Model: "claude-sonnet-5-5", Effort: v1.DebugEffort_DEBUG_EFFORT_HIGH, Failure: v1.Cause_CAUSE_LIMIT_REACHED},
			{Kind: v1.DebugCallKind_DEBUG_CALL_KIND_STATEMENT_READING, Attempt: 2, Model: "claude-sonnet-5-5", Effort: v1.DebugEffort_DEBUG_EFFORT_HIGH, Failure: v1.Cause_CAUSE_KEY_REFUSED},
			{Kind: v1.DebugCallKind_DEBUG_CALL_KIND_STATEMENT_READING, Attempt: 3, Model: "claude-sonnet-5-5", Effort: v1.DebugEffort_DEBUG_EFFORT_HIGH, OutputTokens: 2, Failure: v1.Cause_CAUSE_AI_FAILURE},
			{Kind: v1.DebugCallKind_DEBUG_CALL_KIND_TUTOR_WORDS, Attempt: 1, Model: "claude-sonnet-5-5", Effort: v1.DebugEffort_DEBUG_EFFORT_HIGH, Answer: "{}"},
		}}
	if taken[0].GetAt().AsTime().Unix() != 100 || !proto.Equal(taken[0], want) {
		t.Errorf("the action =\n%v\nwant\n%v", taken[0], want)
	}
	if again := s.Take("a"); len(again) != 0 {
		t.Errorf("a second take = %v", again)
	}

	// On platform AI a limit is the platform's.
	p := s.Begin("a", v1.DebugActionKind_DEBUG_ACTION_KIND_FIND_TASKS, "", time.Now())
	p.Route(tutor.Route{AI: admissionv0.AiRoutePlatformAi}).Debug.Record(tutor.DebugCall{Call: tutor.CallFinding, Err: &provider.LimitError{}})
	p.End()
	if c := s.Take("a")[0].GetCalls()[0]; c.GetFailure() != v1.Cause_CAUSE_PLATFORM_LIMIT || c.GetKind() != v1.DebugCallKind_DEBUG_CALL_KIND_TASK_FINDING {
		t.Errorf("a limit on platform AI = %v", c)
	}

	for i := range 105 {
		b := s.Begin("a", v1.DebugActionKind_DEBUG_ACTION_KIND_CHECK, fmt.Sprint(i), time.Now())
		b.Route(tutor.Route{}).Debug.Record(tutor.DebugCall{Call: tutor.CallChecking, Attempt: 1})
		b.End()
	}
	if kept := s.Take("a"); len(kept) != 100 || kept[0].GetAttemptId() != "5" {
		t.Errorf("kept %d, the first %q; want the last 100", len(kept), kept[0].GetAttemptId())
	}
}

// A reading of a sheet of a module is recorded as the work read, each reader
// with its place, and the essay's assessment as the essay's.
func TestAModuleAction(t *testing.T) {
	t.Parallel()

	s := debug.New(true)
	a := s.Begin("a", v1.DebugActionKind_DEBUG_ACTION_KIND_CHECK, "attempt", time.Now())
	r := a.Route(tutor.Route{AI: admissionv0.AiRoutePlatformAi})
	opus := admission.Model{Provider: "anthropic", ID: "claude-opus-5-5", Effort: "high"}
	for reader := 1; reader <= 2; reader++ {
		r.Debug.Record(tutor.DebugCall{Call: tutor.CallSheet, Attempt: 1, Reader: reader, Model: opus, Answer: "{}",
			Usage: &tutor.Usage{InputTokens: 1500, OutputTokens: 200, USD: 0.01}})
	}
	r.Debug.Record(tutor.DebugCall{Call: tutor.CallRubric, Attempt: 1, Model: opus})
	a.End()
	calls := s.Take("a")[0].GetCalls()
	if len(calls) != 3 || calls[0].GetKind() != v1.DebugCallKind_DEBUG_CALL_KIND_WORK_READING || calls[0].GetReader() != 1 ||
		calls[1].GetReader() != 2 || calls[1].GetCostMicroUsd() != 10000 || calls[2].GetKind() != v1.DebugCallKind_DEBUG_CALL_KIND_ESSAY_ASSESSMENT || calls[2].GetReader() != 0 {
		t.Errorf("the calls = %v", calls)
	}
}
