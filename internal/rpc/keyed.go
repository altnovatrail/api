package rpc

import (
	"context"
	"errors"

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
	// Repeat is the answer of a repeat whose record names a submission, not the
	// answer itself: it reads the submission in the repeat's transaction. Nil
	// for a method whose records hold their answers.
	Repeat func(ctx context.Context, tx pgx.Tx, record store.KeyRecord) (proto.Message, error)
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
// the hash that checks a repeat. Any error but a
// refusal of the key is given back as it came, for the caller to answer.
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
		if err := store.HoldAccount(ctx, tx, caller.AccountID); err != nil {
			return err
		}
		record, found, err := store.RecordOf(ctx, tx, caller.AccountID, key)
		switch {
		case err != nil:
			return err
		case found && (record.Method != k.Method || !idem.Same(req, record.Fingerprint)):
			return errKeyUsed
		case found && record.State != store.KeyDone:
			return errStillGoing
		case found && record.SubmissionID != "":
			repeated, err := k.Repeat(ctx, tx, record)
			if err != nil {
				return err
			}
			answer = repeated.(Res)
			return nil
		case found:
			answer = answer.ProtoReflect().New().Interface().(Res)
			return proto.Unmarshal(record.Response, answer)
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
		if target.SubmissionID == "" {
			// An answer of the contract always marshals.
			target.Response, _ = proto.Marshal(answer)
		}
		return store.RecordDone(ctx, tx, caller.AccountID, key, k.Method, fingerprint, target)
	})
	switch {
	case err == nil && keptRefusal != nil:
		return zero, keptRefusal
	case err == nil:
		return answer, nil
	case errors.Is(err, errKeyUsed):
		return zero, Refuse(ctx, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_ALREADY_USED)
	case errors.Is(err, errStillGoing):
		return zero, Refuse(ctx, connect.CodeUnavailable, v1.Cause_CAUSE_STILL_GOING)
	}
	return zero, err
}
