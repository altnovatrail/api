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

	"github.com/altnovatrail/api/internal/pin"
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

// A statement of the PIN or of a key that fails is the store's failure.
func TestAFailedStatementOfThePINIsAFailure(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	for _, tc := range []struct {
		failOn string
		run    func(tx pgx.Tx) error
	}{
		{"FOR NO KEY UPDATE", func(tx pgx.Tx) error {
			_, err := store.HoldPin(t.Context(), tx, id)
			return err
		}},
		{"FROM idempotency_keys", func(tx pgx.Tx) error {
			_, _, err := store.RecordOf(t.Context(), tx, id, "k")
			return err
		}},
		{"INSERT INTO idempotency_keys", func(tx pgx.Tx) error {
			return store.RecordDone(t.Context(), tx, id, "k", "m", "f", []byte{})
		}},
		{"SET pin_hash", func(tx pgx.Tx) error {
			return store.WritePin(t.Context(), tx, id, pin.State{Hash: "h"})
		}},
	} {
		err := pgx.BeginFunc(t.Context(), &flaky{Pool: pool, failOn: tc.failOn}, tc.run)
		if !errors.Is(err, errDown) {
			t.Errorf("failing on %q = %v, want the failure", tc.failOn, err)
		}
	}
	err := pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		_, err := store.HoldPin(t.Context(), tx, "00000000-0000-0000-0000-000000000000")
		return err
	})
	if !errors.Is(err, store.ErrNoAccount) {
		t.Errorf("HoldPin of no account = %v, want ErrNoAccount", err)
	}
}

// A statement of a profile or of the countries that fails is the store's failure.
func TestAFailedStatementOfAProfileIsAFailure(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	p := store.Profile{Pseudonym: "Owl", Country: "US", VoicingSpeed: "normal", DefaultMode: "by_steps"}
	for _, tc := range []struct {
		failOn string
		run    func(tx pgx.Tx) error
	}{
		{"INSERT INTO profiles", func(tx pgx.Tx) error {
			_, err := store.CreateOwnerProfile(t.Context(), tx, id, p)
			return err
		}},
		{"INSERT INTO consents", func(tx pgx.Tx) error {
			_, err := store.CreateOwnerProfile(t.Context(), tx, id, p)
			return err
		}},
		{"FOR UPDATE", func(tx pgx.Tx) error {
			_, err := store.ProfileFor(t.Context(), tx, id, "00000000-0000-0000-0000-000000000000")
			return err
		}},
		{"UPDATE profiles", func(tx pgx.Tx) error { return store.WriteProfile(t.Context(), tx, p) }},
		{"AND owner", func(tx pgx.Tx) error {
			_, err := store.HasOwnerProfile(t.Context(), tx, id)
			return err
		}},
		{"accepted_countries", func(tx pgx.Tx) error {
			_, err := store.CountryAccepted(t.Context(), tx, "US")
			return err
		}},
		{"accepted_countries", func(tx pgx.Tx) error {
			_, err := store.AllowCountry(t.Context(), tx, "US")
			return err
		}},
		{"accepted_countries", func(tx pgx.Tx) error {
			_, err := store.RemoveCountry(t.Context(), tx, "US")
			return err
		}},
		{"accepted_countries", func(tx pgx.Tx) error {
			_, err := store.Countries(t.Context(), tx)
			return err
		}},
		{"SET active_on", func(tx pgx.Tx) error { return store.TouchProfile(t.Context(), tx, id) }},
	} {
		err := pgx.BeginFunc(t.Context(), &flaky{Pool: pool, failOn: tc.failOn}, tc.run)
		if !errors.Is(err, errDown) {
			t.Errorf("failing on %q = %v, want the failure", tc.failOn, err)
		}
	}
}
