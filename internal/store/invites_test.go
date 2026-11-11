package store_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

// code keeps an invite code of a synthetic text and returns its hash.
func code(t *testing.T, pool *pgxpool.Pool, text string) []byte {
	t.Helper()

	hash := sha256.Sum256([]byte(text))
	if err := store.CreateInvite(t.Context(), pool, hash[:], "closed_slice"); err != nil {
		t.Fatal(err)
	}
	return hash[:]
}

func TestAnInviteCodeIsKeptByItsHashAlone(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	hash := code(t, pool, "SYNTHETIC-CODE")

	var kept []byte
	var tariff string
	var redeemed *time.Time
	err := pool.QueryRow(t.Context(), `SELECT code_sha256, tariff, redeemed_at FROM invite_codes`).Scan(&kept, &tariff, &redeemed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(kept, hash) || tariff != "closed_slice" || redeemed != nil {
		t.Errorf("row = %x %q %v; want the hash, the tariff and no redemption", kept, tariff, redeemed)
	}
	// The same code twice is one code: the second is refused, not kept beside it.
	if err := store.CreateInvite(t.Context(), pool, hash, "closed_slice"); err == nil {
		t.Error("CreateInvite of a code kept already = nil, want an error")
	}
}

func TestAnAccountIsCreatedByItsCode(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	hash := code(t, pool, "SYNTHETIC-CODE")

	before := time.Now()
	id, err := store.CreateAccount(t.Context(), pool, "uid-owner", hash, nil)
	if err != nil {
		t.Fatal(err)
	}
	var uid, tariff string
	var stated time.Time
	var lastRequest *time.Time
	err = pool.QueryRow(t.Context(), `SELECT auth_uid, tariff, adult_stated_at, last_request_on FROM accounts WHERE id = $1`, id).
		Scan(&uid, &tariff, &stated, &lastRequest)
	if err != nil {
		t.Fatal(err)
	}
	if uid != "uid-owner" || tariff != "closed_slice" || stated.Before(before.Add(-time.Minute)) || lastRequest == nil {
		t.Errorf("account = %q %q %v %v; want the UID, the code's tariff, the statement dated now and today's request",
			uid, tariff, stated, lastRequest)
	}
	var boundTo string
	var redeemed *time.Time
	if err := pool.QueryRow(t.Context(), `SELECT account_id, redeemed_at FROM invite_codes`).Scan(&boundTo, &redeemed); err != nil {
		t.Fatal(err)
	}
	if boundTo != id || redeemed == nil {
		t.Errorf("code = bound to %q, redeemed %v; want bound to %s and redeemed", boundTo, redeemed, id)
	}
}

func TestARepeatOfTheCreationFindsTheAccountBeforeTheCode(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	hash := code(t, pool, "SYNTHETIC-CODE")
	if _, err := store.CreateAccount(t.Context(), pool, "uid-owner", hash, nil); err != nil {
		t.Fatal(err)
	}
	// The code it redeemed, and one that is no code at all: either way the account
	// is there, which is what a repeat after a lost answer is told.
	for _, again := range [][]byte{hash, make([]byte, sha256.Size)} {
		if _, err := store.CreateAccount(t.Context(), pool, "uid-owner", again, nil); !errors.Is(err, store.ErrAccountExists) {
			t.Errorf("CreateAccount again = %v, want ErrAccountExists", err)
		}
	}
	if n := count(t, pool, "accounts"); n != 1 {
		t.Errorf("accounts = %d, want 1", n)
	}
}

func TestACodeNotThereOrRedeemedIsNotAccepted(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	hash := code(t, pool, "SYNTHETIC-CODE")
	if _, err := store.CreateAccount(t.Context(), pool, "uid-first", hash, nil); err != nil {
		t.Fatal(err)
	}
	other := sha256.Sum256([]byte("NO-SUCH-CODE"))
	for name, given := range map[string][]byte{"redeemed": hash, "not there": other[:]} {
		if _, err := store.CreateAccount(t.Context(), pool, "uid-second", given, nil); !errors.Is(err, store.ErrCodeNotAccepted) {
			t.Errorf("CreateAccount by a code %s = %v, want ErrCodeNotAccepted", name, err)
		}
	}
	// A code refused leaves no account behind.
	if n := count(t, pool, "accounts"); n != 1 {
		t.Errorf("accounts = %d, want only the first", n)
	}
}

func TestAMarkedAccountIsNotCreatedAgain(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	hash := code(t, pool, "SYNTHETIC-CODE")
	id, err := store.CreateAccount(t.Context(), pool, "uid-owner", hash, nil)
	if err != nil {
		t.Fatal(err)
	}
	mark(t, pool, id)
	fresh := code(t, pool, "ANOTHER-CODE")
	if _, err := store.CreateAccount(t.Context(), pool, "uid-owner", fresh, nil); !errors.Is(err, store.ErrAccountDeleting) {
		t.Errorf("CreateAccount of a marked account = %v, want ErrAccountDeleting", err)
	}
	var redeemed bool
	if err := pool.QueryRow(t.Context(), `SELECT redeemed_at IS NOT NULL FROM invite_codes WHERE code_sha256 = $1`, fresh).
		Scan(&redeemed); err != nil || redeemed {
		t.Errorf("the second code redeemed = %v, %v; want it left as it was", redeemed, err)
	}
}

// race runs n creations at once, from a start they wait for together.
func race(t *testing.T, pool *pgxpool.Pool, n int, create func(i int) error) []error {
	t.Helper()

	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			errs[i] = create(i)
		})
	}
	close(start)
	wg.Wait()
	return errs
}

func TestTwoAccountsRacingForOneCodeGetOneAccount(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	hash := code(t, pool, "SYNTHETIC-CODE")
	uids := []string{"uid-a", "uid-b", "uid-c", "uid-d"}
	errs := race(t, pool, len(uids), func(i int) error {
		_, err := store.CreateAccount(t.Context(), pool, uids[i], hash, nil)
		return err
	})
	created := 0
	for i, err := range errs {
		switch {
		case err == nil:
			created++
		case !errors.Is(err, store.ErrCodeNotAccepted):
			t.Errorf("CreateAccount of %s = %v, want nil or ErrCodeNotAccepted", uids[i], err)
		}
	}
	if created != 1 || count(t, pool, "accounts") != 1 {
		t.Errorf("created %d, %d accounts; want one of each", created, count(t, pool, "accounts"))
	}
}

func TestOneUserRacingWithTwoCodesGetsOneAccount(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	hashes := [][]byte{code(t, pool, "FIRST-CODE"), code(t, pool, "SECOND-CODE")}
	errs := race(t, pool, len(hashes), func(i int) error {
		_, err := store.CreateAccount(t.Context(), pool, "uid-owner", hashes[i], nil)
		return err
	})
	created := 0
	for _, err := range errs {
		switch {
		case err == nil:
			created++
		case !errors.Is(err, store.ErrAccountExists):
			t.Errorf("CreateAccount = %v, want nil or ErrAccountExists", err)
		}
	}
	var redeemed int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM invite_codes WHERE redeemed_at IS NOT NULL`).Scan(&redeemed); err != nil {
		t.Fatal(err)
	}
	// The code of the creation that lost stays as it was, for someone else.
	if created != 1 || redeemed != 1 {
		t.Errorf("created %d, redeemed %d; want one of each", created, redeemed)
	}
}

func TestTheViewIsTheAccountItsProfilesAndItsWaitingConsents(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	other := account(t, pool)
	ctx := t.Context()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE accounts SET pin_hash = 'synthetic' WHERE id = $1`, id)
	// A child's profile created first, then the owner's, which the view still
	// gives first; and a profile of another account, which it never gives.
	var child, owner string
	if err := pool.QueryRow(ctx, `INSERT INTO profiles (account_id, owner, pseudonym, avatar, age_category, country,
			voicing_enabled, voicing_speed, default_mode, interface_language, grade, ask_before_showing, created_at)
		VALUES ($1, false, 'Fox', 'fox', 'under_13', 'US', true, 'slow', 'by_steps', 'en-US', 3, true, now() - interval '1 day')
		RETURNING id`, id).Scan(&child); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO profiles (account_id, owner, pseudonym, age_category, country,
			voicing_enabled, default_mode, ask_before_showing)
		VALUES ($1, true, 'Owl', 'adult', 'CA', false, 'self', false) RETURNING id`, id).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO profiles (account_id, owner, pseudonym, age_category, country, voicing_enabled, default_mode, ask_before_showing)
		VALUES ($1, true, 'Elk', 'adult', 'US', false, 'self', false)`, other)
	// The owner's own consent and the child's of its profile are no consents
	// that wait; one asked for, and one confirmed with no profile yet, are.
	exec(`INSERT INTO consents (account_id, profile_id, subject, state, age_category, country, confirmed_at)
		VALUES ($1, $2, 'adult', 'confirmed', 'adult', 'CA', now()), ($1, $3, 'child', 'confirmed', 'under_13', 'US', now())`,
		id, owner, child)
	var waiting, confirmed string
	if err := pool.QueryRow(ctx, `INSERT INTO consents (account_id, subject, state, age_category, country, requested_at)
		VALUES ($1, 'child', 'waiting', 'from_13_to_consent', 'US', now() - interval '1 hour') RETURNING id`, id).Scan(&waiting); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO consents (account_id, subject, state, age_category, country, confirmed_at)
		VALUES ($1, 'child', 'confirmed', 'under_13', 'MX', now()) RETURNING id`, id).Scan(&confirmed); err != nil {
		t.Fatal(err)
	}

	view, err := store.AccountView(ctx, pool, id)
	if err != nil {
		t.Fatal(err)
	}
	if view.ID != id || !view.PinSet || len(view.Profiles) != 2 || len(view.ChildConsents) != 2 {
		t.Fatalf("view = %+v; want the account with its PIN, two profiles and two consents", view)
	}
	if got := view.Profiles[0]; got.ID != owner || !got.Owner || got.Country != "CA" || got.InterfaceLanguage != nil || got.Grade != nil {
		t.Errorf("first profile = %+v; want the owner's, with no language or grade", got)
	}
	got := view.Profiles[1]
	if got.ID != child || got.Owner || got.Pseudonym != "Fox" || got.Avatar != "fox" || got.AgeCategory != "under_13" ||
		!got.VoicingEnabled || got.VoicingSpeed != "slow" || got.DefaultMode != "by_steps" ||
		got.InterfaceLanguage == nil || *got.InterfaceLanguage != "en-US" || got.Grade == nil || *got.Grade != 3 || !got.AskBeforeShowing {
		t.Errorf("second profile = %+v; want the child's, field by field", got)
	}
	first, second := view.ChildConsents[0], view.ChildConsents[1]
	if first != (store.ChildConsent{ID: waiting, AgeCategory: "from_13_to_consent", Country: "US", State: "waiting",
		Giver: "parent", RequestCode: first.RequestCode, FormState: "not_sent", Applied: 13}) ||
		second != (store.ChildConsent{ID: confirmed, AgeCategory: "under_13", Country: "MX", State: "confirmed",
			Giver: "parent", RequestCode: second.RequestCode, FormState: "not_sent", Applied: 13}) {
		t.Errorf("consents = %+v; want the waiting one, then the confirmed one", view.ChildConsents)
	}

	// An account with nothing yet is a view with nothing.
	if empty, err := store.AccountView(ctx, pool, other); err != nil || empty.PinSet || len(empty.Profiles) != 1 || len(empty.ChildConsents) != 0 {
		t.Errorf("view of the other account = %+v, %v; want its one profile and nothing else", empty, err)
	}
}

func TestTheViewOfAnAccountGoneIsItsRefusal(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	mark(t, pool, id)
	if _, err := store.AccountView(t.Context(), pool, id); !errors.Is(err, store.ErrAccountDeleting) {
		t.Errorf("AccountView of a marked account = %v, want ErrAccountDeleting", err)
	}
	if _, err := store.AccountView(t.Context(), pool, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNoAccount) {
		t.Errorf("AccountView of no account = %v, want ErrNoAccount", err)
	}
}
