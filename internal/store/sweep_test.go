package store_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

// today is the date the tests' rows are judged at, and a year ago its term.
var (
	today   = time.Date(2026, 11, 9, 15, 0, 0, 0, time.UTC)
	yearAgo = today.AddDate(-1, 0, 0)
)

// lastRequest writes the date of the account's last request.
func lastRequest(t *testing.T, pool *pgxpool.Pool, id string, on time.Time) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `UPDATE accounts SET last_request_on = $2 WHERE id = $1`, id, on); err != nil {
		t.Fatal(err)
	}
}

// ownerProfile makes the account's owner's profile, active last on on.
func ownerProfile(t *testing.T, pool *pgxpool.Pool, accountID string, on time.Time) string {
	t.Helper()

	var id string
	if err := pool.QueryRow(t.Context(), `INSERT INTO profiles
		(account_id, owner, pseudonym, age_category, country, voicing_enabled, default_mode, ask_before_showing, active_on)
		VALUES ($1, true, 'Owl', 'adult', 'US', false, 'self', false, $2) RETURNING id`, accountID, on).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func ids(refs []store.AccountRef) []string {
	var got []string
	for _, ref := range refs {
		got = append(got, ref.ID)
	}
	return got
}

// The marks are chosen by their time, a page at a time past the last ID seen.
func TestTheMarksAreChosenByTheirTimeAndPaged(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	var marked []string
	for range 3 {
		id := account(t, pool)
		mark(t, pool, id)
		marked = append(marked, id)
	}
	account(t, pool) // not marked
	slices.Sort(marked)

	now := time.Now().Add(time.Minute)
	first, err := store.MarkedBefore(t.Context(), pool, now, "", 2)
	if err != nil || !slices.Equal(ids(first), marked[:2]) {
		t.Fatalf("the first page = %v, %v; want %v", ids(first), err, marked[:2])
	}
	if first[0].UID == "" {
		t.Error("a mark with no UID")
	}
	rest, err := store.MarkedBefore(t.Context(), pool, now, first[1].ID, 2)
	if err != nil || !slices.Equal(ids(rest), marked[2:]) {
		t.Errorf("the next page = %v, %v; want %v", ids(rest), err, marked[2:])
	}
	// A mark younger than the time is a request's still going.
	if young, err := store.MarkedBefore(t.Context(), pool, time.Now().Add(-time.Minute), "", 10); err != nil || len(young) != 0 {
		t.Errorf("marks before a minute ago = %v, %v; want none", ids(young), err)
	}
}

// An account is idle after 12 months without a request: a day more than that,
// and not on the day itself; one marked, or with no date yet, is not chosen.
func TestTheIdleAccountsAreThoseOfMoreThanAYear(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	idle := account(t, pool)
	lastRequest(t, pool, idle, yearAgo.AddDate(0, 0, -1))
	onTheDay := account(t, pool)
	lastRequest(t, pool, onTheDay, yearAgo)
	marked := account(t, pool)
	lastRequest(t, pool, marked, yearAgo.AddDate(0, 0, -1))
	mark(t, pool, marked)
	account(t, pool) // no date

	got, err := store.IdleAccounts(t.Context(), pool, yearAgo, "", 10)
	if err != nil || !slices.Equal(ids(got), []string{idle}) {
		t.Fatalf("IdleAccounts = %v, %v; want the one idle", ids(got), err)
	}
	if more, err := store.IdleAccounts(t.Context(), pool, yearAgo, idle, 10); err != nil || len(more) != 0 {
		t.Errorf("past the last = %v, %v; want none", ids(more), err)
	}

	err = pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		for id, want := range map[string]bool{idle: true, onTheDay: false, marked: false} {
			ok, err := store.MarkIdle(t.Context(), tx, id, yearAgo)
			if err != nil || ok != want {
				t.Errorf("MarkIdle = %v, %v; want %v", ok, err, want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, "accounts WHERE deleting_at IS NOT NULL"); n != 2 {
		t.Errorf("%d accounts marked, want the idle one and the one marked before", n)
	}
}

// A request that writes today's date while the job takes the row keeps the
// account: the lock waits for it and reads the row again (П-9).
func TestAnAccountThatCameBackWhileChosenIsKept(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	lastRequest(t, pool, id, yearAgo.AddDate(0, 0, -1))
	request, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.TouchAccount(t.Context(), request, id); err != nil {
		t.Fatal(err)
	}

	marked := make(chan bool)
	go func() {
		var ok bool
		_ = pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
			var err error
			ok, err = store.MarkIdle(context.Background(), tx, id, yearAgo)
			return err
		})
		marked <- ok
	}()
	select {
	case <-marked:
		t.Fatal("MarkIdle did not wait for the request")
	case <-time.After(200 * time.Millisecond):
	}
	if err := request.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if <-marked {
		t.Error("the account that came back was marked")
	}
}

// A profile is idle after 12 months without activity, and only of an account
// not marked; one touched while the job takes it is kept.
func TestTheIdleProfilesAreThoseOfMoreThanAYear(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	owner := account(t, pool)
	idle := ownerProfile(t, pool, owner, yearAgo.AddDate(0, 0, -1))
	onTheDay := ownerProfile(t, pool, account(t, pool), yearAgo)
	marked := account(t, pool)
	ownerProfile(t, pool, marked, yearAgo.AddDate(0, 0, -1))
	mark(t, pool, marked)

	got, err := store.IdleProfiles(t.Context(), pool, yearAgo, "", 10)
	if err != nil || len(got) != 1 || got[0] != (store.ProfileRef{AccountID: owner, ID: idle}) {
		t.Fatalf("IdleProfiles = %v, %v; want the one idle", got, err)
	}
	if more, err := store.IdleProfiles(t.Context(), pool, yearAgo, idle, 10); err != nil || len(more) != 0 {
		t.Errorf("past the last = %v, %v; want none", more, err)
	}
	err = pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		if ok, err := store.ProfileIdle(t.Context(), tx, owner, idle, yearAgo); err != nil || !ok {
			t.Errorf("ProfileIdle of the idle one = %v, %v; want true", ok, err)
		}
		if ok, err := store.ProfileIdle(t.Context(), tx, owner, onTheDay, yearAgo); err != nil || ok {
			t.Errorf("ProfileIdle of another account's = %v, %v; want false", ok, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.TouchProfile(t.Context(), pool, idle); err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		ok, err := store.ProfileIdle(t.Context(), tx, owner, idle, yearAgo)
		if err != nil || ok {
			t.Errorf("ProfileIdle of one active today = %v, %v; want false", ok, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The job finds which users have an account and which tuples a profile.
func TestTheAccountsAndProfilesThereAreFound(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	withAccount := account(t, pool)
	marked := account(t, pool)
	mark(t, pool, marked)
	var uids []string
	if err := pool.QueryRow(t.Context(), `SELECT array_agg(auth_uid) FROM accounts`).Scan(&uids); err != nil {
		t.Fatal(err)
	}
	have, err := store.HaveAccounts(t.Context(), pool, append(slices.Clone(uids), "uid-none"))
	if err != nil || len(have) != 2 || have["uid-none"] {
		t.Errorf("HaveAccounts = %v, %v; want the two, the marked one too", have, err)
	}

	profileID := ownerProfile(t, pool, withAccount, today)
	there, err := store.ProfilesThere(t.Context(), pool, []string{profileID, "0190f0a0-0000-7000-8000-0000000000ff", "not-a-uuid"})
	if err != nil || !maps.Equal(there, map[string]string{profileID: withAccount}) {
		t.Errorf("ProfilesThere = %v, %v; want the one there, with its account", there, err)
	}
}

// The records of keys older than a day go, a batch at a time; younger stay.
func TestTheOldKeysAreDropped(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	if _, err := pool.Exec(t.Context(), `INSERT INTO idempotency_keys (account_id, key, method, fingerprint, state, created_at)
		SELECT $1::uuid, 'old-' || g, '/m', 'f', 'going', now() - interval '25 hours' FROM generate_series(1, 1500) AS g
		UNION ALL SELECT $1::uuid, 'young', '/m', 'f', 'going', now() - interval '23 hours'`, id); err != nil {
		t.Fatal(err)
	}
	dropped, err := store.DropKeys(t.Context(), pool, time.Now().Add(-24*time.Hour))
	if err != nil || dropped != 1500 {
		t.Fatalf("DropKeys = %d, %v; want the 1500 old", dropped, err)
	}
	if n := count(t, pool, "idempotency_keys"); n != 1 {
		t.Errorf("%d records left, want the young one", n)
	}
}

// The minimal records of consents deleted before the term go, a batch at a time;
// later ones stay.
func TestTheOldConsentRecordsAreDropped(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	if _, err := pool.Exec(t.Context(), `INSERT INTO child_consent_records (id, consented_at, method, deleted_at)
		SELECT gen_random_uuid(), now() - interval '6 years', 'signed_form', now() - interval '5 years 1 day'
		FROM generate_series(1, 1500)
		UNION ALL SELECT gen_random_uuid(), now() - interval '6 years', 'signed_form', now() - interval '4 years'`); err != nil {
		t.Fatal(err)
	}
	dropped, err := store.DropConsentRecords(t.Context(), pool, time.Now().AddDate(-5, 0, 0))
	if err != nil || dropped != 1500 {
		t.Fatalf("DropConsentRecords = %d, %v; want the 1500 old", dropped, err)
	}
	if n := count(t, pool, "child_consent_records"); n != 1 {
		t.Errorf("%d records left, want the later one", n)
	}
}

// Every query of the job on a database that is down is a failure of the store.
func TestTheJobsQueriesFailOnADatabaseDown(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	down, err := pgxpool.New(t.Context(), "postgres://u@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	down.Close()
	for name, call := range map[string]func() error{
		"MarkedBefore":  func() error { _, err := store.MarkedBefore(t.Context(), down, today, "", 1); return err },
		"IdleAccounts":  func() error { _, err := store.IdleAccounts(t.Context(), down, today, "", 1); return err },
		"IdleProfiles":  func() error { _, err := store.IdleProfiles(t.Context(), down, today, "", 1); return err },
		"HaveAccounts":  func() error { _, err := store.HaveAccounts(t.Context(), down, []string{"u"}); return err },
		"ProfilesThere": func() error { _, err := store.ProfilesThere(t.Context(), down, []string{"p"}); return err },
		"DropKeys":      func() error { _, err := store.DropKeys(t.Context(), down, today); return err },
		"OpenAttemptsOn": func() error {
			_, err := store.OpenAttemptsOn(t.Context(), down, []string{"d"}, "", 1)
			return err
		},
		"OpenRunsOn": func() error { _, err := store.OpenRunsOn(t.Context(), down, []string{"d"}, "", 1); return err },
		"DropConsentRecords": func() error {
			_, err := store.DropConsentRecords(t.Context(), down, today)
			return err
		},
		"MarkIdle": func() error {
			return pgx.BeginFunc(t.Context(), &flaky{Pool: pool, failOn: "FOR UPDATE"}, func(tx pgx.Tx) error {
				_, err := store.MarkIdle(t.Context(), tx, id, today)
				return err
			})
		},
		"MarkIdle's mark": func() error {
			lastRequest(t, pool, id, yearAgo.AddDate(0, 0, -1))
			return pgx.BeginFunc(t.Context(), &flaky{Pool: pool, failOn: "UPDATE accounts"}, func(tx pgx.Tx) error {
				_, err := store.MarkIdle(t.Context(), tx, id, yearAgo)
				return err
			})
		},
		"ProfileIdle": func() error {
			return pgx.BeginFunc(t.Context(), &flaky{Pool: pool, failOn: "FOR UPDATE"}, func(tx pgx.Tx) error {
				_, err := store.ProfileIdle(t.Context(), tx, id, id, today)
				return err
			})
		},
	} {
		if err := call(); err == nil || errors.Is(err, store.ErrNoAccount) {
			t.Errorf("%s on a database down: error = %v, want a failure", name, err)
		}
	}
}

// The open attempts and lesson runs on the packages named are chosen by ID, a
// page at a time past the last seen, with their profile and account: not those
// closed, nor on other packages, nor of an account a deletion marked.
func TestTheOpenOnThePackagesAreChosenAndPaged(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	open, err := e.attempt(t, "")
	if err != nil {
		t.Fatal(err)
	}
	run, err := e.run(t, "example/m/l1")
	if err != nil {
		t.Fatal(err)
	}
	// A profile with its attempt and run closed, and one on another package.
	other := &e3{pool: e.pool, accountID: e.accountID, profileID: ownerProfile(t, e.pool, account(t, e.pool), time.Now())}
	closed, err := other.attempt(t, "")
	if err != nil {
		t.Fatal(err)
	}
	ended, err := other.run(t, "example/m/l1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(t.Context(), `UPDATE attempts SET close_reason = 'abandoned', closed_at = now() WHERE id = $1`, closed); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(t.Context(), `UPDATE lesson_runs SET end_reason = 'abandoned', ended_at = now() WHERE id = $1`, ended); err != nil {
		t.Fatal(err)
	}
	marked := account(t, e.pool)
	deleting := &e3{pool: e.pool, accountID: marked, profileID: ownerProfile(t, e.pool, marked, time.Now())}
	if _, err := deleting.attempt(t, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := deleting.run(t, "example/m/l1"); err != nil {
		t.Fatal(err)
	}
	mark(t, e.pool, marked)

	want := store.OpenOn{ProfileID: e.profileID, AccountID: e.accountID}
	for name, choose := range map[string]func(digests []string, after string) ([]store.OpenOn, error){
		open: func(digests []string, after string) ([]store.OpenOn, error) {
			return store.OpenAttemptsOn(t.Context(), e.pool, digests, after, 10)
		},
		run: func(digests []string, after string) ([]store.OpenOn, error) {
			return store.OpenRunsOn(t.Context(), e.pool, digests, after, 10)
		},
	} {
		want.ID = name
		if got, err := choose([]string{digest}, ""); err != nil || len(got) != 1 || got[0] != want {
			t.Errorf("chosen = %v, %v; want %v alone", got, err, want)
		}
		if got, err := choose([]string{digest}, name); err != nil || len(got) != 0 {
			t.Errorf("past the last = %v, %v; want none", got, err)
		}
		if got, err := choose([]string{"sha256:" + strings.Repeat("b", 64)}, ""); err != nil || len(got) != 0 {
			t.Errorf("on another package = %v, %v; want none", got, err)
		}
	}
}
