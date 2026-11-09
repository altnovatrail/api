package store_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

func TestTheListOfCountriesIsTheOperators(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	ctx := t.Context()
	if ok, err := store.CountryAccepted(ctx, pool, "US"); err != nil || ok {
		t.Fatalf("CountryAccepted at first = %v, %v; want none taken", ok, err)
	}
	for _, c := range []string{"US", "CA", "US"} {
		if _, err := store.AllowCountry(ctx, pool, c); err != nil {
			t.Fatal(err)
		}
	}
	if added, err := store.AllowCountry(ctx, pool, "CA"); err != nil || added {
		t.Errorf("AllowCountry again = %v, %v; want it there already", added, err)
	}
	if list, err := store.Countries(ctx, pool); err != nil || !slices.Equal(list, []string{"CA", "US"}) {
		t.Errorf("Countries = %v, %v; want CA and US", list, err)
	}
	if ok, err := store.CountryAccepted(ctx, pool, "US"); err != nil || !ok {
		t.Errorf("CountryAccepted of US = %v, %v; want it taken", ok, err)
	}
	if removed, err := store.RemoveCountry(ctx, pool, "US"); err != nil || !removed {
		t.Errorf("RemoveCountry = %v, %v; want it removed", removed, err)
	}
	if removed, err := store.RemoveCountry(ctx, pool, "US"); err != nil || removed {
		t.Errorf("RemoveCountry again = %v, %v; want it not there", removed, err)
	}
	if _, err := store.AllowCountry(ctx, pool, "usa"); err == nil {
		t.Error("the schema kept a country of no form")
	}
}

func TestTheOwnersProfileIsOneWithItsConsent(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	ctx := t.Context()
	if _, err := pool.Exec(ctx, `UPDATE accounts SET adult_stated_at = now() WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	p := store.Profile{Pseudonym: "Owl", Country: "US", VoicingSpeed: "normal", DefaultMode: "by_steps"}
	var created store.Profile
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var err error
		created, err = store.CreateOwnerProfile(ctx, tx, id, p)
		return err
	})
	if err != nil || created.ID == "" || !created.Owner || created.AgeCategory != "adult" || created.Pseudonym != "Owl" {
		t.Fatalf("CreateOwnerProfile = %+v, %v; want the owner's adult profile", created, err)
	}
	var consents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM consents WHERE profile_id = $1 AND method = 'owner_statement'`,
		created.ID).Scan(&consents); err != nil || consents != 1 {
		t.Errorf("consents = %d, %v; want the owner's one", consents, err)
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if has, err := store.HasOwnerProfile(ctx, tx, id); err != nil || !has {
			t.Errorf("HasOwnerProfile = %v, %v; want it", has, err)
		}
		_, err := store.CreateOwnerProfile(ctx, tx, id, p)
		return err
	})
	if !errors.Is(err, store.ErrProfileExists) {
		t.Errorf("a second owner's profile = %v, want ErrProfileExists", err)
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := store.CreateOwnerProfile(ctx, tx, "00000000-0000-0000-0000-000000000000", p)
		return err
	})
	if !errors.Is(err, store.ErrNoAccount) {
		t.Errorf("a profile of no account = %v, want ErrNoAccount", err)
	}
}

func TestAProfileIsChangedOnlyByItsAccount(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id, other := account(t, pool), account(t, pool)
	ctx := t.Context()
	var profileID string
	if err := pool.QueryRow(ctx, `INSERT INTO profiles (account_id, owner, pseudonym, age_category, country,
			voicing_enabled, default_mode, ask_before_showing)
		VALUES ($1, false, 'Fox', 'under_13', 'US', true, 'by_steps', true) RETURNING id`, id).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		p, err := store.ProfileFor(ctx, tx, id, profileID)
		if err != nil {
			return err
		}
		grade := int16(4)
		p.Pseudonym, p.Grade, p.AskBeforeShowing = "Lynx", &grade, false
		return store.WriteProfile(ctx, tx, p)
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := store.AccountView(ctx, pool, id)
	if err != nil || view.Profiles[0].Pseudonym != "Lynx" || *view.Profiles[0].Grade != 4 || view.Profiles[0].AskBeforeShowing {
		t.Errorf("the profile after the change = %+v, %v", view.Profiles, err)
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := store.ProfileFor(ctx, tx, other, profileID)
		return err
	})
	if !errors.Is(err, store.ErrNoProfile) {
		t.Errorf("ProfileFor of another account = %v, want ErrNoProfile", err)
	}
}

func TestTheDateOfAProfilesActivityIsWrittenOnceADay(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	ctx := t.Context()
	var profileID string
	if err := pool.QueryRow(ctx, `INSERT INTO profiles (account_id, owner, pseudonym, age_category, country,
			voicing_enabled, default_mode, ask_before_showing, active_on)
		VALUES ($1, true, 'Owl', 'adult', 'US', false, 'self', false, DATE '2026-01-01') RETURNING id`, id).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	if err := store.TouchProfile(ctx, pool, profileID); err != nil {
		t.Fatal(err)
	}
	there, activeOn, err := store.ProfileOf(ctx, pool, id, profileID)
	if err != nil || !there || activeOn.Year() == 2026 && activeOn.YearDay() == 1 {
		t.Fatalf("ProfileOf = %v, %v, %v; want today", there, activeOn, err)
	}
	var before, after string
	if err := pool.QueryRow(ctx, `SELECT xmin::text FROM profiles WHERE id = $1`, profileID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := store.TouchProfile(ctx, pool, profileID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT xmin::text FROM profiles WHERE id = $1`, profileID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Error("a second touch of the day wrote the profile again")
	}
}
