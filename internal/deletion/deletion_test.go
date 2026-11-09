package deletion_test

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/authn/authntest"
	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/deletion"
	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

var errDown = errors.New("the test's service is down")

// family is an account with the owner's profile and a child's, each with its
// consent and tuple, the child's confirmed, and a consent that waits.
type family struct {
	pool                    *pgxpool.Pool
	rights                  *authz.Rights
	signin                  *authntest.Fake
	account, owner, child   string
	childConsent, uid, code string
	confirmedAlone          string
}

func newFamily(t *testing.T) *family {
	t.Helper()

	rights, err := authz.Open(t.Context(), storetest.OpenFGAURL(t, true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rights.Close)
	f := &family{pool: storetest.DB(t), rights: rights, signin: &authntest.Fake{}, uid: "uid-family"}
	ctx := t.Context()
	row := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := f.pool.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.account = row(`INSERT INTO accounts (auth_uid, adult_stated_at, pin_hash, tariff, last_request_on)
		VALUES ($1, now(), '$argon2id$synthetic', 'closed_slice', current_date) RETURNING id`, f.uid)
	f.code = row(`INSERT INTO invite_codes (code_sha256, tariff, redeemed_at, account_id)
		VALUES (sha256('synthetic'), 'closed_slice', now(), $1) RETURNING encode(code_sha256, 'hex')`, f.account)
	f.owner = row(`INSERT INTO profiles (account_id, owner, pseudonym, age_category, country, voicing_enabled,
		default_mode, ask_before_showing) VALUES ($1, true, 'Owl', 'adult', 'US', false, 'self', false) RETURNING id`, f.account)
	row(`INSERT INTO consents (account_id, profile_id, subject, state, age_category, country, method, confirmed_at)
		VALUES ($1, $2, 'adult', 'confirmed', 'adult', 'US', 'owner_statement', now()) RETURNING id`, f.account, f.owner)
	f.child = row(`INSERT INTO profiles (account_id, owner, pseudonym, age_category, country, voicing_enabled,
		default_mode, ask_before_showing) VALUES ($1, false, 'Fox', 'under_13', 'US', true, 'by_steps', true) RETURNING id`, f.account)
	f.childConsent = row(`INSERT INTO consents (account_id, profile_id, subject, state, age_category, country, method, confirmed_at)
		VALUES ($1, $2, 'child', 'confirmed', 'under_13', 'US', 'signed_form', now()) RETURNING id`, f.account, f.child)
	row(`INSERT INTO consents (account_id, subject, state, age_category, country)
		VALUES ($1, 'child', 'waiting', 'under_13', 'US') RETURNING id`, f.account)
	// A consent confirmed whose profile is not made yet.
	f.confirmedAlone = row(`INSERT INTO consents (account_id, subject, state, age_category, country, method, confirmed_at)
		VALUES ($1, 'child', 'confirmed', 'from_consent_to_18', 'US', 'signed_form', now()) RETURNING id`, f.account)
	row(`INSERT INTO idempotency_keys (account_id, key, method, fingerprint, state, response)
		VALUES ($1, 'k', '/m', 'f', 'done', '') RETURNING key`, f.account)
	for _, p := range []string{f.owner, f.child} {
		if err := rights.WriteOwner(ctx, f.account, p); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *family) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM `+sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *family) owns(t *testing.T) []string {
	t.Helper()
	owned, err := f.rights.OwnedProfiles(t.Context(), f.account)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(owned)
	return owned
}

// gone holds that nothing is left of the family but the minimal record of the
// child's consent and the code, redeemed and bound to no one.
func (f *family) gone(t *testing.T) {
	t.Helper()
	for _, table := range []string{"accounts", "profiles", "consents", "idempotency_keys"} {
		if n := f.count(t, table); n != 0 {
			t.Errorf("%s holds %d rows after the deletion, want none", table, n)
		}
	}
	// The records of the two confirmed consents for a child, with a profile and
	// with none yet; the one that waits leaves none.
	if f.count(t, "child_consent_records WHERE id IN ($1, $2) AND method = 'signed_form'", f.childConsent, f.confirmedAlone) != 2 ||
		f.count(t, "child_consent_records") != 2 {
		t.Errorf("child_consent_records hold %d, want the two confirmed consents alone", f.count(t, "child_consent_records"))
	}
	if n := f.count(t, "invite_codes WHERE redeemed_at IS NOT NULL AND account_id IS NULL"); n != 1 {
		t.Errorf("the code redeemed and unbound = %d, want 1", n)
	}
	if owned := f.owns(t); len(owned) != 0 {
		t.Errorf("tuples = %v, want none", owned)
	}
	if removed := f.signin.Removed(); !slices.Contains(removed, f.uid) {
		t.Errorf("users removed = %v, want %s", removed, f.uid)
	}
}

func TestAnAccountIsDeletedInFullInItsOrder(t *testing.T) {
	t.Parallel()

	f := newFamily(t)
	d := deletion.New(f.pool, f.rights, f.signin)
	if err := d.Account(t.Context(), f.account, f.uid); err != nil {
		t.Fatal(err)
	}
	f.gone(t)
	// A repeat finds it gone, and does nothing more.
	if err := d.Finish(t.Context(), f.account, f.uid); err != nil {
		t.Errorf("Finish of an account gone = %v, want nil", err)
	}
}

func TestAProfileIsDeletedWithItsConsentAndTuple(t *testing.T) {
	t.Parallel()

	f := newFamily(t)
	d := deletion.New(f.pool, f.rights, f.signin)
	if err := d.Profile(t.Context(), f.account, f.child); err != nil {
		t.Fatal(err)
	}
	if f.count(t, "profiles WHERE id = $1", f.child) != 0 || f.count(t, "consents WHERE id = $1", f.childConsent) != 0 {
		t.Error("the child's profile or consent is still there")
	}
	if f.count(t, "child_consent_records WHERE id = $1", f.childConsent) != 1 {
		t.Error("the child's consent left no minimal record")
	}
	if owned := f.owns(t); !slices.Equal(owned, []string{f.owner}) {
		t.Errorf("tuples = %v, want the owner's alone", owned)
	}
	// The owner's profile goes the same way, its consent whole, and the last
	// profile leaves the account without profiles.
	if err := d.Profile(t.Context(), f.account, f.owner); err != nil {
		t.Fatal(err)
	}
	if f.count(t, "profiles") != 0 || f.count(t, "child_consent_records") != 1 || f.count(t, "accounts") != 1 ||
		f.count(t, "consents WHERE subject = 'adult'") != 0 {
		t.Error("the owner's profile did not go alone, with its consent whole and no record of it")
	}
	if err := d.Profile(t.Context(), f.account, f.owner); !errors.Is(err, store.ErrNoProfile) {
		t.Errorf("the profile deleted again = %v, want ErrNoProfile", err)
	}
	if _, err := f.pool.Exec(t.Context(), `UPDATE accounts SET deleting_at = now()`); err != nil {
		t.Fatal(err)
	}
	if err := d.Profile(t.Context(), f.account, f.owner); !errors.Is(err, store.ErrAccountDeleting) {
		t.Errorf("a profile of a marked account = %v, want ErrAccountDeleting", err)
	}
}

// failing is the database with the first statement that holds failOn failing.
type failing struct {
	*pgxpool.Pool
	failOn string
}

func (f *failing) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := f.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &failingTx{Tx: tx, failOn: f.failOn}, nil
}

type failingTx struct {
	pgx.Tx
	failOn string
}

type failedRow struct{}

func (failedRow) Scan(...any) error { return errDown }

func (t *failingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, t.failOn) {
		return failedRow{}
	}
	return t.Tx.QueryRow(ctx, sql, args...)
}

func (t *failingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, t.failOn) {
		return pgconn.CommandTag{}, errDown
	}
	return t.Tx.Exec(ctx, sql, args...)
}

// stuck is the rights with what the test names failing.
type stuck struct {
	*authz.Rights
	read, delete bool
}

func (s stuck) OwnedProfiles(ctx context.Context, accountID string) ([]string, error) {
	if s.read {
		return nil, errDown
	}
	return s.Rights.OwnedProfiles(ctx, accountID)
}

func (s stuck) DeleteOwned(ctx context.Context, accountID string, profileIDs []string) error {
	if s.delete {
		return errDown
	}
	return s.Rights.DeleteOwned(ctx, accountID, profileIDs)
}

func (s stuck) DeleteOwner(ctx context.Context, accountID, profileID string) error {
	if s.delete {
		return errDown
	}
	return s.Rights.DeleteOwner(ctx, accountID, profileID)
}

// A failure of step 1 leaves everything as it was; its repeat deletes it all.
func TestAFailureInTheDatabaseLeavesTheAccountAsItWas(t *testing.T) {
	t.Parallel()

	for _, failOn := range []string{"FOR UPDATE", "deleting_at = coalesce", "INSERT INTO child_consent_records",
		"DELETE FROM profiles", "DELETE FROM consents", "DELETE FROM idempotency_keys", "SET adult_stated_at",
		"UPDATE invite_codes"} {
		f := newFamily(t)
		err := deletion.New(&failing{Pool: f.pool, failOn: failOn}, f.rights, f.signin).Account(t.Context(), f.account, f.uid)
		var unfinished *deletion.Unfinished
		if !errors.Is(err, errDown) || errors.As(err, &unfinished) {
			t.Fatalf("failing on %q: %v, want the failure, not unfinished", failOn, err)
		}
		if f.count(t, "accounts WHERE deleting_at IS NULL") != 1 || f.count(t, "profiles") != 2 || len(f.owns(t)) != 2 ||
			len(f.signin.Removed()) != 0 {
			t.Fatalf("failing on %q: the account changed", failOn)
		}
		if err := deletion.New(f.pool, f.rights, f.signin).Account(t.Context(), f.account, f.uid); err != nil {
			t.Fatal(err)
		}
		f.gone(t)
	}
}

// A failure after step 1 is "unfinished": the data is gone and the mark stays,
// and Finish — the daily job's — ends it.
func TestAFailureAfterTheDatabaseIsFinishedLater(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		make func(f *family) *deletion.Deleter
	}{
		{"the tuples are not read", func(f *family) *deletion.Deleter {
			return deletion.New(f.pool, stuck{Rights: f.rights, read: true}, f.signin)
		}},
		{"the tuples are not deleted", func(f *family) *deletion.Deleter {
			return deletion.New(f.pool, stuck{Rights: f.rights, delete: true}, f.signin)
		}},
		{"the user is not removed", func(f *family) *deletion.Deleter {
			f.signin.FailRemoveUser()
			return deletion.New(f.pool, f.rights, f.signin)
		}},
		{"the row is not deleted", func(f *family) *deletion.Deleter {
			return deletion.New(&failing{Pool: f.pool, failOn: "DELETE FROM accounts"}, f.rights, f.signin)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFamily(t)
			err := tc.make(f).Account(t.Context(), f.account, f.uid)
			var unfinished *deletion.Unfinished
			if !errors.As(err, &unfinished) || !errors.Is(err, errDown) && !errors.Is(err, authntest.ErrFailed) {
				t.Fatalf("Account = %v, want unfinished", err)
			}
			if f.count(t, "accounts WHERE deleting_at IS NOT NULL") != 1 || f.count(t, "profiles") != 0 ||
				f.count(t, "consents") != 0 || f.count(t, "child_consent_records") != 2 {
				t.Error("the data is not gone, or the mark not left")
			}
			// The user goes even when the tuples did not.
			if !slices.Contains(f.signin.Removed(), f.uid) {
				t.Error("the user was not asked to go")
			}
			fresh := &authntest.Fake{}
			if err := deletion.New(f.pool, f.rights, fresh).Finish(t.Context(), f.account, f.uid); err != nil {
				t.Fatal(err)
			}
			if f.count(t, "accounts") != 0 || len(f.owns(t)) != 0 || !slices.Contains(fresh.Removed(), f.uid) {
				t.Error("Finish did not end the deletion")
			}
		})
	}
}

// A tuple the deletion of a profile leaves is unfinished: the rows are gone.
func TestAProfileWhoseTupleStaysIsUnfinished(t *testing.T) {
	t.Parallel()

	f := newFamily(t)
	err := deletion.New(f.pool, stuck{Rights: f.rights, delete: true}, f.signin).Profile(t.Context(), f.account, f.child)
	var unfinished *deletion.Unfinished
	if !errors.As(err, &unfinished) || !strings.Contains(err.Error(), "unfinished") {
		t.Fatalf("Profile = %v, want unfinished", err)
	}
	if f.count(t, "profiles WHERE id = $1", f.child) != 0 || len(f.owns(t)) != 2 {
		t.Error("want the row gone and the tuple left, dangling")
	}
	if err := deletion.New(&failing{Pool: f.pool, failOn: "DELETE FROM profiles"}, f.rights, f.signin).
		Profile(t.Context(), f.account, f.owner); !errors.Is(err, errDown) {
		t.Errorf("Profile failing in the database = %v, want the failure", err)
	}
}

// A row about a profile written after the profile is gone fails on its key.
func TestARowOfAProfileGoneFailsOnItsKey(t *testing.T) {
	t.Parallel()

	f := newFamily(t)
	if err := deletion.New(f.pool, f.rights, f.signin).Profile(t.Context(), f.account, f.child); err != nil {
		t.Fatal(err)
	}
	_, err := f.pool.Exec(t.Context(), `INSERT INTO idempotency_keys (account_id, key, method, profile_id, fingerprint, state)
		VALUES ($1, 'late', '/m', $2, 'f', 'going')`, f.account, f.child)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Errorf("a row of the profile gone = %v, want a foreign key's refusal", err)
	}
}

// One step that fails is that step's failure, so that the log names its kind;
// two are both.
func TestAFailureOfFinishIsItsStep(t *testing.T) {
	t.Parallel()

	f := newFamily(t)
	f.signin.FailRemoveUser()
	if _, err := f.pool.Exec(t.Context(), `UPDATE accounts SET deleting_at = now()`); err != nil {
		t.Fatal(err)
	}
	err := deletion.New(f.pool, f.rights, f.signin).Finish(t.Context(), f.account, f.uid)
	var joined interface{ Unwrap() []error }
	if !errors.Is(err, authntest.ErrFailed) || errors.As(err, &joined) || !strings.Contains(err.Error(), "the user") {
		t.Errorf("Finish with the user failing = %v, want that failure alone", err)
	}
	err = deletion.New(f.pool, stuck{Rights: f.rights, read: true}, f.signin).Finish(t.Context(), f.account, f.uid)
	if !errors.Is(err, authntest.ErrFailed) || !errors.Is(err, errDown) {
		t.Errorf("Finish with both failing = %v, want both", err)
	}
}

// The daily job deletes an account after 12 months without a request, the same
// way and with no fresh sign-in, and only when the term holds under the lock
// (П-9).
func TestAnIdleAccountIsDeletedOnlyWhenIdle(t *testing.T) {
	t.Parallel()

	f := newFamily(t)
	d := deletion.New(f.pool, f.rights, f.signin)
	yearAgo := time.Now().UTC().AddDate(-1, 0, 0)
	// The family made a request today: nothing is done.
	if done, err := d.IdleAccount(t.Context(), f.account, f.uid, yearAgo); err != nil || done {
		t.Fatalf("IdleAccount of an active account = %v, %v; want nothing done", done, err)
	}
	if f.count(t, "accounts WHERE deleting_at IS NULL") != 1 || f.count(t, "profiles") != 2 || len(f.signin.Removed()) != 0 {
		t.Fatal("the active account changed")
	}
	if _, err := f.pool.Exec(t.Context(), `UPDATE accounts SET last_request_on = $1`, yearAgo.AddDate(0, 0, -1)); err != nil {
		t.Fatal(err)
	}
	if done, err := d.IdleAccount(t.Context(), f.account, f.uid, yearAgo); err != nil || !done {
		t.Fatalf("IdleAccount = %v, %v; want it deleted", done, err)
	}
	f.gone(t)

	// What does not finish after the database is unfinished, as with a request.
	g := newFamily(t)
	if _, err := g.pool.Exec(t.Context(), `UPDATE accounts SET last_request_on = $1`, yearAgo.AddDate(0, 0, -1)); err != nil {
		t.Fatal(err)
	}
	g.signin.FailRemoveUser()
	done, err := deletion.New(g.pool, g.rights, g.signin).IdleAccount(t.Context(), g.account, g.uid, yearAgo)
	var unfinished *deletion.Unfinished
	if !done || !errors.As(err, &unfinished) || g.count(t, "accounts WHERE deleting_at IS NOT NULL") != 1 {
		t.Errorf("IdleAccount with the user stuck = %v, %v; want unfinished, the mark left", done, err)
	}
}

// The daily job deletes a profile after 12 months without activity, and only
// when the term holds under the lock (П-9).
func TestAnIdleProfileIsDeletedOnlyWhenIdle(t *testing.T) {
	t.Parallel()

	f := newFamily(t)
	d := deletion.New(f.pool, f.rights, f.signin)
	yearAgo := time.Now().UTC().AddDate(-1, 0, 0)
	if done, err := d.IdleProfile(t.Context(), f.account, f.child, yearAgo); err != nil || done {
		t.Fatalf("IdleProfile of an active profile = %v, %v; want nothing done", done, err)
	}
	if _, err := f.pool.Exec(t.Context(), `UPDATE profiles SET active_on = $2 WHERE id = $1`, f.child, yearAgo.AddDate(0, 0, -1)); err != nil {
		t.Fatal(err)
	}
	if done, err := d.IdleProfile(t.Context(), f.account, f.child, yearAgo); err != nil || !done {
		t.Fatalf("IdleProfile = %v, %v; want it deleted", done, err)
	}
	if f.count(t, "profiles WHERE id = $1", f.child) != 0 || f.count(t, "child_consent_records WHERE id = $1", f.childConsent) != 1 {
		t.Error("the idle child's profile did not go, with its minimal record kept")
	}
	if owned := f.owns(t); !slices.Equal(owned, []string{f.owner}) {
		t.Errorf("tuples = %v, want the owner's alone", owned)
	}
	if _, err := f.pool.Exec(t.Context(), `UPDATE profiles SET active_on = $2 WHERE id = $1`, f.owner, yearAgo.AddDate(0, 0, -1)); err != nil {
		t.Fatal(err)
	}
	done, err := deletion.New(f.pool, stuck{Rights: f.rights, delete: true}, f.signin).IdleProfile(t.Context(), f.account, f.owner, yearAgo)
	var unfinished *deletion.Unfinished
	if !done || !errors.As(err, &unfinished) {
		t.Errorf("IdleProfile with its tuple stuck = %v, %v; want unfinished", done, err)
	}
}
