package sweep_test

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/packages/packagestest"
	"github.com/altnovatrail/api/internal/sweep"
)

// unknown is a digest the index of no world knows.
const unknown = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

// after is the job after a rollout on the world.
func (w *world) after(t *testing.T) sweep.Report {
	t.Helper()
	return sweep.After(w.pool, w.set, w.model, logging.New(&w.log, slog.LevelDebug)).Run(t.Context(), time.Now())
}

// attempt makes an open attempt of the profile on the package of digest — of the
// run's segment 1 when run is not empty —, idle for idle.
func (w *world) attempt(t *testing.T, profile, digest, run, idle string) string {
	t.Helper()
	if run != "" {
		return w.id(t, `INSERT INTO attempts (profile_id, package_digest, module_id, module_version, module_format_version,
			module_kit_version, task_id, seed, lesson_run_id, segment, last_action_at)
			VALUES ($1, $2, 'example/choices', '1.0.0', '0.4', 'v0.3.0', 'pick', 1, $3, 1, now() - $4::interval)
			RETURNING id`, profile, digest, run, idle)
	}
	choice := w.id(t, `INSERT INTO engine_choices (profile_id, kind, module_id, task_id)
		VALUES ($1, 'task', 'example/choices', 'pick') RETURNING id`, profile)
	return w.id(t, `INSERT INTO attempts (profile_id, package_digest, module_id, module_version, module_format_version,
		module_kit_version, task_id, seed, choice_id, last_action_at)
		VALUES ($1, $2, 'example/choices', '1.0.0', '0.4', 'v0.3.0', 'pick', 1, $3, now() - $4::interval)
		RETURNING id`, profile, digest, choice, idle)
}

// lessonRun makes an open lesson run of the profile on the package of digest.
func (w *world) lessonRun(t *testing.T, profile, digest string) string {
	t.Helper()
	choice := w.id(t, `INSERT INTO engine_choices (profile_id, kind, module_id, lesson_id)
		VALUES ($1, 'lesson', 'example/choices', 'basics') RETURNING id`, profile)
	return w.id(t, `INSERT INTO lesson_runs (profile_id, lesson_id, module_id, module_version, package_digest, choice_id,
		last_action_at) VALUES ($1, 'example/choices/basics', 'example/choices', '1.0.0', $2, $3, now()) RETURNING id`,
		profile, digest, choice)
}

// closed is the reason the attempt closed with, or "open".
func (w *world) closed(t *testing.T, attemptID string) string {
	t.Helper()
	var reason *string
	if err := w.pool.QueryRow(t.Context(), `SELECT close_reason FROM attempts WHERE id = $1`, attemptID).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason == nil {
		return "open"
	}
	return *reason
}

// ended is the reason the run ended with, or "open".
func (w *world) ended(t *testing.T, runID string) string {
	t.Helper()
	var reason *string
	if err := w.pool.QueryRow(t.Context(), `SELECT end_reason FROM lesson_runs WHERE id = $1`, runID).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason == nil {
		return "open"
	}
	return *reason
}

// The job after a rollout closes "unchecked" each open attempt and lesson run on
// a package the image no longer gives — withdrawn, or out of the window —, writes
// the evidence of the attempts whose package the image holds, makes no engine's
// choice, and leaves what is on a package the index does not know and what an
// account being deleted holds (R07, R49).
func TestTheJobAfterARolloutClosesWhatTheImageNoLongerGives(t *testing.T) {
	t.Parallel()

	w := newWorldOn(t, "answer_leak")
	today := time.Now().UTC()
	account := w.account(t, "uid-learner", today)
	var profiles []string
	for range 6 {
		profiles = append(profiles, w.profile(t, account, today, false))
	}
	// An attempt on the withdrawn package, outside a lesson, and one of a run's
	// segment on it.
	withdrawn := w.attempt(t, profiles[0], string(choices.Digest), "", "1 hour")
	segmentRun := w.lessonRun(t, profiles[1], string(choices.Digest))
	segment := w.attempt(t, profiles[1], string(choices.Digest), segmentRun, "1 hour")
	// An attempt on a package out of the window, and a run on it with no open
	// attempt.
	outOfWindow := w.attempt(t, profiles[2], old, "", "1 hour")
	idleRun := w.lessonRun(t, profiles[3], old)
	// What is on a package the index does not know is the old image's, during a
	// rollout: it stays.
	unknownAttempt := w.attempt(t, profiles[4], unknown, "", "1 hour")
	unknownRun := w.lessonRun(t, profiles[5], unknown)
	// An account being deleted goes with what it holds.
	deleting := w.account(t, "uid-deleting", today)
	deletingProfile := w.profile(t, deleting, today, true)
	deletingAttempt := w.attempt(t, deletingProfile, string(choices.Digest), "", "1 hour")
	if _, err := w.pool.Exec(t.Context(), `UPDATE accounts SET deleting_at = now() WHERE id = $1`, deleting); err != nil {
		t.Fatal(err)
	}
	choicesBefore := w.count(t, "engine_choices")

	report := w.after(t)
	if !report.Clean() || len(report) != len(logging.AfterSteps) {
		t.Fatalf("report = %+v, want a clean pass of the steps of the job", report)
	}
	if s := step(t, report, logging.StepGoneAttempts); s.Done != 3 {
		t.Errorf("%+v, want the three attempts closed", s)
	}
	if s := step(t, report, logging.StepGoneRuns); s.Done != 1 {
		t.Errorf("%+v, want the run with no attempt ended: the trigger ended the other", s)
	}
	for _, id := range []string{withdrawn, segment, outOfWindow} {
		if got := w.closed(t, id); got != "unchecked" {
			t.Errorf("attempt closed %s, want unchecked", got)
		}
	}
	for _, id := range []string{segmentRun, idleRun} {
		if got := w.ended(t, id); got != "unchecked" {
			t.Errorf("run ended %s, want unchecked", got)
		}
	}
	for _, id := range []string{unknownAttempt, deletingAttempt} {
		if got := w.closed(t, id); got != "open" {
			t.Errorf("attempt closed %s, want it left open", got)
		}
	}
	if got := w.ended(t, unknownRun); got != "open" {
		t.Errorf("the run on a package the index does not know ended %s, want it left open", got)
	}
	// The evidence of the attempts on the package the image holds is written;
	// the one out of the window gets none, its package being gone (R49).
	for i, want := range []int{1, 1, 0} {
		if got := w.count(t, "evidence WHERE profile_id = $1", profiles[i]); got != want {
			t.Errorf("profile %d has %d pieces of evidence, want %d", i, got, want)
		}
	}
	if got := w.count(t, "engine_choices"); got != choicesBefore {
		t.Errorf("engine_choices = %d, want %d: an end the job finds is given to nobody", got, choicesBefore)
	}

	log := w.log.String()
	if strings.Count(log, `"message":"swept"`) != len(logging.AfterSteps) {
		t.Errorf("the log has not a line for each step: %s", log)
	}
	for _, named := range append(w.ids, w.devices...) {
		if strings.Contains(log, named) {
			t.Errorf("the log names %s: %s", named, log)
		}
	}

	for _, s := range w.after(t) {
		if !s.Finished || s.Failed != 0 || s.Done != 0 {
			t.Errorf("the second pass: %+v, want nothing done", s)
		}
	}
}

// An open attempt of a run that the first step did not get to — here one on a
// package the index does not know, of a run on a package out of the window — is
// closed with its run, which the trigger ends.
func TestARunEndsWithItsOpenAttempt(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	today := time.Now().UTC()
	profile := w.profile(t, w.account(t, "uid-learner", today), today, true)
	run := w.lessonRun(t, profile, old)
	attempt := w.attempt(t, profile, unknown, run, "1 hour")

	report := w.after(t)
	if s := step(t, report, logging.StepGoneAttempts); !s.Finished || s.Done != 0 {
		t.Errorf("%+v, want nothing for the first step", s)
	}
	if s := step(t, report, logging.StepGoneRuns); !s.Finished || s.Done != 1 {
		t.Errorf("%+v, want the run ended", s)
	}
	if w.closed(t, attempt) != "unchecked" || w.ended(t, run) != "unchecked" {
		t.Errorf("attempt %s, run %s; want both unchecked", w.closed(t, attempt), w.ended(t, run))
	}
}

// The daily job takes the two steps before it closes the abandoned attempts: an
// attempt on a package the image no longer gives is "unchecked", however long
// it was idle, and not "abandoned" (R07).
func TestTheDailyJobClosesThemUncheckedBeforeAbandoned(t *testing.T) {
	t.Parallel()

	w := newWorldOn(t, "unsuitable_content")
	today := time.Now().UTC()
	profile := w.profile(t, w.account(t, "uid-learner", today), today, true)
	attempt := w.attempt(t, profile, string(choices.Digest), "", "8 days")

	report := w.run(t)
	if !report.Clean() {
		t.Fatalf("report = %+v, want a clean pass", report)
	}
	if got := w.closed(t, attempt); got != "unchecked" {
		t.Errorf("attempt closed %s, want unchecked", got)
	}
	if s := step(t, report, logging.StepAbandonedAttempts); s.Done != 0 {
		t.Errorf("%+v, want nothing abandoned", s)
	}
}

// With nothing the image no longer gives, the steps have nothing to choose and
// ask the database nothing.
func TestAnImageThatGivesEverythingClosesNothing(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	gives := packagestest.Set(t, packagestest.Entry{Pkg: choices, Release: "v0.1.0"})
	down, err := pgxpool.New(t.Context(), "postgres://u@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	down.Close()
	report := sweep.After(down, gives, w.model, logging.New(&w.log, slog.LevelDebug)).Run(t.Context(), time.Now())
	if !report.Clean() {
		t.Errorf("report = %+v, want a clean pass that asked nothing", report)
	}
}

// failing is the database with each statement of a transaction that holds
// failOn failing, and with then run on another connection once a page is chosen
// by a query that holds on: what changes between the choice and the item's
// transaction.
type failing struct {
	*pgxpool.Pool
	failOn string
	on     string
	then   func(ctx context.Context)
	// during runs on another connection once a statement of a transaction that
	// holds duringOn has run: what changes inside the item's transaction.
	duringOn string
	during   func(ctx context.Context)
}

func (f failing) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := f.Pool.Query(ctx, sql, args...)
	if f.then != nil && strings.Contains(sql, f.on) {
		f.then(ctx)
	}
	return rows, err
}

func (f failing) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := f.Pool.Begin(ctx)
	return failingTx{Tx: tx, failOn: f.failOn, duringOn: f.duringOn, during: f.during}, err
}

type failingTx struct {
	pgx.Tx
	failOn   string
	duringOn string
	during   func(ctx context.Context)
}

func (f failingTx) fails(sql string) bool { return f.failOn != "" && strings.Contains(sql, f.failOn) }

func (f failingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.fails(sql) {
		return pgconn.CommandTag{}, errDown
	}
	return f.Tx.Exec(ctx, sql, args...)
}

func (f failingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.fails(sql) {
		return downRow{}
	}
	row := f.Tx.QueryRow(ctx, sql, args...)
	if f.during != nil && strings.Contains(sql, f.duringOn) {
		f.during(ctx)
	}
	return row
}

type downRow struct{}

func (downRow) Scan(...any) error { return errDown }

// A statement of an item's transaction that fails is a failure of the item, the
// item stays open and the step goes on; what a request or a deletion changed
// between the choice and the transaction is nothing done and no failure.
func TestAnItemThatFailsOrWentMeanwhileStaysAsItIs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		failOn string
		then   string
		run    bool
		failed int64
	}{
		{name: "the attempt's run", failOn: "FOR UPDATE OF r", failed: 1},
		{name: "the attempt", failOn: "AND id = $2 FOR UPDATE", failed: 1},
		{name: "the close", failOn: "UPDATE attempts SET close_reason", failed: 1},
		{name: "the run", failOn: "AND id = $2 FOR UPDATE", run: true, failed: 1},
		{name: "the run's attempt", failOn: "lesson_run_id = $2 AND closed_at IS NULL", run: true, failed: 1},
		{name: "the run's end", failOn: "UPDATE lesson_runs SET end_reason", run: true, failed: 1},
		{name: "an attempt closed meanwhile", then: `UPDATE attempts SET close_reason = 'abandoned', closed_at = now()`},
		{name: "a run ended meanwhile", run: true, then: `UPDATE lesson_runs SET end_reason = 'abandoned', ended_at = now()`},
		{name: "an account marked meanwhile", then: `UPDATE accounts SET deleting_at = now()`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := newWorldOn(t, "answer_leak")
			today := time.Now().UTC()
			profile := w.profile(t, w.account(t, "uid-learner", today), today, true)
			name := logging.StepGoneAttempts
			db := failing{Pool: w.pool, failOn: tc.failOn, on: "FROM attempts t"}
			if tc.run {
				w.lessonRun(t, profile, old)
				name = logging.StepGoneRuns
				db.on = "FROM lesson_runs r"
			} else {
				w.attempt(t, profile, string(choices.Digest), "", "1 hour")
			}
			if tc.then != "" {
				var once sync.Once
				db.then = func(ctx context.Context) {
					once.Do(func() {
						if _, err := w.pool.Exec(ctx, tc.then); err != nil {
							t.Error(err)
						}
					})
				}
			}
			report := sweep.After(db, w.set, w.model, logging.New(&w.log, slog.LevelDebug)).Run(t.Context(), time.Now())
			if s := step(t, report, name); !s.Finished || s.Done != 0 || s.Failed != tc.failed {
				t.Errorf("%+v, want %d failed and nothing done", s, tc.failed)
			}
			if tc.then == "" && (w.count(t, "attempts WHERE closed_at IS NULL")+w.count(t, "lesson_runs WHERE ended_at IS NULL")) != 1 {
				t.Error("what failed did not stay open")
			}
		})
	}
}

// A run's attempt that the daily job closes "abandoned" while the job holds the
// run — its batch holds the attempt alone — is not closed again, and the run
// ends by itself, rather than staying open on a package the image no longer
// gives.
func TestARunWhoseAttemptClosedMeanwhileEndsByItself(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	today := time.Now().UTC()
	profile := w.profile(t, w.account(t, "uid-learner", today), today, true)
	run := w.lessonRun(t, profile, old)
	attempt := w.attempt(t, profile, unknown, run, "1 hour")
	db := failing{Pool: w.pool, duringOn: "lesson_run_id = $2 AND closed_at IS NULL", during: func(ctx context.Context) {
		if _, err := w.pool.Exec(ctx, `UPDATE attempts SET close_reason = 'abandoned', closed_at = now() WHERE id = $1`, attempt); err != nil {
			t.Error(err)
		}
	}}
	report := sweep.After(db, w.set, w.model, logging.New(&w.log, slog.LevelDebug)).Run(t.Context(), time.Now())
	if s := step(t, report, logging.StepGoneRuns); !s.Finished || s.Done != 1 || s.Failed != 0 {
		t.Errorf("%+v, want the run ended", s)
	}
	if w.closed(t, attempt) != "abandoned" || w.ended(t, run) != "unchecked" {
		t.Errorf("attempt %s, run %s; want the attempt abandoned and the run unchecked", w.closed(t, attempt), w.ended(t, run))
	}
}
