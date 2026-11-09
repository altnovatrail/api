package store_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

// The consents that wait are the account's consents for a child in that state:
// not a confirmed one, not an adult's own and not another account's.
func TestTheWaitingConsentsAreTheAccountsChildrensThatWait(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id, other := account(t, pool), account(t, pool)
	if _, err := pool.Exec(t.Context(), `INSERT INTO consents (account_id, subject, state, age_category, country, confirmed_at)
		VALUES ($1, 'child', 'waiting', 'under_13', 'US', NULL), ($1, 'child', 'waiting', 'under_13', 'US', NULL),
			($1, 'child', 'confirmed', 'under_13', 'US', now()), ($1, 'adult', 'confirmed', 'adult', 'US', now()),
			($2, 'child', 'waiting', 'under_13', 'US', NULL)`, id, other); err != nil {
		t.Fatal(err)
	}
	var waiting int
	err := pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		var err error
		waiting, err = store.WaitingConsents(t.Context(), tx, id)
		return err
	})
	if err != nil || waiting != 2 {
		t.Errorf("WaitingConsents = %d, %v; want the two that wait", waiting, err)
	}

	err = pgx.BeginFunc(t.Context(), &flaky{Pool: pool, failOn: "FROM consents"}, func(tx pgx.Tx) error {
		_, err := store.WaitingConsents(t.Context(), tx, id)
		return err
	})
	if !errors.Is(err, errDown) {
		t.Errorf("WaitingConsents on a failing statement = %v, want the failure", err)
	}
}
