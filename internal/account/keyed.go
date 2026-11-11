package account

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// keyedOnce runs act under the request's idempotency key, by rpc.Once: its
// record holds the answer, which names nothing the request carried. The
// fingerprint is argon2id when slow — a request that carries a PIN or an age
// (R35) — and SHA-256 otherwise: the scan of a form carries neither. The time act is given is the service's clock, to the
// microsecond, as the database keeps it: an answer recorded and a refusal read
// later name one instant.
func keyedOnce[Res proto.Message](ctx context.Context, s *Service, method string, req rpc.Keyed, slow bool,
	act func(tx pgx.Tx, accountID string, now time.Time) (Res, error)) (Res, error) {
	answer, err := rpc.Once(ctx, s.db, rpc.Key{Method: method, Slow: slow}, req,
		func(tx pgx.Tx, accountID string) (Res, store.KeyTarget, error) {
			answer, err := act(tx, accountID, s.now().UTC().Truncate(time.Microsecond))
			return answer, store.KeyTarget{}, err
		})
	if err != nil {
		return answer, refusedProfile(ctx, err)
	}
	return answer, nil
}
