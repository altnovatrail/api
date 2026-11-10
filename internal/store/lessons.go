package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// RunAbandonedAfter is how long an open lesson run goes with no request on it
// or its attempts before it is abandoned (R13).
const RunAbandonedAfter = 30 * 24 * time.Hour

// The end reasons of a lesson run, as the schema writes them.
const (
	RunPassed      = "passed"
	RunUnchecked   = "unchecked"
	RunNotSuitable = "not_suitable"
	RunAbandoned   = "abandoned"
)

// ErrRunBegun is a lesson run that could not begin because another request began
// one meanwhile — the profile's open run of the lesson, or the run of the same
// choice: the request reads it again.
var ErrRunBegun = errors.New("store: the profile has an open run of the lesson")

// LessonRun is a lesson run's row, as the requests on it read it (R13).
type LessonRun struct {
	ID            string
	LessonID      string
	ModuleID      string
	ModuleVersion string
	PackageDigest string
	ChoiceID      string
	LastActionAt  time.Time
	EndReason     *string
}

// runColumns are the columns of a LessonRun, in scanRun's order. Its last action
// is the later of the run's own and its attempts': a request on an attempt of the
// run is an action on the run too, and is not written on the run's row, which
// the requests on the run hold before the attempt (R48).
const runColumns = `id, lesson_id, module_id, module_version, package_digest, choice_id,
	greatest(last_action_at, (SELECT max(a.last_action_at) FROM attempts a
		WHERE a.profile_id = lesson_runs.profile_id AND a.lesson_run_id = lesson_runs.id)), end_reason`

func scanRun(row pgx.Row) (LessonRun, error) {
	var r LessonRun
	err := row.Scan(&r.ID, &r.LessonID, &r.ModuleID, &r.ModuleVersion, &r.PackageDigest, &r.ChoiceID,
		&r.LastActionAt, &r.EndReason)
	return r, err
}

// runOf is the one run the query gives, held when it says FOR UPDATE, and
// whether there is one.
func runOf(ctx context.Context, tx pgx.Tx, what, sql string, args ...any) (LessonRun, bool, error) {
	r, err := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM lesson_runs WHERE `+sql, args...))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return LessonRun{}, false, nil
	case err != nil:
		return LessonRun{}, false, fmt.Errorf("store: read %s: %w", what, err)
	}
	return r, true, nil
}

// HoldLessonRun is the profile's lesson run by its ID, its row held FOR UPDATE
// in tx, so that the requests on one run go one after the other; ErrNotFound
// when the profile has none.
func HoldLessonRun(ctx context.Context, tx pgx.Tx, profileID, runID string) (LessonRun, error) {
	r, found, err := runOf(ctx, tx, "the lesson run", `profile_id = $1 AND id = $2 FOR UPDATE`, profileID, runID)
	if err == nil && !found {
		err = ErrNotFound
	}
	return r, err
}

// RunOfChoice is the run the profile's engine's choice opened, held, and
// whether it opened one.
func RunOfChoice(ctx context.Context, tx pgx.Tx, profileID, choiceID string) (LessonRun, bool, error) {
	return runOf(ctx, tx, "the choice's run", `profile_id = $1 AND choice_id = $2 FOR UPDATE`, profileID, choiceID)
}

// OpenRunOf is the profile's open run of the lesson, by its full ID, held, and
// whether it has one.
func OpenRunOf(ctx context.Context, tx pgx.Tx, profileID, lessonID string) (LessonRun, bool, error) {
	return runOf(ctx, tx, "the open run", `profile_id = $1 AND lesson_id = $2 AND ended_at IS NULL FOR UPDATE`,
		profileID, lessonID)
}

// NewRun is what a lesson run begins with: the lesson, its module and version,
// the package, and the engine's choice that opens it.
type NewRun struct {
	LessonID      string
	ModuleID      string
	ModuleVersion string
	PackageDigest string
	ChoiceID      string
}

// BeginLessonRun begins the run r at at, in tx, and gives its ID. A profile that
// has an open run of the lesson already is ErrRunBegun, and tx can go on: the
// insert runs in a savepoint of its own.
func BeginLessonRun(ctx context.Context, tx pgx.Tx, profileID string, r NewRun, at time.Time) (string, error) {
	var id string
	err := pgx.BeginFunc(ctx, tx, func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `INSERT INTO lesson_runs (profile_id, lesson_id, module_id, module_version,
				package_digest, choice_id, started_at, last_action_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $7) RETURNING id`,
			profileID, r.LessonID, r.ModuleID, r.ModuleVersion, r.PackageDigest, r.ChoiceID, at).Scan(&id)
	})
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == uniqueViolation &&
		(pgErr.ConstraintName == "lesson_runs_one_open" || pgErr.ConstraintName == "lesson_runs_choice_id_key"):
		return "", ErrRunBegun
	case err != nil:
		return "", fmt.Errorf("store: begin the lesson run: %w", err)
	}
	return id, nil
}

// EndLessonRun ends the profile's open run with reason at at; one ended already
// keeps its end. A run that ends "not suitable" closes its open attempt "not
// suitable" with it (R10, R13).
func EndLessonRun(ctx context.Context, tx pgx.Tx, profileID, runID, reason string, at time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE lesson_runs SET end_reason = $3, ended_at = $4
		WHERE profile_id = $1 AND id = $2 AND ended_at IS NULL`, profileID, runID, reason, at); err != nil {
		return fmt.Errorf("store: end the lesson run: %w", err)
	}
	if reason != RunNotSuitable {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE attempts SET close_reason = 'not_suitable', closed_at = $3
		WHERE profile_id = $1 AND lesson_run_id = $2 AND closed_at IS NULL`, profileID, runID, at); err != nil {
		return fmt.Errorf("store: close the run's attempt: %w", err)
	}
	return nil
}

// TouchLessonRun records a request on the profile's open run at at: its last
// action (R13). A request on an attempt of the run is its attempt's action, which
// the run's last action reads too.
func TouchLessonRun(ctx context.Context, tx pgx.Tx, profileID, runID string, at time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE lesson_runs SET last_action_at = $3
		WHERE profile_id = $1 AND id = $2 AND ended_at IS NULL`, profileID, runID, at); err != nil {
		return fmt.Errorf("store: record the run's action: %w", err)
	}
	return nil
}

// EndedSegments is the places of the run's task segments that have ended, in
// order: those whose attempt was closed by a correct answer, by the solution
// shown or by "Done" (R13).
func EndedSegments(ctx context.Context, tx pgx.Tx, profileID, runID string) ([]int, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT segment FROM attempts
		WHERE profile_id = $1 AND lesson_run_id = $2 AND close_reason IN ('correct', 'solution_shown', 'done')
		ORDER BY segment`, profileID, runID)
	if err != nil {
		return nil, fmt.Errorf("store: read the ended segments: %w", err)
	}
	ended, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return nil, fmt.Errorf("store: read the ended segments: %w", err)
	}
	return ended, nil
}

// RunAttempt is the run's open attempt and the place of its segment, and
// whether it has one.
func RunAttempt(ctx context.Context, tx pgx.Tx, profileID, runID string) (OpenAttempt, bool, error) {
	var a OpenAttempt
	err := tx.QueryRow(ctx, `SELECT id, lesson_run_id, segment FROM attempts
		WHERE profile_id = $1 AND lesson_run_id = $2 AND closed_at IS NULL`, profileID, runID).
		Scan(&a.ID, &a.LessonRunID, &a.Segment)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return OpenAttempt{}, false, nil
	case err != nil:
		return OpenAttempt{}, false, fmt.Errorf("store: read the run's open attempt: %w", err)
	}
	return a, true, nil
}

// OpenLessonRun is a profile's open lesson run, as the list of profiles names
// it: its ID, the lesson's full ID and its package.
type OpenLessonRun struct {
	ID            string
	LessonID      string
	PackageDigest string
}

// openLessonRuns is the open lesson runs of each of the account's profiles that
// has some, by the profile's ID, in the order they began.
func openLessonRuns(ctx context.Context, tx pgx.Tx, accountID string) (map[string][]OpenLessonRun, error) {
	rows, err := tx.Query(ctx, `SELECT r.profile_id, r.id, r.lesson_id, r.package_digest FROM lesson_runs r
		JOIN profiles p ON p.id = r.profile_id WHERE p.account_id = $1 AND r.ended_at IS NULL
		ORDER BY r.started_at, r.id`, accountID)
	if err != nil {
		return nil, fmt.Errorf("store: read the open lesson runs: %w", err)
	}
	open := map[string][]OpenLessonRun{}
	var profileID string
	var r OpenLessonRun
	_, err = pgx.ForEachRow(rows, []any{&profileID, &r.ID, &r.LessonID, &r.PackageDigest}, func() error {
		open[profileID] = append(open[profileID], r)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store: read the open lesson runs: %w", err)
	}
	return open, nil
}

// RunLesson is the full ID of the lesson of the profile's run, read with no
// lock: what a run is of never changes.
func RunLesson(ctx context.Context, db Querier, profileID, runID string) (string, error) {
	var lesson string
	if err := db.QueryRow(ctx, `SELECT lesson_id FROM lesson_runs WHERE profile_id = $1 AND id = $2`,
		profileID, runID).Scan(&lesson); err != nil {
		return "", fmt.Errorf("store: read the run's lesson: %w", err)
	}
	return lesson, nil
}

// HoldAttemptRun holds FOR UPDATE, in tx, the lesson run of the profile's
// attempt, when it is one of a run, or of the profile's open attempt when
// attemptID is empty: a request on an attempt holds its run first, as the
// requests on the run do, so that the close of the attempt, which may end the
// run, takes the two rows in the order every request takes them (R48).
func HoldAttemptRun(ctx context.Context, tx pgx.Tx, profileID, attemptID string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT r.id FROM lesson_runs r JOIN attempts a ON a.profile_id = r.profile_id AND a.lesson_run_id = r.id
		WHERE a.profile_id = $1 AND (a.id = nullif($2, '')::uuid OR ($2 = '' AND a.closed_at IS NULL))
		FOR UPDATE OF r`, profileID, attemptID).Scan(&id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("store: hold the attempt's run: %w", err)
	}
	return nil
}
