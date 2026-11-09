package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

// uidOf is the sign-in service's UID of the account.
func uidOf(t *testing.T, pool *pgxpool.Pool, id string) string {
	t.Helper()

	var uid string
	if err := pool.QueryRow(t.Context(), `SELECT auth_uid FROM accounts WHERE id = $1`, id).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	return uid
}

func TestAnAccountIsFoundByItsUID(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	uid := uidOf(t, pool, id)

	found, err := store.AccountByUID(t.Context(), pool, uid)
	if err != nil || found != (store.Account{ID: id}) {
		t.Errorf("AccountByUID = %+v, %v; want %s, not deleting", found, err, id)
	}
	mark(t, pool, id)
	if found, err := store.AccountByUID(t.Context(), pool, uid); err != nil || !found.Deleting {
		t.Errorf("AccountByUID of a marked account = %+v, %v; want deleting", found, err)
	}
	if _, err := store.AccountByUID(t.Context(), pool, "no-such-uid"); !errors.Is(err, store.ErrNoAccount) {
		t.Errorf("AccountByUID of a UID with no account = %v, want ErrNoAccount", err)
	}
}

// version is the row's xmin: the transaction that last wrote it.
func version(t *testing.T, pool *pgxpool.Pool, id string) string {
	t.Helper()

	var xmin string
	if err := pool.QueryRow(t.Context(), `SELECT xmin::text FROM accounts WHERE id = $1`, id).Scan(&xmin); err != nil {
		t.Fatal(err)
	}
	return xmin
}

func TestTheDateOfTheLastRequestIsWrittenOnceADay(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	if _, err := pool.Exec(t.Context(), `UPDATE accounts SET last_request_on = DATE '2026-01-01' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	writes := 0
	for range 3 {
		before := version(t, pool, id)
		if err := store.TouchAccount(t.Context(), pool, id); err != nil {
			t.Fatal(err)
		}
		if version(t, pool, id) != before {
			writes++
		}
	}
	var today bool
	if err := pool.QueryRow(t.Context(),
		`SELECT last_request_on = (now() AT TIME ZONE 'UTC')::date FROM accounts WHERE id = $1`, id).Scan(&today); err != nil {
		t.Fatal(err)
	}
	if !today || writes != 1 {
		t.Errorf("after three requests: today = %v, %d writes; want today and one write", today, writes)
	}
}

func TestAProfileIsTheAccountsOrNone(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	owner, other := account(t, pool), account(t, pool)
	var profileID string
	err := pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		if err := profile(t.Context(), tx, owner); err != nil {
			return err
		}
		return tx.QueryRow(t.Context(), `SELECT id FROM profiles WHERE account_id = $1`, owner).Scan(&profileID)
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		account string
		profile string
		want    bool
	}{
		{"the account's own", owner, profileID, true},
		{"another account's", other, profileID, false},
		{"one that is not there", owner, "00000000-0000-0000-0000-000000000000", false},
	} {
		if got, _, err := store.ProfileOf(t.Context(), pool, tc.account, tc.profile); err != nil || got != tc.want {
			t.Errorf("%s: ProfileOf = %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
}

// A read that cannot run says so, rather than answering as if there were no row.
func TestAReadThatFailsIsAnError(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	pool.Close()
	if _, err := store.AccountByUID(t.Context(), pool, "uid"); err == nil || errors.Is(err, store.ErrNoAccount) {
		t.Errorf("AccountByUID on a closed pool = %v, want a failure", err)
	}
	if err := store.TouchAccount(t.Context(), pool, "00000000-0000-0000-0000-000000000000"); err == nil {
		t.Error("TouchAccount on a closed pool: error = nil")
	}
	if _, _, err := store.ProfileOf(t.Context(), pool, "a", "b"); err == nil {
		t.Error("ProfileOf on a closed pool: error = nil")
	}
	if _, err := store.ConsentGiven(t.Context(), pool, "00000000-0000-0000-0000-000000000000"); err == nil {
		t.Error("ConsentGiven on a closed pool: error = nil")
	}
}

func TestAConsentIsGivenOnceConfirmed(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	var profileID string
	err := pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		if err := profile(t.Context(), tx, id); err != nil {
			return err
		}
		return tx.QueryRow(t.Context(), `SELECT id FROM profiles WHERE account_id = $1`, id).Scan(&profileID)
	})
	if err != nil {
		t.Fatal(err)
	}
	if given, err := store.ConsentGiven(t.Context(), pool, profileID); err != nil || given {
		t.Errorf("ConsentGiven with none = %v, %v; want false", given, err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO consents
		(account_id, profile_id, subject, state, age_category, country, confirmed_at)
		VALUES ($1, $2, 'adult', 'confirmed', 'adult', 'US', now())`, id, profileID); err != nil {
		t.Fatal(err)
	}
	if given, err := store.ConsentGiven(t.Context(), pool, profileID); err != nil || !given {
		t.Errorf("ConsentGiven confirmed = %v, %v; want true", given, err)
	}
}

// An account touched today needs no write; one touched before, or never, does.
func TestAnAccountKnowsWhetherItIsTouchedToday(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	uid := uidOf(t, pool, id)
	found, err := store.AccountByUID(t.Context(), pool, uid)
	if err != nil || found.TouchedOn(time.Now()) {
		t.Errorf("an account never touched: %+v, %v; want it not touched", found, err)
	}
	if err := store.TouchAccount(t.Context(), pool, id); err != nil {
		t.Fatal(err)
	}
	found, err = store.AccountByUID(t.Context(), pool, uid)
	if err != nil || !found.TouchedOn(time.Now()) {
		t.Errorf("an account touched today: %+v, %v; want it touched", found, err)
	}
	if found.TouchedOn(time.Now().Add(48 * time.Hour)) {
		t.Error("an account touched today is touched two days on, too")
	}
}
