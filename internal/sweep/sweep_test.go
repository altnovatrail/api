package sweep_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/authn"
	"github.com/altnovatrail/api/internal/authn/authntest"
	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/model"
	"github.com/altnovatrail/api/internal/packages/packagestest"
	"github.com/altnovatrail/api/internal/store/storetest"
	"github.com/altnovatrail/api/internal/sweep"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

var errDown = errors.New("the test's service is down")

// settle is the longest a request runs, in the tests as in a deployment.
const settle = 30 * time.Second

// The package of the module choices is built once: a build checks the module.
var (
	builtOnce sync.Once
	choices   *modulepackage.Package
)

// world is the job's databases and sign-in service. Every ID and UID in it is
// synthetic.
type world struct {
	pool    *pgxpool.Pool
	model   *model.Model
	fga     *pgx.Conn
	rights  *authz.Rights
	signin  *authntest.Fake
	log     bytes.Buffer
	ids     []string
	devices []string
}

func newWorld(t *testing.T) *world {
	t.Helper()

	url := storetest.OpenFGAURL(t, true)
	rights, err := authz.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rights.Close)
	fga, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fga.Close(context.Background()) })
	builtOnce.Do(func() { choices = packagestest.Own(t, "choices") })
	m, err := model.New(packagestest.Set(t, packagestest.Entry{Pkg: choices, Release: "v0.1.0"}), "v0.1.4")
	if err != nil {
		t.Fatal(err)
	}
	return &world{pool: storetest.DB(t), fga: fga, rights: rights, signin: &authntest.Fake{}, model: m}
}

// sweeper is the job on the world, with rights and signin as given.
func (w *world) sweeper(rights sweep.Rights, signin sweep.Signin) *sweep.Sweeper {
	return sweep.New(w.pool, w.model, rights, signin, settle, logging.New(&w.log, slog.LevelDebug))
}

func (w *world) run(t *testing.T) sweep.Report {
	t.Helper()
	return w.sweeper(w.rights, w.signin).Run(t.Context(), time.Now())
}

func (w *world) id(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var id string
	if err := w.pool.QueryRow(t.Context(), sql, args...).Scan(&id); err != nil {
		t.Fatal(err)
	}
	w.ids = append(w.ids, id)
	return id
}

// account makes an account of uid, last asked on on.
func (w *world) account(t *testing.T, uid string, on time.Time) string {
	t.Helper()
	w.devices = append(w.devices, uid)
	return w.id(t, `INSERT INTO accounts (auth_uid, last_request_on) VALUES ($1, $2) RETURNING id`, uid, on)
}

// profile makes a profile of the account — the owner's, or a child's — active
// last on on, with its tuple.
func (w *world) profile(t *testing.T, accountID string, on time.Time, owner bool) string {
	t.Helper()
	category := map[bool]string{true: "adult", false: "under_13"}[owner]
	id := w.id(t, `INSERT INTO profiles (account_id, owner, pseudonym, age_category, country, voicing_enabled,
		default_mode, ask_before_showing, active_on) VALUES ($1, $3, 'Owl', $4, 'US', false, 'self', NOT $3, $2)
		RETURNING id`, accountID, on, owner, category)
	if err := w.rights.WriteOwner(t.Context(), accountID, id); err != nil {
		t.Fatal(err)
	}
	return id
}

// tuple writes a tuple, as old as age.
func (w *world) tuple(t *testing.T, accountID, profileID string, age time.Duration) {
	t.Helper()
	if err := w.rights.WriteOwner(t.Context(), accountID, profileID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.fga.Exec(t.Context(), `UPDATE tuple SET inserted_at = now() - $2::interval
		WHERE object_id = $1`, profileID, fmt.Sprintf("%d seconds", int(age.Seconds()))); err != nil {
		t.Fatal(err)
	}
}

func (w *world) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM `+sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (w *world) owns(t *testing.T, accountID, profileID string) bool {
	t.Helper()
	can, err := w.rights.Can(t.Context(), accountID, authz.CanManage, profileID)
	if err != nil {
		t.Fatal(err)
	}
	return can
}

func (w *world) removed(uid string) bool {
	for _, gone := range w.signin.Removed() {
		if gone == uid {
			return true
		}
	}
	return false
}

// step is the report's step of name.
func step(t *testing.T, report sweep.Report, name string) sweep.Step {
	t.Helper()
	for _, s := range report {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("the report has no step %s", name)
	return sweep.Step{}
}

func profileID(n int) string { return fmt.Sprintf("0190f0a0-0000-7000-8000-%012d", n) }

// One pass finishes the marks and deletes all that is past its term, and
// nothing that is not; a second pass finds nothing to do.
func TestAPassDeletesWhatIsPastItsTermAndNothingElse(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	now := time.Now().UTC()
	today, longAgo := now, now.AddDate(-1, 0, -1)

	// A mark a request left, and one a request is still on.
	marked := w.account(t, "uid-marked", today)
	w.tuple(t, marked, profileID(1), time.Minute)
	if _, err := w.pool.Exec(t.Context(), `UPDATE accounts SET deleting_at = now() - interval '1 minute' WHERE id = $1`, marked); err != nil {
		t.Fatal(err)
	}
	going := w.account(t, "uid-going", today)
	if _, err := w.pool.Exec(t.Context(), `UPDATE accounts SET deleting_at = now() WHERE id = $1`, going); err != nil {
		t.Fatal(err)
	}
	// An account idle for more than a year, and one idle for exactly a year.
	idle := w.account(t, "uid-idle", longAgo)
	idleProfile := w.profile(t, idle, longAgo, true)
	onTheDay := w.account(t, "uid-on-the-day", now.AddDate(-1, 0, 0))
	// An active account with a profile idle and one active.
	active := w.account(t, "uid-active", today)
	oldProfile := w.profile(t, active, longAgo, false)
	newProfile := w.profile(t, active, today, true)
	// Tuples: dangling and old, dangling and young, and old of another account
	// on a profile that is there.
	w.tuple(t, active, profileID(2), time.Hour)
	w.tuple(t, active, profileID(3), time.Second)
	w.tuple(t, idle+"0", newProfile, time.Hour)
	w.ids = append(w.ids, profileID(1), profileID(2), profileID(3))
	// The users of the sign-in service.
	for uid, seen := range map[string]authn.User{
		"uid-long-gone":  {Created: now.AddDate(0, -3, 0), LastSignIn: now.AddDate(0, -2, 0)},
		"uid-just-made":  {Created: now.Add(-time.Hour)},
		"uid-refreshed":  {Created: now.AddDate(-1, 0, 0), LastRefresh: now.AddDate(0, 0, -29)},
		"uid-never-seen": {},
		"uid-active":     {Created: now.AddDate(-1, 0, 0)},
		"uid-going":      {Created: now.AddDate(-1, 0, 0)},
	} {
		seen.UID = uid
		w.signin.AddUser(seen)
		w.devices = append(w.devices, uid)
	}
	// Records of keys and the changelog, old and young.
	if _, err := w.pool.Exec(t.Context(), `INSERT INTO idempotency_keys (account_id, key, method, fingerprint, state, created_at)
		VALUES ($1, 'old', '/m', 'f', 'going', now() - interval '25 hours'), ($1, 'young', '/m', 'f', 'going', now())`, active); err != nil {
		t.Fatal(err)
	}
	if _, err := w.fga.Exec(t.Context(), `UPDATE changelog SET inserted_at = now() - interval '2 days'
		WHERE object_id = $1`, profileID(2)); err != nil {
		t.Fatal(err)
	}
	// Minimal records of consents for children: deleted a day more than 5 years
	// ago by the calendar, a day short of 5 years, and now. A day on either side
	// keeps the test off the time the pass itself is run at.
	for i, deleted := range []time.Time{now.AddDate(-5, 0, -1), now.AddDate(-5, 0, 1), now} {
		if _, err := w.pool.Exec(t.Context(), `INSERT INTO child_consent_records (id, consented_at, method, deleted_at)
			VALUES ($1, $2, 'signed_form', $2)`, profileID(10+i), deleted); err != nil {
			t.Fatal(err)
		}
		w.ids = append(w.ids, profileID(10+i))
	}

	// An attempt idle for 8 days, with a wrong answer, whose evidence the pass
	// writes once it closes it, and one of the profile idle for long, which goes
	// with it; lesson runs idle for 31 days and for a day.
	for _, profile := range []string{newProfile, oldProfile} {
		choice := w.id(t, `INSERT INTO engine_choices (profile_id, kind, module_id, task_id) VALUES ($1, 'task', 'example/choices', 'pick') RETURNING id`, profile)
		w.id(t, `INSERT INTO attempts (profile_id, package_digest, module_id, module_version, module_format_version,
			module_kit_version, task_id, seed, choice_id, wrong_answers, last_action_at)
			VALUES ($1, $3, 'example/choices', '1.0.0', '0.4', 'v0.3.0', 'pick', 1, $2, 1, now() - interval '8 days')
			RETURNING id`, profile, choice, choices.Digest)
	}
	for i, idleFor := range []string{"31 days", "1 day"} {
		choice := w.id(t, `INSERT INTO engine_choices (profile_id, kind, module_id, lesson_id) VALUES ($1, 'lesson', 'example/m', 'l1') RETURNING id`, newProfile)
		w.id(t, `INSERT INTO lesson_runs (profile_id, lesson_id, module_id, module_version, package_digest, choice_id, last_action_at)
			VALUES ($1, $2, 'example/m', '0.1.0', 'sha256:'||repeat('a', 64), $3, now() - $4::interval) RETURNING id`,
			newProfile, fmt.Sprintf("example/m/l%d", i), choice, idleFor)
	}

	report := w.run(t)
	if !report.Clean() {
		t.Fatalf("report = %+v, want a clean pass", report)
	}
	for name, want := range map[string]int64{
		logging.StepMarks: 1, logging.StepIdleAccounts: 1, logging.StepIdleProfiles: 1, logging.StepUsers: 2,
		logging.StepAbandonedAttempts: 1, logging.StepEvidence: 1, logging.StepAbandonedRuns: 1,
		logging.StepTuples: 2, logging.StepConsentRecords: 1, logging.StepKeys: 1, logging.StepChangelog: 1,
	} {
		if got := step(t, report, name).Done; got != want {
			t.Errorf("%s did %d, want %d", name, got, want)
		}
	}

	if w.count(t, "accounts WHERE id = $1", marked) != 0 || !w.removed("uid-marked") || w.owns(t, marked, profileID(1)) {
		t.Error("the mark was not finished")
	}
	if w.count(t, "accounts WHERE id = $1", going) != 1 || w.removed("uid-going") {
		t.Error("the mark a request is still on was taken")
	}
	if w.count(t, "accounts WHERE id = $1", idle) != 0 || w.count(t, "profiles WHERE id = $1", idleProfile) != 0 ||
		!w.removed("uid-idle") || w.owns(t, idle, idleProfile) {
		t.Error("the idle account was not deleted")
	}
	if w.count(t, "accounts WHERE id = $1 AND deleting_at IS NULL", onTheDay) != 1 {
		t.Error("the account idle for exactly a year was deleted")
	}
	if w.count(t, "profiles WHERE id = $1", oldProfile) != 0 || w.owns(t, active, oldProfile) {
		t.Error("the idle profile was not deleted")
	}
	if w.count(t, "profiles WHERE id = $1", newProfile) != 1 || !w.owns(t, active, newProfile) {
		t.Error("the active profile or its tuple went")
	}
	if w.count(t, "evidence WHERE profile_id = $1", newProfile) != 1 ||
		w.count(t, "skill_states WHERE profile_id = $1 AND last_seq = 1", newProfile) != 1 {
		t.Error("the attempt closed abandoned with a wrong answer has no evidence in the journal and the cache")
	}
	if !w.removed("uid-long-gone") || !w.removed("uid-never-seen") {
		t.Errorf("users removed = %v, want those with no account and long gone", w.signin.Removed())
	}
	for _, kept := range []string{"uid-just-made", "uid-refreshed", "uid-active"} {
		if w.removed(kept) {
			t.Errorf("%s was removed", kept)
		}
	}
	if w.owns(t, active, profileID(2)) || !w.owns(t, active, profileID(3)) || w.owns(t, idle+"0", newProfile) {
		t.Error("the dangling tuples did not go as they should: the old ones, not the young one")
	}
	if w.count(t, "idempotency_keys") != 1 {
		t.Error("the keys' records did not go as they should")
	}
	if w.count(t, "child_consent_records WHERE id = $1", profileID(10)) != 0 || w.count(t, "child_consent_records") != 2 {
		t.Error("the minimal records did not go as they should: the one past 5 years, not the others")
	}
	var old int
	if err := w.fga.QueryRow(t.Context(), `SELECT count(*) FROM changelog WHERE inserted_at < now() - interval '1 day'`).Scan(&old); err != nil || old != 0 {
		t.Errorf("old changelog = %d, %v; want none", old, err)
	}

	// The log says counts, and names nothing it did them to.
	log := w.log.String()
	if strings.Count(log, `"message":"swept"`) != len(logging.Steps) {
		t.Errorf("the log has not a line for each step: %s", log)
	}
	for _, named := range append(w.ids, w.devices...) {
		if strings.Contains(log, named) {
			t.Errorf("the log names %s: %s", named, log)
		}
	}

	again := w.run(t)
	for _, s := range again {
		if !s.Finished || s.Failed != 0 || s.Done != 0 && s.Name != logging.StepChangelog {
			t.Errorf("the second pass: %+v, want nothing done", s)
		}
	}
}

// stuck is the rights with what the test names failing.
type stuck struct {
	*authz.Rights
	tuples, deletes, changelog bool
}

func (s stuck) DeleteOwned(ctx context.Context, accountID string, profileIDs []string) error {
	if s.deletes {
		return errDown
	}
	return s.Rights.DeleteOwned(ctx, accountID, profileIDs)
}

func (s stuck) DeleteOwner(ctx context.Context, accountID, profileID string) error {
	if s.deletes {
		return errDown
	}
	return s.Rights.DeleteOwner(ctx, accountID, profileID)
}

func (s stuck) Tuples(ctx context.Context) iter.Seq2[authz.Owned, error] {
	if s.tuples {
		return func(yield func(authz.Owned, error) bool) { yield(authz.Owned{}, errDown) }
	}
	return s.Rights.Tuples(ctx)
}

func (s stuck) DropChangelog(ctx context.Context, before time.Time) (int64, error) {
	if s.changelog {
		return 0, errDown
	}
	return s.Rights.DropChangelog(ctx, before)
}

// A failure of an item counts, is logged by its kind, and leaves the rest of the
// step and the steps after it to run; the next pass does what it left.
func TestAFailureIsCountedAndTheNextPassDoesWhatItLeft(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	now := time.Now().UTC()
	longAgo := now.AddDate(-1, 0, -1)
	marked := w.account(t, "uid-marked", now)
	if _, err := w.pool.Exec(t.Context(), `UPDATE accounts SET deleting_at = now() - interval '1 minute' WHERE id = $1`, marked); err != nil {
		t.Fatal(err)
	}
	active := w.account(t, "uid-active", now)
	idleProfile := w.profile(t, active, longAgo, false)
	w.tuple(t, active, profileID(9), time.Hour)
	w.signin.AddUser(authn.User{UID: "uid-gone", Created: longAgo})
	w.signin.FailRemoveUser()

	report := w.sweeper(stuck{Rights: w.rights, deletes: true, changelog: true}, w.signin).Run(t.Context(), time.Now())
	if report.Clean() {
		t.Fatal("a pass with failures is clean")
	}
	for name, want := range map[string]int64{
		logging.StepMarks: 1, logging.StepIdleProfiles: 1, logging.StepUsers: 1, logging.StepTuples: 1, logging.StepChangelog: 1,
	} {
		if got := step(t, report, name); got.Failed != want {
			t.Errorf("%s = %+v, want %d failed", name, got, want)
		}
	}
	if !step(t, report, logging.StepKeys).Finished || step(t, report, logging.StepChangelog).Finished {
		t.Error("a step after a failing one did not run, or the changelog's failure was not its end")
	}
	if !strings.Contains(w.log.String(), `"moment":"sweep","step":"marks","error_kind"`) {
		t.Errorf("the log does not name the failure's step: %s", w.log.String())
	}
	// The profile's rows went; its tuple stayed, dangling, for the next pass.
	if w.count(t, "profiles WHERE id = $1", idleProfile) != 0 || !w.owns(t, active, idleProfile) {
		t.Error("want the idle profile's rows gone and its tuple left")
	}

	w.signin = &authntest.Fake{}
	w.signin.AddUser(authn.User{UID: "uid-gone", Created: longAgo})
	// The tuple left is as old as the job needs to take it.
	if _, err := w.fga.Exec(t.Context(), `UPDATE tuple SET inserted_at = now() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	if report := w.run(t); !report.Clean() {
		t.Fatalf("the next pass = %+v, want clean", report)
	}
	if w.count(t, "accounts WHERE id = $1", marked) != 0 || !w.removed("uid-gone") ||
		w.owns(t, active, idleProfile) || w.owns(t, active, profileID(9)) {
		t.Error("the next pass did not do what the first left")
	}
}

// A step whose own query fails ends there, and the rest run.
func TestAStepWhoseQueryFailsEndsThere(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	down, err := pgxpool.New(t.Context(), "postgres://u@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	down.Close()
	w.signin.AddUser(authn.User{UID: "uid-gone"})
	w.tuple(t, profileID(1), profileID(2), time.Hour)
	report := sweep.New(down, w.model, w.rights, w.signin, settle, logging.New(&w.log, slog.LevelDebug)).Run(t.Context(), time.Now())
	for _, s := range report {
		switch s.Name {
		case logging.StepChangelog:
			if !s.Finished {
				t.Errorf("%+v, want the changelog cleared: it is of the database openfga", s)
			}
			continue
		case logging.StepUsers:
			// The account is asked for user by user: a failure of the user, not of
			// the step.
			if !s.Finished || s.Failed != 1 {
				t.Errorf("%+v, want the user's failure", s)
			}
			continue
		}
		if s.Finished || s.Failed != 1 {
			t.Errorf("%+v, want one failure and no end", s)
		}
	}

	failing := &authntest.Fake{}
	failing.FailUsers()
	report = w.sweeper(stuck{Rights: w.rights, tuples: true}, failing).Run(t.Context(), time.Now())
	for _, name := range []string{logging.StepUsers, logging.StepTuples} {
		if s := step(t, report, name); s.Finished || s.Failed != 1 {
			t.Errorf("%+v, want one failure and no end", s)
		}
	}
}

// cutting is a sign-in service that ends the pass's time at its first removal.
type cutting struct {
	*authntest.Fake
	cancel context.CancelFunc
}

func (c cutting) RemoveUser(ctx context.Context, uid string) error {
	c.cancel()
	return ctx.Err()
}

// A pass whose time runs out stops between items, counts no failure for it, and
// leaves the rest to the next pass.
func TestAPassOutOfTimeStopsAndTheNextTakesItUp(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	now := time.Now().UTC()
	for i := range 3 {
		w.signin.AddUser(authn.User{UID: fmt.Sprintf("uid-gone-%d", i)})
	}
	marked := w.account(t, "uid-marked", now)
	if _, err := w.pool.Exec(t.Context(), `UPDATE accounts SET deleting_at = now() - interval '1 minute' WHERE id = $1`, marked); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	report := w.sweeper(w.rights, cutting{Fake: w.signin, cancel: cancel}).Run(ctx, now)
	if report.Clean() {
		t.Fatal("a pass cut short is clean")
	}
	for _, s := range report {
		if s.Failed != 0 {
			t.Errorf("%+v, want no failure for the time", s)
		}
	}
	if s := step(t, report, logging.StepMarks); s.Finished || s.Done != 0 {
		t.Errorf("marks = %+v, want cut short", s)
	}
	if s := step(t, report, logging.StepUsers); s.Finished || s.Done != 0 {
		t.Errorf("users = %+v, want not started", s)
	}

	if report := w.run(t); !report.Clean() || step(t, report, logging.StepMarks).Done != 1 || step(t, report, logging.StepUsers).Done != 3 {
		t.Errorf("the next pass = %+v, want the mark finished and the three users removed", report)
	}
}

// Past a page of each: a hundred and one marks and dangling tuples, a thousand
// and one users.
func TestThePagesAreAllTaken(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	if _, err := w.pool.Exec(t.Context(), `INSERT INTO accounts (auth_uid, deleting_at)
		SELECT 'uid-marked-' || g, now() - interval '1 minute' FROM generate_series(1, 101) AS g`); err != nil {
		t.Fatal(err)
	}
	for i := range 101 {
		if err := w.rights.WriteOwner(t.Context(), profileID(1000), profileID(i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.fga.Exec(t.Context(), `UPDATE tuple SET inserted_at = now() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	for i := range 1001 {
		w.signin.AddUser(authn.User{UID: fmt.Sprintf("uid-gone-%d", i)})
	}
	report := w.run(t)
	if !report.Clean() {
		t.Fatalf("report = %+v, want clean", report)
	}
	// The marks' users are the job's too, and go with their accounts.
	for name, want := range map[string]int64{logging.StepMarks: 101, logging.StepTuples: 101, logging.StepUsers: 1001} {
		if got := step(t, report, name).Done; got != want {
			t.Errorf("%s did %d, want %d", name, got, want)
		}
	}
	if w.count(t, "accounts") != 0 {
		t.Error("marks are left")
	}
}
