package debug

import (
	"context"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/tutor"
	"github.com/altnovatrail/tutor/admission"

	"github.com/altnovatrail/api/internal/rpc"
)

// Service is the DebugService of a Store: api registers it only with the debug
// flag, and without it each method answers unimplemented (R97).
type Service struct {
	apiv1connect.UnimplementedDebugServiceHandler
	store    *Store
	admitted func(tutor.Call) []admission.Model
	// account is the caller's account, as the checks found it.
	account func(context.Context) (string, error)
}

// NewService is the DebugService of s, whose admitted gives the models the
// admission list gives a call for an adult on platform AI, in their order.
func NewService(s *Store, admitted func(tutor.Call) []admission.Model) *Service {
	return &Service{store: s, admitted: admitted, account: accountOf}
}

// accountOf is the account of the caller, whom the checks let through with one.
func accountOf(ctx context.Context) (string, error) {
	caller, ok := rpc.CallerFrom(ctx)
	if !ok || caller.AccountID == "" {
		return "", connect.NewError(connect.CodeInternal, "debug: no account of the caller")
	}
	return caller.AccountID, nil
}

// GetDebug gives the models that can be chosen and the account's choice.
func (s *Service) GetDebug(ctx context.Context, _ *v1.GetDebugRequest) (*v1.GetDebugResponse, error) {
	id, err := s.account(ctx)
	if err != nil {
		return nil, err
	}
	answer := &v1.GetDebugResponse{Models: Models(), Choice: s.store.Choice(id)}
	for _, call := range []tutor.Call{tutor.CallFinding, tutor.CallStatement, tutor.CallSolving, tutor.CallPaper} {
		for _, m := range s.admitted(call) {
			answer.Admitted = append(answer.Admitted, &v1.DebugAdmitted{Kind: callKinds[call], Model: choiceOf(&m)})
		}
	}
	return answer, nil
}

// SetDebugModels sets the account's choice, or refuses one the models do not
// take with invalid_argument.
func (s *Service) SetDebugModels(ctx context.Context, req *v1.SetDebugModelsRequest) (*v1.SetDebugModelsResponse, error) {
	id, err := s.account(ctx)
	if err != nil {
		return nil, err
	}
	choice, err := s.store.SetChoice(id, req.GetChoice())
	if err != nil {
		// A choice refused is invalid_argument with no Refusal.
		return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	return &v1.SetDebugModelsResponse{Choice: choice}, nil
}

// TakeDebugCalls gives the account's actions since the last take and forgets
// them.
func (s *Service) TakeDebugCalls(ctx context.Context, _ *v1.TakeDebugCallsRequest) (*v1.TakeDebugCallsResponse, error) {
	id, err := s.account(ctx)
	if err != nil {
		return nil, err
	}
	return &v1.TakeDebugCallsResponse{Actions: s.store.Take(id)}, nil
}
