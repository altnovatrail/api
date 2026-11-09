package account

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/altnovatrail/api/internal/idem"
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

// keyed is a request that carries an idempotency key.
type keyed interface {
	proto.Message
	GetIdempotencyKey() string
}

// Refusals of a key, made in the transaction and answered after it.
var (
	errKeyUsed    = errors.New("account: the key was used by another request")
	errStillGoing = errors.New("account: the key's request is still going")
)

// once runs act on the account's PIN under the request's idempotency key, in
// one transaction with the record of the key (R17, R35). The account's row is
// held first, so that a repeat beside its first waits for it and finds its
// record. A record of this method and this request gives its answer, and act
// does nothing; of another method or request, CAUSE_KEY_ALREADY_USED; one still
// going, CAUSE_STILL_GOING. Otherwise act acts, and the PIN it leaves and its
// answer are written with the record. A refusal of act rolls everything back and
// is recorded nowhere: a repeat is checked again. The fingerprint is argon2id,
// as the request carries a PIN; it, and the hash of a new PIN, are made before
// the transaction, so that the account's row and a connection of the pool are
// held for no more than one hash — the check of the PIN given, or of a repeat's
// fingerprint.
func once[Res proto.Message](ctx context.Context, s *Service, method string, req keyed,
	act func(state *pin.State, now time.Time) (Res, error)) (Res, error) {
	var answer Res
	key := req.GetIdempotencyKey()
	if !idem.ValidKey(key) {
		return answer, connect.NewError(connect.CodeInvalidArgument, "idempotency_key")
	}
	fingerprint := idem.Fingerprint(req, true)
	caller, _ := rpc.CallerFrom(ctx)
	err := store.WriteUnderAccount(ctx, s.db, caller.AccountID, func(tx pgx.Tx) error {
		state, err := store.HoldPin(ctx, tx, caller.AccountID)
		if err != nil {
			return err
		}
		record, found, err := store.RecordOf(ctx, tx, caller.AccountID, key)
		switch {
		case err != nil:
			return err
		case found && (record.Method != method || !idem.Same(req, record.Fingerprint)):
			return errKeyUsed
		case found && record.State != store.KeyDone:
			return errStillGoing
		case found:
			answer = answer.ProtoReflect().New().Interface().(Res)
			return proto.Unmarshal(record.Response, answer)
		}
		// The time of the PIN is the database's, to the microsecond: the answer
		// recorded and a refusal read later name one instant.
		if answer, err = act(&state, s.now().UTC().Truncate(time.Microsecond)); err != nil {
			return err
		}
		if err := store.WritePin(ctx, tx, caller.AccountID, state); err != nil {
			return err
		}
		// An answer of the contract always marshals.
		written, _ := proto.Marshal(answer)
		return store.RecordDone(ctx, tx, caller.AccountID, key, method, fingerprint, written)
	})
	if err == nil {
		return answer, nil
	}
	var zero Res
	var refusal *connect.Error
	switch {
	case errors.As(err, &refusal):
		return zero, err
	case errors.Is(err, errKeyUsed):
		return zero, rpc.Refuse(ctx, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_ALREADY_USED)
	case errors.Is(err, errStillGoing):
		return zero, rpc.Refuse(ctx, connect.CodeUnavailable, v1.Cause_CAUSE_STILL_GOING)
	}
	return zero, refused(ctx, err)
}
