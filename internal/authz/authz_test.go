package authz_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

// The IDs below are synthetic.
const (
	owner   = "0190f0a0-0000-7000-8000-000000000001"
	other   = "0190f0a0-0000-7000-8000-000000000002"
	profile = "0190f0a0-0000-7000-8000-0000000000a1"
	missing = "0190f0a0-0000-7000-8000-0000000000ff"
)

// rights opens the rights on a migrated database openfga of the test's own.
func rights(t *testing.T) *authz.Rights {
	t.Helper()

	r, err := authz.Open(t.Context(), storetest.OpenFGAURL(t, true))
	if err != nil {
		t.Fatalf("Open error = %v, want nil", err)
	}
	t.Cleanup(r.Close)
	return r
}

func can(t *testing.T, r *authz.Rights, account string, right authz.Right, profile string) bool {
	t.Helper()

	allowed, err := r.Can(t.Context(), account, right, profile)
	if err != nil {
		t.Fatalf("Can error = %v, want nil", err)
	}
	return allowed
}

var everyRight = []authz.Right{authz.CanLearn, authz.CanManage, authz.CanView}

func TestTheOwnerHasEveryRightAndNobodyElseAny(t *testing.T) {
	t.Parallel()

	r := rights(t)
	if err := r.WriteOwner(t.Context(), owner, profile); err != nil {
		t.Fatal(err)
	}
	// A repeat is no error.
	if err := r.WriteOwner(t.Context(), owner, profile); err != nil {
		t.Fatalf("a second WriteOwner error = %v, want nil", err)
	}
	for _, right := range everyRight {
		if !can(t, r, owner, right, profile) {
			t.Errorf("the owner has no %s on the profile", right)
		}
		// Another account and a profile that is not there get one and the same no.
		if can(t, r, other, right, profile) {
			t.Errorf("another account has %s on the profile", right)
		}
		if can(t, r, owner, right, missing) {
			t.Errorf("the owner has %s on a profile that is not there", right)
		}
	}
}

func TestADeletedTupleGivesNoRight(t *testing.T) {
	t.Parallel()

	r := rights(t)
	if err := r.WriteOwner(t.Context(), owner, profile); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteOwner(t.Context(), owner, profile); err != nil {
		t.Fatal(err)
	}
	// Deleting what is not there is no error.
	if err := r.DeleteOwner(t.Context(), owner, profile); err != nil {
		t.Fatalf("a second DeleteOwner error = %v, want nil", err)
	}
	for _, right := range everyRight {
		if can(t, r, owner, right, profile) {
			t.Errorf("the owner still has %s after the tuple is deleted", right)
		}
	}
}

func TestARightNotInTheModelIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := rights(t).Can(t.Context(), owner, authz.Right("teacher"), profile); err == nil {
		t.Error("Can with a right the model has not: error = nil")
	}
}

// The deletion of an account reads its tuples, more than a page of them, and
// deletes them in batches (R21).
func TestTheOwnedProfilesAreReadAndDeletedPastAPage(t *testing.T) {
	t.Parallel()

	r := rights(t)
	var profiles []string
	for i := range 150 {
		id := fmt.Sprintf("0190f0a0-0000-7000-8000-%012d", i)
		profiles = append(profiles, id)
		if err := r.WriteOwner(t.Context(), owner, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.WriteOwner(t.Context(), other, profile); err != nil {
		t.Fatal(err)
	}

	owned, err := r.OwnedProfiles(t.Context(), owner)
	if err != nil {
		t.Fatalf("OwnedProfiles error = %v", err)
	}
	slices.Sort(owned)
	if !slices.Equal(owned, profiles) {
		t.Fatalf("OwnedProfiles = %d profiles, want the 150 of the owner", len(owned))
	}

	if err := r.DeleteOwned(t.Context(), owner, owned); err != nil {
		t.Fatalf("DeleteOwned error = %v", err)
	}
	if left, err := r.OwnedProfiles(t.Context(), owner); err != nil || len(left) != 0 {
		t.Errorf("after DeleteOwned: %d profiles, %v; want none", len(left), err)
	}
	if !can(t, r, other, authz.CanManage, profile) {
		t.Error("DeleteOwned of one account took another's tuple")
	}
}

// The tuple is written inside the transaction of the database api that makes the
// profile, before its COMMIT (R31). A write that fails after it rolls the profile
// back and leaves the tuple dangling: it gives a right to a profile that is not
// there, which the requests answer as "no profile", and T28 removes it.
func TestTheTupleIsWrittenBeforeTheCommit(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	r := rights(t)
	var account string
	if err := pool.QueryRow(t.Context(), `INSERT INTO accounts (auth_uid) VALUES ('uid-synthetic') RETURNING id`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	makeProfile := func(ctx context.Context, tx pgx.Tx) (string, error) {
		var id string
		err := tx.QueryRow(ctx, `INSERT INTO profiles
			(account_id, owner, pseudonym, age_category, country, voicing_enabled, default_mode, ask_before_showing)
			VALUES ($1, false, 'Owl', 'under_13', 'US', true, 'by_steps', true) RETURNING id`, account).Scan(&id)
		return id, err
	}

	var committed string
	err := store.WriteUnderAccount(t.Context(), pool, account, func(tx pgx.Tx) error {
		id, err := makeProfile(t.Context(), tx)
		if err != nil {
			return err
		}
		committed = id
		return r.WriteOwner(t.Context(), account, id)
	})
	if err != nil {
		t.Fatalf("the profile and its tuple: %v", err)
	}
	if !can(t, r, account, authz.CanManage, committed) {
		t.Error("the committed profile has no tuple")
	}

	failed := errors.New("synthetic failure after the tuple")
	var rolledBack string
	err = store.WriteUnderAccount(t.Context(), pool, account, func(tx pgx.Tx) error {
		id, err := makeProfile(t.Context(), tx)
		if err != nil {
			return err
		}
		rolledBack = id
		if err := r.WriteOwner(t.Context(), account, id); err != nil {
			return err
		}
		return failed
	})
	if !errors.Is(err, failed) {
		t.Fatalf("the failed write: error = %v, want %v", err, failed)
	}
	var rows int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM profiles WHERE id = $1`, rolledBack).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 || !can(t, r, account, authz.CanManage, rolledBack) {
		t.Errorf("after the rollback: %d rows of the profile; want none, and the tuple dangling", rows)
	}
}

// The model the binary embeds is section 3.1's, word for word.
func TestTheModelIsSection31s(t *testing.T) {
	t.Parallel()

	written, err := os.ReadFile("../../docs/architecture.md")
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := os.ReadFile("model.fga")
	if err != nil {
		t.Fatal(err)
	}
	section := string(written)
	start := strings.Index(section, "```text\nmodel\n")
	if start < 0 {
		t.Fatal("docs/architecture.md has no block of the model")
	}
	block := section[start+len("```text\n"):]
	block = block[:strings.Index(block, "```")]
	if block != string(embedded) {
		t.Errorf("internal/authz/model.fga is not the model of section 3.1:\n%s\nwant:\n%s", embedded, block)
	}
}

// Every call on rights whose server is closed fails, and says what it did.
func TestEveryCallOnAClosedServerFails(t *testing.T) {
	t.Parallel()

	r, err := authz.Open(t.Context(), storetest.OpenFGAURL(t, true))
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	for name, call := range map[string]func() error{
		"Can":           func() error { _, err := r.Can(t.Context(), owner, authz.CanLearn, profile); return err },
		"WriteOwner":    func() error { return r.WriteOwner(t.Context(), owner, profile) },
		"DeleteOwner":   func() error { return r.DeleteOwner(t.Context(), owner, profile) },
		"OwnedProfiles": func() error { _, err := r.OwnedProfiles(t.Context(), owner); return err },
	} {
		if err := call(); err == nil || !strings.HasPrefix(err.Error(), "authz: ") {
			t.Errorf("%s on a closed server: error = %v, want a failure of authz", name, err)
		}
	}
}

// Open and Migrate on a database whose pool cannot be read fail with no password.
func TestOpenRefusesAStringItCannotRead(t *testing.T) {
	t.Parallel()

	_, err := authz.Open(t.Context(), "postgres://u:not-a-password@host:notaport/openfga")
	if err == nil || strings.Contains(err.Error(), "not-a-password") {
		t.Errorf("Open error = %v, want a refusal that holds no password", err)
	}
}
