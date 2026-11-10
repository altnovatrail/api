package rpc_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
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

// A request across two transactions records its key going while it calls, and
// done after: a repeat beside it is still going, one after it gets the answer
// with no call, one after a request that fell goes again, and a refusal of its
// last part records nothing, so that a repeat goes again.
func TestARequestAcrossItsCallRecordsItGoing(t *testing.T) {
	w := newWorld(t, time.Second)
	p := w.person(t, "uid-across")
	ctx := rpc.WithCaller(t.Context(), rpc.Caller{AccountID: p.accountID})
	key := rpc.Key{Method: "/novatrail.api.v1.AttemptService/SubmitWork", GoingFor: time.Minute}
	calls := 0
	var stillGoing error
	phases := func(finish error) rpc.Phases[int, *v1.SubmitWorkResponse] {
		return rpc.Phases[int, *v1.SubmitWorkResponse]{
			Prepare: func(pgx.Tx, string) (int, store.KeyTarget, error) {
				return 1, store.KeyTarget{ProfileID: p.profileID}, nil
			},
			Call: func(_ context.Context, n int) int {
				calls++
				// A repeat beside it, while it calls.
				_, stillGoing = rpc.Across(ctx, w.pool, key, &v1.SubmitWorkRequest{IdempotencyKey: "across"},
					rpc.Phases[int, *v1.SubmitWorkResponse]{})
				return n + 1
			},
			Finish: func(_ pgx.Tx, n int) (*v1.SubmitWorkResponse, store.KeyTarget, error) {
				if finish != nil {
					return nil, store.KeyTarget{}, finish
				}
				return &v1.SubmitWorkResponse{WrongAnswers: int32(n)}, store.KeyTarget{ProfileID: p.profileID}, nil
			},
		}
	}
	req := &v1.SubmitWorkRequest{IdempotencyKey: "across"}
	got, err := rpc.Across(ctx, w.pool, key, req, phases(nil))
	if err != nil || got.GetWrongAnswers() != 2 {
		t.Fatalf("Across = %v, %v; want the answer of the last part", got, err)
	}
	refusedAs(t, stillGoing, connect.CodeUnavailable, v1.Cause_CAUSE_STILL_GOING)
	again, err := rpc.Across(ctx, w.pool, key, req, phases(nil))
	if err != nil || !proto.Equal(got, again) || calls != 1 {
		t.Errorf("the repeat = %v, %v after %d calls; want the answer after one", again, err, calls)
	}

	// A request that fell left its record going an hour ago: the repeat goes again.
	fell := &v1.SubmitWorkRequest{IdempotencyKey: "fell"}
	if _, err := w.pool.Exec(t.Context(), `INSERT INTO idempotency_keys (account_id, key, method, fingerprint, state, created_at)
		VALUES ($1, 'fell', $2, $3, 'going', now() - interval '1 hour')`, p.accountID, key.Method, idem.Fingerprint(fell, false)); err != nil {
		t.Fatal(err)
	}
	if _, err := rpc.Across(ctx, w.pool, key, fell, phases(nil)); err != nil || calls != 2 {
		t.Errorf("the repeat of a request that fell = %v after %d calls, want it called again", err, calls)
	}

	// A refusal of the last part, kept or not, leaves no record.
	errRefused := errors.New("refused")
	for name, finish := range map[string]error{"refused": errRefused, "kept": rpc.Keep(errRefused)} {
		_, err := rpc.Across(ctx, w.pool, key, &v1.SubmitWorkRequest{IdempotencyKey: name}, phases(finish))
		if !errors.Is(err, errRefused) || recorded(t, w, p, name) {
			t.Errorf("a refusal %s of the last part = %v, recorded %v; want it, recorded nowhere", name, err, recorded(t, w, p, name))
		}
	}
	// A refusal of the first part, kept or not, records nothing and calls nothing:
	// the two refusals of the last part called.
	for name, prepare := range map[string]error{"first refused": errRefused, "first kept": rpc.Keep(errRefused)} {
		ph := phases(nil)
		ph.Prepare = func(pgx.Tx, string) (int, store.KeyTarget, error) { return 0, store.KeyTarget{}, prepare }
		_, err := rpc.Across(ctx, w.pool, key, &v1.SubmitWorkRequest{IdempotencyKey: name}, ph)
		if !errors.Is(err, errRefused) || recorded(t, w, p, name) || calls != 4 {
			t.Errorf("a refusal %s of the first part = %v, recorded %v, %d calls", name, err, recorded(t, w, p, name), calls)
		}
	}
	_, err = rpc.Across(ctx, w.pool, key, &v1.SubmitWorkRequest{}, phases(nil))
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
}

// flakyTxs begins transactions on a pool, and in each after the first `after`
// fails the statements whose SQL holds failOn.
type flakyTxs struct {
	pool   *pgxpool.Pool
	failOn string
	after  int
	begun  int
}

func (f *flakyTxs) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	f.begun++
	if f.begun <= f.after {
		return tx, nil
	}
	return &flakyTx{Tx: tx, failOn: f.failOn}, nil
}

// flakyTx is a transaction whose statements holding failOn fail.
type flakyTx struct {
	pgx.Tx
	failOn string
}

func (t *flakyTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, t.failOn) {
		return failedRow{}
	}
	return t.Tx.QueryRow(ctx, sql, args...)
}

func (t *flakyTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, t.failOn) {
		return pgconn.CommandTag{}, errDown
	}
	return t.Tx.Exec(ctx, sql, args...)
}

// A database that fails in either transaction of a request across its call is
// the failure, given back as it came.
func TestARequestAcrossAFailingDatabaseFails(t *testing.T) {
	w := newWorld(t, time.Second)
	p := w.person(t, "uid-across-down")
	ctx := rpc.WithCaller(t.Context(), rpc.Caller{AccountID: p.accountID})
	key := rpc.Key{Method: "/novatrail.api.v1.AttemptService/SubmitWork", GoingFor: time.Minute}
	phases := rpc.Phases[int, *v1.SubmitWorkResponse]{
		Prepare: func(pgx.Tx, string) (int, store.KeyTarget, error) { return 0, store.KeyTarget{}, nil },
		Call:    func(_ context.Context, n int) int { return n },
		Finish: func(pgx.Tx, int) (*v1.SubmitWorkResponse, store.KeyTarget, error) {
			return &v1.SubmitWorkResponse{}, store.KeyTarget{}, nil
		},
	}
	for name, db := range map[string]*flakyTxs{
		"the account held first":   {pool: w.pool, failOn: "FOR NO KEY UPDATE"},
		"the key's record read":    {pool: w.pool, failOn: "FROM idempotency_keys"},
		"the account held at last": {pool: w.pool, failOn: "FOR NO KEY UPDATE", after: 1},
	} {
		_, err := rpc.Across(ctx, db, key, &v1.SubmitWorkRequest{IdempotencyKey: name}, phases)
		if !errors.Is(err, errDown) || recorded(t, w, p, name) {
			t.Errorf("%s failing = %v, recorded %v; want the failure, recorded nowhere", name, err, recorded(t, w, p, name))
		}
	}
}
