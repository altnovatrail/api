package store_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// quotaWorld is an account of a tariff with two profiles, and a family day.
type quotaWorld struct {
	pool                  *pgxpool.Pool
	account, owner, child string
	day                   quota.Day
}

func newQuotaWorld(t *testing.T, tariff string) quotaWorld {
	t.Helper()
	pool := storetest.DB(t)
	w := quotaWorld{pool: pool, account: account(t, pool),
		day: quota.Day{Start: time.Date(2026, 11, 12, 5, 0, 0, 0, time.UTC), End: time.Date(2026, 11, 13, 5, 0, 0, 0, time.UTC)}}
	if _, err := pool.Exec(t.Context(), `UPDATE accounts SET tariff = nullif($2, '') WHERE id = $1`, w.account, tariff); err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		into  *string
		owner bool
	}{{&w.owner, true}, {&w.child, false}} {
		if err := pool.QueryRow(t.Context(), `INSERT INTO profiles
			(account_id, owner, pseudonym, age_category, country, voicing_enabled, default_mode, ask_before_showing)
			VALUES ($1, $2, 'Owl', CASE WHEN $2 THEN 'adult' ELSE 'under_13' END, 'US', false, 'self', NOT $2) RETURNING id`,
			w.account, p.owner).Scan(p.into); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// charge is a charge of amount on the profile.
func (w quotaWorld) charge(profile string, amount quota.Micro) store.Charge {
	return store.Charge{AccountID: w.account, ProfileID: profile, Day: w.day, Amount: amount}
}

// reserve reserves c in a transaction that holds the account's row, as a
// request under a key does.
func (w quotaWorld) reserve(t *testing.T, c store.Charge) quota.Spent {
	t.Helper()
	var spent quota.Spent
	err := pgx.BeginFunc(t.Context(), w.pool, func(tx pgx.Tx) error {
		if err := store.HoldAccount(t.Context(), tx, w.account); err != nil {
			return err
		}
		var err error
		spent, err = store.ReserveQuota(t.Context(), tx, c)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return spent
}

// counter is what the table's counter of the owner's day holds, and whether
// there is one.
func (w quotaWorld) counter(t *testing.T, table, column, id string) (quota.Counter, bool) {
	t.Helper()
	var c quota.Counter
	err := w.pool.QueryRow(t.Context(), `SELECT spent_micro_usd, reserved_micro_usd FROM `+table+`
		WHERE `+column+` = $1 AND day_start = $2`, id, w.day.Start).Scan(&c.Spent, &c.Reserved)
	if errors.Is(err, pgx.ErrNoRows) {
		return quota.Counter{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return c, true
}

// A reserve fits while what the profile and the account have spent and hold in
// reserve leaves room under each cap, and the counters of a day are made by its
// first: one that does not fit writes nothing, and says which cap it met first.
// A settlement takes the reserve off and adds what the call cost, or nothing
// when no call went.
func TestAReserveFitsUnderBothCapsAndASettlementCountsWhatItCost(t *testing.T) {
	t.Parallel()

	w := newQuotaWorld(t, quota.Probe) // a profile $0.08, the account $0.10
	if spent, err := store.QuotaFits(t.Context(), w.pool, w.charge(w.child, quota.TypicalHandwriting)); err != nil || spent != quota.Fits {
		t.Errorf("a typical task on a day with no counter = %q, %v", spent, err)
	}
	if _, found := w.counter(t, "account_quota", "account_id", w.account); found {
		t.Error("QuotaFits made a counter")
	}
	if spent := w.reserve(t, w.charge(w.child, 50_000)); spent != quota.Fits {
		t.Fatalf("a reserve under the caps = %q", spent)
	}
	if spent := w.reserve(t, w.charge(w.child, 30_001)); spent != quota.ProfileCap {
		t.Errorf("a reserve over the profile's cap = %q", spent)
	}
	if spent := w.reserve(t, w.charge(w.owner, 50_001)); spent != quota.AccountCap {
		t.Errorf("a reserve of the owner over the account's cap = %q", spent)
	}
	if spent := w.reserve(t, w.charge(w.owner, 50_000)); spent != quota.Fits {
		t.Errorf("a reserve of the owner to the account's cap = %q", spent)
	}
	if c, _ := w.counter(t, "account_quota", "account_id", w.account); c != (quota.Counter{Reserved: 100_000}) {
		t.Errorf("the account's counter = %+v, want both reserves", c)
	}
	if c, _ := w.counter(t, "profile_quota", "profile_id", w.child); c != (quota.Counter{Reserved: 50_000}) {
		t.Errorf("the child's counter = %+v", c)
	}
	// The child's call cost less than its reserve, the owner's none: no call went.
	if err := store.SettleQuota(t.Context(), w.pool, w.charge(w.child, 50_000), 21_000); err != nil {
		t.Fatal(err)
	}
	if err := store.SettleQuota(t.Context(), w.pool, w.charge(w.owner, 50_000), 0); err != nil {
		t.Fatal(err)
	}
	if c, _ := w.counter(t, "account_quota", "account_id", w.account); c != (quota.Counter{Spent: 21_000}) {
		t.Errorf("the account's counter after = %+v", c)
	}
	if c, _ := w.counter(t, "profile_quota", "profile_id", w.child); c != (quota.Counter{Spent: 21_000}) {
		t.Errorf("the child's counter after = %+v", c)
	}
	if spent, _ := store.QuotaFits(t.Context(), w.pool, w.charge(w.child, quota.TypicalHandwriting)); spent != quota.ProfileCap {
		t.Errorf("a typical task after $0.021 of $0.08 = %q, want the profile's cap", spent)
	}
	// A settlement of a counter that is gone is none, and no failure.
	gone := w.charge(w.child, 1)
	gone.Day.Start = gone.Day.Start.Add(-24 * time.Hour)
	if err := store.SettleQuota(t.Context(), w.pool, gone, 5); err != nil {
		t.Errorf("a settlement of no counter = %v", err)
	}
}

// An account with no tariff, or a tariff with no caps, has no room at all:
// platform AI is closed to it (R11).
func TestAnAccountWithNoTariffHasNoRoom(t *testing.T) {
	t.Parallel()

	for _, tariff := range []string{"", "plus"} {
		w := newQuotaWorld(t, tariff)
		if spent := w.reserve(t, w.charge(w.child, 1)); spent != quota.NoTariff {
			t.Errorf("a reserve on the tariff %q = %q", tariff, spent)
		}
		if _, found := w.counter(t, "account_quota", "account_id", w.account); found {
			t.Errorf("a refused reserve made a counter")
		}
	}
	w := newQuotaWorld(t, quota.ClosedSlice)
	if _, err := store.QuotaFits(t.Context(), w.pool, store.Charge{AccountID: "00000000-0000-0000-0000-000000000000"}); !errors.Is(err, store.ErrNoAccount) {
		t.Errorf("the room of no account = %v", err)
	}
}

// Two reserves of one account beside each other, from two devices, go one
// after the other on the account's row: of two that each fit alone, and not
// together, one fits.
func TestTwoReservesBesideEachOtherDoNotPassTheCap(t *testing.T) {
	t.Parallel()

	w := newQuotaWorld(t, quota.Probe)
	results := make(chan quota.Spent, 2)
	for _, profile := range []string{w.owner, w.child} {
		go func() {
			var spent quota.Spent
			err := pgx.BeginFunc(t.Context(), w.pool, func(tx pgx.Tx) error {
				if err := store.HoldAccount(t.Context(), tx, w.account); err != nil {
					return err
				}
				var err error
				spent, err = store.ReserveQuota(t.Context(), tx, w.charge(profile, 60_000))
				// Hold the row a while, so that the other waits for it.
				time.Sleep(50 * time.Millisecond)
				return err
			})
			if err != nil {
				spent = "failed: " + quota.Spent(err.Error())
			}
			results <- spent
		}()
	}
	got := map[quota.Spent]int{}
	for range 2 {
		got[<-results]++
	}
	if got[quota.Fits] != 1 || got[quota.AccountCap] != 1 {
		t.Errorf("two reserves beside each other = %v, want one to fit and one over the account's cap", got)
	}
	if c, _ := w.counter(t, "account_quota", "account_id", w.account); c.Reserved != 60_000 {
		t.Errorf("the account holds %+v in reserve, want one reserve", c)
	}
}

// The deletion of a profile takes its counters and leaves the account's; the
// deletion of the account takes them all (R12).
func TestTheCountersGoWithTheirProfileAndTheirAccount(t *testing.T) {
	t.Parallel()

	w := newQuotaWorld(t, quota.ClosedSlice)
	w.reserve(t, w.charge(w.child, 9000))
	w.reserve(t, w.charge(w.owner, 9000))
	if err := pgx.BeginFunc(t.Context(), w.pool, func(tx pgx.Tx) error { return store.DeleteProfileRows(t.Context(), tx, w.child) }); err != nil {
		t.Fatal(err)
	}
	if _, found := w.counter(t, "profile_quota", "profile_id", w.child); found {
		t.Error("the deleted profile's counter stayed")
	}
	if c, found := w.counter(t, "account_quota", "account_id", w.account); !found || c.Reserved != 18_000 {
		t.Errorf("the account's counter after the profile's deletion = %+v, %v; want it as it was", c, found)
	}
	mark(t, w.pool, w.account)
	if err := pgx.BeginFunc(t.Context(), w.pool, func(tx pgx.Tx) error { return store.ClearAccount(t.Context(), tx, w.account) }); err != nil {
		t.Fatal(err)
	}
	if count(t, w.pool, "account_quota") != 0 || count(t, w.pool, "profile_quota") != 0 {
		t.Error("the account's deletion left counters")
	}
}

// The flag "platform AI closed" is one for the deployment: set by the first
// close, which keeps its time, and cleared by the operator alone.
func TestTheFlagOfPlatformAIIsOneForTheDeployment(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	if closed, err := store.PlatformAIClosed(t.Context(), pool); err != nil || closed {
		t.Fatalf("a new deployment's platform AI closed = %v, %v", closed, err)
	}
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	if set, err := store.ClosePlatformAI(t.Context(), pool, now); err != nil || !set {
		t.Errorf("the first close = %v, %v", set, err)
	}
	if set, err := store.ClosePlatformAI(t.Context(), pool, now.Add(time.Hour)); err != nil || set {
		t.Errorf("a second close = %v, %v; want the flag set already", set, err)
	}
	var since time.Time
	if err := pool.QueryRow(t.Context(), `SELECT closed_at FROM platform_ai`).Scan(&since); err != nil || !since.Equal(now) {
		t.Errorf("closed since %v (%v), want the first close", since, err)
	}
	if closed, _ := store.PlatformAIClosed(t.Context(), pool); !closed {
		t.Error("the flag set reads open")
	}
	if was, err := store.OpenPlatformAI(t.Context(), pool); err != nil || !was {
		t.Errorf("the opening = %v, %v", was, err)
	}
	if was, err := store.OpenPlatformAI(t.Context(), pool); err != nil || was {
		t.Errorf("a second opening = %v, %v; want it open already", was, err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO platform_ai (one) VALUES (true)`); err == nil {
		t.Error("a second row of the flag went in")
	}
}

// The counters whose day ended before the term go, and the others stay.
func TestTheCountersPastTheirTermAreDropped(t *testing.T) {
	t.Parallel()

	w := newQuotaWorld(t, quota.ClosedSlice)
	w.reserve(t, w.charge(w.child, 9000))
	if n, err := store.DropQuotaCounters(t.Context(), w.pool, w.day.End); err != nil || n != 0 {
		t.Errorf("a drop at the day's end = %d, %v; want none", n, err)
	}
	if n, err := store.DropQuotaCounters(t.Context(), w.pool, w.day.End.Add(time.Second)); err != nil || n != 2 {
		t.Errorf("a drop after the day's end = %d, %v; want the profile's and the account's", n, err)
	}
}

// SetTariff gives the account of the code the tariff, and an account that no
// code is bound to, or one being deleted, is none.
func TestTheTariffOfAnAccountIsSetByItsCode(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	hash := sha256.Sum256([]byte("a code"))
	if err := store.CreateInvite(t.Context(), pool, hash[:], quota.ClosedSlice); err != nil {
		t.Fatal(err)
	}
	if err := store.SetTariff(t.Context(), pool, hash[:], quota.Probe); !errors.Is(err, store.ErrNoAccount) {
		t.Errorf("the tariff of a code no account redeemed = %v, want ErrNoAccount", err)
	}
	id, err := store.CreateAccount(t.Context(), pool, "uid-tariff", hash[:], nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetTariff(t.Context(), pool, hash[:], quota.Probe); err != nil {
		t.Fatal(err)
	}
	var tariff string
	if err := pool.QueryRow(t.Context(), `SELECT tariff FROM accounts WHERE id = $1`, id).Scan(&tariff); err != nil || tariff != quota.Probe {
		t.Errorf("the tariff = %q (%v), want the probe", tariff, err)
	}
	mark(t, pool, id)
	if err := store.SetTariff(t.Context(), pool, hash[:], quota.ClosedSlice); !errors.Is(err, store.ErrNoAccount) {
		t.Errorf("the tariff of an account being deleted = %v, want ErrNoAccount", err)
	}
}

// A statement of the quota that fails is a failure, never a fit or a day.
func TestAFailedStatementOfTheQuotaIsAFailure(t *testing.T) {
	t.Parallel()

	w := newQuotaWorld(t, quota.ClosedSlice)
	now := w.day.Start.Add(time.Hour)
	c := w.charge(w.child, 9000)
	for _, tc := range []struct {
		failOn string
		run    func(tx pgx.Tx) error
	}{
		{"SELECT utc_offset_minutes", func(tx pgx.Tx) error {
			_, err := store.FamilyDay(t.Context(), tx, w.account, nil, now)
			return err
		}},
		{"UPDATE accounts SET utc_offset_minutes", func(tx pgx.Tx) error {
			_, err := store.FamilyDay(t.Context(), tx, w.account, nil, now)
			return err
		}},
		{"SELECT tariff", func(tx pgx.Tx) error { _, err := store.ReserveQuota(t.Context(), tx, c); return err }},
		{"FROM profile_quota", func(tx pgx.Tx) error { _, err := store.ReserveQuota(t.Context(), tx, c); return err }},
		{"INSERT INTO account_quota", func(tx pgx.Tx) error { _, err := store.ReserveQuota(t.Context(), tx, c); return err }},
		{"SELECT closed_at", func(tx pgx.Tx) error { _, err := store.PlatformAIClosed(t.Context(), tx); return err }},
		{"UPDATE platform_ai SET closed_at = $1", func(tx pgx.Tx) error { _, err := store.ClosePlatformAI(t.Context(), tx, now); return err }},
		{"UPDATE platform_ai SET closed_at = NULL", func(tx pgx.Tx) error { _, err := store.OpenPlatformAI(t.Context(), tx); return err }},
		{"DELETE FROM account_quota", func(tx pgx.Tx) error { _, err := store.DropQuotaCounters(t.Context(), tx, now); return err }},
		{"DELETE FROM profile_quota", func(tx pgx.Tx) error { _, err := store.DropQuotaCounters(t.Context(), tx, now); return err }},
	} {
		if err := pgx.BeginFunc(t.Context(), &flaky{Pool: w.pool, failOn: tc.failOn}, tc.run); !errors.Is(err, errDown) {
			t.Errorf("a failed %q = %v, want the failure", tc.failOn, err)
		}
	}
	if err := store.SettleQuota(t.Context(), &flaky{Pool: w.pool, failOn: "UPDATE profile_quota"}, c, 1); !errors.Is(err, errDown) {
		t.Errorf("a failed settlement = %v, want the failure", err)
	}
	if err := store.SetTariff(t.Context(), &flakyQuerier{w.pool, "UPDATE accounts SET tariff"}, []byte("x"), quota.Probe); !errors.Is(err, errDown) {
		t.Errorf("a failed tariff = %v, want the failure", err)
	}
}

// flakyQuerier is a pool whose statements that hold failOn fail.
type flakyQuerier struct {
	*pgxpool.Pool
	failOn string
}

func (f *flakyQuerier) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, f.failOn) {
		return pgconn.CommandTag{}, errDown
	}
	return f.Pool.Exec(ctx, sql, args...)
}
