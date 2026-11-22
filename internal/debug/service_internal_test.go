package debug

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/tutor"
	"github.com/altnovatrail/tutor/admission"

	"github.com/altnovatrail/api/internal/rpc"
)

// The service answers each account with the right by its own choice and calls,
// with the models of the list for the calls it admits; a choice the models do
// not take is invalid_argument; a caller with no account is no answer, and an
// account without the right is permission_denied, but on a store that gives it
// to every account (R97).
func TestTheService(t *testing.T) {
	t.Parallel()

	opus := admission.Model{Provider: anthropic, ID: "claude-opus-5-5", Effort: "medium"}
	haiku := admission.Model{Provider: anthropic, ID: "claude-haiku-5-5", Effort: "high"}
	s := NewService(New(false), func(c tutor.Call) []admission.Model {
		if c == tutor.CallFinding {
			return []admission.Model{haiku, opus}
		}
		return nil
	})
	s.caller = func(context.Context) (rpc.Caller, bool) { return rpc.Caller{AccountID: "a", DebugModels: true}, true }
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

	calls := func(s *Service) []error {
		return []error{
			func() error { _, err := s.GetDebug(ctx, &v1.GetDebugRequest{}); return err }(),
			func() error { _, err := s.SetDebugModels(ctx, &v1.SetDebugModelsRequest{}); return err }(),
			func() error { _, err := s.TakeDebugCalls(ctx, &v1.TakeDebugCallsRequest{}); return err }(),
		}
	}
	for _, err := range calls(NewService(New(true), nil)) {
		if connect.CodeOf(err) != connect.CodeInternal {
			t.Errorf("no account = %v, want internal", err)
		}
	}
	s.caller = func(context.Context) (rpc.Caller, bool) { return rpc.Caller{AccountID: "b"}, true }
	for _, err := range calls(s) {
		var ce *connect.Error
		if !errors.As(err, &ce) || ce.Code() != connect.CodePermissionDenied || len(ce.Details()) != 0 {
			t.Errorf("an account without the right = %v, want permission_denied with no Refusal", err)
		}
	}
	every := NewService(New(true), s.admitted)
	every.caller = s.caller
	if _, err := every.GetDebug(ctx, &v1.GetDebugRequest{}); err != nil {
		t.Errorf("an account of a store that gives every account the right = %v, want an answer", err)
	}
}

// A read of an account with no choice and no action keeps no entry of it; a
// choice and an action kept do.
func TestAReadKeepsNoAccount(t *testing.T) {
	t.Parallel()

	s := New(false)
	if s.Chosen("a") || s.Choice("a").GetFirst() != nil || len(s.Take("a")) != 0 {
		t.Error("an account with no choice reads as one with a choice or actions")
	}
	s.Begin("a", v1.DebugActionKind_DEBUG_ACTION_KIND_CHECK, "", time.Now()).End()
	if len(s.accounts) != 0 {
		t.Errorf("the store holds %d accounts after reads and an action with no call, want none", len(s.accounts))
	}
	a := s.Begin("b", v1.DebugActionKind_DEBUG_ACTION_KIND_CHECK, "", time.Now())
	a.Solved(nil, 0)
	a.End()
	if _, err := s.SetChoice("c", &v1.DebugChoice{}); err != nil {
		t.Fatal(err)
	}
	if len(s.accounts) != 2 {
		t.Errorf("the store holds %d accounts, want the action's and the choice's", len(s.accounts))
	}
}
