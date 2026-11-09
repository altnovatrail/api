package rpc_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/altnovatrail/api/internal/authn"
	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/rpc"
)

var errDown = errors.New("the test's database is down")

// flaky is the database with one statement that fails: the first whose SQL holds
// failOn, and Begin when failBegin is set.
type flaky struct {
	rpc.DB
	failOn    string
	failBegin bool
}

type failedRow struct{}

func (failedRow) Scan(...any) error { return errDown }

func (f *flaky) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.failOn != "" && strings.Contains(sql, f.failOn) {
		return failedRow{}
	}
	return f.DB.QueryRow(ctx, sql, args...)
}

func (f *flaky) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.failOn != "" && strings.Contains(sql, f.failOn) {
		return pgconn.CommandTag{}, errDown
	}
	return f.DB.Exec(ctx, sql, args...)
}

func (f *flaky) Begin(ctx context.Context) (pgx.Tx, error) {
	if f.failBegin {
		return nil, errDown
	}
	return f.DB.Begin(ctx)
}

// downRights is a store of rights that does not answer.
type downRights struct{}

func (downRights) DeleteOwner(context.Context, string, string) error { return errDown }

func (downRights) DeleteOwned(context.Context, string, []string) error { return errDown }

func (downRights) OwnedProfiles(context.Context, string) ([]string, error) { return nil, errDown }

func (downRights) Can(context.Context, string, authz.Right, string) (bool, error) {
	return false, errDown
}

// brokenSignin is a sign-in service that fails with no refusal of its own.
type brokenSignin struct{ authn.Port }

func (brokenSignin) Verify(context.Context, string) (authn.Token, error) {
	return authn.Token{}, errDown
}

// A check that cannot finish — the database, the rights, the sign-in service —
// is "unavailable", with no cause, and its line names the failure by its kind
// and nothing of it more.
func TestAFailedCheckIsUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		token  func(w *world, p person) string
		adjust func(d *rpc.Deps)
	}{
		{"the account cannot be found", nil, func(d *rpc.Deps) { d.DB = &flaky{DB: d.DB, failOn: "auth_uid"} }},
		{"the account cannot be touched", nil, func(d *rpc.Deps) { d.DB = &flaky{DB: d.DB, failOn: "SET last_request_on"} }},
		{"the profile cannot be read", nil, func(d *rpc.Deps) { d.DB = &flaky{DB: d.DB, failOn: "FROM profiles"} }},
		{"the profile cannot be touched", func(w *world, p person) string {
			// A date of another day, which the request writes.
			if _, err := w.pool.Exec(context.Background(), `UPDATE profiles SET active_on = DATE '2026-01-01'`); err != nil {
				panic(err)
			}
			return p.token
		}, func(d *rpc.Deps) { d.DB = &flaky{DB: d.DB, failOn: "SET active_on"} }},
		{"the rights do not answer", nil, func(d *rpc.Deps) { d.Rights = downRights{} }},
		{"the sign-in service breaks", nil, func(d *rpc.Deps) { d.Signin = brokenSignin{d.Signin} }},
		{"a revoked token's account cannot be found", func(w *world, p person) string {
			w.signin.Refuse("token-r", authn.Revoked, p.uid)
			return "token-r"
		}, func(d *rpc.Deps) { d.DB = &flaky{DB: d.DB, failOn: "auth_uid"} }},
		{"a gone user's account cannot be found", func(w *world, p person) string {
			w.signin.Refuse("token-g", authn.NoUser, p.uid)
			return "token-g"
		}, func(d *rpc.Deps) { d.DB = &flaky{DB: d.DB, failOn: "auth_uid"} }},
		{"a gone user's account cannot be marked", func(w *world, p person) string {
			w.signin.Refuse("token-m", authn.NoUser, p.uid)
			return "token-m"
		}, func(d *rpc.Deps) { d.DB = &flaky{DB: d.DB, failBegin: true} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t, time.Second, tc.adjust)
			p := w.person(t, "uid-failing")
			token := p.token
			if tc.token != nil {
				token = tc.token(w, p)
			}
			_, err := w.client.UpdateProfile(as(t, token), &v1.UpdateProfileRequest{ProfileId: p.profileID})
			refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
			log := w.log.String()
			if !strings.Contains(log, `"severity":"ERROR"`) || !strings.Contains(log, `"error_kind":"`) {
				t.Errorf("the line does not name the failure:\n%s", log)
			}
			if strings.Contains(log, errDown.Error()) || strings.Contains(log, p.uid) {
				t.Errorf("the line holds the failure's words or the UID:\n%s", log)
			}
		})
	}
}

// A handler's own error with no code is "unknown" to the client, and its kind
// goes to the line.
func TestAHandlersBareErrorIsLoggedByItsKind(t *testing.T) {
	w := newWorld(t, time.Second)
	p := w.person(t, "uid-bare")
	w.accounts.bare = true

	_, err := w.client.GetAccount(as(t, p.token), &v1.GetAccountRequest{})
	refusedAs(t, err, connect.CodeUnknown, v1.Cause_CAUSE_UNSPECIFIED)
	if log := w.log.String(); !strings.Contains(log, `"error_kind":"`) || !strings.Contains(log, `"code":"unknown"`) {
		t.Errorf("the line of a bare error:\n%s", log)
	}
}

// stuckRights is a store of rights that answers the checks and deletes nothing.
type stuckRights struct{ *authz.Rights }

func (stuckRights) DeleteOwner(context.Context, string, string) error { return errDown }

func (stuckRights) DeleteOwned(context.Context, string, []string) error { return errDown }

// A tuple the deletion's repeat cannot delete is a failure: the repeat after it
// deletes it.
func TestADanglingTupleThatDoesNotGoIsUnavailable(t *testing.T) {
	w := newWorld(t, time.Second, func(d *rpc.Deps) { d.Rights = stuckRights{d.Rights.(*authz.Rights)} })
	p := w.person(t, "uid-stuck")
	if _, err := w.pool.Exec(t.Context(), `DELETE FROM profiles WHERE id = $1`, p.profileID); err != nil {
		t.Fatal(err)
	}
	_, err := w.client.DeleteProfile(as(t, p.token), &v1.DeleteProfileRequest{ProfileId: p.profileID})
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
}

// A user gone whose account's tuples do not go is the account gone all the
// same: the mark stays, and the daily job finishes it.
func TestAUserGoneWhoseDeletionDoesNotFinishLeavesTheMark(t *testing.T) {
	w := newWorld(t, time.Second, func(d *rpc.Deps) { d.Rights = stuckRights{d.Rights.(*authz.Rights)} })
	p := w.person(t, "uid-gone-stuck")
	w.signin.Refuse(p.token, authn.NoUser, p.uid)
	_, err := w.client.GetAccount(as(t, p.token), &v1.GetAccountRequest{})
	refusedAs(t, err, connect.CodeUnauthenticated, v1.Cause_CAUSE_ACCOUNT_GONE)
	if !w.deleting(t, p.accountID) {
		t.Error("the account is not marked")
	}
	if log := w.log.String(); !strings.Contains(log, `"error_kind"`) || !strings.Contains(log, `"cause":"CAUSE_ACCOUNT_GONE"`) {
		t.Errorf("the line does not name what is left to finish:\n%s", log)
	}
	var profiles int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM profiles`).Scan(&profiles); err != nil || profiles != 0 {
		t.Errorf("profiles = %d, %v; want the data gone with the mark", profiles, err)
	}
}
