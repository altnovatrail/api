package debug

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/tutor"
	"github.com/altnovatrail/tutor/admission"
)

// The service answers each account by its own choice and calls, with the models
// of the list for the calls it admits; a choice the models do not take is
// invalid_argument; a caller with no account is no answer.
func TestTheService(t *testing.T) {
	t.Parallel()

	opus := admission.Model{Provider: anthropic, ID: "claude-opus-5-5", Effort: "medium"}
	haiku := admission.Model{Provider: anthropic, ID: "claude-haiku-5-5", Effort: "high"}
	s := NewService(New(), func(c tutor.Call) []admission.Model {
		if c == tutor.CallFinding {
			return []admission.Model{haiku, opus}
		}
		return nil
	})
	s.account = func(context.Context) (string, error) { return "a", nil }
	ctx := t.Context()

	got, err := s.GetDebug(ctx, &v1.GetDebugRequest{})
	if err != nil || len(got.GetModels()) != 4 || got.GetChoice().GetFirst() != nil || len(got.GetAdmitted()) != 2 ||
		got.GetAdmitted()[0].GetKind() != v1.DebugCallKind_DEBUG_CALL_KIND_TASK_FINDING || got.GetAdmitted()[0].GetModel().GetModel() != "claude-haiku-5-5" ||
		got.GetAdmitted()[1].GetModel().GetModel() != "claude-opus-5-5" || !got.GetAdmitted()[1].GetModel().GetThinking() {
		t.Fatalf("GetDebug = %v, %v", got, err)
	}
	set, err := s.SetDebugModels(ctx, &v1.SetDebugModelsRequest{Choice: &v1.DebugChoice{First: &v1.DebugModelChoice{Model: "claude-haiku-5-5", Effort: v1.DebugEffort_DEBUG_EFFORT_LOW}}})
	if err != nil || set.GetChoice().GetFirst().GetModel() != "claude-haiku-5-5" || set.GetChoice().GetFirst().GetThinking() {
		t.Errorf("SetDebugModels = %v, %v", set, err)
	}
	_, err = s.SetDebugModels(ctx, &v1.SetDebugModelsRequest{Choice: &v1.DebugChoice{First: &v1.DebugModelChoice{Model: "claude-fable-5-1"}}})
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("Fable not thinking = %v, want invalid_argument", err)
	}
	a := s.store.Begin("a", v1.DebugActionKind_DEBUG_ACTION_KIND_FIND_TASKS, "", time.Now())
	a.Solved(nil, 0)
	a.End()
	if taken, err := s.TakeDebugCalls(ctx, &v1.TakeDebugCallsRequest{}); err != nil || len(taken.GetActions()) != 1 {
		t.Errorf("TakeDebugCalls = %v, %v; want the one action", taken, err)
	}

	alone := NewService(New(), nil)
	for _, err := range []error{
		func() error { _, err := alone.GetDebug(ctx, &v1.GetDebugRequest{}); return err }(),
		func() error { _, err := alone.SetDebugModels(ctx, &v1.SetDebugModelsRequest{}); return err }(),
		func() error { _, err := alone.TakeDebugCalls(ctx, &v1.TakeDebugCallsRequest{}); return err }(),
	} {
		if connect.CodeOf(err) != connect.CodeInternal {
			t.Errorf("no account = %v, want internal", err)
		}
	}
}
