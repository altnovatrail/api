package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

// familyDay is the account's family day after a request at now that sent the
// offset, in a transaction of its own that holds the account's row.
func familyDay(t *testing.T, pool *pgxpool.Pool, accountID string, offset *int32, now time.Time) quota.Day {
	t.Helper()
	var d quota.Day
	err := pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		if err := store.HoldAccount(t.Context(), tx, accountID); err != nil {
			return err
		}
		var err error
		d, err = store.FamilyDay(t.Context(), tx, accountID, offset, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// kept is the offset and the day the account's row keeps.
func kept(t *testing.T, pool *pgxpool.Pool, accountID string) (offset *int32, start, end *time.Time) {
	t.Helper()
	if err := pool.QueryRow(t.Context(), `SELECT utc_offset_minutes, day_starts_at, day_ends_at FROM accounts WHERE id = $1`,
		accountID).Scan(&offset, &start, &end); err != nil {
		t.Fatal(err)
	}
	return offset, start, end
}

func minutes(m int32) *int32 { return &m }

// The account keeps the offset a request sent and the day that holds now, and
// writes only what changed; a read of today writes nothing, and the deletion of
// the account clears both (R11, R12).
func TestTheAccountKeepsItsFamilyDay(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	if d, err := store.Today(t.Context(), pool, id, now); err != nil || d.Start != now.Truncate(24*time.Hour) {
		t.Errorf("Today of an account with no day = %+v, %v; want the day of UTC", d, err)
	}
	if offset, start, end := kept(t, pool, id); offset != nil || start != nil || end != nil {
		t.Errorf("Today wrote %v %v %v", offset, start, end)
	}
	d := familyDay(t, pool, id, minutes(-300), now)
	offset, start, end := kept(t, pool, id)
	if offset == nil || *offset != -300 || !start.Equal(d.Start) || !end.Equal(d.End) || d.End != time.Date(2026, 11, 13, 5, 0, 0, 0, time.UTC) {
		t.Errorf("the day kept = %v %v %v, want −5 h and %+v", offset, start, end, d)
	}
	before := version(t, pool, id)
	if again := familyDay(t, pool, id, minutes(-300), now.Add(time.Hour)); again != d || version(t, pool, id) != before {
		t.Errorf("a request within the day = %+v, and the row was written: %v", again, version(t, pool, id) != before)
	}
	familyDay(t, pool, id, minutes(7), now.Add(2*time.Hour))
	if offset, _, _ := kept(t, pool, id); *offset != -300 {
		t.Errorf("an offset out of bounds was kept: %d", *offset)
	}
	if today, err := store.Today(t.Context(), pool, id, d.End); err != nil || today.Start != d.End {
		t.Errorf("Today after the day's end = %+v, %v; want the next day", today, err)
	}

	mark(t, pool, id)
	if err := pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error { return store.ClearAccount(t.Context(), tx, id) }); err != nil {
		t.Fatal(err)
	}
	if offset, start, end := kept(t, pool, id); offset != nil || start != nil || end != nil {
		t.Errorf("the deletion left %v %v %v", offset, start, end)
	}
	if _, err := store.Today(t.Context(), pool, "00000000-0000-0000-0000-000000000000", now); !errors.Is(err, store.ErrNoAccount) {
		t.Errorf("Today of no account = %v, want ErrNoAccount", err)
	}
}
