package account

import (
	"context"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/altnovatrail/api/internal/pin"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// SetPin sets the PIN of the section for adults, while the account has none.
func (s *Service) SetPin(ctx context.Context, req *v1.SetPinRequest) (*v1.SetPinResponse, error) {
	if !pin.Valid(req.GetPin()) {
		return nil, malformed("pin")
	}
	hash := pin.Hash(req.GetPin())
	return once(ctx, s, apiv1connect.AccountServiceSetPinProcedure, req, func(state *pin.State, _ time.Time) (*v1.SetPinResponse, error) {
		if state.Set() {
			return nil, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_PIN_ALREADY_SET)
		}
		state.Put(hash)
		return &v1.SetPinResponse{}, nil
	})
}

// VerifyPin checks a PIN: a wrong one counts toward closing the input.
func (s *Service) VerifyPin(ctx context.Context, req *v1.VerifyPinRequest) (*v1.VerifyPinResponse, error) {
	if !pin.Valid(req.GetPin()) {
		return nil, malformed("pin")
	}
	return once(ctx, s, apiv1connect.AccountServiceVerifyPinProcedure, req, func(state *pin.State, now time.Time) (*v1.VerifyPinResponse, error) {
		check, err := try(ctx, state, req.GetPin(), now)
		if err != nil {
			return nil, err
		}
		return &v1.VerifyPinResponse{Check: check}, nil
	})
}

// ChangePin changes the PIN by the PIN before it, which counts as VerifyPin
// counts it; a wrong one changes nothing.
func (s *Service) ChangePin(ctx context.Context, req *v1.ChangePinRequest) (*v1.ChangePinResponse, error) {
	if !pin.Valid(req.GetPin()) {
		return nil, malformed("pin")
	}
	if !pin.Valid(req.GetNewPin()) {
		return nil, malformed("new_pin")
	}
	hash := pin.Hash(req.GetNewPin())
	return once(ctx, s, apiv1connect.AccountServiceChangePinProcedure, req, func(state *pin.State, now time.Time) (*v1.ChangePinResponse, error) {
		check, err := try(ctx, state, req.GetPin(), now)
		if err != nil {
			return nil, err
		}
		if check.GetMatches() {
			state.Put(hash)
		}
		return &v1.ChangePinResponse{Check: check}, nil
	})
}

// ResetPin changes a forgotten PIN by a fresh sign-in, which the checks have
// held last (R20): it sets the new PIN whether there was one or not, and opens
// the input.
func (s *Service) ResetPin(ctx context.Context, req *v1.ResetPinRequest) (*v1.ResetPinResponse, error) {
	if !pin.Valid(req.GetNewPin()) {
		return nil, malformed("new_pin")
	}
	hash := pin.Hash(req.GetNewPin())
	return once(ctx, s, apiv1connect.AccountServiceResetPinProcedure, req, func(state *pin.State, _ time.Time) (*v1.ResetPinResponse, error) {
		state.Put(hash)
		return &v1.ResetPinResponse{}, nil
	})
}

// try compares a PIN with the one set, in a check of the contract: none set,
// or the input closed, is a refusal that counts nothing.
func try(ctx context.Context, state *pin.State, given string, now time.Time) (*v1.PinCheck, error) {
	if !state.Set() {
		return nil, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_PIN)
	}
	if state.Locked(now) {
		return nil, rpc.Refuse(ctx, connect.CodeResourceExhausted, v1.Cause_CAUSE_PIN_LOCKED, func(r *v1.Refusal) {
			r.Data = &v1.Refusal_LockedUntil{LockedUntil: timestamppb.New(state.LockedUntil)}
		})
	}
	check := state.Try(given, now)
	out := &v1.PinCheck{Matches: check.Matches, AttemptsLeft: int32(check.Left)}
	if !check.LockedUntil.IsZero() {
		out.LockedUntil = timestamppb.New(check.LockedUntil)
	}
	return out, nil
}

// once runs act on the account's PIN under the request's idempotency key: the
// PIN is read and written in the transaction of the key's record (R35). Its
// fingerprint is slow, as the request carries a PIN.
func once[Res proto.Message](ctx context.Context, s *Service, method string, req keyed,
	act func(state *pin.State, now time.Time) (Res, error)) (Res, error) {
	return keyedOnce(ctx, s, method, req, func(tx pgx.Tx, accountID string, now time.Time) (Res, error) {
		var zero Res
		state, err := store.HoldPin(ctx, tx, accountID)
		if err != nil {
			return zero, err
		}
		answer, err := act(&state, now)
		if err != nil {
			return zero, err
		}
		return answer, store.WritePin(ctx, tx, accountID, state)
	})
}
