package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// AbandonedAfter is how long an open attempt goes with no action before it is
// abandoned (R10): the check of a request closes it then, and so does the daily
// job.
const AbandonedAfter = 7 * 24 * time.Hour

// uniqueViolation is the SQLSTATE of a row that a unique index refuses.
const uniqueViolation = "23505"

// The refusals of the rows of an attempt.
var (
	// ErrNotFound is an attempt, a lesson run or an engine's choice that the
	// profile does not have: another profile's is the same (R04).
	ErrNotFound = errors.New("store: the profile has no such row")
	// ErrChoiceBegun is an engine's choice of a task that has begun an attempt
	// already (contract, BeginAttempt).
	ErrChoiceBegun = errors.New("store: the choice has begun an attempt already")
)

// Choice is what an engine's choice names, as far as the beginning of an
// attempt reads it: its kind, and the module and task of a choice of a task.
type Choice struct {
	Kind     string
	ModuleID string
	TaskID   string
}

// ChoiceOf is the profile's engine's choice by its ID, or ErrNotFound.
func ChoiceOf(ctx context.Context, db Querier, profileID, choiceID string) (Choice, error) {
	var c Choice
	var module, task *string
	err := db.QueryRow(ctx, `SELECT kind, module_id, task_id FROM engine_choices WHERE profile_id = $1 AND id = $2`,
		profileID, choiceID).Scan(&c.Kind, &module, &task)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Choice{}, ErrNotFound
	case err != nil:
		return Choice{}, fmt.Errorf("store: read the choice: %w", err)
	}
	// Either may be there without the other on a choice of a lesson.
	if module != nil {
		c.ModuleID = *module
	}
	if task != nil {
		c.TaskID = *task
	}
	return c, nil
}

// LessonRunThere says whether the profile has the lesson run, open or ended.
func LessonRunThere(ctx context.Context, db Querier, profileID, runID string) (bool, error) {
	var there bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM lesson_runs WHERE profile_id = $1 AND id = $2)`,
		profileID, runID).Scan(&there)
	if err != nil {
		return false, fmt.Errorf("store: read the lesson run: %w", err)
	}
	return there, nil
}

// Attempt is an attempt's row, as the requests on it read it.
type Attempt struct {
	ID             string
	PackageDigest  string
	ModuleID       string
	ModuleVersion  string
	FormatVersion  string
	ModuleKit      string
	TaskID         string
	Seed           uint64
	Instance       *int
	ChoiceID       *string
	LessonRunID    *string
	Segment        *int
	PreviousID     *string
	LastActionAt   time.Time
	CloseReason    *string
	WrongAnswers   int
	StepTechniques bool
	// RequestedTechniques and LeadingQuestion say whether a hint asked for gave
	// the task's techniques, and its leading question (R47).
	RequestedTechniques bool
	LeadingQuestion     bool
}

// attemptColumns are the columns of an Attempt, in scanAttempt's order.
const attemptColumns = `id, package_digest, module_id, module_version, module_format_version,
	module_kit_version, task_id, seed, instance, choice_id, lesson_run_id, segment, previous_attempt_id,
	last_action_at, close_reason, wrong_answers, step_techniques_at IS NOT NULL,
	requested_techniques_at IS NOT NULL, leading_question_at IS NOT NULL`

func scanAttempt(row pgx.Row) (Attempt, error) {
	var a Attempt
	var seed int64
	err := row.Scan(&a.ID, &a.PackageDigest, &a.ModuleID, &a.ModuleVersion, &a.FormatVersion, &a.ModuleKit,
		&a.TaskID, &seed, &a.Instance, &a.ChoiceID, &a.LessonRunID, &a.Segment, &a.PreviousID, &a.LastActionAt,
		&a.CloseReason, &a.WrongAnswers, &a.StepTechniques, &a.RequestedTechniques, &a.LeadingQuestion)
	// The seed is module-kit's uint64, kept as the bits of a bigint.
	a.Seed = uint64(seed)
	return a, err
}

// HoldAttempt is the profile's attempt by its ID, its row held FOR UPDATE in
// tx, so that the requests on one attempt go one after the other; ErrNotFound
// when the profile has none.
func HoldAttempt(ctx context.Context, tx pgx.Tx, profileID, attemptID string) (Attempt, error) {
	a, err := scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptColumns+` FROM attempts
		WHERE profile_id = $1 AND id = $2 FOR UPDATE`, profileID, attemptID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Attempt{}, ErrNotFound
	case err != nil:
		return Attempt{}, fmt.Errorf("store: read the attempt: %w", err)
	}
	return a, nil
}

// AttemptTask is the package and the task of the profile's attempt, read with
// no lock: what an attempt is of never changes, so a request may choose its way
// by it before it holds the attempt.
func AttemptTask(ctx context.Context, db Querier, profileID, attemptID string) (digest, taskID string, err error) {
	err = db.QueryRow(ctx, `SELECT package_digest, task_id FROM attempts WHERE profile_id = $1 AND id = $2`,
		profileID, attemptID).Scan(&digest, &taskID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", "", ErrNotFound
	case err != nil:
		return "", "", fmt.Errorf("store: read the attempt's task: %w", err)
	}
	return digest, taskID, nil
}

// NewAttempt is what an attempt begins with: its package, the versions its
// verdict repeats by, its task, seed and instance, and one of the three ways it
// begins.
type NewAttempt struct {
	PackageDigest string
	ModuleID      string
	ModuleVersion string
	FormatVersion string
	ModuleKit     string
	TaskID        string
	Seed          uint64
	Instance      *int
	ChoiceID      *string
	PreviousID    *string
}

// HoldOpenAttempt is the profile's open attempt, its row held FOR UPDATE in tx,
// and whether it has one.
func HoldOpenAttempt(ctx context.Context, tx pgx.Tx, profileID string) (Attempt, bool, error) {
	a, err := scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptColumns+` FROM attempts
		WHERE profile_id = $1 AND closed_at IS NULL FOR UPDATE`, profileID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Attempt{}, false, nil
	case err != nil:
		return Attempt{}, false, fmt.Errorf("store: read the open attempt: %w", err)
	}
	return a, true, nil
}

// BeginAttempt begins a at at, in tx, once the profile's open attempt is closed:
// the profile has one open attempt (R10). A choice that has begun an attempt
// already is ErrChoiceBegun.
func BeginAttempt(ctx context.Context, tx pgx.Tx, profileID string, a NewAttempt, at time.Time) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `INSERT INTO attempts (profile_id, package_digest, module_id, module_version,
			module_format_version, module_kit_version, task_id, seed, instance, choice_id, previous_attempt_id,
			started_at, last_action_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12) RETURNING id`,
		profileID, a.PackageDigest, a.ModuleID, a.ModuleVersion, a.FormatVersion, a.ModuleKit, a.TaskID,
		int64(a.Seed), a.Instance, a.ChoiceID, a.PreviousID, at).Scan(&id)
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "attempts_choice_id_key":
		return "", ErrChoiceBegun
	case err != nil:
		return "", fmt.Errorf("store: begin the attempt: %w", err)
	}
	return id, nil
}

// ChoiceBegun says whether the engine's choice has begun an attempt.
func ChoiceBegun(ctx context.Context, db Querier, profileID, choiceID string) (bool, error) {
	var begun bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM attempts WHERE profile_id = $1 AND choice_id = $2)`,
		profileID, choiceID).Scan(&begun)
	if err != nil {
		return false, fmt.Errorf("store: read the choice's attempt: %w", err)
	}
	return begun, nil
}

// CloseAttempt closes the open attempt with reason at at; one closed already
// keeps its close.
func CloseAttempt(ctx context.Context, tx pgx.Tx, profileID, attemptID, reason string, at time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE attempts SET close_reason = $3, closed_at = $4
		WHERE profile_id = $1 AND id = $2 AND closed_at IS NULL`, profileID, attemptID, reason, at); err != nil {
		return fmt.Errorf("store: close the attempt: %w", err)
	}
	return nil
}

// Submit records a submission with a verdict of the attempt, by its answer and
// the observation of its work, as the attempt's next, and counts it a wrong
// answer when wrong: the attempt's last action is at. It gives the submission's
// ID.
func Submit(ctx context.Context, tx pgx.Tx, profileID, attemptID string, answer, observation []byte, wrong bool, at time.Time) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `INSERT INTO submissions (profile_id, attempt_id, number, submitted_at, answer, observation)
		SELECT $1, $2, coalesce(max(number), 0) + 1, $3, $4, $5 FROM submissions WHERE profile_id = $1 AND attempt_id = $2
		RETURNING id`, profileID, attemptID, at, answer, observation).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("store: record the submission: %w", err)
	}
	wrongs := 0
	if wrong {
		wrongs = 1
	}
	if _, err := tx.Exec(ctx, `UPDATE attempts SET wrong_answers = wrong_answers + $3, last_action_at = $4
		WHERE profile_id = $1 AND id = $2`, profileID, attemptID, wrongs, at); err != nil {
		return "", fmt.Errorf("store: count the submission: %w", err)
	}
	return id, nil
}

// LastSubmission is the answer of the attempt's last submission with a
// verdict, and whether it has one.
func LastSubmission(ctx context.Context, db Querier, profileID, attemptID string) ([]byte, bool, error) {
	var answer []byte
	err := db.QueryRow(ctx, `SELECT answer FROM submissions WHERE profile_id = $1 AND attempt_id = $2
		ORDER BY number DESC LIMIT 1`, profileID, attemptID).Scan(&answer)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, false, nil
	case err != nil:
		return nil, false, fmt.Errorf("store: read the last submission: %w", err)
	}
	return answer, true, nil
}

// SubmissionAnswer is the answer of the profile's submission by its ID.
func SubmissionAnswer(ctx context.Context, db Querier, profileID, submissionID string) ([]byte, error) {
	var answer []byte
	if err := db.QueryRow(ctx, `SELECT answer FROM submissions WHERE profile_id = $1 AND id = $2`,
		profileID, submissionID).Scan(&answer); err != nil {
		return nil, fmt.Errorf("store: read the submission: %w", err)
	}
	return answer, nil
}

// GiveStepTechniques records that the techniques of the attempt's task were
// given at a step, the first time they were, and that the attempt's last action
// is at.
func GiveStepTechniques(ctx context.Context, tx pgx.Tx, profileID, attemptID string, at time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE attempts SET step_techniques_at = coalesce(step_techniques_at, $3),
		last_action_at = $3 WHERE profile_id = $1 AND id = $2`, profileID, attemptID, at); err != nil {
		return fmt.Errorf("store: record the techniques given: %w", err)
	}
	return nil
}

// OpenAttempt is a profile's open attempt, as the list of profiles names it:
// its ID, and its lesson run and segment when it is one of a run.
type OpenAttempt struct {
	ID          string
	LessonRunID *string
	Segment     *int
}

// openAttempts is the open attempt of each of the account's profiles that has
// one, by the profile's ID.
func openAttempts(ctx context.Context, tx pgx.Tx, accountID string) (map[string]OpenAttempt, error) {
	rows, err := tx.Query(ctx, `SELECT a.profile_id, a.id, a.lesson_run_id, a.segment FROM attempts a
		JOIN profiles p ON p.id = a.profile_id WHERE p.account_id = $1 AND a.closed_at IS NULL`, accountID)
	if err != nil {
		return nil, fmt.Errorf("store: read the open attempts: %w", err)
	}
	open := map[string]OpenAttempt{}
	var profileID string
	var a OpenAttempt
	_, err = pgx.ForEachRow(rows, []any{&profileID, &a.ID, &a.LessonRunID, &a.Segment}, func() error {
		open[profileID] = a
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store: read the open attempts: %w", err)
	}
	return open, nil
}
