package learning_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/module-kit/template"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/account"
	"github.com/altnovatrail/api/internal/admission"
	"github.com/altnovatrail/api/internal/authn"
	"github.com/altnovatrail/api/internal/authn/authntest"
	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/invite"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/packages/packagestest"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/server"
	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
	"github.com/altnovatrail/api/internal/version"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

// module is the module of the test's own package, choices.
const module = "example/choices"

// unknown is a digest the index does not know.
const unknown = "sha256:00000000000000000000000000000000000000000000000000000000000000ff"

// The packages are built once: a build checks the module.
var (
	builtOnce        sync.Once
	current, earlier *modulepackage.Package
)

// world is the server of the contract with attempts on the module choices:
// 1.0.0, current, and 0.9.0, withdrawn.
type world struct {
	pool     *pgxpool.Pool
	rights   *authz.Rights
	signin   *authntest.Fake
	clock    *clock
	accounts apiv1connect.AccountServiceClient
	learning apiv1connect.LearningServiceClient
	attempts apiv1connect.AttemptServiceClient
	current  *modulepackage.Package
	earlier  *modulepackage.Package
	// log is what the server wrote to its log.
	log *lockedBuffer
	// denyManage takes the right can_manage away from every account, for the
	// action of an adult; the checks before a handler still see the rights.
	denyManage atomic.Bool
}

// adultRights is the rights the action of an adult is asked of: the world's,
// unless the test takes can_manage away.
type adultRights struct{ w *world }

func (r adultRights) Can(ctx context.Context, accountID string, right authz.Right, profileID string) (bool, error) {
	if right == authz.CanManage && r.w.denyManage.Load() {
		return false, nil
	}
	return r.w.rights.Can(ctx, accountID, right, profileID)
}

// clock is the service's clock: now, moved on by the test.
type clock struct {
	mu    sync.Mutex
	ahead time.Duration
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.ahead)
}

func (c *clock) move(by time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ahead += by
}

// newWorld is the world of a deployment that declares no facts, at the stage
// "full": no AI is admitted. wrap, when given, is what the attempts see of their
// database, which the checks see as it is.
func newWorld(t *testing.T, wrap ...func(*pgxpool.Pool) rpc.DB) *world {
	t.Helper()
	return newGatedWorld(t, gate(t, "", config.RungFull), wrap...)
}

// newGatedWorld is the world whose attempts ask g.
func newGatedWorld(t *testing.T, g *admission.Gate, wrap ...func(*pgxpool.Pool) rpc.DB) *world {
	t.Helper()
	return worldOf(t, g, nil, wrap...)
}

// worldOf is the world whose attempts ask g, with opts; its log is written at
// the level info to w.log.
func worldOf(t *testing.T, g *admission.Gate, opts []learning.Option, wrap ...func(*pgxpool.Pool) rpc.DB) *world {
	t.Helper()

	builtOnce.Do(func() {
		current, earlier = packagestest.Own(t, "choices"), packagestest.Own(t, "choices", "0.9.0")
	})
	set := packagestest.Set(t,
		packagestest.Entry{Pkg: current, Release: "v0.2.0"},
		packagestest.Entry{Pkg: earlier, Release: "v0.1.0", Withdrawn: "wrong_key"},
	)
	rights, err := authz.Open(t.Context(), storetest.OpenFGAURL(t, true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rights.Close)
	w := &world{pool: storetest.DB(t), rights: rights, signin: &authntest.Fake{}, clock: &clock{},
		current: current, earlier: earlier, log: &lockedBuffer{}}
	var db rpc.DB = w.pool
	if len(wrap) > 0 {
		db = wrap[0](w.pool)
	}
	// The record going lives as long as the server lets a request run, as serve sets it.
	s, err := learning.New(set, g, append([]learning.Option{learning.WithClock(w.clock.now),
		learning.WithRequestTimeout(10 * time.Second)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	log := logging.New(w.log, slog.LevelInfo)
	srv := rpc.NewServer(rpc.Deps{Signin: w.signin, DB: w.pool, Rights: rights, Log: log, RequestTimeout: 10 * time.Second},
		rpc.Handlers{Account: account.New(w.pool, rights, w.signin), Learning: s.Learning(db), Attempt: s.Attempts(db, adultRights{w})})
	httpServer := httptest.NewServer(server.Routes(log, "", func(mux *http.ServeMux) { rpc.Mount(mux, srv) }))
	t.Cleanup(httpServer.Close)
	transport := connect.NewClient(connecthttp.NewTransport(httpServer.Client(), httpServer.URL))
	w.accounts = apiv1connect.NewAccountServiceClient(transport)
	w.learning = apiv1connect.NewLearningServiceClient(transport)
	w.attempts = apiv1connect.NewAttemptServiceClient(transport)
	return w
}

// learner is a signed-in owner with an account, the owner's profile, an adult's,
// and a child's profile under 13, both with their consents confirmed and the
// module choices connected.
type learner struct {
	ctx                     context.Context
	accountID, adult, child string
}

func (w *world) learner(t *testing.T) learner {
	t.Helper()
	uid := uuid.NewString()
	w.signin.Accept("token-"+uid, authn.Token{UID: uid, AuthTime: time.Now()})
	ctx, info := connect.NewClientContext(t.Context())
	info.RequestHeader().Set("Authorization", "Bearer token-"+uid)
	code, hash := invite.New()
	if err := store.CreateInvite(t.Context(), w.pool, hash[:], invite.ClosedSlice); err != nil {
		t.Fatal(err)
	}
	created, err := w.accounts.CreateAccount(ctx, &v1.CreateAccountRequest{InviteCode: code, AdultStated: true})
	if err != nil {
		t.Fatal(err)
	}
	w.exec(t, `INSERT INTO accepted_countries (country) VALUES ('US') ON CONFLICT DO NOTHING`)
	pseudonym := "Owl"
	owner, err := w.accounts.CreateOwnerProfile(ctx, &v1.CreateOwnerProfileRequest{
		Country: "US", Settings: &v1.ProfileSettings{Pseudonym: &pseudonym},
	})
	if err != nil {
		t.Fatal(err)
	}
	l := learner{ctx: ctx, accountID: created.GetAccount().GetAccountId(), adult: owner.GetProfile().GetProfileId()}
	l.child = w.child(t, l, "under_13", true)
	w.exec(t, `INSERT INTO connected_modules (profile_id, module_id) VALUES ($1, $3), ($2, $3)`, l.adult, l.child, module)
	return l
}

// child is a child's profile of the account, of category, with its consent
// confirmed or with none. The confirmation of a child's consent is epic E4's:
// the test writes it.
func (w *world) child(t *testing.T, l learner, category string, confirmed bool) string {
	t.Helper()
	id := w.id(t, `INSERT INTO profiles
		(account_id, owner, pseudonym, age_category, country, voicing_enabled, default_mode, ask_before_showing)
		VALUES ($1, false, 'Fox', $2, 'US', true, 'by_steps', true) RETURNING id`, l.accountID, category)
	if confirmed {
		w.exec(t, `INSERT INTO consents (account_id, profile_id, subject, state, age_category, country, confirmed_at)
			VALUES ($1, $2, 'child', 'confirmed', $3, 'US', now())`, l.accountID, id, category)
	}
	if err := w.rights.WriteOwner(t.Context(), l.accountID, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (w *world) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := w.pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func (w *world) id(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var id string
	if err := w.pool.QueryRow(t.Context(), sql, args...).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// choice is an engine's choice of the task of the profile, as the engine's code
// will write it.
func (w *world) choice(t *testing.T, profile, task string) string {
	t.Helper()
	return w.id(t, `INSERT INTO engine_choices (profile_id, kind, module_id, task_id) VALUES ($1, 'task', $2, $3)
		RETURNING id`, profile, module, task)
}

// lessonChoice is a new engine's choice of the module's lesson for the profile,
// as the engine's code will write it.
func (w *world) lessonChoice(t *testing.T, profile, lesson string) string {
	t.Helper()
	return w.id(t, `INSERT INTO engine_choices (profile_id, kind, module_id, lesson_id) VALUES ($1, 'lesson', $2, $3)
		RETURNING id`, profile, module, lesson)
}

// begin begins an attempt of the task on the profile by a new choice.
func (w *world) begin(t *testing.T, l learner, profile, task string) *v1.AttemptStart {
	t.Helper()
	res, err := w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: profile, IdempotencyKey: uuid.NewString(),
		Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: w.choice(t, profile, task)}})
	if err != nil {
		t.Fatalf("BeginAttempt %s: %v", task, err)
	}
	return res.GetAttempt()
}

// attempt is an open attempt of the task on the package of digest, written as
// no beginning of this code writes one: of a task that AI checks, or on a
// package the image does not give.
func (w *world) attempt(t *testing.T, profile, digest, task string) string {
	t.Helper()
	w.exec(t, `UPDATE attempts SET close_reason = 'left', closed_at = now() WHERE profile_id = $1 AND closed_at IS NULL`, profile)
	return w.id(t, `INSERT INTO attempts (profile_id, package_digest, module_id, module_version, module_format_version,
			module_kit_version, task_id, seed, choice_id)
		VALUES ($1, $2, $3, '1.0.0', '0.4', 'v0.3.0', $4, 7, $5) RETURNING id`,
		profile, digest, module, task, w.choice(t, profile, task))
}

// closeOf is the close reason of the attempt, empty while it is open.
func (w *world) closeOf(t *testing.T, attempt string) string {
	t.Helper()
	var reason *string
	if err := w.pool.QueryRow(t.Context(), `SELECT close_reason FROM attempts WHERE id = $1`, attempt).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason == nil {
		return ""
	}
	return *reason
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
		t.Fatalf("refusal = %v %v (%v), want %v %v", connectErr.Code(), got, err, code, cause)
	}
	return refusal
}

// tap is an option tapped in the slot of the item, each from 1.
func tap(item, slot int32, value string) *v1.TappedChoice {
	return &v1.TappedChoice{AnswerSlot: &observationv1.AnswerSlotPlace{Item: item, Slot: slot}, Value: value}
}

// submit sends the options tapped on the attempt.
func (w *world) submit(l learner, profile, attempt string, taps ...*v1.TappedChoice) (*v1.SubmitWorkResponse, error) {
	return w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: profile, AttemptId: attempt,
		IdempotencyKey: uuid.NewString(), TappedChoices: taps})
}

// An attempt begins by an engine's choice on the current version of its module,
// with the open view of its task, and nothing closed of the task reaches the
// device: neither its key nor its solution, technique or leading question.
func TestAnAttemptBeginsByAChoiceWithTheOpenViewOfItsTask(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	start := w.begin(t, l, l.child, "pick")
	if start.GetPackageDigest() != string(w.current.Digest) || start.GetSegment() != nil ||
		start.GetTask().GetModuleFormatVersion() != "0.4" {
		t.Errorf("the attempt = %v, want one on the current version, outside a lesson", start)
	}
	var open map[string]any
	if err := json.Unmarshal(start.GetTask().GetJson(), &open); err != nil {
		t.Fatal(err)
	}
	if open["id"] != "pick" || open["statement"] == nil {
		t.Errorf("the open view = %s, want the task pick", start.GetTask().GetJson())
	}
	for _, closed := range []string{"check", "key", "solution", "techniques", "leading_question", "traps", "first-seen"} {
		if strings.Contains(string(start.GetTask().GetJson()), closed) {
			t.Errorf("the open view holds %q: %s", closed, start.GetTask().GetJson())
		}
	}
	// The attempt keeps its package and the versions its verdict repeats by.
	var digest, moduleVersion, format, kit, task string
	var instance *int
	if err := w.pool.QueryRow(t.Context(), `SELECT package_digest, module_version, module_format_version,
		module_kit_version, task_id, instance FROM attempts WHERE id = $1`, start.GetAttemptId()).
		Scan(&digest, &moduleVersion, &format, &kit, &task, &instance); err != nil {
		t.Fatal(err)
	}
	if digest != string(w.current.Digest) || moduleVersion != "1.0.0" || format != "0.4" || kit != version.ModuleKit() ||
		task != "pick" || instance != nil {
		t.Errorf("the row = %s %s %s %s %s %v", digest, moduleVersion, format, kit, task, instance)
	}
	// The list of profiles names the open attempt.
	account, err := w.accounts.GetAccount(l.ctx, &v1.GetAccountRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range account.GetAccount().GetProfiles() {
		want := ""
		if p.GetProfileId() == l.child {
			want = start.GetAttemptId()
		}
		if got := p.GetOpenAttempt().GetAttemptId(); got != want || p.GetOpenAttempt().GetSegment() != nil {
			t.Errorf("the open attempt of %s = %v, want %q", p.GetProfileId(), p.GetOpenAttempt(), want)
		}
	}
}

// A repeat under its key gets the same attempt; the key with another request is
// refused, and the choice begins one attempt.
func TestABeginningRepeatsByItsKey(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	choice := w.choice(t, l.child, "pick")
	req := &v1.BeginAttemptRequest{ProfileId: l.child, IdempotencyKey: "k-1",
		Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: choice}}
	first, err := w.learning.BeginAttempt(l.ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	again, err := w.learning.BeginAttempt(l.ctx, req)
	if err != nil || !proto.Equal(first, again) {
		t.Errorf("the repeat = %v, %v; want %v", again, err, first)
	}
	other := proto.Clone(req).(*v1.BeginAttemptRequest)
	other.Start = &v1.BeginAttemptRequest_ChoiceId{ChoiceId: w.choice(t, l.child, "pairs")}
	_, err = w.learning.BeginAttempt(l.ctx, other)
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_ALREADY_USED)
	req.IdempotencyKey = "k-2"
	_, err = w.learning.BeginAttempt(l.ctx, req)
	refusedAs(t, err, connect.CodeAlreadyExists, v1.Cause_CAUSE_ALREADY_EXISTS)
}

// A new attempt closes the open one "left", and the same task again gets a seed
// of its own: a template's instance is the one its seed picks.
func TestANewAttemptLeavesTheOpenOneAndATemplateGetsItsInstance(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	first := w.begin(t, l, l.child, "greater")
	res, err := w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: l.child, IdempotencyKey: uuid.NewString(),
		Start: &v1.BeginAttemptRequest_PreviousAttemptId{PreviousAttemptId: first.GetAttemptId()}})
	if err != nil {
		t.Fatal(err)
	}
	second := res.GetAttempt()
	if got := w.closeOf(t, first.GetAttemptId()); got != "left" {
		t.Errorf("the first attempt is closed %q, want left", got)
	}
	var previous string
	seeds := map[string]uint64{}
	for _, a := range []*v1.AttemptStart{first, second} {
		var seed int64
		var picked int
		if err := w.pool.QueryRow(t.Context(), `SELECT seed, instance, coalesce(previous_attempt_id::text, '')
			FROM attempts WHERE id = $1`, a.GetAttemptId()).Scan(&seed, &picked, &previous); err != nil {
			t.Fatal(err)
		}
		seeds[a.GetAttemptId()] = uint64(seed)
		want, err := template.AttemptInstance(uint64(seed), "greater", len(w.current.View.Instances["greater"]))
		if err != nil || picked != want {
			t.Errorf("the instance = %d, want %d by the seed (%v)", picked, want, err)
		}
		numbers := w.current.View.Instances["greater"][picked]
		if !strings.Contains(string(a.GetTask().GetJson()), `"`+strconv.FormatInt(numbers["a"], 10)+` or `+strconv.FormatInt(numbers["b"], 10)+`?"`) {
			t.Errorf("the open view = %s, want the numbers of instance %d", a.GetTask().GetJson(), picked)
		}
	}
	if previous != first.GetAttemptId() || seeds[first.GetAttemptId()] == seeds[second.GetAttemptId()] {
		t.Errorf("again: previous %s, seeds %v; want the first attempt and a seed of its own", previous, seeds)
	}
	if _, err := w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: l.child, AttemptId: first.GetAttemptId()}); err == nil {
		t.Error("the attempt left is still open")
	}
}

// Each refusal of a beginning, before anything is written.
func TestABeginningIsRefusedWhenItCannotBegin(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	teen := w.child(t, l, "from_13_to_consent", true)
	waiting := w.child(t, l, "under_13", false)
	w.exec(t, `INSERT INTO connected_modules (profile_id, module_id) VALUES ($1, $2)`, teen, module)
	lessonChoice := w.lessonChoice(t, l.child, "basics")
	run := w.id(t, `INSERT INTO lesson_runs (profile_id, lesson_id, module_id, module_version, package_digest, choice_id)
		VALUES ($1, 'example/choices/lesson/basics', $2, '1.0.0', $3, $4) RETURNING id`, l.child, module, string(w.current.Digest), lessonChoice)
	unconnected := w.id(t, `INSERT INTO engine_choices (profile_id, kind, module_id, task_id) VALUES ($1, 'task', 'example/other', 'pick')
		RETURNING id`, l.child)
	byChoice := func(profile, task string) *v1.BeginAttemptRequest {
		return &v1.BeginAttemptRequest{ProfileId: profile, Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: w.choice(t, profile, task)}}
	}
	withProvider := byChoice(l.child, "pick")
	withProvider.OwnKeyProvider = "anthropic"
	for name, tc := range map[string]struct {
		req   *v1.BeginAttemptRequest
		code  connect.Code
		cause v1.Cause
	}{
		"no start":                     {&v1.BeginAttemptRequest{ProfileId: l.child}, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED},
		"no consent":                   {&v1.BeginAttemptRequest{ProfileId: waiting}, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT},
		"a provider for a child":       {withProvider, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE},
		"a choice not there":           {&v1.BeginAttemptRequest{ProfileId: l.child, Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: uuid.NewString()}}, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND},
		"a choice that is no ID":       {&v1.BeginAttemptRequest{ProfileId: l.child, Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: "x"}}, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND},
		"another profile's choice":     {&v1.BeginAttemptRequest{ProfileId: l.adult, Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: w.choice(t, l.child, "pick")}}, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND},
		"a choice of a lesson":         {&v1.BeginAttemptRequest{ProfileId: l.child, Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: lessonChoice}}, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED},
		"a module not connected":       {&v1.BeginAttemptRequest{ProfileId: l.child, Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: unconnected}}, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND},
		"no version for the learner":   {byChoice(teen, "pick"), connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION},
		"no such task":                 {byChoice(l.child, "gone"), connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION},
		"a task for adults":            {byChoice(l.child, "grown-ups"), connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_SUITABLE},
		"a task AI reads":              {byChoice(l.child, "apples"), connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED},
		"a task with a rubric":         {byChoice(l.adult, "story"), connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED},
		"an earlier attempt not there": {&v1.BeginAttemptRequest{ProfileId: l.child, Start: &v1.BeginAttemptRequest_PreviousAttemptId{PreviousAttemptId: uuid.NewString()}}, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND},
		"an earlier attempt no ID":     {&v1.BeginAttemptRequest{ProfileId: l.child, Start: &v1.BeginAttemptRequest_PreviousAttemptId{PreviousAttemptId: "x"}}, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND},
		"a segment before the first":   {&v1.BeginAttemptRequest{ProfileId: l.child, Start: &v1.BeginAttemptRequest_Segment{Segment: &v1.SegmentPlace{LessonRunId: run}}}, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED},
		"a run that is no ID":          {&v1.BeginAttemptRequest{ProfileId: l.child, Start: &v1.BeginAttemptRequest_Segment{Segment: &v1.SegmentPlace{LessonRunId: "x", Segment: 1}}}, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND},
		"a run not there":              {&v1.BeginAttemptRequest{ProfileId: l.child, Start: &v1.BeginAttemptRequest_Segment{Segment: &v1.SegmentPlace{LessonRunId: uuid.NewString(), Segment: 1}}}, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND},
		"a segment that is no task":    {&v1.BeginAttemptRequest{ProfileId: l.child, Start: &v1.BeginAttemptRequest_Segment{Segment: &v1.SegmentPlace{LessonRunId: run, Segment: 1}}}, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED},
		"a segment past the lesson":    {&v1.BeginAttemptRequest{ProfileId: l.child, Start: &v1.BeginAttemptRequest_Segment{Segment: &v1.SegmentPlace{LessonRunId: run, Segment: 9}}}, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED},
	} {
		if tc.req.IdempotencyKey == "" {
			tc.req.IdempotencyKey = uuid.NewString()
		}
		_, err := w.learning.BeginAttempt(l.ctx, tc.req)
		t.Run(name, func(t *testing.T) { refusedAs(t, err, tc.code, tc.cause) })
	}
	_, err := w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: l.child,
		Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: w.choice(t, l.child, "pick")}})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	var attempts int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM attempts`).Scan(&attempts); err != nil || attempts != 0 {
		t.Errorf("the refusals began %d attempts (%v), want none", attempts, err)
	}
	// An adult's own key, by its provider, takes the same path.
	adult := byChoice(l.adult, "grown-ups")
	adult.IdempotencyKey, adult.OwnKeyProvider = uuid.NewString(), "anthropic"
	if _, err := w.learning.BeginAttempt(l.ctx, adult); err != nil {
		t.Errorf("an adult's beginning with a provider: %v", err)
	}
}

// A wrong option is "not_yet" and a wrong answer, with what was read; every item
// correct closes the attempt "correct", with the task solved.
func TestASubmissionGetsItsVerdictAndTheCorrectOneSolvesTheTask(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	a := w.begin(t, l, l.child, "pairs").GetAttemptId()
	wrong, err := w.submit(l, l.child, a, tap(1, 1, "21"), tap(2, 1, "13"))
	if err != nil {
		t.Fatal(err)
	}
	if got := verdicts(wrong); got != "correct[correct 21] not_yet[not_yet 13]" || wrong.GetWrongAnswers() != 1 ||
		wrong.GetSolved() != nil || wrong.GetOutcome() != nil {
		t.Errorf("the wrong answer = %s, %v", got, wrong)
	}
	blank, err := w.submit(l, l.child, a, tap(1, 1, "21"))
	if err != nil {
		t.Fatal(err)
	}
	if got := verdicts(blank); got != "correct[correct 21] unreadable[unreadable blank]" || blank.GetWrongAnswers() != 1 {
		t.Errorf("a submission with a slot not tapped = %s, %d; want it no wrong answer", got, blank.GetWrongAnswers())
	}
	open, err := w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: l.child, AttemptId: a})
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(&v1.SubmitWorkResponse{Items: open.GetAttempt().GetItems()}, &v1.SubmitWorkResponse{Items: blank.GetItems()}) ||
		open.GetAttempt().GetWrongAnswers() != 1 {
		t.Errorf("the open attempt = %v, want the verdicts of the last submission", open.GetAttempt())
	}
	right, err := w.submit(l, l.child, a, tap(1, 1, "21"), tap(2, 1, "30"))
	if err != nil {
		t.Fatal(err)
	}
	solved := right.GetSolved()
	if got := verdicts(right); got != "correct[correct 21] correct[correct 30]" || right.GetWrongAnswers() != 1 || solved == nil {
		t.Fatalf("the right answer = %s, %v", got, right)
	}
	if len(solved.GetKeys()) != 2 || solved.GetKeys()[0].GetKey() != "21" || solved.GetKeys()[1].GetAnswerSlot().GetItem() != 2 ||
		solved.GetKeys()[1].GetKey() != "30" || !strings.Contains(string(solved.GetSolution()), "two tens") ||
		solved.GetModuleFormatVersion() != "0.4" {
		t.Errorf("the task solved = %v", solved)
	}
	if got := w.closeOf(t, a); got != "correct" {
		t.Errorf("the attempt is closed %q, want correct", got)
	}
	_, err = w.submit(l, l.child, a, tap(1, 1, "21"), tap(2, 1, "30"))
	refusal := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED)
	if refusal.GetCloseReason() != v1.CloseReason_CLOSE_REASON_CORRECT {
		t.Errorf("the refusal = %v, want the close reason correct", refusal)
	}
	var submissions int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM submissions WHERE attempt_id = $1`, a).Scan(&submissions); err != nil || submissions != 3 {
		t.Errorf("the attempt has %d submissions (%v), want 3", submissions, err)
	}
}

// verdicts is the verdicts of an answer, item by item, each with its answer
// slots' verdicts and reasons or values.
func verdicts(res *v1.SubmitWorkResponse) string {
	var items []string
	for _, item := range res.GetItems() {
		var slots []string
		for _, s := range item.GetAnswerSlots() {
			slots = append(slots, s.GetVerdict()+" "+s.GetReason()+s.GetValue())
		}
		items = append(items, item.GetVerdict()+"["+strings.Join(slots, ", ")+"]")
	}
	return strings.Join(items, " ")
}

// A repeat under its key gets the verdict recorded, and records nothing again;
// the key's record names the submission and holds no answer.
func TestASubmissionRepeatsByItsKey(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	a := w.begin(t, l, l.child, "pick").GetAttemptId()
	req := &v1.SubmitWorkRequest{ProfileId: l.child, AttemptId: a, IdempotencyKey: "s-1", TappedChoices: []*v1.TappedChoice{tap(1, 1, "five")}}
	first, err := w.attempts.SubmitWork(l.ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	again, err := w.attempts.SubmitWork(l.ctx, req)
	if err != nil || !proto.Equal(first, again) || again.GetWrongAnswers() != 1 {
		t.Errorf("the repeat = %v, %v; want %v", again, err, first)
	}
	var wrongAnswers, submissions int
	var response []byte
	if err := w.pool.QueryRow(t.Context(), `SELECT a.wrong_answers, (SELECT count(*) FROM submissions s WHERE s.attempt_id = a.id),
		(SELECT response FROM idempotency_keys WHERE key = 's-1') FROM attempts a WHERE a.id = $1`, a).
		Scan(&wrongAnswers, &submissions, &response); err != nil {
		t.Fatal(err)
	}
	if wrongAnswers != 1 || submissions != 1 || response != nil {
		t.Errorf("after the repeat: %d wrong answers, %d submissions, a response %x; want one, one, none", wrongAnswers, submissions, response)
	}
	other := proto.Clone(req).(*v1.SubmitWorkRequest)
	other.TappedChoices = []*v1.TappedChoice{tap(1, 1, "three and four")}
	_, err = w.attempts.SubmitWork(l.ctx, other)
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_ALREADY_USED)
}

// Each refusal of a submission whose form the task does not take, and of a key
// that is not the profile's to send.
func TestASubmissionThatTheTaskDoesNotTakeIsRefused(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	pick := w.begin(t, l, l.child, "pick").GetAttemptId()
	second := w.child(t, l, "under_13", true)
	work := sheet(crop(t))
	secondApples := w.attempt(t, second, string(w.current.Digest), "apples")
	for name, tc := range map[string]struct {
		req   *v1.SubmitWorkRequest
		code  connect.Code
		cause v1.Cause
	}{
		"no key":                        {&v1.SubmitWorkRequest{ProfileId: l.child, AttemptId: pick}, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED},
		"a tap of no slot":              {&v1.SubmitWorkRequest{ProfileId: l.child, AttemptId: pick, TappedChoices: []*v1.TappedChoice{tap(1, 2, "five")}}, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED},
		"a slot tapped twice":           {&v1.SubmitWorkRequest{ProfileId: l.child, AttemptId: pick, TappedChoices: []*v1.TappedChoice{tap(1, 1, "five"), tap(1, 1, "five")}}, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED},
		"work on a tap's task":          {&v1.SubmitWorkRequest{ProfileId: l.child, AttemptId: pick, Work: work}, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED},
		"a child's own key":             {&v1.SubmitWorkRequest{ProfileId: l.child, AttemptId: pick, OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "k"}}, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE},
		"an attempt not there":          {&v1.SubmitWorkRequest{ProfileId: l.child, AttemptId: uuid.NewString()}, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND},
		"another's attempt":             {&v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: pick}, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND},
		"an ID that is none":            {&v1.SubmitWorkRequest{ProfileId: l.child, AttemptId: "x"}, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND},
		"an essay":                      {&v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: w.attempt(t, l.adult, string(w.current.Digest), "story")}, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED},
		"no work where AI reads":        {&v1.SubmitWorkRequest{ProfileId: second, AttemptId: secondApples}, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED},
		"a crop that is no PNG":         {&v1.SubmitWorkRequest{ProfileId: second, AttemptId: secondApples, Work: sheet([]byte{1})}, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED},
		"a crop wider than tutor reads": {&v1.SubmitWorkRequest{ProfileId: second, AttemptId: secondApples, Work: sheet(pngOf(t, 4097, 1))}, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED},
		"a crop cut short":              {&v1.SubmitWorkRequest{ProfileId: second, AttemptId: secondApples, Work: sheet(crop(t)[:60])}, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED},
		"a workbook": {&v1.SubmitWorkRequest{ProfileId: second, AttemptId: secondApples,
			Work: &v1.Work{Surface: &v1.Work_Workbook{Workbook: &v1.WorkbookWork{}}}}, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED},
	} {
		if name != "no key" {
			tc.req.IdempotencyKey = uuid.NewString()
		}
		_, err := w.attempts.SubmitWork(l.ctx, tc.req)
		t.Run(name, func(t *testing.T) { refusedAs(t, err, tc.code, tc.cause) })
	}
	// Work where AI reads, with no key of the platform: tutor admits no model, and
	// the slot is unchecked, not admitted.
	apples := w.attempt(t, l.adult, string(w.current.Digest), "apples")
	res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: apples,
		IdempotencyKey: uuid.NewString(), Work: work})
	if err != nil {
		t.Fatal(err)
	}
	if got := verdicts(res); got != "unchecked[unchecked ]" || res.GetOutcome().GetCause() != v1.Cause_CAUSE_NOT_ADMITTED ||
		res.GetWrongAnswers() != 0 {
		t.Errorf("work where AI reads = %s, %v; want unchecked, not admitted", got, res)
	}
}

// The check of a request closes an attempt on a package the image no longer
// gives "unchecked", and one with no action for 7 days "abandoned", and the
// close stays; a package the index does not know waits, with nothing written.
func TestTheCheckOfARequestClosesWhatCannotGoOn(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	withdrawn := w.attempt(t, l.child, string(w.earlier.Digest), "pick")
	_, err := w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: l.child, AttemptId: withdrawn})
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED); r.GetCloseReason() != v1.CloseReason_CLOSE_REASON_UNCHECKED {
		t.Errorf("the refusal = %v, want unchecked", r)
	}
	if got := w.closeOf(t, withdrawn); got != "unchecked" {
		t.Errorf("the attempt is closed %q, want unchecked", got)
	}
	// A submission under its key closes it too, and records no key.
	withdrawn = w.attempt(t, l.child, string(w.earlier.Digest), "pick")
	_, err = w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.child, AttemptId: withdrawn, IdempotencyKey: "u-1",
		TappedChoices: []*v1.TappedChoice{tap(1, 1, "five")}})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED)
	var keys int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM idempotency_keys WHERE key = 'u-1'`).Scan(&keys); err != nil ||
		keys != 0 || w.closeOf(t, withdrawn) != "unchecked" {
		t.Errorf("the submission recorded %d keys (%v) and closed %q; want none, unchecked", keys, err, w.closeOf(t, withdrawn))
	}
	notKnown := w.attempt(t, l.child, unknown, "pick")
	_, err = w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: l.child, AttemptId: notKnown})
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_PACKAGE_UNKNOWN)
	// A submission too: what its task is, the image cannot tell.
	_, err = w.submit(l, l.child, notKnown, tap(1, 1, "five"))
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_PACKAGE_UNKNOWN)
	if got := w.closeOf(t, notKnown); got != "" {
		t.Errorf("the attempt on a package not known is closed %q, want open", got)
	}
	a := w.begin(t, l, l.child, "pick").GetAttemptId()
	w.clock.move(store.AbandonedAfter - time.Minute)
	if _, err := w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: l.child, AttemptId: a}); err != nil {
		t.Errorf("an attempt just short of its term: %v", err)
	}
	w.clock.move(time.Minute)
	_, err = w.attempts.BeginStep(l.ctx, &v1.BeginStepRequest{ProfileId: l.child, AttemptId: a, Step: 2})
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED); r.GetCloseReason() != v1.CloseReason_CLOSE_REASON_ABANDONED {
		t.Errorf("the refusal = %v, want abandoned", r)
	}
	if got := w.closeOf(t, a); got != "abandoned" {
		t.Errorf("the attempt is closed %q, want abandoned", got)
	}
}

// A step whose gives gives techniques gives the task's, and the attempt records
// it once; the open attempt then holds them, and reading it is no action.
func TestAStepGivesTheTechniquesOfTheTask(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	a := w.begin(t, l, l.child, "pick").GetAttemptId()
	for _, step := range []int32{0, 1, 3, 4} {
		_, err := w.attempts.BeginStep(l.ctx, &v1.BeginStepRequest{ProfileId: l.child, AttemptId: a, Step: step})
		refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	}
	var firstAt time.Time
	for range 2 {
		res, err := w.attempts.BeginStep(l.ctx, &v1.BeginStepRequest{ProfileId: l.child, AttemptId: a, Step: 2})
		if err != nil || strings.Join(res.GetTechniques().GetTechniqueIds(), ",") != "count-on" {
			t.Fatalf("BeginStep = %v, %v; want count-on", res, err)
		}
		var at time.Time
		if err := w.pool.QueryRow(t.Context(), `SELECT step_techniques_at FROM attempts WHERE id = $1`, a).Scan(&at); err != nil {
			t.Fatal(err)
		}
		if !firstAt.IsZero() && !at.Equal(firstAt) {
			t.Errorf("the techniques were given at %v, then %v; want the first time kept", firstAt, at)
		}
		firstAt = at
	}
	var lastAction time.Time
	if err := w.pool.QueryRow(t.Context(), `SELECT last_action_at FROM attempts WHERE id = $1`, a).Scan(&lastAction); err != nil {
		t.Fatal(err)
	}
	open, err := w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: l.child, AttemptId: a})
	if err != nil {
		t.Fatal(err)
	}
	got := open.GetAttempt()
	if strings.Join(got.GetStepTechniqueIds(), ",") != "count-on" || got.GetStart().GetAttemptId() != a || len(got.GetItems()) != 0 ||
		got.GetWrongAnswers() != 0 || got.GetRequestedLeadingQuestion() != "" {
		t.Errorf("the open attempt = %v", got)
	}
	var after time.Time
	if err := w.pool.QueryRow(t.Context(), `SELECT last_action_at FROM attempts WHERE id = $1`, a).Scan(&after); err != nil || !after.Equal(lastAction) {
		t.Errorf("reading the attempt moved its last action from %v to %v (%v)", lastAction, after, err)
	}
	// A task with no techniques gives none.
	pairs := w.begin(t, l, l.child, "greater").GetAttemptId()
	res, err := w.attempts.BeginStep(l.ctx, &v1.BeginStepRequest{ProfileId: l.child, AttemptId: pairs, Step: 2})
	if err != nil || len(res.GetTechniques().GetTechniqueIds()) != 0 {
		t.Errorf("BeginStep of a task with no techniques = %v, %v", res, err)
	}
}

// "Done" is for a task with a rubric, and one has no submission yet.
func TestDoneIsOfATaskWithARubric(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	a := w.begin(t, l, l.child, "pick").GetAttemptId()
	_, err := w.attempts.Done(l.ctx, &v1.DoneRequest{ProfileId: l.child, AttemptId: a})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	story := w.attempt(t, l.adult, string(w.current.Digest), "story")
	_, err = w.attempts.Done(l.ctx, &v1.DoneRequest{ProfileId: l.adult, AttemptId: story})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_SUBMISSION)
	w.begin(t, l, l.adult, "grown-ups")
	_, err = w.attempts.Done(l.ctx, &v1.DoneRequest{ProfileId: l.adult, AttemptId: story})
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED); r.GetCloseReason() != v1.CloseReason_CLOSE_REASON_LEFT {
		t.Errorf("the refusal = %v, want left", r)
	}
}

// Each method of the profile with no consent confirmed is "no consent".
func TestAProfileWithNoConsentIsRefused(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	waiting := w.child(t, l, "under_13", false)
	id := uuid.NewString()
	for name, call := range map[string]func() error{
		"GetOpenAttempt": func() error {
			_, err := w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: waiting, AttemptId: id})
			return err
		},
		"SubmitWork": func() error {
			_, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: waiting, AttemptId: id, IdempotencyKey: id})
			return err
		},
		"BeginStep": func() error {
			_, err := w.attempts.BeginStep(l.ctx, &v1.BeginStepRequest{ProfileId: waiting, AttemptId: id, Step: 2})
			return err
		},
		"Done": func() error {
			_, err := w.attempts.Done(l.ctx, &v1.DoneRequest{ProfileId: waiting, AttemptId: id})
			return err
		},
		"GetHint": func() error {
			_, err := w.attempts.GetHint(l.ctx, &v1.GetHintRequest{ProfileId: waiting, AttemptId: id})
			return err
		},
		"GetRungWords": func() error {
			_, err := w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: waiting, AttemptId: id, IdempotencyKey: id})
			return err
		},
		"ExplainDifferently": func() error {
			_, err := w.attempts.ExplainDifferently(l.ctx, &v1.ExplainDifferentlyRequest{ProfileId: waiting, AttemptId: id, IdempotencyKey: id})
			return err
		},
		"GetCorrectAnswerWords": func() error {
			_, err := w.attempts.GetCorrectAnswerWords(l.ctx, &v1.GetCorrectAnswerWordsRequest{ProfileId: waiting, AttemptId: id, IdempotencyKey: id})
			return err
		},
		"ShowSolution": func() error {
			_, err := w.attempts.ShowSolution(l.ctx, &v1.ShowSolutionRequest{ProfileId: waiting, AttemptId: id, IdempotencyKey: id})
			return err
		},
	} {
		err := call()
		t.Run(name, func(t *testing.T) { refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT) })
	}
}

// The methods of the epic's later tasks answer unimplemented.
func TestTheMethodsOfLaterTasksAreUnimplemented(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	_, err := w.learning.ChooseNext(l.ctx, &v1.ChooseNextRequest{ProfileId: l.child, IdempotencyKey: "c"})
	refusedAs(t, err, connect.CodeUnimplemented, v1.Cause_CAUSE_UNSPECIFIED)
	_, err = w.attempts.SubmitEssay(l.ctx, &v1.SubmitEssayRequest{ProfileId: l.child, AttemptId: uuid.NewString()})
	refusedAs(t, err, connect.CodeUnimplemented, v1.Cause_CAUSE_UNSPECIFIED)
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

func (f *failing) Begin(ctx context.Context) (pgx.Tx, error) {
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

func (f *failingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, f.failOn) {
		return failedRow{}
	}
	return f.Tx.QueryRow(ctx, sql, args...)
}

// Begin is a savepoint of the transaction, whose statements fail as its own.
func (f *failingTx) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := f.Tx.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &failingTx{Tx: tx, failOn: f.failOn}, nil
}

func (f *failingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, f.failOn) {
		return nil, errDown
	}
	return f.Tx.Query(ctx, sql, args...)
}

// A database that fails is unavailable, at each step of each request.
func TestADatabaseThatFailsIsUnavailable(t *testing.T) {
	t.Parallel()

	for _, failOn := range []string{
		"FROM profiles p", "FOR NO KEY UPDATE", "FROM idempotency_keys", "INSERT INTO idempotency_keys",
		"FROM engine_choices", "choice_id = $2", "FROM connected_modules", "INSERT INTO attempts",
		"FROM attempts\n", "SET close_reason", "INSERT INTO submissions", "SET wrong_answers", "SELECT answer",
		"SET step_techniques_at", "FROM lesson_runs", "closed_at IS NULL FOR UPDATE",
	} {
		t.Run(failOn, func(t *testing.T) {
			t.Parallel()

			w := newWorld(t, func(p *pgxpool.Pool) rpc.DB { return &failing{Pool: p, failOn: failOn} })
			l := w.learner(t)
			var errs []error
			res, err := w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: l.child, IdempotencyKey: uuid.NewString(),
				Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: w.choice(t, l.child, "pick")}})
			errs = append(errs, err)
			a := res.GetAttempt().GetAttemptId()
			if a == "" {
				a = w.attempt(t, l.child, string(w.current.Digest), "pick")
			}
			_, err = w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: l.child, IdempotencyKey: uuid.NewString(),
				Start: &v1.BeginAttemptRequest_Segment{Segment: &v1.SegmentPlace{LessonRunId: uuid.NewString(), Segment: 1}}})
			errs = append(errs, err)
			_, err = w.attempts.BeginStep(l.ctx, &v1.BeginStepRequest{ProfileId: l.child, AttemptId: a, Step: 2})
			errs = append(errs, err)
			_, err = w.submit(l, l.child, a, tap(1, 1, "five"))
			errs = append(errs, err)
			_, err = w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: l.child, AttemptId: a})
			errs = append(errs, err)
			_, err = w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: l.child, IdempotencyKey: uuid.NewString(),
				Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: w.choice(t, l.child, "pairs")}})
			errs = append(errs, err)
			unavailable := false
			for _, err := range errs {
				if connect.CodeOf(err) == connect.CodeUnavailable {
					unavailable = true
				}
			}
			if !unavailable {
				t.Errorf("no request was unavailable: %v", errs)
			}
		})
	}
}

// A repeat of a submission whose submission cannot be read is unavailable.
func TestARepeatThatCannotReadItsSubmissionIsUnavailable(t *testing.T) {
	t.Parallel()

	w := newWorld(t, func(p *pgxpool.Pool) rpc.DB {
		return &failing{Pool: p, failOn: "FROM submissions WHERE profile_id = $1 AND id"}
	})
	l := w.learner(t)
	a := w.begin(t, l, l.child, "pick").GetAttemptId()
	req := &v1.SubmitWorkRequest{ProfileId: l.child, AttemptId: a, IdempotencyKey: "r", TappedChoices: []*v1.TappedChoice{tap(1, 1, "five")}}
	if _, err := w.attempts.SubmitWork(l.ctx, req); err != nil {
		t.Fatal(err)
	}
	_, err := w.attempts.SubmitWork(l.ctx, req)
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
}

// A build that recorded no version of module-kit runs no attempts: each keeps it.
func TestABuildWithNoVersionOfModuleKitOrNoGateIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := learning.New(nil, gate(t, "", config.RungFull), learning.WithModuleKit("")); err == nil {
		t.Error("New with no version of module-kit: error = nil")
	}
	if _, err := learning.New(nil, nil); err == nil {
		t.Error("New with no gate: error = nil")
	}
}

// The version of module-kit an attempt keeps is the one go.mod pins: the binary
// that runs attempts links it.
func TestTheVersionOfModuleKitIsTheOneGoModPins(t *testing.T) {
	t.Parallel()

	goMod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	pinned := regexp.MustCompile(`(?m)^\s*github\.com/altnovatrail/module-kit (v\S+)$`).FindSubmatch(goMod)
	if pinned == nil {
		t.Fatal("go.mod pins no module-kit")
	}
	if got := version.ModuleKit(); got != string(pinned[1]) {
		t.Errorf("version.ModuleKit() = %q, want %s", got, pinned[1])
	}
}

// A new attempt closes the open one as the check of a request would have: one
// past its term "abandoned", one on a package the image no longer gives
// "unchecked", and the rest "left".
func TestANewAttemptClosesALapsedOneByItsLapse(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	withdrawn := w.attempt(t, l.child, string(w.earlier.Digest), "pick")
	idle := w.begin(t, l, l.child, "pick").GetAttemptId()
	if got := w.closeOf(t, withdrawn); got != "unchecked" {
		t.Errorf("the attempt on a withdrawn package is closed %q, want unchecked", got)
	}
	w.clock.move(store.AbandonedAfter + time.Minute)
	w.begin(t, l, l.child, "pairs")
	if got := w.closeOf(t, idle); got != "abandoned" {
		t.Errorf("the attempt past its term is closed %q, want abandoned", got)
	}
	// A choice of a lesson that names its module is still no choice of a task.
	lesson := w.lessonChoice(t, l.child, "basics")
	_, err := w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: l.child, IdempotencyKey: uuid.NewString(),
		Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: lesson}})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
}

// gate is the gate of a deployment that declares facts, YAML in a file of its
// own when not empty, at the stage rung, with the platform's key of Anthropic.
func gate(t *testing.T, facts, rung string) *admission.Gate {
	t.Helper()
	environ := []string{"API_ADMISSION_RUNG=" + rung, "API_ANTHROPIC_KEY=sk-test"}
	if facts != "" {
		path := filepath.Join(t.TempDir(), "facts.yaml")
		if err := os.WriteFile(path, []byte(facts), 0o600); err != nil {
			t.Fatal(err)
		}
		environ = append(environ, "API_ADMISSION_FACTS_FILE="+path)
	}
	cfg, err := config.AdmissionFrom(environ)
	if err != nil {
		t.Fatal(err)
	}
	g, _, err := admission.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// adultFacts are the facts that open the reading of handwriting by Anthropic's
// models on platform AI for an adult.
const adultFacts = `anthropic:
  data_processing_agreement: [adult]
  minimal_retention: [adult]
`

// A task of handwriting begins only where the gate admits its reading: by the
// facts of the deployment, its stage and the learner's age category, or by the
// own key of a provider whose models are admitted, and only in a form tutor
// reads: one answer slot bound anywhere on a handwriting sheet.
func TestATaskOfHandwritingAsksTheGate(t *testing.T) {
	t.Parallel()

	// A case is the profile, the task and the provider of an own key, and what the
	// beginning answers.
	type beginning struct {
		child    bool
		task     string
		provider string
		code     connect.Code
	}
	// admitted is no refusal: the attempt begins.
	admitted, refused := connect.Code(0), connect.CodeFailedPrecondition
	for name, tc := range map[string]struct {
		facts, rung string
		cases       map[string]beginning
	}{
		"no facts, the stage author": {"", config.RungAuthor, map[string]beginning{
			"platform AI":             {false, "apples", "", refused},
			"an own key of Anthropic": {false, "apples", "anthropic", admitted},
			"an own key of OpenAI":    {false, "apples", "openai", refused},
		}},
		"no facts, the stage full": {"", config.RungFull, map[string]beginning{
			"an own key of Anthropic": {false, "apples", "anthropic", refused},
		}},
		"facts for an adult, the stage author": {adultFacts, config.RungAuthor, map[string]beginning{
			"an adult":   {false, "apples", "", admitted},
			"a child":    {true, "apples", "", refused},
			"a rubric":   {false, "story", "", refused},
			"a zone":     {false, "parity", "", refused},
			"a workbook": {false, "table", "", refused},
		}},
		"facts for an adult, the stage full": {adultFacts, config.RungFull, map[string]beginning{
			"an adult": {false, "apples", "", refused},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := newGatedWorld(t, gate(t, tc.facts, tc.rung))
			l := w.learner(t)
			for name, b := range tc.cases {
				profile := l.adult
				if b.child {
					profile = l.child
				}
				_, err := w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: profile, IdempotencyKey: uuid.NewString(),
					OwnKeyProvider: b.provider, Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: w.choice(t, profile, b.task)}})
				t.Run(name, func(t *testing.T) {
					if b.code == admitted {
						if err != nil {
							t.Errorf("an admitted beginning: %v", err)
						}
						return
					}
					refusedAs(t, err, b.code, v1.Cause_CAUSE_NOT_ADMITTED)
				})
			}
		})
	}
}

// lockedBuffer is a log the server and the test may share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
