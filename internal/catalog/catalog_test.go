package catalog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	"github.com/altnovatrail/module-kit/modulepackage"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/account"
	"github.com/altnovatrail/api/internal/authn"
	"github.com/altnovatrail/api/internal/authn/authntest"
	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/catalog"
	"github.com/altnovatrail/api/internal/invite"
	"github.com/altnovatrail/api/internal/legal"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/packages/packagestest"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/server"
	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

// appOrigin is the origin of app the test's server answers CORS to.
const appOrigin = "https://app.example"

// outOfWindow is the digest of a version of a format api does not read.
const outOfWindow = "sha256:0000000000000000000000000000000000000000000000000000000000000003"

// unknown is a digest the index does not know.
const unknown = "sha256:00000000000000000000000000000000000000000000000000000000000000ff"

// world is the server of the contract with the catalog of a set of packages:
//   - example/smallest, for adults: 0.1.0, current, and 0.2.0, withdrawn;
//   - example/with-media-and-notice, for children under 13 and adults;
//   - example/with-solver, for children under 13, with a skill graph;
//   - example/older, of a format api does not read.
type world struct {
	pool                        *pgxpool.Pool
	rights                      *authz.Rights
	signin                      *authntest.Fake
	log                         *syncBuffer
	url                         string
	http                        *http.Client
	accounts                    apiv1connect.AccountServiceClient
	modules                     apiv1connect.ModuleServiceClient
	packages                    apiv1connect.PackageServiceClient
	older, newer, media, solver *modulepackage.Package
	dir                         string
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

// The packages are built once: a build checks the module, and the solver's runs.
var (
	builtOnce                   sync.Once
	older, newer, media, solver *modulepackage.Package
)

// newWorld is the world; wrap, when given, is what ModuleService sees of its
// database, which the checks see as it is.
func newWorld(t *testing.T, wrap ...func(*pgxpool.Pool) catalog.DB) *world {
	t.Helper()

	builtOnce.Do(func() {
		older, newer = packagestest.Built(t, "smallest"), packagestest.Built(t, "smallest", "0.2.0")
		media, solver = packagestest.Built(t, "with-media-and-notice"), packagestest.Built(t, "with-solver")
	})
	set := packagestest.Set(t,
		packagestest.Entry{Pkg: older, Release: "v0.1.0"},
		packagestest.Entry{Pkg: newer, Release: "v0.2.0", Withdrawn: "unsuitable_content"},
		packagestest.Entry{Pkg: media, Release: "v0.2.0"},
		packagestest.Entry{Pkg: solver, Release: "v0.2.0"},
		packagestest.Entry{ID: "example/older", Version: "0.1.0", Format: "0.3", Digest: outOfWindow, Release: "v0.1.0"},
	)
	cat, err := catalog.New(set)
	if err != nil {
		t.Fatal(err)
	}
	rights, err := authz.Open(t.Context(), storetest.OpenFGAURL(t, true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rights.Close)
	w := &world{pool: storetest.DB(t), rights: rights, signin: &authntest.Fake{}, log: &syncBuffer{},
		older: older, newer: newer, media: media, solver: solver, dir: set.Dir}
	var db catalog.DB = w.pool
	if len(wrap) > 0 {
		db = wrap[0](w.pool)
	}
	log := logging.New(w.log, slog.LevelInfo)
	srv := rpc.NewServer(rpc.Deps{Signin: w.signin, DB: w.pool, Rights: rights, Log: log, RequestTimeout: 10 * time.Second},
		rpc.Handlers{Account: account.New(w.pool, rights, w.signin), Module: cat.Modules(db), Package: cat.Packages()})
	httpServer := httptest.NewServer(server.Routes(log, appOrigin, func(mux *http.ServeMux) {
		rpc.Mount(mux, srv)
		mux.Handle(catalog.FilesPattern, cat.Files(log))
	}))
	t.Cleanup(httpServer.Close)
	w.url, w.http = httpServer.URL, httpServer.Client()
	transport := connect.NewClient(connecthttp.NewTransport(w.http, w.url, connecthttp.WithHTTPGet()))
	w.accounts = apiv1connect.NewAccountServiceClient(transport)
	w.modules = apiv1connect.NewModuleServiceClient(transport)
	w.packages = apiv1connect.NewPackageServiceClient(transport)
	return w
}

// as is a context of its own whose calls carry the user's token.
func as(t *testing.T, uid string) context.Context {
	ctx, info := connect.NewClientContext(t.Context())
	info.RequestHeader().Set("Authorization", "Bearer token-"+uid)
	return ctx
}

// learner is a signed-in owner with an account and the owner's profile, an
// adult's, and a child's profile under 13, both with their consents confirmed.
type learner struct {
	ctx                     context.Context
	accountID, adult, child string
}

func (w *world) learner(t *testing.T, uid string) learner {
	t.Helper()
	w.signin.Accept("token-"+uid, authn.Token{UID: uid, AuthTime: time.Now()})
	ctx := as(t, uid)
	code, hash := invite.New()
	if err := store.CreateInvite(t.Context(), w.pool, hash[:], invite.ClosedSlice); err != nil {
		t.Fatal(err)
	}
	created, err := w.accounts.CreateAccount(ctx, &v1.CreateAccountRequest{InviteCode: code, AdultStated: true,
		AcceptedTexts: legal.Spec().Refs(legal.PrivacyPolicy, legal.TermsOfUse)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.pool.Exec(t.Context(), `INSERT INTO accepted_countries (country) VALUES ('US') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	pseudonym := "Owl"
	owner, err := w.accounts.CreateOwnerProfile(ctx, &v1.CreateOwnerProfileRequest{
		Country: "US", Settings: &v1.ProfileSettings{Pseudonym: &pseudonym},
	})
	if err != nil {
		t.Fatal(err)
	}
	l := learner{ctx: ctx, accountID: created.GetAccount().GetAccountId(), adult: owner.GetProfile().GetProfileId()}
	// The confirmation of a child's consent is epic E4's: the test writes it.
	if err := w.pool.QueryRow(t.Context(), `INSERT INTO profiles
		(account_id, owner, pseudonym, age_category, country, voicing_enabled, default_mode, ask_before_showing)
		VALUES ($1, false, 'Fox', 'under_13', 'US', true, 'by_steps', true) RETURNING id`, l.accountID).Scan(&l.child); err != nil {
		t.Fatal(err)
	}
	if _, err := w.pool.Exec(t.Context(), `INSERT INTO consents
		(account_id, profile_id, subject, state, age_category, country, confirmed_at)
		VALUES ($1, $2, 'child', 'confirmed', 'under_13', 'US', now())`, l.accountID, l.child); err != nil {
		t.Fatal(err)
	}
	if err := w.rights.WriteOwner(t.Context(), l.accountID, l.child); err != nil {
		t.Fatal(err)
	}
	return l
}

// refusedAs fails the test unless err is code with cause, and returns its
// Refusal; CAUSE_UNSPECIFIED is an error with no Refusal.
func refusedAs(t *testing.T, err error, code connect.Code, cause v1.Cause) {
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
}

// get is an answer of the server to a GET with no header, as fetch sends one.
func (w *world) get(t *testing.T, path string, header ...string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, w.url+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", appOrigin)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	res, err := w.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var body bytes.Buffer
	if _, err := body.ReadFrom(res.Body); err != nil {
		t.Fatal(err)
	}
	return res, body.Bytes()
}

// The four open views go without a token, by the digest alone; a part the module
// does not have is an answer without its view.
func TestTheOpenViewsComeByDigestWithoutAToken(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := t.Context()
	manifest, err := w.packages.GetManifest(ctx, &v1.GetManifestRequest{PackageDigest: string(w.older.Digest)})
	if err != nil {
		t.Fatal(err)
	}
	var open map[string]any
	if err := json.Unmarshal(manifest.GetManifest().GetJson(), &open); err != nil {
		t.Fatal(err)
	}
	if open["id"] != "example/smallest" || open["version"] != "0.1.0" || manifest.GetManifest().GetModuleFormatVersion() != "0.4" {
		t.Errorf("the open view of the manifest = %s, %s", manifest.GetManifest().GetJson(), manifest.GetManifest().GetModuleFormatVersion())
	}

	graph, err := w.packages.GetSkillGraph(ctx, &v1.GetSkillGraphRequest{PackageDigest: string(w.solver.Digest)})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.GetSkillGraph().GetJson()) == 0 || len(graph.GetSkillTiers()) != len(w.solver.View.SkillTiers) || len(graph.GetSkillTiers()) == 0 {
		t.Errorf("GetSkillGraph = %v, want the graph and a tier of each of its %d skills", graph, len(w.solver.View.SkillTiers))
	}
	for skill, tier := range w.solver.View.SkillTiers {
		if graph.GetSkillTiers()[string(skill)] != int32(tier) {
			t.Errorf("the tier of %s = %d, want %d", skill, graph.GetSkillTiers()[string(skill)], tier)
		}
	}
	for _, closed := range []string{"traps", "trap_values", "check"} {
		if strings.Contains(string(graph.GetSkillGraph().GetJson()), closed) {
			t.Errorf("the open view of the graph holds %q", closed)
		}
	}

	none, err := w.packages.GetSkillGraph(ctx, &v1.GetSkillGraphRequest{PackageDigest: string(w.older.Digest)})
	if err != nil || none.GetSkillGraph() != nil || len(none.GetSkillTiers()) != 0 {
		t.Errorf("GetSkillGraph of a module with no graph = %v, %v", none, err)
	}
	techniques, err := w.packages.GetTechniques(ctx, &v1.GetTechniquesRequest{PackageDigest: string(w.older.Digest)})
	if err != nil || techniques.GetTechniques() != nil {
		t.Errorf("GetTechniques of a module with none = %v, %v", techniques, err)
	}
	steps, err := w.packages.GetSolvingSteps(ctx, &v1.GetSolvingStepsRequest{PackageDigest: string(w.older.Digest)})
	if err != nil || steps.GetSolvingSteps() != nil {
		t.Errorf("GetSolvingSteps of a module with none = %v, %v", steps, err)
	}
}

// A version the image no longer gives, a digest it does not know and one that
// is no digest are refused, by each of the four.
func TestTheOpenViewsAreRefusedForWhatTheImageDoesNotGive(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := t.Context()
	calls := map[string]func(string) error{
		"manifest": func(d string) error {
			_, err := w.packages.GetManifest(ctx, &v1.GetManifestRequest{PackageDigest: d})
			return err
		},
		"skill graph": func(d string) error {
			_, err := w.packages.GetSkillGraph(ctx, &v1.GetSkillGraphRequest{PackageDigest: d})
			return err
		},
		"techniques": func(d string) error {
			_, err := w.packages.GetTechniques(ctx, &v1.GetTechniquesRequest{PackageDigest: d})
			return err
		},
		"solving steps": func(d string) error {
			_, err := w.packages.GetSolvingSteps(ctx, &v1.GetSolvingStepsRequest{PackageDigest: d})
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			refusedAs(t, call(string(w.newer.Digest)), connect.CodeFailedPrecondition, v1.Cause_CAUSE_VERSION_NOT_GIVEN)
			refusedAs(t, call(outOfWindow), connect.CodeFailedPrecondition, v1.Cause_CAUSE_VERSION_NOT_GIVEN)
			refusedAs(t, call(unknown), connect.CodeUnavailable, v1.Cause_CAUSE_PACKAGE_UNKNOWN)
			refusedAs(t, call("sha256:"+strings.Repeat("A", 64)), connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
			refusedAs(t, call(""), connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
		})
	}
}

// connectGet is the address of a Connect GET of a method of PackageService with
// the message msg in JSON, as app sends one.
func connectGet(method, msg string) string {
	return "/novatrail.api.v1.PackageService/" + method + "?connect=v1&encoding=json&message=" + url.QueryEscape(msg)
}

// An open view is kept for a day by the HTTP cache, and a refusal by nobody.
func TestAnOpenViewIsCachedForADayAndARefusalIsNot(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	res, body := w.get(t, connectGet("GetManifest", `{"package_digest":"`+string(w.older.Digest)+`"}`))
	if res.StatusCode != http.StatusOK || res.Header.Get("Cache-Control") != "public, max-age=86400" {
		t.Errorf("GET of a manifest = %d, Cache-Control %q: %s", res.StatusCode, res.Header.Get("Cache-Control"), body)
	}
	if res.Header.Get("Access-Control-Allow-Origin") != appOrigin {
		t.Errorf("GET of a manifest answers CORS to %q", res.Header.Get("Access-Control-Allow-Origin"))
	}
	for _, digest := range []string{string(w.newer.Digest), unknown, "x"} {
		res, body := w.get(t, connectGet("GetManifest", `{"package_digest":"`+digest+`"}`))
		if res.StatusCode == http.StatusOK || res.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("GET of the manifest of %s = %d, Cache-Control %q: %s", digest, res.StatusCode, res.Header.Get("Cache-Control"), body)
		}
	}
}

// CheckPackages names each digest of the request, in its order, with whether the
// image gives it; a digest that is no digest refuses the request.
func TestCheckPackagesGivesEachDigestInItsOrder(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t, "uid-owner")
	digests := []string{unknown, string(w.media.Digest), string(w.newer.Digest), outOfWindow, string(w.media.Digest)}
	res, err := w.packages.CheckPackages(l.ctx, &v1.CheckPackagesRequest{PackageDigests: digests})
	if err != nil {
		t.Fatal(err)
	}
	want := []v1.PackageStatus{
		v1.PackageStatus_PACKAGE_STATUS_UNKNOWN, v1.PackageStatus_PACKAGE_STATUS_GIVEN,
		v1.PackageStatus_PACKAGE_STATUS_NOT_GIVEN, v1.PackageStatus_PACKAGE_STATUS_NOT_GIVEN,
		v1.PackageStatus_PACKAGE_STATUS_GIVEN,
	}
	if len(res.GetPackages()) != len(want) {
		t.Fatalf("CheckPackages = %v, want %d", res, len(want))
	}
	for i, check := range res.GetPackages() {
		if check.GetPackageDigest() != digests[i] || check.GetStatus() != want[i] {
			t.Errorf("check %d = %v, want %s %v", i, check, digests[i], want[i])
		}
	}
	if res, err := w.packages.CheckPackages(l.ctx, &v1.CheckPackagesRequest{}); err != nil || len(res.GetPackages()) != 0 {
		t.Errorf("CheckPackages of none = %v, %v", res, err)
	}
	_, err = w.packages.CheckPackages(l.ctx, &v1.CheckPackagesRequest{PackageDigests: []string{string(w.media.Digest), "sha256:x"}})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	_, err = w.packages.CheckPackages(t.Context(), &v1.CheckPackagesRequest{PackageDigests: digests})
	refusedAs(t, err, connect.CodeUnauthenticated, v1.Cause_CAUSE_UNSPECIFIED)
}

func catalogOf(t *testing.T, w *world, ctx context.Context, profileID string) map[string]string {
	t.Helper()
	res, err := w.modules.ListCatalog(ctx, &v1.ListCatalogRequest{ProfileId: profileID})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	var ids []string
	for _, m := range res.GetModules() {
		got[m.GetModuleId()] = m.GetPackageDigest()
		ids = append(ids, m.GetModuleId())
	}
	if !sorted(ids) {
		t.Errorf("the catalog is not in the order of its IDs: %v", ids)
	}
	return got
}

func sorted(ids []string) bool {
	for i := 1; i < len(ids); i++ {
		if ids[i-1] >= ids[i] {
			return false
		}
	}
	return true
}

// The catalog of a profile is the modules whose current version suits it by
// its audience, each with that version's digest.
func TestTheCatalogIsWhatSuitsTheProfile(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t, "uid-owner")
	adult := catalogOf(t, w, l.ctx, l.adult)
	if len(adult) != 2 || adult["example/smallest"] != string(w.older.Digest) || adult["example/with-media-and-notice"] != string(w.media.Digest) {
		t.Errorf("the catalog of an adult = %v, want the current smallest and the one with media", adult)
	}
	child := catalogOf(t, w, l.ctx, l.child)
	if len(child) != 2 || child["example/with-solver"] != string(w.solver.Digest) || child["example/with-media-and-notice"] != string(w.media.Digest) {
		t.Errorf("the catalog of a child = %v, want the one with media and the one with the solver", child)
	}
}

func connectedOf(modules []*v1.ConnectedModule) string {
	var parts []string
	for _, m := range modules {
		parts = append(parts, m.GetModuleId()+"="+m.GetPackageDigest())
	}
	return strings.Join(parts, " ")
}

// A module is connected once, in the order of the connections, with the digest
// of its current version, and disconnected without a trace.
func TestModulesAreConnectedAndDisconnected(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t, "uid-owner")
	connectTo := func(profileID, id string) (*v1.ConnectModuleResponse, error) {
		return w.modules.ConnectModule(l.ctx, &v1.ConnectModuleRequest{ProfileId: profileID, ModuleId: id})
	}
	if _, err := connectTo(l.adult, "example/with-media-and-notice"); err != nil {
		t.Fatal(err)
	}
	if _, err := connectTo(l.adult, "example/smallest"); err != nil {
		t.Fatal(err)
	}
	again, err := connectTo(l.adult, "example/with-media-and-notice")
	if err != nil {
		t.Fatal(err)
	}
	want := "example/with-media-and-notice=" + string(w.media.Digest) + " example/smallest=" + string(w.older.Digest)
	if got := connectedOf(again.GetModules()); got != want {
		t.Errorf("connected = %s, want %s", got, want)
	}
	if again.GetModules()[0].GetConnectedAt().AsTime().IsZero() {
		t.Error("a connected module has no time of its connection")
	}

	_, err = connectTo(l.adult, "example/none")
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	_, err = connectTo(l.adult, "example/with-solver")
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION)
	_, err = connectTo(l.adult, "example/older")
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION)

	// A module connected whose current version does not suit the learner, or that
	// has none, comes with no digest.
	for _, id := range []string{"example/with-solver", "example/older"} {
		if err := store.ConnectModule(t.Context(), w.pool, l.adult, id); err != nil {
			t.Fatal(err)
		}
	}
	listed, err := w.modules.ListConnectedModules(l.ctx, &v1.ListConnectedModulesRequest{ProfileId: l.adult})
	if err != nil {
		t.Fatal(err)
	}
	if got := connectedOf(listed.GetModules()); got != want+" example/older= example/with-solver=" && got != want+" example/with-solver= example/older=" {
		t.Errorf("connected = %s, want the two with no digest after %s", got, want)
	}

	// Its repeat is no refusal: a module connected already stays as it is,
	// though it has no current version for the learner now.
	repeat, err := connectTo(l.adult, "example/with-solver")
	if err != nil || len(repeat.GetModules()) != 4 {
		t.Errorf("ConnectModule of a module connected already = %v, %v; want the modules connected", repeat, err)
	}

	none, err := w.modules.DisconnectModule(l.ctx, &v1.DisconnectModuleRequest{ProfileId: l.adult, ModuleId: "example/none"})
	if err != nil || len(none.GetModules()) != 4 {
		t.Errorf("DisconnectModule of a module not connected = %v, %v", none, err)
	}
	gone, err := w.modules.DisconnectModule(l.ctx, &v1.DisconnectModuleRequest{ProfileId: l.adult, ModuleId: "example/with-media-and-notice"})
	if err != nil || len(gone.GetModules()) != 3 || gone.GetModules()[0].GetModuleId() != "example/smallest" {
		t.Errorf("DisconnectModule = %v, %v", gone, err)
	}

	child, err := connectTo(l.child, "example/with-solver")
	if err != nil || connectedOf(child.GetModules()) != "example/with-solver="+string(w.solver.Digest) {
		t.Errorf("the child's connection = %v, %v", child, err)
	}
}

// A profile whose consent is not confirmed gets none of the modules, and a
// profile that is not the account's is none.
func TestTheModulesWaitForTheConsent(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t, "uid-owner")
	if _, err := w.pool.Exec(t.Context(), `DELETE FROM consents WHERE profile_id = $1`, l.child); err != nil {
		t.Fatal(err)
	}
	_, err := w.modules.ListCatalog(l.ctx, &v1.ListCatalogRequest{ProfileId: l.child})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)
	_, err = w.modules.ConnectModule(l.ctx, &v1.ConnectModuleRequest{ProfileId: l.child, ModuleId: "example/with-solver"})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)
	_, err = w.modules.DisconnectModule(l.ctx, &v1.DisconnectModuleRequest{ProfileId: l.child, ModuleId: "example/with-solver"})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)
	_, err = w.modules.ListConnectedModules(l.ctx, &v1.ListConnectedModulesRequest{ProfileId: l.child})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)

	other := w.learner(t, "uid-other")
	_, err = w.modules.ListCatalog(other.ctx, &v1.ListCatalogRequest{ProfileId: l.adult})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
}

// failing is the pool, whose statements that hold failOn fail.
type failing struct {
	*pgxpool.Pool
	failOn string
}

var errDown = errors.New("the test's database is down")

type failedRow struct{}

func (failedRow) Scan(...any) error { return errDown }

func (f *failing) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, f.failOn) {
		return failedRow{}
	}
	return f.Pool.QueryRow(ctx, sql, args...)
}

func (f *failing) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, f.failOn) {
		return nil, errDown
	}
	return f.Pool.Query(ctx, sql, args...)
}

func (f *failing) Begin(ctx context.Context) (pgx.Tx, error) {
	if f.failOn == "BEGIN" {
		return nil, errDown
	}
	tx, err := f.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &failingTx{Tx: tx, failOn: f.failOn}, nil
}

// failingTx is a transaction of the pool, whose statements that hold failOn
// fail.
type failingTx struct {
	pgx.Tx
	failOn string
}

func (f *failingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, f.failOn) {
		return pgconn.CommandTag{}, errDown
	}
	return f.Tx.Exec(ctx, sql, args...)
}

func (f *failingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, f.failOn) {
		return nil, errDown
	}
	return f.Tx.Query(ctx, sql, args...)
}

// A database that fails is unavailable, at each step of a request.
func TestADatabaseThatFailsIsUnavailable(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		failOn string
		// fail is the requests that fail: connect, connect a module that cannot
		// be, disconnect and list.
		fail [4]bool
	}{
		{"consents", [4]bool{true, true, true, true}},
		{"BEGIN", [4]bool{true, false, true, false}},
		{"INSERT INTO connected_modules", [4]bool{true, false, false, false}},
		{"DELETE FROM connected_modules", [4]bool{false, false, true, false}},
		{"ORDER BY connected_at", [4]bool{true, true, true, true}},
	} {
		t.Run(tc.failOn, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, func(pool *pgxpool.Pool) catalog.DB { return &failing{Pool: pool, failOn: tc.failOn} })
			l := w.learner(t, "uid-owner")
			requests := [4]func() error{
				func() error {
					_, err := w.modules.ConnectModule(l.ctx, &v1.ConnectModuleRequest{ProfileId: l.adult, ModuleId: "example/smallest"})
					return err
				},
				func() error {
					_, err := w.modules.ConnectModule(l.ctx, &v1.ConnectModuleRequest{ProfileId: l.adult, ModuleId: "example/with-solver"})
					return err
				},
				func() error {
					_, err := w.modules.DisconnectModule(l.ctx, &v1.DisconnectModuleRequest{ProfileId: l.adult, ModuleId: "example/smallest"})
					return err
				},
				func() error {
					_, err := w.modules.ListConnectedModules(l.ctx, &v1.ListConnectedModulesRequest{ProfileId: l.adult})
					return err
				},
			}
			for i, request := range requests {
				err := request()
				if tc.fail[i] {
					refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
				} else if connect.CodeOf(err) == connect.CodeUnavailable {
					t.Errorf("request %d failed: %v", i, err)
				}
			}
		})
	}
}

// A request of an account marked "deleting" after the checks writes nothing.
func TestAnAccountMarkedMeanwhileIsGone(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t, "uid-owner")
	marked := &marking{Pool: w.pool, accountID: l.accountID}
	w2 := newWorldOn(t, w, marked)
	_, err := w2.ConnectModule(l.ctx, &v1.ConnectModuleRequest{ProfileId: l.adult, ModuleId: "example/smallest"})
	refusedAs(t, err, connect.CodeUnauthenticated, v1.Cause_CAUSE_ACCOUNT_GONE)
	if n, _ := store.ConnectedModules(t.Context(), w.pool, l.adult); len(n) != 0 {
		t.Errorf("an account marked has %d modules connected", len(n))
	}
}

// marking is the pool, which marks the account "deleting" as the request
// begins its write, as a deletion on another device would.
type marking struct {
	*pgxpool.Pool
	accountID string
}

func (m *marking) Begin(ctx context.Context) (pgx.Tx, error) {
	if _, err := m.Exec(ctx, `UPDATE accounts SET deleting_at = now() WHERE id = $1`, m.accountID); err != nil {
		return nil, err
	}
	return m.Pool.Begin(ctx)
}

// newWorldOn is a ModuleService of w's catalog on db, behind the same checks.
func newWorldOn(t *testing.T, w *world, db catalog.DB) apiv1connect.ModuleServiceClient {
	t.Helper()
	set := packagestest.Set(t, packagestest.Entry{Pkg: w.older, Release: "v0.1.0"})
	cat, err := catalog.New(set)
	if err != nil {
		t.Fatal(err)
	}
	log := logging.New(w.log, slog.LevelInfo)
	srv := rpc.NewServer(rpc.Deps{Signin: w.signin, DB: w.pool, Rights: w.rights, Log: log, RequestTimeout: 10 * time.Second},
		rpc.Handlers{Module: cat.Modules(db)})
	httpServer := httptest.NewServer(server.Routes(log, "", func(mux *http.ServeMux) { rpc.Mount(mux, srv) }))
	t.Cleanup(httpServer.Close)
	return apiv1connect.NewModuleServiceClient(connect.NewClient(connecthttp.NewTransport(httpServer.Client(), httpServer.URL)))
}
