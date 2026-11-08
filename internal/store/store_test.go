package store_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

// account makes an account and returns its ID. The UID is synthetic.
func account(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	var id string
	err := pool.QueryRow(t.Context(),
		`INSERT INTO accounts (auth_uid) VALUES ('uid-' || gen_random_uuid()) RETURNING id`).Scan(&id)
	if err != nil {
		t.Fatalf("make an account: %v", err)
	}
	return id
}

// mark marks an account "deleting", as the deletion of an account does (R21).
func mark(t *testing.T, pool *pgxpool.Pool, id string) {
	t.Helper()

	err := pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error { return store.MarkDeleting(t.Context(), tx, id) })
	if err != nil {
		t.Fatalf("mark the account: %v", err)
	}
}

// profile writes the owner's profile of an account in tx.
func profile(ctx context.Context, tx pgx.Tx, accountID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO profiles
		(account_id, owner, pseudonym, age_category, country, voicing_enabled, default_mode, ask_before_showing)
		VALUES ($1, true, 'Owl', 'adult', 'US', false, 'self', false)`, accountID)
	return err
}

func count(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestAWriteUnderALiveAccountCommits(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	err := store.WriteUnderAccount(t.Context(), pool, id, func(tx pgx.Tx) error {
		return profile(t.Context(), tx, id)
	})
	if err != nil {
		t.Fatalf("WriteUnderAccount error = %v, want nil", err)
	}
	if got := count(t, pool, "profiles"); got != 1 {
		t.Errorf("%d profiles after the write, want 1", got)
	}
}

// The test the helper is for: a write under an account marked "deleting" is
// refused and writes nothing (R21).
func TestAWriteUnderAMarkedAccountIsRefused(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	mark(t, pool, id)
	ran := false
	err := store.WriteUnderAccount(t.Context(), pool, id, func(tx pgx.Tx) error {
		ran = true
		return profile(t.Context(), tx, id)
	})
	if !errors.Is(err, store.ErrAccountDeleting) {
		t.Fatalf("WriteUnderAccount error = %v, want %v", err, store.ErrAccountDeleting)
	}
	if ran || count(t, pool, "profiles") != 0 {
		t.Error("the write under a marked account ran")
	}
}

func TestAWriteUnderNoAccountIsRefused(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	err := store.WriteUnderAccount(t.Context(), pool, "0190f0a0-0000-7000-8000-000000000000", func(pgx.Tx) error {
		t.Error("the write under no account ran")
		return nil
	})
	if !errors.Is(err, store.ErrNoAccount) {
		t.Fatalf("WriteUnderAccount error = %v, want %v", err, store.ErrNoAccount)
	}
}

func TestAnAccountIDThatIsNoUUIDFails(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	err := store.WriteUnderAccount(t.Context(), pool, "not-a-uuid", func(pgx.Tx) error { return nil })
	if err == nil || errors.Is(err, store.ErrNoAccount) || !strings.Contains(err.Error(), "store: hold the account") {
		t.Errorf("WriteUnderAccount error = %v, want a failure to hold the account", err)
	}
}

// A failed write rolls back, and its error comes back as it is.
func TestAFailedWriteRollsBack(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	failed := errors.New("synthetic failure")
	err := store.WriteUnderAccount(t.Context(), pool, id, func(tx pgx.Tx) error {
		if err := profile(t.Context(), tx, id); err != nil {
			return err
		}
		return failed
	})
	if !errors.Is(err, failed) {
		t.Fatalf("WriteUnderAccount error = %v, want %v", err, failed)
	}
	if got := count(t, pool, "profiles"); got != 0 {
		t.Errorf("%d profiles after a failed write, want 0", got)
	}
}

// A write that holds the account before its mark commits first: MarkDeleting
// waits for it, and the next write sees the mark (R21). The writes below do not
// touch the account's row, which is the case a plain UPDATE of the mark would
// not wait for, and then update it, which FOR KEY SHARE lets them do.
func TestTheMarkWaitsForAWriteThatHoldsTheAccount(t *testing.T) {
	t.Parallel()

	for name, touch := range map[string]bool{"a write that does not touch the account": false, "a write that updates it": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pool := storetest.DB(t)
			id := account(t, pool)
			holding, released := make(chan struct{}), make(chan struct{})
			// The write is let go on every way out of the test, before its pool is
			// closed: a failure below would otherwise leave it holding a connection.
			release := sync.OnceFunc(func() { close(released) })
			t.Cleanup(release)
			wrote := make(chan error, 1)
			go func() {
				wrote <- store.WriteUnderAccount(context.Background(), pool, id, func(tx pgx.Tx) error {
					if touch {
						if _, err := tx.Exec(context.Background(), `UPDATE accounts SET last_request_on = current_date WHERE id = $1`, id); err != nil {
							return err
						}
					}
					close(holding)
					<-released
					return profile(context.Background(), tx, id)
				})
			}()
			<-holding

			marked := make(chan error, 1)
			go func() {
				marked <- pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
					return store.MarkDeleting(context.Background(), tx, id)
				})
			}()
			select {
			case err := <-marked:
				t.Fatalf("the mark did not wait for the write that holds the account: %v", err)
			case <-time.After(200 * time.Millisecond):
			}

			release()
			if err := <-wrote; err != nil {
				t.Fatalf("the write that held the account: %v", err)
			}
			if err := <-marked; err != nil {
				t.Fatalf("the mark: %v", err)
			}
			err := store.WriteUnderAccount(t.Context(), pool, id, func(pgx.Tx) error { return nil })
			if !errors.Is(err, store.ErrAccountDeleting) {
				t.Errorf("the write after the mark: error = %v, want %v", err, store.ErrAccountDeleting)
			}
			if got := count(t, pool, "profiles"); got != 1 {
				t.Errorf("%d profiles, want the 1 the write before the mark made", got)
			}
		})
	}
}

// Marking is the first step, and a repeat of the deletion marks again: the time
// of the first mark stays.
func TestMarkDeleting(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	mark(t, pool, id)
	var first time.Time
	if err := pool.QueryRow(t.Context(), `SELECT deleting_at FROM accounts WHERE id = $1`, id).Scan(&first); err != nil {
		t.Fatal(err)
	}
	mark(t, pool, id)
	var second time.Time
	if err := pool.QueryRow(t.Context(), `SELECT deleting_at FROM accounts WHERE id = $1`, id).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if !second.Equal(first) {
		t.Errorf("a second mark moved the time from %v to %v", first, second)
	}

	for _, missing := range []string{"0190f0a0-0000-7000-8000-000000000000", "not-a-uuid"} {
		err := pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error { return store.MarkDeleting(t.Context(), tx, missing) })
		if err == nil || (missing != "not-a-uuid" && !errors.Is(err, store.ErrNoAccount)) {
			t.Errorf("MarkDeleting(%s) error = %v, want a refusal", missing, err)
		}
	}
}

// The schema holds what R21 and the contract ask of it.
func TestTheSchemaRefuses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		write func(ctx context.Context, pool *pgxpool.Pool, id string) error
		code  string
	}{
		{"a second profile of the owner", func(ctx context.Context, pool *pgxpool.Pool, id string) error {
			return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
				if err := profile(ctx, tx, id); err != nil {
					return err
				}
				return profile(ctx, tx, id)
			})
		}, "23505"},
		{"a code bound but not redeemed", func(ctx context.Context, pool *pgxpool.Pool, id string) error {
			_, err := pool.Exec(ctx, `INSERT INTO invite_codes (code_sha256, tariff, account_id)
				VALUES (sha256('synthetic'), 'closed_slice', $1)`, id)
			return err
		}, "23514"},
		{"an offset that is no quarter of an hour", func(ctx context.Context, pool *pgxpool.Pool, id string) error {
			_, err := pool.Exec(ctx, `UPDATE accounts SET utc_offset_minutes = 10 WHERE id = $1`, id)
			return err
		}, "23514"},
		{"an offset past +14 hours", func(ctx context.Context, pool *pgxpool.Pool, id string) error {
			_, err := pool.Exec(ctx, `UPDATE accounts SET utc_offset_minutes = 855 WHERE id = $1`, id)
			return err
		}, "23514"},
		{"an adult that asks before showing", func(ctx context.Context, pool *pgxpool.Pool, id string) error {
			_, err := pool.Exec(ctx, `INSERT INTO profiles
				(account_id, owner, pseudonym, age_category, country, voicing_enabled, default_mode, ask_before_showing)
				VALUES ($1, false, 'Owl', 'adult', 'US', false, 'self', true)`, id)
			return err
		}, "23514"},
		{"an owner who is a child", func(ctx context.Context, pool *pgxpool.Pool, id string) error {
			_, err := pool.Exec(ctx, `INSERT INTO profiles
				(account_id, owner, pseudonym, age_category, country, voicing_enabled, default_mode, ask_before_showing)
				VALUES ($1, true, 'Owl', 'under_13', 'US', true, 'by_steps', true)`, id)
			return err
		}, "23514"},
		{"a country that is no code", func(ctx context.Context, pool *pgxpool.Pool, id string) error {
			_, err := pool.Exec(ctx, `INSERT INTO profiles
				(account_id, owner, pseudonym, age_category, country, voicing_enabled, default_mode, ask_before_showing)
				VALUES ($1, true, 'Owl', 'adult', 'usa', false, 'self', false)`, id)
			return err
		}, "23514"},
		{"a waiting consent with a profile", func(ctx context.Context, pool *pgxpool.Pool, id string) error {
			return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
				if err := profile(ctx, tx, id); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, `INSERT INTO consents (account_id, profile_id, subject, state, age_category, country)
					SELECT $1, id, 'child', 'waiting', 'under_13', 'US' FROM profiles WHERE account_id = $1`, id)
				return err
			})
		}, "23514"},
		{"a row about a profile that is gone", func(ctx context.Context, pool *pgxpool.Pool, id string) error {
			_, err := pool.Exec(ctx, `INSERT INTO consents (account_id, profile_id, subject, state, age_category, country, confirmed_at)
				VALUES ($1, gen_random_uuid(), 'adult', 'confirmed', 'adult', 'US', now())`, id)
			return err
		}, "23503"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pool := storetest.DB(t)
			err := tc.write(t.Context(), pool, account(t, pool))
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tc.code {
				t.Errorf("the write: error = %v, want SQLSTATE %s", err, tc.code)
			}
		})
	}
}

// The deletion of an account unbinds its code, which stays redeemed (R21).
func TestADeletedAccountLeavesItsCodeRedeemed(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	id := account(t, pool)
	if _, err := pool.Exec(t.Context(), `INSERT INTO invite_codes (code_sha256, tariff, redeemed_at, account_id)
		VALUES (sha256('synthetic'), 'closed_slice', now(), $1)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `DELETE FROM accounts WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	var bound *string
	var redeemed *time.Time
	if err := pool.QueryRow(t.Context(), `SELECT account_id, redeemed_at FROM invite_codes`).Scan(&bound, &redeemed); err != nil {
		t.Fatal(err)
	}
	if bound != nil || redeemed == nil {
		t.Errorf("the code after its account: bound %v, redeemed %v; want unbound and redeemed", bound, redeemed)
	}
}

func TestOpenRefusesAStringItCannotRead(t *testing.T) {
	t.Parallel()

	_, err := store.Open(t.Context(), "postgres://u:not-a-password@host:notaport/api")
	if err == nil || strings.Contains(err.Error(), "not-a-password") {
		t.Errorf("Open error = %v, want a refusal that holds no password", err)
	}
}
