package store_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

// The checks of a key are none on a new account, and are read as written, in
// their order.
func TestTheChecksOfAKeyAreWrittenAndReadAsTheyAre(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	ctx := t.Context()
	at := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	written := []time.Time{at, at.Add(time.Minute), at.Add(time.Hour)}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		read, err := store.HoldKeyChecks(ctx, tx, id)
		if err != nil {
			return err
		}
		if len(read) != 0 {
			t.Errorf("HoldKeyChecks of a new account = %v, want none", read)
		}
		return store.WriteKeyChecks(ctx, tx, id, written)
	})
	if err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		read, err := store.HoldKeyChecks(ctx, tx, id)
		if err != nil {
			return err
		}
		if !slices.EqualFunc(read, written, time.Time.Equal) {
			t.Errorf("HoldKeyChecks = %v, want %v", read, written)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A statement of the checks of a key that fails is the store's failure, and an
// account that is not there is ErrNoAccount.
func TestAFailedStatementOfTheChecksOfAKeyIsAFailure(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	for _, tc := range []struct {
		failOn string
		run    func(tx pgx.Tx) error
	}{
		{"SELECT key_checked_at", func(tx pgx.Tx) error {
			_, err := store.HoldKeyChecks(t.Context(), tx, id)
			return err
		}},
		{"SET key_checked_at", func(tx pgx.Tx) error {
			return store.WriteKeyChecks(t.Context(), tx, id, []time.Time{time.Now()})
		}},
	} {
		err := pgx.BeginFunc(t.Context(), &flaky{Pool: pool, failOn: tc.failOn}, tc.run)
		if !errors.Is(err, errDown) {
			t.Errorf("failing on %q = %v, want the failure", tc.failOn, err)
		}
	}
	err := pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		_, err := store.HoldKeyChecks(t.Context(), tx, "00000000-0000-0000-0000-000000000000")
		return err
	})
	if !errors.Is(err, store.ErrNoAccount) {
		t.Errorf("HoldKeyChecks of no account = %v, want ErrNoAccount", err)
	}
}
