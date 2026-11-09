package account

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/idem"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

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

// keyedOnce runs act under the request's idempotency key, in one transaction
// with the record of the key (R17, R35). The account's row is held FOR NO KEY
// UPDATE first, so that a repeat beside its first waits for it and finds its
// record. A record of this method and this request gives its answer, and act
// does nothing; of another method or request, CAUSE_KEY_ALREADY_USED; one still
// going, CAUSE_STILL_GOING. Otherwise act acts, and its answer is written with
// the record. A refusal of act rolls everything back and is recorded nowhere: a
// repeat is checked again. The fingerprint is argon2id: the requests with a key
// of AccountService carry a PIN or an age (R35), and the fast one of SHA-256
// comes with the first request of epic E3 that carries neither. It is made
// before the transaction, so that the
// account's row and a connection of the pool are held for no more than the hash
// that checks a repeat. The time act is given is the database's, to the
// microsecond: an answer recorded and a refusal read later name one instant.
func keyedOnce[Res proto.Message](ctx context.Context, s *Service, method string, req keyed,
	act func(tx pgx.Tx, accountID string, now time.Time) (Res, error)) (Res, error) {
	var answer, zero Res
	key := req.GetIdempotencyKey()
	if !idem.ValidKey(key) {
		return zero, malformed("idempotency_key")
	}
	fingerprint := idem.Fingerprint(req, true)
	caller, _ := rpc.CallerFrom(ctx)
	err := store.WriteUnderAccount(ctx, s.db, caller.AccountID, func(tx pgx.Tx) error {
		if err := store.HoldAccount(ctx, tx, caller.AccountID); err != nil {
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
		if answer, err = act(tx, caller.AccountID, s.now().UTC().Truncate(time.Microsecond)); err != nil {
			return err
		}
		// An answer of the contract always marshals.
		written, _ := proto.Marshal(answer)
		return store.RecordDone(ctx, tx, caller.AccountID, key, method, fingerprint, written)
	})
	switch {
	case err == nil:
		return answer, nil
	case errors.Is(err, errKeyUsed):
		return zero, rpc.Refuse(ctx, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_ALREADY_USED)
	case errors.Is(err, errStillGoing):
		return zero, rpc.Refuse(ctx, connect.CodeUnavailable, v1.Cause_CAUSE_STILL_GOING)
	}
	return zero, refusedProfile(ctx, err)
}
