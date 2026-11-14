package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/altnovatrail/api/internal/store"
)

// storageOn switches the storage of the rich profile's works on.
func (r rich) storageOn(t *testing.T) {
	t.Helper()
	version := int32(1)
	if err := pgx.BeginFunc(t.Context(), r.pool, func(tx pgx.Tx) error {
		return store.SwitchWorkStorageOn(t.Context(), tx, r.profile, store.GiverOwner, &version, time.Now())
	}); err != nil {
		t.Fatal(err)
	}
}

func (r rich) put(ctx context.Context, w store.Work) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error { return store.PutWork(ctx, tx, r.account, r.profile, w) })
}

// A work is kept, while the storage is on, in the place of the one kept of its
// attempt before; it is read with its bytes, listed without them, and erased,
// and who decides its storage is the giver of the profile's consent.
func TestAWorkIsKeptInThePlaceOfTheOneBefore(t *testing.T) {
	t.Parallel()

	r := newRich(t)
	ctx := t.Context()
	if _, err := r.pool.Exec(ctx, `DELETE FROM works`); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	w := store.Work{AttemptID: r.attempt, Kind: store.WorkEssay, SizeBytes: 2, StoredAt: at, Bytes: []byte{1, 2}}
	if err := r.put(ctx, w); !errors.Is(err, store.ErrStorageOff) {
		t.Errorf("PutWork with the storage off = %v, want ErrStorageOff", err)
	}
	r.storageOn(t)
	if err := r.put(ctx, w); err != nil {
		t.Fatal(err)
	}
	w.Kind, w.SizeBytes, w.StoredAt, w.Bytes = store.WorkHandwriting, 3, at.Add(time.Minute), []byte{3, 4, 5}
	if err := r.put(ctx, w); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetWork(ctx, r.pool, r.profile, r.attempt); err != nil || got.Kind != w.Kind || got.SizeBytes != 3 ||
		!got.StoredAt.Equal(w.StoredAt) || string(got.Bytes) != "\x03\x04\x05" {
		t.Errorf("GetWork = %+v, %v; want the last one put", got, err)
	}
	if kind, err := store.KeptWorkKind(ctx, r.pool, r.profile, r.attempt); err != nil || kind != store.WorkHandwriting {
		t.Errorf("KeptWorkKind = %q, %v", kind, err)
	}
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		works, err := store.ListWorks(ctx, tx, r.profile)
		if err != nil || len(works) != 1 || works[0].AttemptID != r.attempt || works[0].Bytes != nil || works[0].SizeBytes != 3 {
			t.Errorf("ListWorks = %+v, %v; want one, without its bytes", works, err)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if giver, err := store.WorkDecider(ctx, r.pool, r.account, r.profile); err != nil || giver != store.GiverOwner {
		t.Errorf("WorkDecider = %q, %v; want the owner", giver, err)
	}

	none := "00000000-0000-0000-0000-000000000000"
	if err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error { return store.PutWork(ctx, tx, none, r.profile, w) }); !errors.Is(err, store.ErrNoProfile) {
		t.Errorf("PutWork of another account = %v, want ErrNoProfile", err)
	}
	if err := r.put(ctx, store.Work{AttemptID: none, Kind: store.WorkDrawing, StoredAt: at, Bytes: []byte{}}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("PutWork of no attempt = %v, want ErrNotFound", err)
	}
	if _, err := store.WorkDecider(ctx, r.pool, none, r.profile); !errors.Is(err, store.ErrNoProfile) {
		t.Errorf("WorkDecider of another account = %v, want ErrNoProfile", err)
	}
	if err := store.DeleteWork(ctx, r.pool, r.profile, r.attempt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetWork(ctx, r.pool, r.profile, r.attempt); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetWork after DeleteWork = %v, want ErrNotFound", err)
	}
	if _, err := store.KeptWorkKind(ctx, r.pool, r.profile, r.attempt); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("KeptWorkKind after DeleteWork = %v, want ErrNotFound", err)
	}
}

// A work put and the switch of the storage off wait for each other on the
// profile's row: a work put before the switch commits is erased by it, and one
// after is refused (R70). Each side waits here no longer than its lock_timeout.
func TestAWorkPutAndTheSwitchOffWaitForEachOther(t *testing.T) {
	t.Parallel()

	r := newRich(t)
	ctx := t.Context()
	r.storageOn(t)
	w := store.Work{AttemptID: r.attempt, Kind: store.WorkHandwriting, SizeBytes: 1, StoredAt: time.Now(), Bytes: []byte{7}}
	waits := func(fn func(tx pgx.Tx) error) {
		t.Helper()
		err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '200ms'`); err != nil {
				return err
			}
			return fn(tx)
		})
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
			t.Errorf("the other side = %v, want it to wait for the lock", err)
		}
	}

	off, err := r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SwitchWorkStorageOff(ctx, off, r.profile, time.Now()); err != nil {
		t.Fatal(err)
	}
	waits(func(tx pgx.Tx) error { return store.PutWork(ctx, tx, r.account, r.profile, w) })
	if err := off.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	put, err := r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutWork(ctx, put, r.account, r.profile, w); err != nil {
		t.Fatal(err)
	}
	waits(func(tx pgx.Tx) error { return store.SwitchWorkStorageOff(ctx, tx, r.profile, time.Now()) })
	if err := put.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error { return store.SwitchWorkStorageOff(ctx, tx, r.profile, time.Now()) }); err != nil {
		t.Fatal(err)
	}
	if n := count(t, r.pool, "works"); n != 0 {
		t.Errorf("the works after the switch off = %d, want none", n)
	}
	if err := r.put(ctx, w); !errors.Is(err, store.ErrStorageOff) {
		t.Errorf("PutWork after the switch off = %v, want ErrStorageOff", err)
	}
}

// Each statement on the works that fails is the store's failure.
func TestAFailedStatementOnTheWorksIsAFailure(t *testing.T) {
	t.Parallel()

	r := newRich(t)
	ctx := t.Context()
	r.storageOn(t)
	w := store.Work{AttemptID: r.attempt, Kind: store.WorkHandwriting, SizeBytes: 1, StoredAt: time.Now(), Bytes: []byte{7}}
	for _, tc := range []struct {
		failOn string
		run    func(tx pgx.Tx) error
	}{
		{"SELECT work_storage FROM profiles", func(tx pgx.Tx) error { return store.PutWork(ctx, tx, r.account, r.profile, w) }},
		{"INSERT INTO works", func(tx pgx.Tx) error { return store.PutWork(ctx, tx, r.account, r.profile, w) }},
		{"SELECT attempt_id, kind, size_bytes", func(tx pgx.Tx) error {
			_, err := store.ListWorks(ctx, tx, r.profile)
			return err
		}},
		{"SELECT kind, size_bytes", func(tx pgx.Tx) error {
			_, err := store.GetWork(ctx, tx, r.profile, r.attempt)
			return err
		}},
		{"SELECT kind FROM works", func(tx pgx.Tx) error {
			_, err := store.KeptWorkKind(ctx, tx, r.profile, r.attempt)
			return err
		}},
		{"DELETE FROM works", func(tx pgx.Tx) error { return store.DeleteWork(ctx, tx, r.profile, r.attempt) }},
		{"SELECT giver FROM consents", func(tx pgx.Tx) error {
			_, err := store.WorkDecider(ctx, tx, r.account, r.profile)
			return err
		}},
	} {
		err := pgx.BeginFunc(ctx, &flaky{Pool: r.pool, failOn: tc.failOn}, tc.run)
		if !errors.Is(err, errDown) {
			t.Errorf("failing on %q = %v, want the failure", tc.failOn, err)
		}
	}
}
