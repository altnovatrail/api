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

func moduleIDs(modules []store.ConnectedModule) []string {
	var ids []string
	for _, m := range modules {
		ids = append(ids, m.ModuleID)
	}
	return ids
}

// A module is connected once, in the order of the connections and then by its
// bytes, disconnected without a trace, and goes with its profile.
func TestTheModulesOfAProfileAreConnectedOnceAndGoWithIt(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	ctx := t.Context()
	accountID := account(t, pool)
	profileID := ownerProfile(t, pool, accountID, time.Now())

	// The owner's profile of this test has no consent: one is written.
	learner, err := store.LearnerOf(ctx, pool, accountID, profileID)
	if err != nil || learner != (store.Learner{AgeCategory: "adult", Country: "US"}) {
		t.Errorf("LearnerOf with no consent = %+v, %v", learner, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO consents (account_id, profile_id, subject, state, age_category, country, confirmed_at)
		VALUES ($1, $2, 'adult', 'confirmed', 'adult', 'US', now())`, accountID, profileID); err != nil {
		t.Fatal(err)
	}
	learner, err = store.LearnerOf(ctx, pool, accountID, profileID)
	if err != nil || learner != (store.Learner{AgeCategory: "adult", Consented: true, Country: "US"}) {
		t.Errorf("LearnerOf = %+v, %v; want an adult with the consent", learner, err)
	}
	if _, err := store.LearnerOf(ctx, pool, account(t, pool), profileID); !errors.Is(err, store.ErrNoProfile) {
		t.Errorf("LearnerOf of another account's profile = %v, want ErrNoProfile", err)
	}

	for _, id := range []string{"example/b", "example/a", "example/b"} {
		if err := store.ConnectModule(ctx, pool, profileID, id); err != nil {
			t.Fatal(err)
		}
	}
	// Two connected in one transaction share its time: then by their bytes, "Z"
	// before "a", whatever the collation of the database.
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := store.ConnectModule(ctx, tx, profileID, "example/c"); err != nil {
			return err
		}
		return store.ConnectModule(ctx, tx, profileID, "example/Z")
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.ConnectedModules(ctx, pool, profileID)
	want := []string{"example/b", "example/a", "example/Z", "example/c"}
	if err != nil || !slices.Equal(moduleIDs(got), want) {
		t.Fatalf("ConnectedModules = %v, %v; want %v", moduleIDs(got), err, want)
	}
	if got[0].ConnectedAt.IsZero() || got[1].ConnectedAt.Before(got[0].ConnectedAt) {
		t.Errorf("the times of the connections = %v", got)
	}

	for _, id := range []string{"example/a", "example/none"} {
		if err := store.DisconnectModule(ctx, pool, profileID, id); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := store.ConnectedModules(ctx, pool, profileID); err != nil || len(got) != 3 {
		t.Errorf("after a disconnection = %v, %v", moduleIDs(got), err)
	}

	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return store.DeleteProfileRows(ctx, tx, profileID) }); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, "connected_modules"); n != 0 {
		t.Errorf("%d modules left of a deleted profile", n)
	}
	if err := store.ConnectModule(ctx, pool, profileID, "example/a"); !errors.Is(err, store.ErrNoProfile) {
		t.Errorf("ConnectModule of a deleted profile = %v, want ErrNoProfile", err)
	}
}

// A statement that fails is the store's failure.
func TestTheModulesFailWithTheirStatements(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	ctx := t.Context()
	accountID := account(t, pool)
	profileID := ownerProfile(t, pool, accountID, time.Now())
	db := &flaky{Pool: pool, failOn: "connected_modules"}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	for name, err := range map[string]error{
		"connect":    store.ConnectModule(ctx, tx, profileID, "example/a"),
		"disconnect": store.DisconnectModule(ctx, tx, profileID, "example/a"),
	} {
		if !errors.Is(err, errDown) {
			t.Errorf("%s = %v, want the failure", name, err)
		}
	}
	if _, err := store.ConnectedModules(ctx, tx, profileID); !errors.Is(err, errDown) {
		t.Errorf("ConnectedModules = %v, want the failure", err)
	}
	broken := &flaky{Pool: pool, failOn: "age_category"}
	btx, err := broken.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = btx.Rollback(ctx) })
	if _, err := store.LearnerOf(ctx, btx, accountID, profileID); !errors.Is(err, errDown) {
		t.Errorf("LearnerOf = %v, want the failure", err)
	}
}
