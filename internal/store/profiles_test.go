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
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return store.HoldProfile(ctx, tx, id, profileID) }); err != nil {
		t.Errorf("HoldProfile of the account's profile = %v", err)
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return store.HoldProfile(ctx, tx, other, profileID) })
	if !errors.Is(err, store.ErrNoProfile) {
		t.Errorf("HoldProfile of another account = %v, want ErrNoProfile", err)
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

func TestAChildsProfileIsMadeOfItsConsent(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id, other := account(t, pool), account(t, pool)
	ctx := t.Context()
	var waiting store.ChildConsent
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var err error
		waiting, err = store.AskChildConsent(ctx, tx, id, store.NewConsent{AgeCategory: "under_13", Country: "US", Applied: 13, Giver: store.GiverParent, RequestCode: "ABCDE-FGHJK"})
		return err
	})
	if err != nil || waiting.ID == "" || waiting.State != store.ConsentWaiting {
		t.Fatalf("CreateChildConsent = %+v, %v; want one that waits", waiting, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE consents SET state = 'confirmed', confirmed_at = now(), method = 'synthetic'`); err != nil {
		t.Fatal(err)
	}
	var created store.Profile
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		c, err := store.ConsentFor(ctx, tx, id, waiting.ID)
		if err != nil || c.State != store.ConsentConfirmed || c.ProfileID != "" {
			t.Errorf("ConsentFor = %+v, %v; want it confirmed and with no profile", c, err)
		}
		p := store.Profile{Pseudonym: "Fox", VoicingEnabled: true, VoicingSpeed: "normal", DefaultMode: "by_steps", AskBeforeShowing: true}
		created, err = store.CreateChildProfile(ctx, tx, id, c, p)
		return err
	})
	if err != nil || created.AgeCategory != "under_13" || created.Country != "US" || created.Owner {
		t.Fatalf("CreateChildProfile = %+v, %v; want the consent's category and country", created, err)
	}
	var seq int64
	if err := pool.QueryRow(ctx, `SELECT last_seq FROM skill_states WHERE profile_id = $1`, created.ID).Scan(&seq); err != nil || seq != 0 {
		t.Errorf("the child's skill state = %d, %v; want a row at 0", seq, err)
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if c, err := store.ConsentFor(ctx, tx, id, waiting.ID); err != nil || c.ProfileID != created.ID {
			t.Errorf("ConsentFor after = %+v, %v; want it bound to %s", c, err, created.ID)
		}
		_, err := store.ConsentFor(ctx, tx, other, waiting.ID)
		return err
	})
	if !errors.Is(err, store.ErrNoConsent) {
		t.Errorf("ConsentFor of another account = %v, want ErrNoConsent", err)
	}
}

func TestAnIDIsTheDatabasesOnlyAsItWritesIt(t *testing.T) {
	t.Parallel()

	if !store.CanonicalID("0b5f3a2e-1c4d-4e8f-9a7b-6c5d4e3f2a1b") {
		t.Error("a lowercase UUID with its dashes is not canonical")
	}
	for _, id := range []string{"", "not-a-uuid", "0B5F3A2E-1C4D-4E8F-9A7B-6C5D4E3F2A1B",
		"urn:uuid:0b5f3a2e-1c4d-4e8f-9a7b-6c5d4e3f2a1b", "0b5f3a2e1c4d4e8f9a7b6c5d4e3f2a1b"} {
		if store.CanonicalID(id) {
			t.Errorf("CanonicalID(%q) = true", id)
		}
	}
}
