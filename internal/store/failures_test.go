package store_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

var errDown = errors.New("the test's database is down")

// flaky is a pool whose transactions fail the first statement whose SQL holds
// failOn.
type flaky struct {
	*pgxpool.Pool
	failOn string
}

func (f *flaky) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := f.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &flakyTx{Tx: tx, failOn: f.failOn}, nil
}

func (f *flaky) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx, err := f.Pool.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &flakyTx{Tx: tx, failOn: f.failOn}, nil
}

type flakyTx struct {
	pgx.Tx
	failOn string
}

type failedRow struct{}

func (failedRow) Scan(...any) error { return errDown }

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

func (t *flakyTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, t.failOn) {
		return nil, errDown
	}
	return t.Tx.Query(ctx, sql, args...)
}

// A statement that fails is the store's failure, never one of its refusals, and
// leaves nothing written.
func TestAFailedStatementIsAFailure(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	for _, failOn := range []string{"INSERT INTO accounts", "UPDATE invite_codes"} {
		hash := code(t, pool, "CODE-"+failOn)
		if _, err := store.CreateAccount(t.Context(), &flaky{Pool: pool, failOn: failOn}, "uid-owner", hash); !errors.Is(err, errDown) {
			t.Errorf("CreateAccount failing on %q = %v, want the failure", failOn, err)
		}
	}
	if n := count(t, pool, "accounts"); n != 0 {
		t.Errorf("accounts = %d after failures, want none", n)
	}
	hash := sha256.Sum256([]byte("ANOTHER-CODE"))
	if _, err := store.CreateAccount(t.Context(), pool, "uid-owner", code(t, pool, "FIRST")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateAccount(t.Context(), &flaky{Pool: pool, failOn: "SELECT deleting_at"}, "uid-owner", hash[:]); !errors.Is(err, errDown) {
		t.Errorf("CreateAccount failing to find the account there = %v, want the failure", err)
	}

	id := account(t, pool)
	for _, failOn := range []string{"FROM accounts", "FROM profiles", "FROM consents"} {
		if _, err := store.AccountView(t.Context(), &flaky{Pool: pool, failOn: failOn}, id); !errors.Is(err, errDown) {
			t.Errorf("AccountView failing on %q = %v, want the failure", failOn, err)
		}
	}
}
