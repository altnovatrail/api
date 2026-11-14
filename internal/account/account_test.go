package account_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/account"
	"github.com/altnovatrail/api/internal/authn"
	"github.com/altnovatrail/api/internal/authn/authntest"
	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/export"
	"github.com/altnovatrail/api/internal/forms"
	"github.com/altnovatrail/api/internal/invite"
	"github.com/altnovatrail/api/internal/legal"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/model"
	"github.com/altnovatrail/api/internal/packages/packagestest"
	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/server"
	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

// world is the server of the contract with the AccountService, on a database of
// the test's own and a fake sign-in service.
type world struct {
	pool   *pgxpool.Pool
	rights *authz.Rights
	signin *authntest.Fake
	client apiv1connect.AccountServiceClient
	clock  *clock
	log    *syncBuffer
	// forms is the folder the scans of the forms of the world lie in.
	forms string
	// ownerFails makes the writing of a tuple fail, and deleteFails its deletion.
	ownerFails, deleteFails atomic.Bool
	// denyView makes the handlers' can_view false, and viewFails fail: the checks
	// before them ask the rights as they are.
	denyView, viewFails atomic.Bool
}

// owners is the rights the handlers write the tuples to, which fail when the
// test says.
type owners struct {
	*authz.Rights
	w *world
}

var errOwner = errors.New("the test's store of rights does not write")

func (o owners) WriteOwner(ctx context.Context, accountID, profileID string) error {
	if o.w.ownerFails.Load() {
		return errOwner
	}
	return o.Rights.WriteOwner(ctx, accountID, profileID)
}

func (o owners) DeleteOwner(ctx context.Context, accountID, profileID string) error {
	if o.w.deleteFails.Load() {
		return errOwner
	}
	return o.Rights.DeleteOwner(ctx, accountID, profileID)
}

func (o owners) DeleteOwned(ctx context.Context, accountID string, profileIDs []string) error {
	if o.w.deleteFails.Load() {
		return errOwner
	}
	return o.Rights.DeleteOwned(ctx, accountID, profileIDs)
}

func (o owners) Can(ctx context.Context, accountID string, right authz.Right, profileID string) (bool, error) {
	switch {
	case right == authz.CanView && o.w.viewFails.Load():
		return false, errOwner
	case right == authz.CanView && o.w.denyView.Load():
		return false, nil
	}
	return o.Rights.Can(ctx, accountID, right, profileID)
}

func (w *world) owners(rights *authz.Rights) owners {
	w.rights = rights
	return owners{Rights: rights, w: w}
}

// clock is the time the handlers go by, which a test moves.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *clock) set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = at
}

// syncBuffer is the log, which the server writes from its goroutines.
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

// newWorld is the world; wrap, when given, is what the handlers see of its
// database, which the checks see as it is.
func newWorld(t *testing.T, wrap ...func(*pgxpool.Pool) rpc.DB) *world {
	t.Helper()
	return newWorldWith(t, []account.Option{account.WithExport(exporter(t))}, wrap...)
}

// newWorldWith is the world with options of its own, beside its clock and the
// folder of its forms when options is not nil.
func newWorldWith(t *testing.T, options []account.Option, wrap ...func(*pgxpool.Pool) rpc.DB) *world {
	t.Helper()

	rights, err := authz.Open(t.Context(), storetest.OpenFGAURL(t, true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rights.Close)
	w := &world{pool: storetest.DB(t), signin: &authntest.Fake{}, clock: &clock{now: time.Now()}, log: &syncBuffer{},
		forms: t.TempDir()}
	var db rpc.DB = w.pool
	if len(wrap) > 0 {
		db = wrap[0](w.pool)
	}
	log := logging.New(w.log, slog.LevelInfo)
	if options != nil {
		options = append([]account.Option{account.WithForms(forms.Dir(w.forms))}, options...)
	}
	options = append(options, account.WithClock(w.clock.Now))
	srv := rpc.NewServer(rpc.Deps{Signin: w.signin, DB: w.pool, Rights: rights, Log: log, RequestTimeout: 10 * time.Second},
		rpc.Handlers{Account: account.New(db, w.owners(rights), w.signin,
			options...)})
	httpServer := httptest.NewServer(server.Routes(log, "", func(mux *http.ServeMux) { rpc.Mount(mux, srv) }))
	t.Cleanup(httpServer.Close)
	transport := connect.NewClient(connecthttp.NewTransport(httpServer.Client(), httpServer.URL))
	w.client = apiv1connect.NewAccountServiceClient(transport)
	return w
}

// signIn is a user of the sign-in service, signed in now; the context's calls
// carry its token.
func (w *world) signIn(t *testing.T, uid string) context.Context {
	t.Helper()

	token := "token-" + uid
	w.signin.Accept(token, authn.Token{UID: uid, AuthTime: time.Now()})
	return as(t, uid)
}

// as is a context of its own whose calls carry the user's token: a call
// writes into its context, so calls at once each take one.
func as(t *testing.T, uid string) context.Context {
	ctx, info := connect.NewClientContext(t.Context())
	info.RequestHeader().Set("Authorization", "Bearer token-"+uid)
	return ctx
}

// code is a new invite code, kept as the operator's command keeps it.
func (w *world) code(t *testing.T) string {
	t.Helper()

	code, hash := invite.New()
	if err := store.CreateInvite(t.Context(), w.pool, hash[:], quota.ClosedSlice); err != nil {
		t.Fatal(err)
	}
	return code
}

// refusedAs fails the test unless err is code with cause, and returns its
// Refusal; CAUSE_UNSPECIFIED is an error with no Refusal.
func refusedAs(t *testing.T, err error, code connect.Code, cause v1.Cause) *v1.Refusal {
	t.Helper()

	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		t.Fatalf("error = %v, want %v %v", err, code, cause)
	}
	var refusal *v1.Refusal
	for _, detail := range connectErr.Details() {
		if msg, err := connectproto.UnmarshalErrorDetail(detail); err == nil {
			if r, ok := msg.(*v1.Refusal); ok {
				refusal = r
			}
		}
	}
	if got := refusal.GetCause(); connectErr.Code() != code || got != cause {
		t.Fatalf("refusal = %v %v, want %v %v", connectErr.Code(), got, code, cause)
	}
	return refusal
}

func create(ctx context.Context, w *world, code string) (*v1.CreateAccountResponse, error) {
	return w.client.CreateAccount(ctx, &v1.CreateAccountRequest{InviteCode: code, AdultStated: true,
		AcceptedTexts: legal.Spec().On(w.clock.Now()).Refs(legal.PrivacyPolicy, legal.TermsOfUse)})
}

func TestAnOwnerWithACodeCreatesTheAccountAndReadsIt(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.signIn(t, "uid-owner")
	_, err := w.client.GetAccount(ctx, &v1.GetAccountRequest{})
	refusedAs(t, err, connect.CodePermissionDenied, v1.Cause_CAUSE_NO_ACCESS)

	created, err := create(ctx, w, w.code(t))
	if err != nil {
		t.Fatal(err)
	}
	id := created.GetAccount().GetAccountId()
	if id == "" || created.GetAccount().GetPinSet() || len(created.GetAccount().GetProfiles()) != 0 {
		t.Errorf("CreateAccount = %v, want an account with no PIN and no profile", created)
	}
	got, err := w.client.GetAccount(ctx, &v1.GetAccountRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(got.GetAccount(), created.GetAccount()) {
		t.Errorf("GetAccount = %v, want the account created, %v", got, created)
	}
}

func TestARepeatOfTheCreationIsTheAccountThereAlready(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.signIn(t, "uid-owner")
	code := w.code(t)
	if _, err := create(ctx, w, code); err != nil {
		t.Fatal(err)
	}
	// The code it redeemed, one that is no code, and a statement left out: the
	// account comes first.
	_, err := create(ctx, w, code)
	refusedAs(t, err, connect.CodeAlreadyExists, v1.Cause_CAUSE_ALREADY_EXISTS)
	_, err = create(ctx, w, "not a code")
	refusedAs(t, err, connect.CodeAlreadyExists, v1.Cause_CAUSE_ALREADY_EXISTS)
	_, err = w.client.CreateAccount(ctx, &v1.CreateAccountRequest{InviteCode: code})
	refusedAs(t, err, connect.CodeAlreadyExists, v1.Cause_CAUSE_ALREADY_EXISTS)
}

func TestACodeOfAnotherOrRedeemedOrNoneIsNotAccepted(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	code := w.code(t)
	if _, err := create(w.signIn(t, "uid-first"), w, code); err != nil {
		t.Fatal(err)
	}
	ctx := w.signIn(t, "uid-second")
	unknown, _ := invite.New()
	for name, given := range map[string]string{
		"redeemed by another": code,
		"never made":          unknown,
		"of no shape":         "0123-4567",
		"empty":               "",
	} {
		_, err := create(ctx, w, given)
		if err == nil {
			t.Fatalf("CreateAccount by a code %s = nil, want CAUSE_CODE_NOT_ACCEPTED", name)
		}
		refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_CODE_NOT_ACCEPTED)
	}
	_, err := w.client.GetAccount(ctx, &v1.GetAccountRequest{})
	refusedAs(t, err, connect.CodePermissionDenied, v1.Cause_CAUSE_NO_ACCESS)
}

func TestNoAccountIsCreatedWithoutTheStatement(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.signIn(t, "uid-owner")
	code := w.code(t)
	_, err := w.client.CreateAccount(ctx, &v1.CreateAccountRequest{InviteCode: code})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	// The code is not spent by the refusal.
	if _, err := create(ctx, w, code); err != nil {
		t.Errorf("CreateAccount with the statement after = %v, want nil", err)
	}
}

func TestAMarkedAccountIsGoneToItsCreationToo(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.signIn(t, "uid-owner")
	created, err := create(ctx, w, w.code(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.pool.Exec(t.Context(), `UPDATE accounts SET deleting_at = now() WHERE id = $1`,
		created.GetAccount().GetAccountId()); err != nil {
		t.Fatal(err)
	}
	_, err = create(ctx, w, w.code(t))
	refusedAs(t, err, connect.CodeUnauthenticated, v1.Cause_CAUSE_ACCOUNT_GONE)
	_, err = w.client.GetAccount(ctx, &v1.GetAccountRequest{})
	refusedAs(t, err, connect.CodeUnauthenticated, v1.Cause_CAUSE_ACCOUNT_GONE)
}

func TestTheAccountGivesItsProfilesAndConsentsInTheContractsWords(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.signIn(t, "uid-owner")
	created, err := create(ctx, w, w.code(t))
	if err != nil {
		t.Fatal(err)
	}
	id := created.GetAccount().GetAccountId()
	var owner, child, consent string
	if err := w.pool.QueryRow(t.Context(), `INSERT INTO profiles (account_id, owner, pseudonym, age_category, country,
			voicing_enabled, default_mode, ask_before_showing)
		VALUES ($1, true, 'Owl', 'adult', 'CA', false, 'self', false) RETURNING id`, id).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := w.pool.QueryRow(t.Context(), `INSERT INTO profiles (account_id, owner, pseudonym, avatar, age_category, country,
			voicing_enabled, voicing_speed, default_mode, interface_language, grade, ask_before_showing)
		VALUES ($1, false, 'Fox', 'fox', 'under_13', 'US', true, 'slow', 'by_steps', 'en-US', 3, true) RETURNING id`, id).Scan(&child); err != nil {
		t.Fatal(err)
	}
	if err := w.pool.QueryRow(t.Context(), `INSERT INTO consents (account_id, subject, state, age_category, country)
		VALUES ($1, 'child', 'waiting', 'from_13_to_consent', 'US') RETURNING id`, id).Scan(&consent); err != nil {
		t.Fatal(err)
	}
	// The child is in a lesson run, on the attempt of its second segment.
	digest := "sha256:" + strings.Repeat("a", 64)
	var run, attempt string
	if err := w.pool.QueryRow(t.Context(), `WITH c AS (
			INSERT INTO engine_choices (profile_id, kind, module_id, lesson_id) VALUES ($1, 'lesson', 'example/m', 'l1') RETURNING id
		)
		INSERT INTO lesson_runs (profile_id, lesson_id, module_id, module_version, package_digest, choice_id)
		SELECT $1, 'example/m/lesson/l1', 'example/m', '0.1.0', $2, id FROM c RETURNING id`, child, digest).Scan(&run); err != nil {
		t.Fatal(err)
	}
	if err := w.pool.QueryRow(t.Context(), `INSERT INTO attempts (profile_id, package_digest, module_id, module_version,
			module_format_version, module_kit_version, task_id, seed, lesson_run_id, segment)
		VALUES ($1, $2, 'example/m', '0.1.0', '0.4', 'v0.3.0', 't1', 1, $3, 2) RETURNING id`, child, digest, run).Scan(&attempt); err != nil {
		t.Fatal(err)
	}

	if _, err := w.pool.Exec(t.Context(), `UPDATE accounts SET pin_hash = 'synthetic' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}

	got, err := w.client.GetAccount(ctx, &v1.GetAccountRequest{})
	if err != nil {
		t.Fatal(err)
	}
	off, on, language, grade := false, true, "en-US", int32(3)
	want := &v1.Account{
		AccountId: id,
		PinSet:    true,
		Profiles: []*v1.Profile{{
			ProfileId: owner, Owner: true, Pseudonym: "Owl", AgeCategory: "adult", Country: "CA",
			PresentationStyle: &v1.PresentationStyle{Voicing: &v1.Voicing{Enabled: &off, Speed: v1.VoicingSpeed_VOICING_SPEED_NORMAL}},
			DefaultMode:       v1.Mode_MODE_SELF,
		}, {
			ProfileId: child, Pseudonym: "Fox", Avatar: "fox", AgeCategory: "under_13", Country: "US",
			PresentationStyle: &v1.PresentationStyle{Voicing: &v1.Voicing{Enabled: &on, Speed: v1.VoicingSpeed_VOICING_SPEED_SLOW}},
			DefaultMode:       v1.Mode_MODE_BY_STEPS, InterfaceLanguage: &language, Grade: &grade, AskBeforeShowing: true,
			OpenAttempt:    &v1.OpenAttemptRef{AttemptId: attempt, Segment: &v1.SegmentPlace{LessonRunId: run, Segment: 2}},
			OpenLessonRuns: []*v1.LessonRunRef{{LessonRunId: run, Lesson: "example/m/lesson/l1", PackageDigest: digest}},
		}},
		ChildConsents: []*v1.ChildConsent{{
			ConsentId: consent, AgeCategory: "from_13_to_consent", Country: "US", State: v1.ConsentState_CONSENT_STATE_WAITING,
			Giver: v1.ConsentGiver_CONSENT_GIVER_PARENT, RequestCode: got.GetAccount().GetChildConsents()[0].GetRequestCode(),
			Form: v1.FormState_FORM_STATE_NOT_SENT,
		}},
	}
	if !proto.Equal(got.GetAccount(), want) {
		t.Errorf("GetAccount = %v\nwant %v", got.GetAccount(), want)
	}
}

func TestADatabaseThatFailsIsUnavailable(t *testing.T) {
	t.Parallel()

	broken, err := pgxpool.New(t.Context(), storetest.URL(t, true))
	if err != nil {
		t.Fatal(err)
	}
	broken.Close()
	w := newWorld(t, func(*pgxpool.Pool) rpc.DB { return broken })
	ctx := w.signIn(t, "uid-owner")
	_, err = create(ctx, w, w.code(t))
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
	// A request of no form looks for an account made beside it, and fails as well.
	_, err = create(ctx, w, "not a code")
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
	// An account for the checks to find, and a view that fails all the same.
	if _, err := w.pool.Exec(t.Context(), `INSERT INTO accounts (auth_uid) VALUES ('uid-owner')`); err != nil {
		t.Fatal(err)
	}
	_, err = w.client.GetAccount(ctx, &v1.GetAccountRequest{})
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
}

// racing is the handlers' database, which lets a request beside this one make
// the account between the checks and the handler.
type racing struct {
	rpc.DB
	once   sync.Once
	before func()
}

func (r *racing) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	r.once.Do(r.before)
	return r.DB.QueryRow(ctx, sql, args...)
}

// An account made since the checks is still told before the statement and the
// code, as a repeat is.
func TestAnAccountMadeBesideComesBeforeTheForm(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		marked  bool
		request *v1.CreateAccountRequest
		code    connect.Code
		cause   v1.Cause
	}{
		{"no statement", false, &v1.CreateAccountRequest{InviteCode: "0123-4567-89AB-CDEF"}, connect.CodeAlreadyExists, v1.Cause_CAUSE_ALREADY_EXISTS},
		{"no code", false, &v1.CreateAccountRequest{AdultStated: true}, connect.CodeAlreadyExists, v1.Cause_CAUSE_ALREADY_EXISTS},
		{"marked since", true, &v1.CreateAccountRequest{AdultStated: true}, connect.CodeUnauthenticated, v1.Cause_CAUSE_ACCOUNT_GONE},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld(t, func(pool *pgxpool.Pool) rpc.DB {
				return &racing{DB: pool, before: func() {
					if _, err := pool.Exec(context.Background(), `INSERT INTO accounts (auth_uid, deleting_at)
					VALUES ('uid-owner', CASE WHEN $1 THEN now() END)`, tc.marked); err != nil {
						t.Error(err)
					}
				}}
			})
			_, err := w.client.CreateAccount(w.signIn(t, "uid-owner"), tc.request)
			refusedAs(t, err, tc.code, tc.cause)
		})
	}
}

// exporter is the export of the tests, on a set of packages with none: what a
// package tells of an attempt, the tests of the package export check.
func exporter(t *testing.T) *export.Exporter {
	t.Helper()
	set := packagestest.Set(t, packagestest.Entry{ID: "example/old", Version: "0.1.0", Format: "0.3",
		Digest: formatv0.Digest("sha256:" + strings.Repeat("b", 64)), Release: "v0.1.0"})
	m, err := model.New(set, "v0.1.4")
	if err != nil {
		t.Fatal(err)
	}
	x, err := export.New(set, m)
	if err != nil {
		t.Fatal(err)
	}
	return x
}
