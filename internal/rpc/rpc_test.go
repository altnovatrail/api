package rpc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/authn"
	"github.com/altnovatrail/api/internal/authn/authntest"
	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/server"
	"github.com/altnovatrail/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

// syncBuffer is a log the server and the test may share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// accounts is the tests' handler of AccountService: every method it defines
// answers once the checks let it through, and writes down whom they let through.
type accounts struct {
	apiv1connect.UnimplementedAccountServiceHandler
	mu      sync.Mutex
	callers []rpc.Caller
	// wait makes GetAccount wait for the end of its request's time.
	wait bool
	// bare makes GetAccount fail with an error that is no error of Connect.
	bare bool
}

func (a *accounts) let(ctx context.Context) {
	caller, _ := rpc.CallerFrom(ctx)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.callers = append(a.callers, caller)
}

func (a *accounts) last() (rpc.Caller, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.callers) == 0 {
		return rpc.Caller{}, 0
	}
	return a.callers[len(a.callers)-1], len(a.callers)
}

func (a *accounts) GetAccount(ctx context.Context, _ *v1.GetAccountRequest) (*v1.GetAccountResponse, error) {
	if a.bare {
		return nil, errors.New("a handler's own failure")
	}
	if a.wait {
		<-ctx.Done()
		return nil, connect.NewError(connect.CodeDeadlineExceeded, "")
	}
	a.let(ctx)
	caller, _ := rpc.CallerFrom(ctx)
	return &v1.GetAccountResponse{Account: &v1.Account{AccountId: caller.AccountID, PinSet: true}}, nil
}

func (a *accounts) CreateAccount(ctx context.Context, _ *v1.CreateAccountRequest) (*v1.CreateAccountResponse, error) {
	a.let(ctx)
	return &v1.CreateAccountResponse{}, nil
}

func (a *accounts) UpdateProfile(ctx context.Context, _ *v1.UpdateProfileRequest) (*v1.UpdateProfileResponse, error) {
	a.let(ctx)
	return &v1.UpdateProfileResponse{}, nil
}

func (a *accounts) DeleteProfile(ctx context.Context, _ *v1.DeleteProfileRequest) (*v1.DeleteProfileResponse, error) {
	a.let(ctx)
	return &v1.DeleteProfileResponse{}, nil
}

func (a *accounts) DeleteAccount(ctx context.Context, _ *v1.DeleteAccountRequest) (*v1.DeleteAccountResponse, error) {
	a.let(ctx)
	return &v1.DeleteAccountResponse{}, nil
}

// world is a server of the contract on a database, a store of rights and a
// fake sign-in service of the test's own.
type world struct {
	pool     *pgxpool.Pool
	rights   *authz.Rights
	signin   *authntest.Fake
	accounts *accounts
	log      *syncBuffer
	url      string
	client   apiv1connect.AccountServiceClient
	packages apiv1connect.PackageServiceClient
}

func newWorld(t *testing.T, timeout time.Duration, adjust ...func(*rpc.Deps)) *world {
	t.Helper()

	rights, err := authz.Open(t.Context(), storetest.OpenFGAURL(t, true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rights.Close)
	w := &world{pool: storetest.DB(t), rights: rights, signin: &authntest.Fake{}, accounts: &accounts{}, log: &syncBuffer{}}
	log := logging.New(w.log, slog.LevelInfo)
	deps := rpc.Deps{Signin: w.signin, DB: w.pool, Rights: rights, Log: log, RequestTimeout: timeout}
	for _, change := range adjust {
		change(&deps)
	}
	srv := rpc.NewServer(deps, rpc.Handlers{Account: w.accounts})
	httpServer := httptest.NewServer(server.Routes(log, "", func(mux *http.ServeMux) { rpc.Mount(mux, srv) }))
	t.Cleanup(httpServer.Close)
	w.url = httpServer.URL
	transport := connecthttp.NewTransport(httpServer.Client(), httpServer.URL)
	w.client = apiv1connect.NewAccountServiceClient(connect.NewClient(transport))
	w.packages = apiv1connect.NewPackageServiceClient(connect.NewClient(transport))
	return w
}

// person is an account of the world with its owner's profile and its tuple,
// signed in now: its token is "token-<uid>".
type person struct {
	uid, accountID, profileID, token string
}

func (w *world) person(t *testing.T, uid string) person {
	t.Helper()

	p := person{uid: uid, token: "token-" + uid}
	if err := w.pool.QueryRow(t.Context(), `INSERT INTO accounts (auth_uid) VALUES ($1) RETURNING id`, uid).Scan(&p.accountID); err != nil {
		t.Fatal(err)
	}
	err := w.pool.QueryRow(t.Context(), `INSERT INTO profiles
		(account_id, owner, pseudonym, age_category, country, voicing_enabled, default_mode, ask_before_showing)
		VALUES ($1, true, 'Owl', 'adult', 'US', false, 'self', false) RETURNING id`, p.accountID).Scan(&p.profileID)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.rights.WriteOwner(t.Context(), p.accountID, p.profileID); err != nil {
		t.Fatal(err)
	}
	w.signin.Accept(p.token, authn.Token{UID: uid, AuthTime: time.Now()})
	return p
}

func (w *world) markDeleting(t *testing.T, accountID string) {
	t.Helper()
	if _, err := w.pool.Exec(t.Context(), `UPDATE accounts SET deleting_at = now() WHERE id = $1`, accountID); err != nil {
		t.Fatal(err)
	}
}

func (w *world) deleting(t *testing.T, accountID string) bool {
	t.Helper()
	var marked bool
	if err := w.pool.QueryRow(t.Context(), `SELECT deleting_at IS NOT NULL FROM accounts WHERE id = $1`, accountID).Scan(&marked); err != nil {
		t.Fatal(err)
	}
	return marked
}

// as is a context whose calls carry the token.
func as(t *testing.T, token string) context.Context {
	ctx, info := connect.NewClientContext(t.Context())
	if token != "" {
		info.RequestHeader().Set("Authorization", "Bearer "+token)
	}
	return ctx
}

// refusal is the code of err and the Refusal it carries, if any.
func refusal(t *testing.T, err error) (connect.Code, *v1.Refusal) {
	t.Helper()
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		t.Fatalf("error = %v, want an error of Connect", err)
	}
	for _, detail := range connectErr.Details() {
		msg, err := connectproto.UnmarshalErrorDetail(detail)
		if err != nil {
			t.Fatal(err)
		}
		if r, ok := msg.(*v1.Refusal); ok {
			return connectErr.Code(), r
		}
	}
	return connectErr.Code(), nil
}

// refusedAs fails the test unless err is code with cause; CAUSE_UNSPECIFIED is
// an error with no Refusal.
func refusedAs(t *testing.T, err error, code connect.Code, cause v1.Cause) *v1.Refusal {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %v %v", code, cause)
	}
	gotCode, r := refusal(t, err)
	gotCause := v1.Cause_CAUSE_UNSPECIFIED
	if r != nil {
		gotCause = r.GetCause()
	}
	if gotCode != code || gotCause != cause {
		t.Fatalf("refusal = %v %v, want %v %v", gotCode, gotCause, code, cause)
	}
	return r
}

func TestAnOwnersRequestReachesItsHandler(t *testing.T) {
	w := newWorld(t, time.Second)
	p := w.person(t, "uid-owner")

	if _, err := w.client.UpdateProfile(as(t, p.token), &v1.UpdateProfileRequest{ProfileId: p.profileID}); err != nil {
		t.Fatalf("UpdateProfile error = %v, want nil", err)
	}
	if caller, _ := w.accounts.last(); caller.UID != p.uid || caller.AccountID != p.accountID {
		t.Errorf("caller = %+v, want the owner's account", caller)
	}
	if _, err := w.client.GetAccount(as(t, p.token), &v1.GetAccountRequest{}); err != nil {
		t.Errorf("GetAccount error = %v, want nil", err)
	}
}

// Each class of a refused token is its answer, by the table of architecture
// 3.10.
func TestEachClassOfATokenIsItsAnswer(t *testing.T) {
	w := newWorld(t, time.Second)
	marked := w.person(t, "uid-marked")
	w.markDeleting(t, marked.accountID)
	alive := w.person(t, "uid-alive")

	for _, tc := range []struct {
		name  string
		class authn.Class
		uid   string
		code  connect.Code
		cause v1.Cause
	}{
		{"expired", authn.Expired, "", connect.CodeUnauthenticated, v1.Cause_CAUSE_UNSPECIFIED},
		{"invalid", authn.Invalid, "", connect.CodeUnauthenticated, v1.Cause_CAUSE_UNSPECIFIED},
		{"disabled", authn.Disabled, "", connect.CodeUnauthenticated, v1.Cause_CAUSE_UNSPECIFIED},
		{"revoked, of an account not marked", authn.Revoked, alive.uid, connect.CodeUnauthenticated, v1.Cause_CAUSE_UNSPECIFIED},
		{"revoked, of no account", authn.Revoked, "uid-none", connect.CodeUnauthenticated, v1.Cause_CAUSE_UNSPECIFIED},
		{"revoked, of a marked account", authn.Revoked, marked.uid, connect.CodeUnauthenticated, v1.Cause_CAUSE_ACCOUNT_GONE},
		{"of a user who is gone, with no account", authn.NoUser, "uid-none", connect.CodeUnauthenticated, v1.Cause_CAUSE_ACCOUNT_GONE},
		{"of a user who is gone, marked", authn.NoUser, marked.uid, connect.CodeUnauthenticated, v1.Cause_CAUSE_ACCOUNT_GONE},
		{"failed", authn.Failed, "", connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := "token-" + strings.ReplaceAll(tc.name, " ", "-")
			w.signin.Refuse(token, tc.class, tc.uid)
			_, err := w.client.GetAccount(as(t, token), &v1.GetAccountRequest{})
			refusedAs(t, err, tc.code, tc.cause)
		})
	}
	_, err := w.client.GetAccount(as(t, ""), &v1.GetAccountRequest{})
	refusedAs(t, err, connect.CodeUnauthenticated, v1.Cause_CAUSE_UNSPECIFIED)
	if w.deleting(t, alive.accountID) {
		t.Error("a revoked token marked its account")
	}
	if _, n := w.accounts.last(); n != 0 {
		t.Errorf("%d refused requests reached the handler", n)
	}
}

// A user deleted past api — in the project's console — takes the account with
// it: the request marks it, and answers that it is gone (R20).
func TestAUserGonePastAPIMarksTheAccount(t *testing.T) {
	w := newWorld(t, time.Second)
	p := w.person(t, "uid-gone")
	w.signin.Refuse(p.token, authn.NoUser, p.uid)

	_, err := w.client.GetAccount(as(t, p.token), &v1.GetAccountRequest{})
	refusedAs(t, err, connect.CodeUnauthenticated, v1.Cause_CAUSE_ACCOUNT_GONE)
	if !w.deleting(t, p.accountID) {
		t.Error("the account of a user who is gone is not marked")
	}
}

// A marked account gets "the account is gone" on every request, creating the
// account among them.
func TestAMarkedAccountIsGoneOnEveryRequest(t *testing.T) {
	w := newWorld(t, time.Second)
	p := w.person(t, "uid-leaving")
	w.markDeleting(t, p.accountID)

	for name, call := range map[string]func(context.Context) error{
		"GetAccount": func(ctx context.Context) error {
			_, err := w.client.GetAccount(ctx, &v1.GetAccountRequest{})
			return err
		},
		"CreateAccount": func(ctx context.Context) error {
			_, err := w.client.CreateAccount(ctx, &v1.CreateAccountRequest{})
			return err
		},
		"UpdateProfile": func(ctx context.Context) error {
			_, err := w.client.UpdateProfile(ctx, &v1.UpdateProfileRequest{ProfileId: p.profileID})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			refusedAs(t, call(as(t, p.token)), connect.CodeUnauthenticated, v1.Cause_CAUSE_ACCOUNT_GONE)
		})
	}
}

// A UID with no account may only create one: anything else is "no access".
func TestAUIDWithNoAccountMayOnlyCreateOne(t *testing.T) {
	w := newWorld(t, time.Second)
	w.signin.Accept("token-new", authn.Token{UID: "uid-new", AuthTime: time.Now()})

	_, err := w.client.GetAccount(as(t, "token-new"), &v1.GetAccountRequest{})
	refusedAs(t, err, connect.CodePermissionDenied, v1.Cause_CAUSE_NO_ACCESS)
	if _, err := w.client.CreateAccount(as(t, "token-new"), &v1.CreateAccountRequest{}); err != nil {
		t.Fatalf("CreateAccount error = %v, want nil", err)
	}
	if caller, _ := w.accounts.last(); caller.UID != "uid-new" || caller.AccountID != "" {
		t.Errorf("caller = %+v, want the UID with no account", caller)
	}
}

// Another account's profile, one that is not there, one that is no UUID and one
// whose tuple dangles get one and the same refusal, before anything is read by
// the ID.
func TestAProfileNotOfTheAccountIsNone(t *testing.T) {
	w := newWorld(t, time.Second)
	p := w.person(t, "uid-me")
	other := w.person(t, "uid-other")
	dangling := w.person(t, "uid-dangling")
	if _, err := w.pool.Exec(t.Context(), `DELETE FROM profiles WHERE id = $1`, dangling.profileID); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct{ token, profile string }{
		"another account's":     {p.token, other.profileID},
		"one that is not there": {p.token, "00000000-0000-4000-8000-000000000000"},
		"one that is no UUID":   {p.token, "../profiles"},
		"one written as a URN":  {p.token, "urn:uuid:" + p.profileID},
		"one in uppercase":      {p.token, strings.ToUpper(p.profileID)},
		"a dangling tuple's":    {dangling.token, dangling.profileID},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := w.client.UpdateProfile(as(t, tc.token), &v1.UpdateProfileRequest{ProfileId: tc.profile})
			refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
		})
	}
	if _, n := w.accounts.last(); n != 0 {
		t.Errorf("%d refused requests reached the handler", n)
	}
}

// The fresh sign-in is checked last, after the right: a stale one on a profile
// that is not the account's is "no profile", and on its own profile it is "a
// fresh sign-in is needed", with how old one may be.
func TestTheFreshSignInIsCheckedLast(t *testing.T) {
	w := newWorld(t, time.Second)
	p := w.person(t, "uid-stale")
	other := w.person(t, "uid-someone")
	w.signin.Accept(p.token, authn.Token{UID: p.uid, AuthTime: time.Now().Add(-10 * time.Minute)})

	_, err := w.client.DeleteProfile(as(t, p.token), &v1.DeleteProfileRequest{ProfileId: other.profileID})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
	_, err = w.client.DeleteProfile(as(t, p.token), &v1.DeleteProfileRequest{ProfileId: p.profileID})
	r := refusedAs(t, err, connect.CodeUnauthenticated, v1.Cause_CAUSE_FRESH_SIGN_IN_NEEDED)
	if r.GetMaxAuthAgeSeconds() != 300 {
		t.Errorf("max_auth_age_seconds = %d, want 300", r.GetMaxAuthAgeSeconds())
	}
	_, err = w.client.DeleteAccount(as(t, p.token), &v1.DeleteAccountRequest{})
	refusedAs(t, err, connect.CodeUnauthenticated, v1.Cause_CAUSE_FRESH_SIGN_IN_NEEDED)

	w.signin.Accept(p.token, authn.Token{UID: p.uid, AuthTime: time.Now()})
	if _, err := w.client.DeleteProfile(as(t, p.token), &v1.DeleteProfileRequest{ProfileId: p.profileID}); err != nil {
		t.Errorf("DeleteProfile freshly signed in: error = %v, want nil", err)
	}
	if _, err := w.client.DeleteAccount(as(t, p.token), &v1.DeleteAccountRequest{}); err != nil {
		t.Errorf("DeleteAccount freshly signed in: error = %v, want nil", err)
	}
}

// An open view goes with no token; a method not built yet answers unimplemented,
// once its checks let it through.
func TestOpenViewsNeedNoTokenAndTheRestIsUnimplemented(t *testing.T) {
	w := newWorld(t, time.Second)
	p := w.person(t, "uid-reader")

	_, err := w.packages.GetManifest(as(t, ""), &v1.GetManifestRequest{})
	refusedAs(t, err, connect.CodeUnimplemented, v1.Cause_CAUSE_UNSPECIFIED)
	_, err = w.packages.CheckPackages(as(t, ""), &v1.CheckPackagesRequest{})
	refusedAs(t, err, connect.CodeUnauthenticated, v1.Cause_CAUSE_UNSPECIFIED)
	_, err = w.client.ExportAccount(as(t, p.token), &v1.ExportAccountRequest{})
	refusedAs(t, err, connect.CodeUnimplemented, v1.Cause_CAUSE_UNSPECIFIED)
}

// A request runs no longer than its time limit.
func TestARequestEndsAtItsTimeLimit(t *testing.T) {
	w := newWorld(t, 50*time.Millisecond)
	p := w.person(t, "uid-slow")
	w.accounts.wait = true

	_, err := w.client.GetAccount(as(t, p.token), &v1.GetAccountRequest{})
	refusedAs(t, err, connect.CodeDeadlineExceeded, v1.Cause_CAUSE_UNSPECIFIED)
}

// JSON is taken and written with the protobuf names (R18).
func TestJSONHasTheProtobufNames(t *testing.T) {
	w := newWorld(t, time.Second)
	p := w.person(t, "uid-json")

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		w.url+apiv1connect.AccountServiceGetAccountProcedure, strings.NewReader(`{"unknown_field": 1}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.token)
	answer, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer answer.Body.Close()
	var body map[string]map[string]any
	if err := json.NewDecoder(answer.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if answer.StatusCode != http.StatusOK || body["account"]["account_id"] != p.accountID || body["account"]["pin_set"] != true {
		t.Errorf("GetAccount in JSON = %d %v, want the account by the protobuf names", answer.StatusCode, body)
	}
}

// The date of the last request is today after a request, written once a day.
func TestARequestTouchesItsAccount(t *testing.T) {
	w := newWorld(t, time.Second)
	p := w.person(t, "uid-touch")
	if _, err := w.pool.Exec(t.Context(), `UPDATE accounts SET last_request_on = DATE '2026-01-01' WHERE id = $1`, p.accountID); err != nil {
		t.Fatal(err)
	}

	if _, err := w.client.GetAccount(as(t, p.token), &v1.GetAccountRequest{}); err != nil {
		t.Fatal(err)
	}
	var today bool
	if err := w.pool.QueryRow(t.Context(),
		`SELECT last_request_on = (now() AT TIME ZONE 'UTC')::date FROM accounts WHERE id = $1`, p.accountID).Scan(&today); err != nil {
		t.Fatal(err)
	}
	if !today {
		t.Error("the date of the last request is not today")
	}
}

// The request's line names its method, code, cause and token class, and nothing
// of who asked: no UID, token or ID.
func TestTheLogLineHoldsNoIdentifier(t *testing.T) {
	w := newWorld(t, time.Second)
	p := w.person(t, "uid-logged-person")
	w.signin.Refuse("token-revoked-one", authn.Revoked, p.uid)

	_, _ = w.client.UpdateProfile(as(t, p.token), &v1.UpdateProfileRequest{ProfileId: p.profileID})
	_, _ = w.client.GetAccount(as(t, "token-revoked-one"), &v1.GetAccountRequest{})
	_, _ = w.client.UpdateProfile(as(t, p.token), &v1.UpdateProfileRequest{ProfileId: "00000000-0000-4000-8000-000000000000"})

	log := w.log.String()
	for _, secret := range []string{p.uid, p.token, p.accountID, p.profileID, "token-revoked-one"} {
		if strings.Contains(log, secret) {
			t.Errorf("the log holds %q:\n%s", secret, log)
		}
	}
	var lines []map[string]any
	for line := range strings.Lines(log) {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("a line is not JSON: %q", line)
		}
		if _, lost := decoded["dropped_fields"]; lost {
			t.Errorf("a line lost fields to the guard: %v", decoded)
		}
		lines = append(lines, decoded)
	}
	want := []map[string]any{
		{"message": "request", "method": apiv1connect.AccountServiceUpdateProfileProcedure, "code": "ok"},
		{"message": "request", "method": apiv1connect.AccountServiceGetAccountProcedure, "code": "unauthenticated", "token_class": "revoked"},
		{"message": "request", "method": apiv1connect.AccountServiceUpdateProfileProcedure, "code": "not_found", "cause": "CAUSE_NO_PROFILE"},
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %v, want %d", lines, len(want))
	}
	for i, fields := range want {
		for name, value := range fields {
			if lines[i][name] != value {
				t.Errorf("line %d: %s = %v, want %v", i, name, lines[i][name], value)
			}
		}
		if _, timed := lines[i]["duration_ms"]; !timed {
			t.Errorf("line %d has no duration", i)
		}
	}
}

// A learning request of E3 asks for the profile's consent: none confirmed is
// "no consent"; a confirmed one lets it on; a database that does not answer is
// "unavailable".
func TestALearningRequestNeedsItsConsent(t *testing.T) {
	w := newWorld(t, time.Second)
	p := w.person(t, "uid-consent")

	err := rpc.RequireConsent(t.Context(), w.pool, p.profileID)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)
	if _, err := w.pool.Exec(t.Context(), `INSERT INTO consents
		(account_id, profile_id, subject, state, age_category, country, confirmed_at)
		VALUES ($1, $2, 'adult', 'confirmed', 'adult', 'US', now())`, p.accountID, p.profileID); err != nil {
		t.Fatal(err)
	}
	if err := rpc.RequireConsent(t.Context(), w.pool, p.profileID); err != nil {
		t.Errorf("RequireConsent with a confirmed consent = %v, want nil", err)
	}
	down := &flaky{DB: w.pool, failOn: "FROM consents"}
	refusedAs(t, rpc.RequireConsent(t.Context(), down, p.profileID), connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
}

// panicking is a handler of AccountService that panics.
type panicking struct {
	apiv1connect.UnimplementedAccountServiceHandler
}

func (panicking) GetAccount(context.Context, *v1.GetAccountRequest) (*v1.GetAccountResponse, error) {
	panic("a handler's fault")
}

// A handler's panic is answered by the server's recovery, and its request still
// has its line, beside the line of the panic.
func TestAPanicOfAHandlerHasItsLine(t *testing.T) {
	rights, err := authz.Open(t.Context(), storetest.OpenFGAURL(t, true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rights.Close)
	pool := storetest.DB(t)
	signin := &authntest.Fake{}
	var buf syncBuffer
	log := logging.New(&buf, slog.LevelInfo)
	srv := rpc.NewServer(rpc.Deps{Signin: signin, DB: pool, Rights: rights, Log: log}, rpc.Handlers{Account: panicking{}})
	httpServer := httptest.NewServer(server.Routes(log, "", func(mux *http.ServeMux) { rpc.Mount(mux, srv) }))
	t.Cleanup(httpServer.Close)
	if _, err := pool.Exec(t.Context(), `INSERT INTO accounts (auth_uid) VALUES ('uid-panics')`); err != nil {
		t.Fatal(err)
	}
	signin.Accept("token-panics", authn.Token{UID: "uid-panics", AuthTime: time.Now()})

	client := apiv1connect.NewAccountServiceClient(connect.NewClient(connecthttp.NewTransport(httpServer.Client(), httpServer.URL)))
	if _, err := client.GetAccount(as(t, "token-panics"), &v1.GetAccountRequest{}); err == nil {
		t.Fatal("a panicking handler answered with no error")
	}
	log2 := buf.String()
	if !strings.Contains(log2, `"message":"request"`) || !strings.Contains(log2, `"code":"internal"`) ||
		!strings.Contains(log2, `"message":"panic"`) {
		t.Errorf("the log of a panic:\n%s", log2)
	}
}
