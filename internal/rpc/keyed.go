package rpc

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/idem"
	"github.com/altnovatrail/api/internal/store"
)

// Keyed is a request that carries an idempotency key.
type Keyed interface {
	proto.Message
	GetIdempotencyKey() string
}

// Refusals of a key, made in the transaction and answered after it.
var (
	errKeyUsed    = errors.New("rpc: the key was used by another request")
	errStillGoing = errors.New("rpc: the key's request is still going")
)

// kept is a refusal that keeps what act wrote before it.
type kept struct{ refusal error }

func (k kept) Error() string { return k.refusal.Error() }

// Keep is a refusal of act that commits what act wrote before it, with no record
// of the key: the close of an attempt that the check of a request found
// abandoned, or on a package the image no longer gives (R07, R10). A repeat is
// checked again, and finds what was written.
func Keep(refusal error) error { return kept{refusal: refusal} }

// Kept is the refusal err keeps, when it is one of Keep: for a request with no
// key, which commits what it wrote and answers the refusal after it.
func Kept(err error) (refusal error, ok bool) {
	var k kept
	if errors.As(err, &k) {
		return k.refusal, true
	}
	return nil, false
}

// Key is how a method records its requests under their keys.
type Key struct {
	// Method is the procedure of the contract.
	Method string
	// Slow is a fingerprint by argon2id, of a request that carries a PIN or an
	// age; SHA-256 otherwise (R35).
	Slow bool
	// Repeat is the answer of a repeat, read in the repeat's transaction, when
	// the record does not hold the answer itself: the submission it names, or the
	// words the attempt keeps, which the record does not (R47). Nil for a method
	// whose records hold their answers.
	Repeat func(ctx context.Context, tx pgx.Tx, record store.KeyRecord) (proto.Message, error)
	// GoingFor is the longest a request may run (R33): a record going older than
	// that is of a request that fell, and a repeat goes again. Zero for a method
	// that never records a key going.
	GoingFor time.Duration
}

// Once runs act under the request's idempotency key, in one transaction with
// the record of the key, under the account of the caller (R17, R35). The
// account's row is held FOR NO KEY UPDATE first, so that a repeat beside its
// first waits for it and finds its record. A record of this method and this
// request gives its answer, and act does nothing; of another method or request,
// CAUSE_KEY_ALREADY_USED; one still going, CAUSE_STILL_GOING. Otherwise act
// acts, and its target is written with the record: the answer, unless it names
// the submission a repeat reads instead. A refusal of act rolls everything back
// and is recorded nowhere, but one of Keep commits what act wrote: either way a
// repeat is checked again. The fingerprint is made before the transaction, so
// that the account's row and a connection of the pool are held for no more than
// the hash that checks a repeat. Any error but a refusal of the key is given
// back as it came, for the caller to answer.
func Once[Res proto.Message](ctx context.Context, db store.Beginner, k Key, req Keyed,
	act func(tx pgx.Tx, accountID string) (Res, store.KeyTarget, error)) (Res, error) {
	var answer, zero Res
	key := req.GetIdempotencyKey()
	if !idem.ValidKey(key) {
		return zero, Malformed("idempotency_key")
	}
	fingerprint := idem.Fingerprint(req, k.Slow)
	caller, _ := CallerFrom(ctx)
	var keptRefusal error
	err := store.WriteUnderAccount(ctx, db, caller.AccountID, func(tx pgx.Tx) error {
		repeated, found, err := recorded[Res](ctx, tx, k, req, caller.AccountID, key)
		switch {
		case err != nil:
			return err
		case found:
			answer = repeated
			return nil
		}
		var target store.KeyTarget
		answer, target, err = act(tx, caller.AccountID)
		if refusal, ok := Kept(err); ok {
			keptRefusal = refusal
			return nil
		}
		if err != nil {
			return err
		}
		return store.RecordDone(ctx, tx, caller.AccountID, key, k.Method, fingerprint, withResponse(answer, target))
	})
	switch {
	case err == nil && keptRefusal != nil:
		return zero, keptRefusal
	case err == nil:
		return answer, nil
	}
	return zero, keyRefusal(ctx, err)
}

// dropTimeout bounds the transaction that drops the record going of a request
// whose last part failed.
const dropTimeout = 5 * time.Second

// Phases are the three parts of a request under a key that calls AI between
// two transactions (R17, R46): Prepare checks what the request acts on and names
// its target, in the transaction that records the key going; Call calls AI
// outside any transaction, holding no row and no connection; and Finish writes
// what came of it, in the transaction that records the key done. S is what one
// part gives the next.
type Phases[S any, Res proto.Message] struct {
	Prepare func(tx pgx.Tx, accountID string) (S, store.KeyTarget, error)
	Call    func(ctx context.Context, s S) S
	Finish  func(tx pgx.Tx, s S) (Res, store.KeyTarget, error)
}

// Across runs the phases of a request under its key, as Once runs one action,
// with the record going between its two transactions: a repeat beside it is
// CAUSE_STILL_GOING while it calls AI, and one after it gets its answer, with no
// call. A record going past k.GoingFor is of a request that fell, and the repeat
// goes again (R17). A refusal of Prepare records nothing, and one of Keep
// commits what Prepare wrote, as in Once. A refusal of Finish rolls back what
// Finish wrote, unless it is one of Keep, and either way drops the record going,
// so that a repeat is checked again.
func Across[S any, Res proto.Message](ctx context.Context, db store.Beginner, k Key, req Keyed, p Phases[S, Res]) (Res, error) {
	var answer, zero Res
	key := req.GetIdempotencyKey()
	if !idem.ValidKey(key) {
		return zero, Malformed("idempotency_key")
	}
	fingerprint := idem.Fingerprint(req, k.Slow)
	caller, _ := CallerFrom(ctx)
	var state S
	var made time.Time
	var repeated bool
	var keptRefusal error
	err := store.WriteUnderAccount(ctx, db, caller.AccountID, func(tx pgx.Tx) error {
		got, found, err := recorded[Res](ctx, tx, k, req, caller.AccountID, key)
		switch {
		case err != nil:
			return err
		case found:
			answer, repeated = got, true
			return nil
		}
		var target store.KeyTarget
		state, target, err = p.Prepare(tx, caller.AccountID)
		if refusal, ok := Kept(err); ok {
			keptRefusal = refusal
			return nil
		}
		if err != nil {
			return err
		}
		made, err = store.RecordGoing(ctx, tx, caller.AccountID, key, k.Method, fingerprint, target)
		return err
	})
	switch {
	case err != nil:
		return zero, keyRefusal(ctx, err)
	case repeated:
		return answer, nil
	case keptRefusal != nil:
		return zero, keptRefusal
	}

	state = p.Call(ctx, state)

	err = store.WriteUnderAccount(ctx, db, caller.AccountID, func(tx pgx.Tx) error {
		if err := store.HoldAccount(ctx, tx, caller.AccountID); err != nil {
			return err
		}
		var target store.KeyTarget
		var err error
		answer, target, err = p.Finish(tx, state)
		if refusal, ok := Kept(err); ok {
			keptRefusal = refusal
			return store.DropRecord(ctx, tx, caller.AccountID, key, made)
		}
		if err != nil {
			return err
		}
		return store.FinishRecord(ctx, tx, caller.AccountID, key, made, withResponse(answer, target))
	})
	switch {
	case err == nil && keptRefusal != nil:
		return zero, keptRefusal
	case err == nil:
		return answer, nil
	}
	// What Finish wrote is rolled back; the record going goes too, in a
	// transaction of its own — one the request's end does not cut short, since a
	// deadline passed or a client gone is the likeliest failure here. If that
	// fails as well, the record outlives the longest a request may run, and a
	// repeat goes again.
	dropCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dropTimeout)
	defer cancel()
	_ = store.WriteUnderAccount(dropCtx, db, caller.AccountID, func(tx pgx.Tx) error {
		return store.DropRecord(dropCtx, tx, caller.AccountID, key, made)
	})
	return zero, keyRefusal(ctx, err)
}

// recorded is the answer a repeat gets from the record of key, held under the
// account's row in tx, and whether there is one to give: a record done of this
// method and this request. Of another, errKeyUsed; one going, errStillGoing,
// unless it is older than k.GoingFor, when it is of a request that fell and is
// dropped for the repeat to go again.
func recorded[Res proto.Message](ctx context.Context, tx pgx.Tx, k Key, req Keyed, accountID, key string) (Res, bool, error) {
	var zero Res
	if err := store.HoldAccount(ctx, tx, accountID); err != nil {
		return zero, false, err
	}
	record, found, err := store.RecordOf(ctx, tx, accountID, key)
	switch {
	case err != nil:
		return zero, false, err
	case !found:
		return zero, false, nil
	case record.Method != k.Method || !idem.Same(req, record.Fingerprint):
		return zero, false, errKeyUsed
	case record.State != store.KeyDone && k.GoingFor > 0 && record.Age > k.GoingFor:
		return zero, false, store.DropRecord(ctx, tx, accountID, key, time.Time{})
	case record.State != store.KeyDone:
		return zero, false, errStillGoing
	case k.Repeat != nil:
		repeated, err := k.Repeat(ctx, tx, record)
		if err != nil {
			return zero, false, err
		}
		return repeated.(Res), true, nil
	}
	answer := zero.ProtoReflect().New().Interface().(Res)
	return answer, true, proto.Unmarshal(record.Response, answer)
}

// withResponse is target with the answer marshalled into it, unless it names
// the submission a repeat reads instead, or holds what of the answer the
// record keeps already.
func withResponse(answer proto.Message, target store.KeyTarget) store.KeyTarget {
	if target.SubmissionID == "" && target.Response == nil {
		// An answer of the contract always marshals.
		target.Response, _ = proto.Marshal(answer)
	}
	return target
}

// keyRefusal answers the refusals of a key, and gives any other error back as
// it came.
func keyRefusal(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, errKeyUsed):
		return Refuse(ctx, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_ALREADY_USED)
	case errors.Is(err, errStillGoing):
		return Refuse(ctx, connect.CodeUnavailable, v1.Cause_CAUSE_STILL_GOING)
	}
	return err
}
