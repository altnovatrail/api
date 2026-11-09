package store_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

func TestTheRowsOfAnAccountGoAndTheMinimalRecordsStay(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	ctx := t.Context()
	var profileID, consentID string
	if err := pool.QueryRow(ctx, `INSERT INTO profiles (account_id, owner, pseudonym, age_category, country,
			voicing_enabled, default_mode, ask_before_showing)
		VALUES ($1, false, 'Fox', 'under_13', 'US', true, 'by_steps', true) RETURNING id`, id).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO consents (account_id, profile_id, subject, state, age_category, country, confirmed_at)
		VALUES ($1, $2, 'child', 'confirmed', 'under_13', 'US', now()) RETURNING id`, id, profileID).Scan(&consentID); err != nil {
		t.Fatal(err)
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := store.MarkDeleting(ctx, tx, id); err != nil {
			return err
		}
		return store.ClearAccount(ctx, tx, id)
	})
	if err != nil {
		t.Fatal(err)
	}
	var method string
	if err := pool.QueryRow(ctx, `SELECT method FROM child_consent_records WHERE id = $1`, consentID).Scan(&method); err != nil || method != "" {
		t.Errorf("the minimal record = %q, %v; want it, with no method recorded", method, err)
	}
	if n := count(t, pool, "profiles"); n != 0 {
		t.Errorf("profiles = %d after the clearing, want none", n)
	}
	// The last step deletes the row, and a repeat finds it gone; the record stays.
	for range 2 {
		if err := store.DeleteAccountRow(ctx, pool, id); err != nil {
			t.Fatal(err)
		}
	}
	if count(t, pool, "accounts") != 0 || count(t, pool, "child_consent_records") != 1 {
		t.Error("want the account gone and its record kept")
	}
}

func TestAFailedStatementOfADeletionIsAFailure(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	for _, tc := range []struct {
		failOn string
		run    func(tx pgx.Tx) error
	}{
		{"INSERT INTO child_consent_records", func(tx pgx.Tx) error { return store.DeleteProfileRows(t.Context(), tx, id) }},
		{"DELETE FROM profiles", func(tx pgx.Tx) error { return store.DeleteProfileRows(t.Context(), tx, id) }},
		{"UPDATE invite_codes", func(tx pgx.Tx) error { return store.ClearAccount(t.Context(), tx, id) }},
	} {
		if err := pgx.BeginFunc(t.Context(), &flaky{Pool: pool, failOn: tc.failOn}, tc.run); !errors.Is(err, errDown) {
			t.Errorf("failing on %q = %v, want the failure", tc.failOn, err)
		}
	}
	for _, failOn := range []string{"FOR UPDATE", "DELETE FROM consents", "DELETE FROM accounts"} {
		if err := store.DeleteAccountRow(t.Context(), &flaky{Pool: pool, failOn: failOn}, id); !errors.Is(err, errDown) {
			t.Errorf("DeleteAccountRow failing on %q = %v, want the failure", failOn, err)
		}
	}
}
