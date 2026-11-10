package store_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/pin"
	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

var errDown = errors.New("the test's database is down")

// flaky is a pool whose transactions fail the first statement whose SQL holds
// failOn.
type flaky struct {
	*pgxpool.Pool
	failOn string
}

func (f *flaky) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := f.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &flakyTx{Tx: tx, failOn: f.failOn}, nil
}

func (f *flaky) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx, err := f.Pool.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &flakyTx{Tx: tx, failOn: f.failOn}, nil
}

type flakyTx struct {
	pgx.Tx
	failOn string
}

type failedRow struct{}

func (failedRow) Scan(...any) error { return errDown }

func (t *flakyTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, t.failOn) {
		return failedRow{}
	}
	return t.Tx.QueryRow(ctx, sql, args...)
}

func (t *flakyTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, t.failOn) {
		return pgconn.CommandTag{}, errDown
	}
	return t.Tx.Exec(ctx, sql, args...)
}

func (t *flakyTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, t.failOn) {
		return nil, errDown
	}
	return t.Tx.Query(ctx, sql, args...)
}

// A statement that fails is the store's failure, never one of its refusals, and
// leaves nothing written.
func TestAFailedStatementIsAFailure(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	for _, failOn := range []string{"INSERT INTO accounts", "UPDATE invite_codes"} {
		hash := code(t, pool, "CODE-"+failOn)
		if _, err := store.CreateAccount(t.Context(), &flaky{Pool: pool, failOn: failOn}, "uid-owner", hash); !errors.Is(err, errDown) {
			t.Errorf("CreateAccount failing on %q = %v, want the failure", failOn, err)
		}
	}
	if n := count(t, pool, "accounts"); n != 0 {
		t.Errorf("accounts = %d after failures, want none", n)
	}
	hash := sha256.Sum256([]byte("ANOTHER-CODE"))
	if _, err := store.CreateAccount(t.Context(), pool, "uid-owner", code(t, pool, "FIRST")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateAccount(t.Context(), &flaky{Pool: pool, failOn: "SELECT deleting_at"}, "uid-owner", hash[:]); !errors.Is(err, errDown) {
		t.Errorf("CreateAccount failing to find the account there = %v, want the failure", err)
	}

	id := account(t, pool)
	for _, failOn := range []string{"FROM accounts", "FROM profiles", "FROM consents"} {
		if _, err := store.AccountView(t.Context(), &flaky{Pool: pool, failOn: failOn}, id); !errors.Is(err, errDown) {
			t.Errorf("AccountView failing on %q = %v, want the failure", failOn, err)
		}
	}
}

// A statement of the PIN or of a key that fails is the store's failure.
func TestAFailedStatementOfThePINIsAFailure(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	for _, tc := range []struct {
		failOn string
		run    func(tx pgx.Tx) error
	}{
		{"FOR NO KEY UPDATE", func(tx pgx.Tx) error {
			_, err := store.HoldPin(t.Context(), tx, id)
			return err
		}},
		{"FROM idempotency_keys", func(tx pgx.Tx) error {
			_, _, err := store.RecordOf(t.Context(), tx, id, "k")
			return err
		}},
		{"INSERT INTO idempotency_keys", func(tx pgx.Tx) error {
			return store.RecordDone(t.Context(), tx, id, "k", "m", "f", store.KeyTarget{Response: []byte{}})
		}},
		{"SET pin_hash", func(tx pgx.Tx) error {
			return store.WritePin(t.Context(), tx, id, pin.State{Hash: "h"})
		}},
	} {
		err := pgx.BeginFunc(t.Context(), &flaky{Pool: pool, failOn: tc.failOn}, tc.run)
		if !errors.Is(err, errDown) {
			t.Errorf("failing on %q = %v, want the failure", tc.failOn, err)
		}
	}
	err := pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		_, err := store.HoldPin(t.Context(), tx, "00000000-0000-0000-0000-000000000000")
		return err
	})
	if !errors.Is(err, store.ErrNoAccount) {
		t.Errorf("HoldPin of no account = %v, want ErrNoAccount", err)
	}
}

// A statement of a profile or of the countries that fails is the store's failure.
func TestAFailedStatementOfAProfileIsAFailure(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	p := store.Profile{Pseudonym: "Owl", Country: "US", VoicingSpeed: "normal", DefaultMode: "by_steps"}
	for _, tc := range []struct {
		failOn string
		run    func(tx pgx.Tx) error
	}{
		{"INSERT INTO profiles", func(tx pgx.Tx) error {
			_, err := store.CreateOwnerProfile(t.Context(), tx, id, p)
			return err
		}},
		{"INSERT INTO consents", func(tx pgx.Tx) error {
			_, err := store.CreateOwnerProfile(t.Context(), tx, id, p)
			return err
		}},
		{"FOR UPDATE", func(tx pgx.Tx) error {
			_, err := store.ProfileFor(t.Context(), tx, id, "00000000-0000-0000-0000-000000000000")
			return err
		}},
		{"UPDATE profiles", func(tx pgx.Tx) error { return store.WriteProfile(t.Context(), tx, p) }},
		{"AND owner", func(tx pgx.Tx) error {
			_, err := store.HasOwnerProfile(t.Context(), tx, id)
			return err
		}},
		{"accepted_countries", func(tx pgx.Tx) error {
			_, err := store.CountryAccepted(t.Context(), tx, "US")
			return err
		}},
		{"accepted_countries", func(tx pgx.Tx) error {
			_, err := store.AllowCountry(t.Context(), tx, "US")
			return err
		}},
		{"accepted_countries", func(tx pgx.Tx) error {
			_, err := store.RemoveCountry(t.Context(), tx, "US")
			return err
		}},
		{"accepted_countries", func(tx pgx.Tx) error {
			_, err := store.Countries(t.Context(), tx)
			return err
		}},
		{"SET active_on", func(tx pgx.Tx) error { return store.TouchProfile(t.Context(), tx, id) }},
		{"INSERT INTO consents", func(tx pgx.Tx) error {
			_, err := store.CreateChildConsent(t.Context(), tx, id, "under_13", "US")
			return err
		}},
		{"FROM consents", func(tx pgx.Tx) error {
			_, err := store.ConsentFor(t.Context(), tx, id, "00000000-0000-0000-0000-000000000000")
			return err
		}},
		{"INSERT INTO profiles", func(tx pgx.Tx) error {
			_, err := store.CreateChildProfile(t.Context(), tx, id, store.Consent{}, p)
			return err
		}},
		{"UPDATE consents", func(tx pgx.Tx) error {
			c, err := store.CreateChildConsent(t.Context(), tx, id, "under_13", "US")
			if err != nil {
				return err
			}
			// The profile is made of it; the binding is what fails.
			_, err = store.CreateChildProfile(t.Context(), tx, id, store.Consent{ChildConsent: c}, p)
			return err
		}},
		{"INSERT INTO skill_states", func(tx pgx.Tx) error {
			_, err := store.CreateOwnerProfile(t.Context(), tx, id, p)
			return err
		}},
		{"INSERT INTO skill_states", func(tx pgx.Tx) error {
			c, err := store.CreateChildConsent(t.Context(), tx, id, "under_13", "US")
			if err != nil {
				return err
			}
			// Confirmed, as epic E4 will: the profile is made and bound, and its skill
			// state is what fails.
			if _, err := tx.Exec(t.Context(), `UPDATE consents SET state = 'confirmed', confirmed_at = now(), method = 'synthetic'
				WHERE id = $1`, c.ID); err != nil {
				return err
			}
			_, err = store.CreateChildProfile(t.Context(), tx, id, store.Consent{ChildConsent: c}, p)
			return err
		}},
		{"FOR NO KEY UPDATE", func(tx pgx.Tx) error { return store.HoldAccount(t.Context(), tx, id) }},
	} {
		err := pgx.BeginFunc(t.Context(), &flaky{Pool: pool, failOn: tc.failOn}, tc.run)
		if !errors.Is(err, errDown) {
			t.Errorf("failing on %q = %v, want the failure", tc.failOn, err)
		}
	}
}

// A statement of an attempt that fails is the store's failure.
func TestAFailedStatementOfAnAttemptIsAFailure(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	profile := ownerProfile(t, pool, id, time.Now())
	none := "00000000-0000-0000-0000-000000000000"
	for _, tc := range []struct {
		failOn string
		run    func(tx pgx.Tx) error
	}{
		{"FROM engine_choices", func(tx pgx.Tx) error {
			_, err := store.ChoiceOf(t.Context(), tx, profile, none)
			return err
		}},
		{"FROM lesson_runs", func(tx pgx.Tx) error {
			_, err := store.LessonRunThere(t.Context(), tx, profile, none)
			return err
		}},
		{"FROM attempts", func(tx pgx.Tx) error {
			_, err := store.HoldAttempt(t.Context(), tx, profile, none)
			return err
		}},
		{"closed_at IS NULL FOR UPDATE", func(tx pgx.Tx) error {
			_, _, err := store.HoldOpenAttempt(t.Context(), tx, profile)
			return err
		}},
		{"INSERT INTO attempts", func(tx pgx.Tx) error {
			_, err := store.BeginAttempt(t.Context(), tx, profile, store.NewAttempt{}, time.Now())
			return err
		}},
		{"choice_id = $2", func(tx pgx.Tx) error {
			_, err := store.ChoiceBegun(t.Context(), tx, profile, none)
			return err
		}},
		{"SET close_reason = $3", func(tx pgx.Tx) error {
			return store.CloseAttempt(t.Context(), tx, profile, none, "left", time.Now())
		}},
		{"INSERT INTO submissions", func(tx pgx.Tx) error {
			_, err := store.Submit(t.Context(), tx, profile, none, []byte{}, nil, false, time.Now())
			return err
		}},
		{"FROM submissions", func(tx pgx.Tx) error {
			_, _, err := store.LastSubmission(t.Context(), tx, profile, none)
			return err
		}},
		{"FROM submissions", func(tx pgx.Tx) error {
			_, err := store.SubmissionAnswer(t.Context(), tx, profile, none)
			return err
		}},
		{"SET step_techniques_at", func(tx pgx.Tx) error {
			return store.GiveStepTechniques(t.Context(), tx, profile, none, time.Now())
		}},
		{"FROM connected_modules", func(tx pgx.Tx) error {
			_, err := store.ModuleConnected(t.Context(), tx, profile, "example/m")
			return err
		}},
		{"'going', nullif", func(tx pgx.Tx) error {
			_, err := store.RecordGoing(t.Context(), tx, none, "k", "/m", "sha256:00", store.KeyTarget{})
			return err
		}},
		{"SET state = 'done'", func(tx pgx.Tx) error {
			return store.FinishRecord(t.Context(), tx, none, "k", time.Now(), store.KeyTarget{})
		}},
		{"DELETE FROM idempotency_keys", func(tx pgx.Tx) error {
			return store.DropRecord(t.Context(), tx, none, "k", time.Time{})
		}},
	} {
		err := pgx.BeginFunc(t.Context(), &flaky{Pool: pool, failOn: tc.failOn}, tc.run)
		if !errors.Is(err, errDown) {
			t.Errorf("failing on %q = %v, want the failure", tc.failOn, err)
		}
	}
	if _, err := store.AccountView(t.Context(), &flaky{Pool: pool, failOn: "FROM attempts"}, id); !errors.Is(err, errDown) {
		t.Errorf("AccountView failing on its open attempts = %v, want the failure", err)
	}
}
