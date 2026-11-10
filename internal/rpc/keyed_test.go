package rpc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/idem"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// A request under its key acts once: a repeat gets the answer recorded, another
// request under the key is refused, one still going too, and a refusal records
// nothing, unless it keeps what was written.
func TestARequestUnderAKeyActsOnce(t *testing.T) {
	w := newWorld(t, time.Second)
	p := w.person(t, "uid-once")
	ctx := rpc.WithCaller(t.Context(), rpc.Caller{AccountID: p.accountID})
	key := rpc.Key{Method: "/novatrail.api.v1.AttemptService/Done"}
	acts := 0
	act := func(pgx.Tx, string) (*v1.DoneResponse, store.KeyTarget, error) {
		acts++
		return &v1.DoneResponse{Solved: &v1.SolvedTask{ModuleFormatVersion: "0.4"}}, store.KeyTarget{ProfileID: p.profileID}, nil
	}
	req := &v1.SubmitWorkRequest{ProfileId: p.profileID, AttemptId: "a", IdempotencyKey: "k"}
	first, err := rpc.Once(ctx, w.pool, key, req, act)
	if err != nil {
		t.Fatal(err)
	}
	again, err := rpc.Once(ctx, w.pool, key, req, act)
	if err != nil || !proto.Equal(first, again) || acts != 1 {
		t.Errorf("the repeat = %v, %v after %d acts; want %v after one", again, err, acts, first)
	}
	other := &v1.SubmitWorkRequest{ProfileId: p.profileID, AttemptId: "b", IdempotencyKey: "k"}
	_, err = rpc.Once(ctx, w.pool, key, other, act)
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_ALREADY_USED)
	_, err = rpc.Once(ctx, w.pool, rpc.Key{Method: "/novatrail.api.v1.AttemptService/Other"}, req, act)
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_ALREADY_USED)

	going := &v1.SubmitWorkRequest{IdempotencyKey: "going"}
	if _, err := w.pool.Exec(t.Context(), `INSERT INTO idempotency_keys (account_id, key, method, fingerprint, state)
		VALUES ($1, 'going', $2, $3, 'going')`, p.accountID, key.Method, idem.Fingerprint(going, false)); err != nil {
		t.Fatal(err)
	}
	_, err = rpc.Once(ctx, w.pool, key, going, act)
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_STILL_GOING)

	_, err = rpc.Once(ctx, w.pool, key, &v1.SubmitWorkRequest{}, act)
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)

	errRefused := errors.New("refused")
	_, err = rpc.Once(ctx, w.pool, key, &v1.SubmitWorkRequest{IdempotencyKey: "refused"},
		func(pgx.Tx, string) (*v1.DoneResponse, store.KeyTarget, error) {
			return nil, store.KeyTarget{}, errRefused
		})
	if !errors.Is(err, errRefused) || recorded(t, w, p, "refused") {
		t.Errorf("a refusal = %v, recorded %v; want it as it came, recorded nowhere", err, recorded(t, w, p, "refused"))
	}
	_, err = rpc.Once(ctx, w.pool, key, &v1.SubmitWorkRequest{IdempotencyKey: "kept"},
		func(tx pgx.Tx, _ string) (*v1.DoneResponse, store.KeyTarget, error) {
			_, err := tx.Exec(t.Context(), `UPDATE profiles SET pseudonym = 'Kept' WHERE id = $1`, p.profileID)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			return nil, store.KeyTarget{}, rpc.Keep(errRefused)
		})
	var pseudonym string
	if err := w.pool.QueryRow(t.Context(), `SELECT pseudonym FROM profiles WHERE id = $1`, p.profileID).Scan(&pseudonym); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(err, errRefused) || pseudonym != "Kept" || recorded(t, w, p, "kept") {
		t.Errorf("a refusal kept = %v, wrote %q, recorded %v; want it, Kept, nothing", err, pseudonym, recorded(t, w, p, "kept"))
	}
	if refusal, ok := rpc.Kept(rpc.Keep(errRefused)); !ok || refusal != errRefused || rpc.Keep(errRefused).Error() != "refused" {
		t.Errorf("Kept(Keep) = %v, %v", refusal, ok)
	}
	if _, ok := rpc.Kept(errRefused); ok {
		t.Error("Kept of a refusal not kept: ok")
	}
}

// A repeat of a request whose record names a submission gets that submission's
// answer, which the method reads.
func TestARepeatOfASubmissionReadsItsAnswer(t *testing.T) {
	w := newWorld(t, time.Second)
	p := w.person(t, "uid-submission")
	ctx := rpc.WithCaller(t.Context(), rpc.Caller{AccountID: p.accountID})
	var submission string
	if err := w.pool.QueryRow(t.Context(), `WITH c AS (
			INSERT INTO engine_choices (profile_id, kind, module_id, task_id) VALUES ($1, 'task', 'example/m', 't') RETURNING id
		), a AS (
			INSERT INTO attempts (profile_id, package_digest, module_id, module_version, module_format_version,
				module_kit_version, task_id, seed, choice_id)
			SELECT $1, 'sha256:' || repeat('0', 64), 'example/m', '1.0.0', '0.4', 'v0.3.0', 't', 1, id FROM c RETURNING id
		)
		INSERT INTO submissions (profile_id, attempt_id, number, answer) SELECT $1, id, 1, '\x00' FROM a RETURNING id`,
		p.profileID).Scan(&submission); err != nil {
		t.Fatal(err)
	}
	answer := &v1.SubmitWorkResponse{WrongAnswers: 1}
	key := rpc.Key{Method: "/novatrail.api.v1.AttemptService/SubmitWork",
		Repeat: func(_ context.Context, _ pgx.Tx, record store.KeyRecord) (proto.Message, error) {
			if record.SubmissionID != submission {
				return nil, errors.New("another submission")
			}
			return answer, nil
		}}
	req := &v1.SubmitWorkRequest{ProfileId: p.profileID, IdempotencyKey: "s"}
	act := func(pgx.Tx, string) (*v1.SubmitWorkResponse, store.KeyTarget, error) {
		return answer, store.KeyTarget{ProfileID: p.profileID, SubmissionID: submission}, nil
	}
	if _, err := rpc.Once(ctx, w.pool, key, req, act); err != nil {
		t.Fatal(err)
	}
	if got, err := rpc.Once(ctx, w.pool, key, req, act); err != nil || got != answer {
		t.Errorf("the repeat = %v, %v; want the submission's answer", got, err)
	}
	key.Repeat = func(context.Context, pgx.Tx, store.KeyRecord) (proto.Message, error) { return nil, errors.New("down") }
	if _, err := rpc.Once(ctx, w.pool, key, req, act); err == nil {
		t.Error("a repeat that cannot read its submission: error = nil")
	}
}

// recorded says whether the person's account has a record of key.
func recorded(t *testing.T, w *world, p person, key string) bool {
	t.Helper()
	var n int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM idempotency_keys WHERE account_id = $1 AND key = $2`,
		p.accountID, key).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}
