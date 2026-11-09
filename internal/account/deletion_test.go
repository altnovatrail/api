package account_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/authn"
	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/rpc"
)

// stale is the user's token with a sign-in of ten minutes ago: no fresh one.
func (w *world) stale(t *testing.T, uid string) context.Context {
	t.Helper()
	w.signin.Accept("token-"+uid, authn.Token{UID: uid, AuthTime: time.Now().Add(-10 * time.Minute)})
	return as(t, uid)
}

func TestAProfileIsDeletedByAFreshSignIn(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	id := w.ownerProfile(t, ctx).GetProfileId()
	_, err := w.client.DeleteProfile(w.stale(t, "uid-owner"), &v1.DeleteProfileRequest{ProfileId: id})
	refusedAs(t, err, connect.CodeUnauthenticated, v1.Cause_CAUSE_FRESH_SIGN_IN_NEEDED)

	ctx = w.signIn(t, "uid-owner")
	if _, err := w.client.DeleteProfile(ctx, &v1.DeleteProfileRequest{ProfileId: id}); err != nil {
		t.Fatal(err)
	}
	// The last profile leaves the account without profiles; a repeat is told
	// there is none, which app reads as deleted already.
	account, err := w.client.GetAccount(ctx, &v1.GetAccountRequest{})
	if err != nil || len(account.GetAccount().GetProfiles()) != 0 {
		t.Errorf("GetAccount = %v, %v; want the account and no profile", account, err)
	}
	_, err = w.client.DeleteProfile(ctx, &v1.DeleteProfileRequest{ProfileId: id})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
}

func TestAProfileWhoseTupleStaysIsDeletedAndItsRepeatDropsTheTuple(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	id := w.ownerProfile(t, ctx).GetProfileId()
	var accountID string
	if err := w.pool.QueryRow(t.Context(), `SELECT id FROM accounts`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	w.deleteFails.Store(true)
	if _, err := w.client.DeleteProfile(ctx, &v1.DeleteProfileRequest{ProfileId: id}); err != nil {
		t.Fatalf("DeleteProfile with its tuple stuck = %v, want deleted", err)
	}
	if log := w.log.String(); !strings.Contains(log, `"method":"/novatrail.api.v1.AccountService/DeleteProfile","code":"ok"`) ||
		!strings.Contains(log, `"error_kind"`) {
		t.Errorf("the log does not name the tuple left: %s", log)
	}
	if can, err := w.rights.Can(t.Context(), accountID, authz.CanManage, id); err != nil || !can {
		t.Fatalf("the tuple = %v, %v; want it left, dangling", can, err)
	}
	w.deleteFails.Store(false)
	_, err := w.client.DeleteProfile(ctx, &v1.DeleteProfileRequest{ProfileId: id})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
	if can, err := w.rights.Can(t.Context(), accountID, authz.CanManage, id); err != nil || can {
		t.Errorf("the tuple after the repeat = %v, %v; want it deleted", can, err)
	}
}

func TestAnAccountIsDeletedByAFreshSignInAndThenGone(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	w.ownerProfile(t, ctx)
	_, err := w.client.DeleteAccount(w.stale(t, "uid-owner"), &v1.DeleteAccountRequest{})
	refusedAs(t, err, connect.CodeUnauthenticated, v1.Cause_CAUSE_FRESH_SIGN_IN_NEEDED)

	ctx = w.signIn(t, "uid-owner")
	if _, err := w.client.DeleteAccount(ctx, &v1.DeleteAccountRequest{}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"accounts", "profiles", "consents", "idempotency_keys"} {
		if n := w.count(t, table); n != 0 {
			t.Errorf("%s holds %d rows, want none", table, n)
		}
	}
	if removed := w.signin.Removed(); len(removed) != 1 || removed[0] != "uid-owner" {
		t.Errorf("users removed = %v, want the owner", removed)
	}
	// The sign-in service has no such user now: a token still alive on another
	// device is told the account is gone.
	w.signin.Refuse("token-uid-owner", authn.NoUser, "uid-owner")
	_, err = w.client.GetAccount(ctx, &v1.GetAccountRequest{})
	refusedAs(t, err, connect.CodeUnauthenticated, v1.Cause_CAUSE_ACCOUNT_GONE)
}

func TestAnAccountWhoseDeletionDoesNotFinishIsDeletedAndMarked(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	w.ownerProfile(t, ctx)
	w.deleteFails.Store(true)
	if _, err := w.client.DeleteAccount(ctx, &v1.DeleteAccountRequest{}); err != nil {
		t.Fatalf("DeleteAccount with its tuples stuck = %v, want deleted", err)
	}
	if w.count(t, "accounts WHERE deleting_at IS NOT NULL") != 1 || w.count(t, "profiles") != 0 {
		t.Error("want the data gone and the mark left for the job")
	}
	// Its repeat is told the account is gone, by the mark.
	_, err := w.client.DeleteAccount(ctx, &v1.DeleteAccountRequest{})
	refusedAs(t, err, connect.CodeUnauthenticated, v1.Cause_CAUSE_ACCOUNT_GONE)
}

func TestADeletionThatCannotStartIsUnavailable(t *testing.T) {
	t.Parallel()

	broken, err := pgxpool.New(t.Context(), "postgres://u@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	broken.Close()
	w := newWorld(t, func(*pgxpool.Pool) rpc.DB { return broken })
	ctx := w.signIn(t, "uid-owner")
	if _, err := w.pool.Exec(t.Context(), `INSERT INTO accounts (auth_uid) VALUES ('uid-owner')`); err != nil {
		t.Fatal(err)
	}
	_, err = w.client.DeleteAccount(ctx, &v1.DeleteAccountRequest{})
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
	if w.count(t, "accounts WHERE deleting_at IS NULL") != 1 {
		t.Error("the account changed")
	}
}

// markingFirst is the handlers' database, which marks the account "deleting" —
// a deletion beside the request — before the request's transaction begins.
type markingFirst struct {
	rpc.DB
	pool  *pgxpool.Pool
	armed atomic.Bool
}

func (m *markingFirst) Begin(ctx context.Context) (pgx.Tx, error) {
	if m.armed.Load() {
		_, _ = m.pool.Exec(context.Background(), `UPDATE accounts SET deleting_at = now()`)
	}
	return m.DB.Begin(ctx)
}

// A write that passed the checks before the mark writes nothing after it.
func TestAWriteUnderAMarkedAccountIsRefused(t *testing.T) {
	t.Parallel()

	var db *markingFirst
	w := newWorld(t, func(pool *pgxpool.Pool) rpc.DB {
		db = &markingFirst{DB: pool, pool: pool}
		return db
	})
	ctx := w.owner(t, "uid-owner")
	db.armed.Store(true)
	_, err := w.client.SetPin(ctx, &v1.SetPinRequest{Pin: thePIN, IdempotencyKey: "k"})
	refusedAs(t, err, connect.CodeUnauthenticated, v1.Cause_CAUSE_ACCOUNT_GONE)
	if n := w.count(t, "accounts WHERE pin_hash IS NOT NULL"); n != 0 {
		t.Error("a PIN was written under the marked account")
	}
}

func TestAnExportIsNotGivenUntilItsFormatIs(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	id := w.ownerProfile(t, ctx).GetProfileId()
	_, err := w.client.ExportProfile(ctx, &v1.ExportProfileRequest{ProfileId: id})
	refusedAs(t, err, connect.CodeUnimplemented, v1.Cause_CAUSE_UNSPECIFIED)
	_, err = w.client.ExportAccount(ctx, &v1.ExportAccountRequest{})
	refusedAs(t, err, connect.CodeUnimplemented, v1.Cause_CAUSE_UNSPECIFIED)
}
