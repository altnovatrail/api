package authz_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

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
		"Tuples": func() error {
			for _, err := range r.Tuples(t.Context()) {
				return err
			}
			return nil
		},
		"DropChangelog": func() error { _, err := r.DropChangelog(t.Context(), time.Now()); return err },
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

// The daily job reads every tuple of the store, past a page, with when it was
// written, to find those left dangling (R21).
func TestEveryTupleIsReadWithItsTime(t *testing.T) {
	t.Parallel()

	r := rights(t)
	before := time.Now().Add(-time.Minute)
	want := map[string]string{profile: other}
	// Tuples of other kinds are not an owner's, and are not given.
	for _, tuple := range [][3]string{
		{"user:" + owner, "teacher", "classroom:c1"},
		{"classroom:c1", "classroom", "learner:" + profile},
	} {
		if err := r.WriteTuple(t.Context(), tuple[0], tuple[1], tuple[2]); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.WriteOwner(t.Context(), other, profile); err != nil {
		t.Fatal(err)
	}
	for i := range 120 {
		id := fmt.Sprintf("0190f0a0-0000-7000-8000-%012d", i)
		want[id] = owner
		if err := r.WriteOwner(t.Context(), owner, id); err != nil {
			t.Fatal(err)
		}
	}

	got := map[string]string{}
	for tuple, err := range r.Tuples(t.Context()) {
		if err != nil {
			t.Fatalf("Tuples error = %v", err)
		}
		if tuple.Written.Before(before) || tuple.Written.After(time.Now().Add(time.Minute)) {
			t.Errorf("tuple %s written at %v, want about now", tuple.ProfileID, tuple.Written)
		}
		got[tuple.ProfileID] = tuple.AccountID
	}
	if !maps.Equal(got, want) {
		t.Errorf("Tuples = %d tuples, want the %d written", len(got), len(want))
	}

	// A stop the caller asks for ends the list.
	for range r.Tuples(t.Context()) {
		break
	}
}

// The changelog keeps who owned which profile: the daily job deletes what is
// older than a day, and nothing younger, in batches (R05; R27 canvas-api).
func TestTheChangelogIsClearedOfWhatIsOld(t *testing.T) {
	t.Parallel()

	url := storetest.OpenFGAURL(t, true)
	r, err := authz.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	if err := r.WriteOwner(t.Context(), owner, profile); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteOwner(t.Context(), owner, profile); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	// Past a batch of old rows, as a store of many deletions would have them.
	if _, err := conn.Exec(t.Context(), `INSERT INTO changelog
		(store, object_type, object_id, relation, _user, operation, ulid, inserted_at)
		SELECT store, object_type, object_id || g, relation, _user, operation, ulid || g, now() - interval '2 days'
		FROM changelog, generate_series(1, 1200) AS g LIMIT 1200`); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		var n int
		if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM changelog`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(); n != 1202 {
		t.Fatalf("the changelog holds %d rows, want 1202", n)
	}

	dropped, err := r.DropChangelog(t.Context(), time.Now().Add(-24*time.Hour))
	if err != nil || dropped != 1200 {
		t.Fatalf("DropChangelog = %d, %v; want the 1200 old rows", dropped, err)
	}
	if n := count(); n != 2 {
		t.Errorf("the changelog holds %d rows, want the 2 of today", n)
	}
}
