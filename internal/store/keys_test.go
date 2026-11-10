package store_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/pin"
	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

func TestAKeyIsRecordedWithItsAnswerInItsAccountsSpace(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id, other := account(t, pool), account(t, pool)
	ctx := t.Context()
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, found, err := store.RecordOf(ctx, tx, id, "k"); err != nil || found {
			t.Errorf("RecordOf before = %v, %v; want none", found, err)
		}
		return store.RecordDone(ctx, tx, id, "k", "/m", "sha256:00", store.KeyTarget{Response: []byte{1, 2}})
	})
	if err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		record, found, err := store.RecordOf(ctx, tx, id, "k")
		if err != nil || !found || record.Method != "/m" || record.Fingerprint != "sha256:00" ||
			record.State != store.KeyDone || !bytes.Equal(record.Response, []byte{1, 2}) {
			t.Errorf("RecordOf = %+v, %v, %v; want the record done with its answer", record, found, err)
		}
		// The same key of another account is another key.
		if _, found, err := store.RecordOf(ctx, tx, other, "k"); err != nil || found {
			t.Errorf("RecordOf of another account = %v, %v; want none", found, err)
		}
		return store.RecordDone(ctx, tx, other, "k", "/m", "sha256:00", store.KeyTarget{Response: []byte{}})
	})
	if err != nil {
		t.Errorf("the same key of another account: %v, want it recorded", err)
	}
	// A record done holds its answer, even an empty one.
	if _, err := pool.Exec(ctx, `INSERT INTO idempotency_keys (account_id, key, method, fingerprint, state)
		VALUES ($1, 'no-answer', '/m', 'f', 'done')`, id); err == nil {
		t.Error("a record done with no answer was kept")
	}
}

func TestThePINIsWrittenAndReadAsItIs(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	ctx := t.Context()
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	written := pin.State{Hash: "$argon2id$synthetic", Failed: []time.Time{at, at.Add(time.Minute)}, LockedUntil: at.Add(pin.LockFor)}
	for _, state := range []pin.State{{}, written, {Hash: "$argon2id$other"}} {
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			read, err := store.HoldPin(ctx, tx, id)
			if err != nil {
				return err
			}
			if state.Hash == "" {
				// The account as it was made: no PIN, no count.
				if read.Hash != "" || len(read.Failed) != 0 || !read.LockedUntil.IsZero() {
					t.Errorf("HoldPin of a new account = %+v, want nothing", read)
				}
				return nil
			}
			return store.WritePin(ctx, tx, id, state)
		})
		if err != nil {
			t.Fatal(err)
		}
		if state.Hash == "" {
			continue
		}
		err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			read, err := store.HoldPin(ctx, tx, id)
			if err != nil {
				return err
			}
			if read.Hash != state.Hash || len(read.Failed) != len(state.Failed) || (len(read.Failed) > 0 && !read.Failed[0].Equal(state.Failed[0])) ||
				!read.LockedUntil.Equal(state.LockedUntil) {
				t.Errorf("HoldPin = %+v, want %+v", read, state)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
