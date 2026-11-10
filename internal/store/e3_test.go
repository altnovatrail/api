package store_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

// digest is a package digest of the tests.
var digest = "sha256:" + strings.Repeat("a", 64)

// e3 is a profile with what epic E3 keeps of it, as the tests write it.
type e3 struct {
	pool      *pgxpool.Pool
	accountID string
	profileID string
}

func newE3(t *testing.T) *e3 {
	t.Helper()
	pool := storetest.DB(t)
	accountID := account(t, pool)
	return &e3{pool: pool, accountID: accountID, profileID: ownerProfile(t, pool, accountID, time.Now())}
}

// choice is a new engine's choice of the profile, of a task, which names its
// module and task, or of a lesson.
func (e *e3) choice(t *testing.T, kind string) string {
	t.Helper()
	var id string
	if err := e.pool.QueryRow(t.Context(), `INSERT INTO engine_choices (profile_id, kind, module_id, task_id)
		SELECT $1, $2, m, m FROM (SELECT CASE WHEN $2 = 'task' THEN 'example/m' END AS m) AS task RETURNING id`,
		e.profileID, kind).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// run is a lesson run of lesson by a new choice.
func (e *e3) run(t *testing.T, lesson string) (string, error) {
	t.Helper()
	var id string
	err := e.pool.QueryRow(t.Context(), `INSERT INTO lesson_runs
		(profile_id, lesson_id, module_id, module_version, package_digest, choice_id)
		VALUES ($1, $2, 'example/m', '0.1.0', $3, $4) RETURNING id`, e.profileID, lesson, digest, e.choice(t, "lesson")).Scan(&id)
	return id, err
}

// attempt is an attempt with the columns extra sets to args, or by a new choice
// of a task when extra is empty.
func (e *e3) attempt(t *testing.T, extra string, args ...any) (string, error) {
	t.Helper()
	if extra == "" {
		extra, args = "choice_id", []any{e.choice(t, "task")}
	}
	all := append([]any{e.profileID, digest}, args...)
	values := make([]string, len(args))
	for i := range args {
		values[i] = "$" + strconv.Itoa(3+i)
	}
	columns := extra
	vals := strings.Join(values, ", ")
	var id string
	err := e.pool.QueryRow(t.Context(), `INSERT INTO attempts (profile_id, package_digest, module_id, module_version,
		module_format_version, module_kit_version, task_id, seed, `+columns+`)
		VALUES ($1, $2, 'example/m', '0.1.0', '0.4', 'v0.3.0', 't1', -1, `+vals+`) RETURNING id`, all...).Scan(&id)
	return id, err
}

func sqlstate(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// The schema holds one open attempt of a profile, one open run of a lesson, a
// close and an end with their times, an attempt that begins one way, a choice
// that begins one attempt or run, and a journal that is only added to.
func TestTheSchemaOfE3HoldsItsRules(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	open, err := e.attempt(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.attempt(t, ""); sqlstate(err) != "23505" {
		t.Errorf("a second open attempt = %v, want a unique violation", err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE attempts SET close_reason = 'left', closed_at = now() WHERE id = $1`, open); err != nil {
		t.Fatal(err)
	}
	again, err := e.attempt(t, "previous_attempt_id", open)
	if err != nil {
		t.Fatalf("an attempt again, the first closed: %v", err)
	}

	first, err := e.run(t, "example/m/l1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.run(t, "example/m/l1"); sqlstate(err) != "23505" {
		t.Errorf("a second open run of the lesson = %v, want a unique violation", err)
	}
	if _, err := e.run(t, "example/m/l2"); err != nil {
		t.Errorf("an open run of another lesson: %v", err)
	}

	for name, tc := range map[string]struct {
		sql  string
		args []any
		code string
	}{
		"a close with no time":              {`UPDATE attempts SET close_reason = 'left' WHERE id = $1`, []any{again}, "23514"},
		"a time with no close":              {`UPDATE attempts SET closed_at = now() WHERE id = $1`, []any{again}, "23514"},
		"a close of no reason":              {`UPDATE attempts SET close_reason = 'gone', closed_at = now() WHERE id = $1`, []any{again}, "23514"},
		"an end of no reason":               {`UPDATE lesson_runs SET end_reason = 'gone', ended_at = now() WHERE id = $1`, []any{first}, "23514"},
		"an end with no time":               {`UPDATE lesson_runs SET end_reason = 'passed' WHERE id = $1`, []any{first}, "23514"},
		"a segment of no run":               {`UPDATE attempts SET segment = 1 WHERE id = $1`, []any{again}, "23514"},
		"two ways to begin":                 {`UPDATE attempts SET choice_id = (SELECT id FROM engine_choices LIMIT 1) WHERE id = $1`, []any{again}, "23514"},
		"a digest that is none":             {`UPDATE attempts SET package_digest = 'sha256:x' WHERE id = $1`, []any{again}, "23514"},
		"a choice of two runs":              {`UPDATE lesson_runs SET choice_id = (SELECT choice_id FROM lesson_runs WHERE id <> $1 LIMIT 1) WHERE id = $1`, []any{first}, "23505"},
		"a negative instance":               {`UPDATE attempts SET instance = -1 WHERE id = $1`, []any{again}, "23514"},
		"a choice of two attempts":          {`UPDATE attempts SET previous_attempt_id = NULL, choice_id = (SELECT choice_id FROM attempts WHERE id = $2) WHERE id = $1`, []any{again, open}, "23505"},
		"a choice of a task of no task":     {`INSERT INTO engine_choices (profile_id, kind, module_id) VALUES ($1, 'task', 'example/m')`, []any{e.profileID}, "23514"},
		"a submission before the first":     {`INSERT INTO submissions (profile_id, attempt_id, number, answer) VALUES ($1, $2, 0, '\x00')`, []any{e.profileID, again}, "23514"},
		"a submission of another's attempt": {`INSERT INTO submissions (profile_id, attempt_id, number, answer) VALUES ($1, $2, 1, '\x00')`, []any{ownerProfile(t, e.pool, account(t, e.pool), time.Now()), again}, "23503"},
		"negative wrong answers":            {`UPDATE attempts SET wrong_answers = -1 WHERE id = $1`, []any{again}, "23514"},
		"a record done of nothing":          {`INSERT INTO idempotency_keys (account_id, key, method, fingerprint, state) VALUES ($1, 'n', '/m', 'f', 'done')`, []any{e.accountID}, "23514"},
	} {
		if _, err := e.pool.Exec(ctx, tc.sql, tc.args...); sqlstate(err) != tc.code {
			t.Errorf("%s = %v, want SQLSTATE %s", name, err, tc.code)
		}
	}

	if _, err := e.pool.Exec(ctx, `UPDATE attempts SET close_reason = 'left', closed_at = now() WHERE id = $1`, again); err != nil {
		t.Fatal(err)
	}
	if _, err := e.attempt(t, "lesson_run_id, segment", first, 1); err != nil {
		t.Errorf("an attempt of a segment: %v", err)
	}

	if _, err := e.pool.Exec(ctx, `INSERT INTO evidence (profile_id, seq, attempt_id, package_digest, module_id, evidence)
		VALUES ($1, 1, $2, $3, 'example/m', '\x00')`, e.profileID, open, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO evidence (profile_id, seq, attempt_id, package_digest, module_id, evidence)
		VALUES ($1, 1, $2, $3, 'example/m', '\x00')`, e.profileID, again, digest); sqlstate(err) != "23505" {
		t.Errorf("evidence under a number taken = %v, want a unique violation", err)
	}
	for name, sql := range map[string]string{
		"a change of the journal":     `UPDATE evidence SET evidence = '\x01' WHERE profile_id = $1`,
		"a deletion from the journal": `DELETE FROM evidence WHERE profile_id = $1`,
	} {
		if _, err := e.pool.Exec(ctx, sql, e.profileID); sqlstate(err) != "23001" {
			t.Errorf("%s = %v, want restrict_violation", name, err)
		}
	}
	if _, err := e.pool.Exec(ctx, `TRUNCATE evidence`); sqlstate(err) != "23001" {
		t.Errorf("a truncation of the journal = %v, want restrict_violation", err)
	}

	// A close and a beginning, once written, stay.
	for name, sql := range map[string]string{
		"a close changed":     `UPDATE attempts SET close_reason = 'abandoned' WHERE id = $1`,
		"a close undone":      `UPDATE attempts SET close_reason = NULL, closed_at = NULL WHERE id = $1`,
		"a beginning changed": `UPDATE attempts SET started_at = now() - interval '1 day' WHERE id = $1`,
	} {
		if _, err := e.pool.Exec(ctx, sql, open); sqlstate(err) != "23001" {
			t.Errorf("%s = %v, want restrict_violation", name, err)
		}
	}
	if _, err := e.pool.Exec(ctx, `UPDATE attempts SET last_action_at = now() WHERE id = $1`, open); err != nil {
		t.Errorf("an action on a closed attempt's row: %v", err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE lesson_runs SET end_reason = 'passed', ended_at = now() WHERE id = $1`, first); err != nil {
		t.Fatal(err)
	}
	for name, sql := range map[string]string{
		"an end changed":            `UPDATE lesson_runs SET end_reason = 'abandoned' WHERE id = $1`,
		"a run's beginning changed": `UPDATE lesson_runs SET started_at = now() - interval '1 day' WHERE id = $1`,
	} {
		if _, err := e.pool.Exec(ctx, sql, first); sqlstate(err) != "23001" {
			t.Errorf("%s = %v, want restrict_violation", name, err)
		}
	}
}

// A choice opens what its kind is, and a row links only to rows of its own
// profile.
func TestTheLinksOfE3StayInTheirKindAndProfile(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	if _, err := e.attempt(t, "choice_id", e.choice(t, "lesson")); sqlstate(err) != "23503" {
		t.Errorf("an attempt by a choice of a lesson = %v, want a foreign key violation", err)
	}
	var id string
	err := e.pool.QueryRow(t.Context(), `INSERT INTO lesson_runs
		(profile_id, lesson_id, module_id, module_version, package_digest, choice_id)
		VALUES ($1, 'example/m/l1', 'example/m', '0.1.0', $2, $3) RETURNING id`, e.profileID, digest, e.choice(t, "task")).Scan(&id)
	if sqlstate(err) != "23503" {
		t.Errorf("a run by a choice of a task = %v, want a foreign key violation", err)
	}
	if _, err := e.pool.Exec(t.Context(), `INSERT INTO engine_choices (profile_id, kind) VALUES ($1, 'quiz')`, e.profileID); sqlstate(err) != "23514" {
		t.Errorf("a choice of no kind = %v, want a check violation", err)
	}

	other := &e3{pool: e.pool, accountID: e.accountID}
	other.profileID = ownerProfile(t, e.pool, account(t, e.pool), time.Now())
	theirs, err := other.run(t, "example/m/l1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.attempt(t, "lesson_run_id, segment", theirs, 1); sqlstate(err) != "23503" {
		t.Errorf("an attempt on another profile's run = %v, want a foreign key violation", err)
	}
	theirAttempt, err := other.attempt(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.attempt(t, "previous_attempt_id", theirAttempt); sqlstate(err) != "23503" {
		t.Errorf("an attempt again after another profile's = %v, want a foreign key violation", err)
	}
	if _, err := e.attempt(t, "choice_id", other.choice(t, "task")); sqlstate(err) != "23503" {
		t.Errorf("an attempt by another profile's choice = %v, want a foreign key violation", err)
	}
}

// What E3 keeps of a profile goes with it, and with its account.
func TestWhatE3KeepsGoesWithTheProfile(t *testing.T) {
	t.Parallel()

	for name, del := range map[string]func(ctx context.Context, tx pgx.Tx, e *e3) error{
		"the profile": func(ctx context.Context, tx pgx.Tx, e *e3) error {
			return store.DeleteProfileRows(ctx, tx, e.profileID)
		},
		"the account": func(ctx context.Context, tx pgx.Tx, e *e3) error {
			if err := store.MarkDeleting(ctx, tx, e.accountID); err != nil {
				return err
			}
			return store.ClearAccount(ctx, tx, e.accountID)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newE3(t)
			ctx := t.Context()
			if _, err := e.pool.Exec(ctx, `INSERT INTO skill_states (profile_id) VALUES ($1)`, e.profileID); err != nil {
				t.Fatal(err)
			}
			run, err := e.run(t, "example/m/l1")
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := e.attempt(t, "lesson_run_id, segment", run, 1)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.attempt(t, "previous_attempt_id", attempt); sqlstate(err) != "23505" {
				t.Fatalf("a second open attempt = %v", err)
			}
			if _, err := e.pool.Exec(ctx, `INSERT INTO evidence (profile_id, seq, attempt_id, package_digest, module_id, evidence)
				VALUES ($1, 1, $2, $3, 'example/m', '\x00')`, e.profileID, attempt, digest); err != nil {
				t.Fatal(err)
			}
			if _, err := e.pool.Exec(ctx, `INSERT INTO idempotency_keys
				(account_id, key, method, profile_id, attempt_id, lesson_run_id, fingerprint, state)
				VALUES ($1, 'k', '/novatrail.api.v1.AttemptService/SubmitWork', $2, $3, $4, 'f', 'going')`,
				e.accountID, e.profileID, attempt, run); err != nil {
				t.Fatal(err)
			}
			var submission string
			if err := e.pool.QueryRow(ctx, `INSERT INTO submissions (profile_id, attempt_id, number, answer)
				VALUES ($1, $2, 1, '\x00') RETURNING id`, e.profileID, attempt).Scan(&submission); err != nil {
				t.Fatal(err)
			}
			if _, err := e.pool.Exec(ctx, `INSERT INTO idempotency_keys
				(account_id, key, method, profile_id, attempt_id, submission_id, fingerprint, state)
				VALUES ($1, 's', '/novatrail.api.v1.AttemptService/SubmitWork', $2, $3, $4, 'f', 'done')`,
				e.accountID, e.profileID, attempt, submission); err != nil {
				t.Fatal(err)
			}
			if err := pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error { return del(ctx, tx, e) }); err != nil {
				t.Fatal(err)
			}
			for _, table := range []string{"engine_choices", "lesson_runs", "attempts", "evidence", "skill_states", "submissions", "idempotency_keys"} {
				if n := count(t, e.pool, table); n != 0 {
					t.Errorf("%d rows of %s left", n, table)
				}
			}
		})
	}
}

// A profile made has the row of its skill state, empty and at number 0, and so
// does every profile there before the migration.
func TestEveryProfileHasItsSkillState(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	url := storetest.URL(t, false)
	before := fstest.MapFS{}
	names, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if filepath.Base(name) >= "0005" {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		before[filepath.Base(name)] = &fstest.MapFile{Data: data}
	}
	if _, _, err := store.MigrateFiles(ctx, url, before); err != nil {
		t.Fatal(err)
	}
	pool, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	old := ownerProfile(t, pool, account(t, pool), time.Now())
	if _, _, err := store.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}

	accountID := account(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE accounts SET adult_stated_at = now() WHERE id = $1`, accountID); err != nil {
		t.Fatal(err)
	}
	var made store.Profile
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var err error
		made, err = store.CreateOwnerProfile(ctx, tx, accountID, store.Profile{Pseudonym: "Owl", Country: "US",
			VoicingSpeed: "normal", DefaultMode: "self"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{old, made.ID} {
		var seq int64
		var state []byte
		if err := pool.QueryRow(ctx, `SELECT last_seq, state FROM skill_states WHERE profile_id = $1`, id).Scan(&seq, &state); err != nil || seq != 0 || state != nil {
			t.Errorf("the skill state of %s = %d, %v, %v; want an empty row at 0", id, seq, state, err)
		}
	}
}

// The job closes the open attempts and ends the open runs idle since its term,
// and nothing else.
func TestTheJobClosesWhatWasAbandoned(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	idle, err := e.attempt(t, "")
	if err != nil {
		t.Fatal(err)
	}
	idleRun, err := e.run(t, "example/m/l1")
	if err != nil {
		t.Fatal(err)
	}
	liveRun, err := e.run(t, "example/m/l2")
	if err != nil {
		t.Fatal(err)
	}
	endedRun, err := e.run(t, "example/m/l3")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE attempts SET last_action_at = now() - interval '8 days'`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE lesson_runs SET last_action_at = now() - interval '31 days' WHERE id <> $1`, liveRun); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE lesson_runs SET end_reason = 'passed', ended_at = now() - interval '31 days' WHERE id = $1`, endedRun); err != nil {
		t.Fatal(err)
	}
	var activeOn time.Time
	if err := e.pool.QueryRow(ctx, `SELECT active_on FROM profiles WHERE id = $1`, e.profileID).Scan(&activeOn); err != nil {
		t.Fatal(err)
	}

	week := time.Now().Add(-7 * 24 * time.Hour)
	if n, err := store.CloseAbandonedAttempts(ctx, e.pool, week, time.Now()); err != nil || n != 1 {
		t.Errorf("CloseAbandonedAttempts = %d, %v; want the one idle", n, err)
	}
	if n, err := store.CloseAbandonedAttempts(ctx, e.pool, week, time.Now()); err != nil || n != 0 {
		t.Errorf("CloseAbandonedAttempts again = %d, %v; want none", n, err)
	}
	month := time.Now().Add(-30 * 24 * time.Hour)
	if n, err := store.EndAbandonedRuns(ctx, e.pool, month, time.Now()); err != nil || n != 1 {
		t.Errorf("EndAbandonedRuns = %d, %v; want the one idle", n, err)
	}

	var reason string
	if err := e.pool.QueryRow(ctx, `SELECT close_reason FROM attempts WHERE id = $1`, idle).Scan(&reason); err != nil || reason != "abandoned" {
		t.Errorf("the idle attempt = %q, %v", reason, err)
	}
	for id, want := range map[string]string{idleRun: "abandoned", liveRun: "", endedRun: "passed"} {
		var got *string
		if err := e.pool.QueryRow(ctx, `SELECT end_reason FROM lesson_runs WHERE id = $1`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if (got == nil && want != "") || (got != nil && *got != want) {
			t.Errorf("the run %s ended %v, want %q", id, got, want)
		}
	}
	var after time.Time
	if err := e.pool.QueryRow(ctx, `SELECT active_on FROM profiles WHERE id = $1`, e.profileID).Scan(&after); err != nil || !after.Equal(activeOn) {
		t.Errorf("the close moved the profile's activity from %v to %v", activeOn, after)
	}
}

// The job's statements that fail are its failures.
func TestTheJobOfE3FailsWithItsStatements(t *testing.T) {
	t.Parallel()

	pool := storetest.DB(t)
	ctx := t.Context()
	tx, err := (&flaky{Pool: pool, failOn: "UPDATE"}).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	if _, err := store.CloseAbandonedAttempts(ctx, tx, time.Now(), time.Now()); !errors.Is(err, errDown) {
		t.Errorf("CloseAbandonedAttempts = %v, want the failure", err)
	}
	if _, err := store.EndAbandonedRuns(ctx, tx, time.Now(), time.Now()); !errors.Is(err, errDown) {
		t.Errorf("EndAbandonedRuns = %v, want the failure", err)
	}
}

// An attempt begins by a choice of a task, which begins one; its submissions
// are numbered in order, counted as wrong answers when they are, and read back;
// the techniques given at a step keep the first time; and its close holds.
func TestTheRowsOfAnAttemptAreWrittenAndRead(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	none := "00000000-0000-0000-0000-000000000000"
	task, lesson := e.choice(t, "task"), e.choice(t, "lesson")
	if _, err := e.pool.Exec(ctx, `UPDATE engine_choices SET module_id = 'example/m' WHERE id = $1`, lesson); err != nil {
		t.Fatal(err)
	}
	if c, err := store.ChoiceOf(ctx, e.pool, e.profileID, task); err != nil || c.Kind != "task" || c.ModuleID != "example/m" || c.TaskID != "example/m" {
		t.Errorf("ChoiceOf a task = %+v, %v", c, err)
	}
	if c, err := store.ChoiceOf(ctx, e.pool, e.profileID, lesson); err != nil || c.Kind != "lesson" || c.ModuleID != "example/m" || c.TaskID != "" {
		t.Errorf("ChoiceOf a lesson with its module = %+v, %v", c, err)
	}
	if _, err := store.ChoiceOf(ctx, e.pool, e.profileID, none); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ChoiceOf none = %v, want ErrNotFound", err)
	}
	instance := 2
	at := time.Now().UTC().Truncate(time.Microsecond)
	var id string
	err := pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		if _, found, err := store.HoldOpenAttempt(ctx, tx, e.profileID); err != nil || found {
			t.Errorf("HoldOpenAttempt before = %v, %v; want none", found, err)
		}
		var err error
		id, err = store.BeginAttempt(ctx, tx, e.profileID, store.NewAttempt{PackageDigest: digest, ModuleID: "example/m",
			ModuleVersion: "0.1.0", FormatVersion: "0.4", ModuleKit: "v0.3.0", TaskID: "t1", Seed: 1 << 63,
			Instance: &instance, ChoiceID: &task}, at)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if begun, err := store.ChoiceBegun(ctx, e.pool, e.profileID, task); err != nil || !begun {
		t.Errorf("ChoiceBegun = %v, %v; want true", begun, err)
	}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		a, found, err := store.HoldOpenAttempt(ctx, tx, e.profileID)
		if err != nil || !found || a.ID != id || a.Seed != 1<<63 || *a.Instance != 2 || *a.ChoiceID != task ||
			a.CloseReason != nil || !a.LastActionAt.Equal(at) || a.StepTechniques {
			t.Errorf("HoldOpenAttempt = %+v, %v, %v", a, found, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE attempts SET close_reason = 'left', closed_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		_, err := store.BeginAttempt(ctx, tx, e.profileID, store.NewAttempt{PackageDigest: digest, ModuleID: "example/m",
			ModuleVersion: "0.1.0", FormatVersion: "0.4", ModuleKit: "v0.3.0", TaskID: "t1", ChoiceID: &task}, at)
		return err
	})
	if !errors.Is(err, store.ErrChoiceBegun) {
		t.Errorf("a second attempt of the choice = %v, want ErrChoiceBegun", err)
	}
	later := at.Add(time.Minute)
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		if _, err := store.Submit(ctx, tx, e.profileID, id, []byte{1}, []byte{9}, true, at); err != nil {
			return err
		}
		if _, err := store.Submit(ctx, tx, e.profileID, id, []byte{2}, nil, false, later); err != nil {
			return err
		}
		if err := store.GiveStepTechniques(ctx, tx, e.profileID, id, at); err != nil {
			return err
		}
		return store.GiveStepTechniques(ctx, tx, e.profileID, id, later)
	})
	if err != nil {
		t.Fatal(err)
	}
	if digest, task, err := store.AttemptTask(ctx, e.pool, e.profileID, id); err != nil || digest == "" || task == "" {
		t.Errorf("AttemptTask = %q, %q, %v; want the attempt's package and task", digest, task, err)
	}
	if _, _, err := store.AttemptTask(ctx, e.pool, e.profileID, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("AttemptTask of none = %v, want ErrNotFound", err)
	}
	if answer, found, err := store.LastSubmission(ctx, e.pool, e.profileID, id); err != nil || !found || answer[0] != 2 {
		t.Errorf("LastSubmission = %v, %v, %v; want the second", answer, found, err)
	}
	var first string
	var given time.Time
	var observation []byte
	if err := e.pool.QueryRow(ctx, `SELECT s.id, a.step_techniques_at, s.observation FROM submissions s JOIN attempts a ON a.id = s.attempt_id
		WHERE s.attempt_id = $1 AND s.number = 1`, id).Scan(&first, &given, &observation); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(observation, []byte{9}) {
		t.Errorf("the first submission keeps the observation %v, want its own", observation)
	}
	if answer, err := store.SubmissionAnswer(ctx, e.pool, e.profileID, first); err != nil || answer[0] != 1 || !given.Equal(at) {
		t.Errorf("SubmissionAnswer = %v, %v; techniques given at %v, want the first time", answer, err, given)
	}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		a, err := store.HoldAttempt(ctx, tx, e.profileID, id)
		if err != nil || a.WrongAnswers != 1 || !a.LastActionAt.Equal(later) || !a.StepTechniques {
			t.Errorf("HoldAttempt = %+v, %v; want one wrong answer, the last action later, techniques given", a, err)
		}
		if err := store.CloseAttempt(ctx, tx, e.profileID, id, "correct", later); err != nil {
			return err
		}
		// A close kept is not written over.
		return store.CloseAttempt(ctx, tx, e.profileID, id, "left", later)
	})
	if err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		a, err := store.HoldAttempt(ctx, tx, e.profileID, id)
		if err != nil || *a.CloseReason != "correct" {
			t.Errorf("the attempt closed = %+v, %v; want correct", a, err)
		}
		if _, err := store.HoldAttempt(ctx, tx, e.profileID, none); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("HoldAttempt of none = %v, want ErrNotFound", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.LastSubmission(ctx, e.pool, e.profileID, none); err != nil || found {
		t.Errorf("LastSubmission of none = %v, %v", found, err)
	}
	run, err := e.run(t, "example/m/l1")
	if err != nil {
		t.Fatal(err)
	}
	if there, err := store.LessonRunThere(ctx, e.pool, e.profileID, run); err != nil || !there {
		t.Errorf("LessonRunThere = %v, %v; want true", there, err)
	}
	if connected, err := store.ModuleConnected(ctx, e.pool, e.profileID, "example/m"); err != nil || connected {
		t.Errorf("ModuleConnected = %v, %v; want false", connected, err)
	}
}
