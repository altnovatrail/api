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
// abandoned: the check of a request closes it then, and so does the daily job.
const AbandonedAfter = 7 * 24 * time.Hour

// uniqueViolation is the SQLSTATE of a row that a unique index refuses.
const uniqueViolation = "23505"

// The refusals of the rows of an attempt.
var (
	// ErrNotFound is an attempt, a lesson run or an engine's choice that the
	// profile does not have: another profile's is the same.
	ErrNotFound = errors.New("store: the profile has no such row")
	// ErrChoiceBegun is an engine's choice of a task that has begun an attempt
	// already.
	ErrChoiceBegun = errors.New("store: the choice has begun an attempt already")
	// ErrPaper is an attempt of a task from paper, which has no package and no
	// task of a module.
	ErrPaper = errors.New("store: the attempt is of a task from paper")
)

// Choice is what an engine's choice names, as far as the beginning of an
// attempt or a lesson run reads it: its kind, its module, and the task of a
// choice of a task or the lesson's local ID of a choice of a lesson.
type Choice struct {
	Kind     string
	ModuleID string
	TaskID   string
	LessonID string
}

// ChoiceOf is the profile's engine's choice by its ID, or ErrNotFound.
func ChoiceOf(ctx context.Context, db Querier, profileID, choiceID string) (Choice, error) {
	var c Choice
	var module, task, lesson *string
	err := db.QueryRow(ctx, `SELECT kind, module_id, task_id, lesson_id FROM engine_choices WHERE profile_id = $1 AND id = $2`,
		profileID, choiceID).Scan(&c.Kind, &module, &task, &lesson)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Choice{}, ErrNotFound
	case err != nil:
		return Choice{}, fmt.Errorf("store: read the choice: %w", err)
	}
	c.ModuleID, c.TaskID, c.LessonID = deref(module), deref(task), deref(lesson)
	return c, nil
}

// Attempt is an attempt's row, as the requests on it read it. One of a task
// from paper has Paper, and the fields of a module, its package, its task and
// its seed empty.
type Attempt struct {
	ID             string
	Paper          bool
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
	// the task's techniques, and its leading question.
	RequestedTechniques bool
	LeadingQuestion     bool
	// CorrectWordsGiven says whether the tutor's words after a correct answer were
	// given on it.
	CorrectWordsGiven bool
	// PaperKey is the key of an attempt of a task from paper, the message Key of
	// novatrail.paper.v1, while the attempt is open: nil before the first
	// submission derived it.
	PaperKey []byte
	// PaperReading is the reading of the statement of an attempt of a task from
	// paper, the message Reading of novatrail.paper.v1, while the attempt is open:
	// nil before "Recognise" made it.
	PaperReading []byte
}

// attemptColumns are the columns of an Attempt, in scanAttempt's order.
const attemptColumns = `id, kind = 'paper', coalesce(package_digest, ''), coalesce(module_id, ''),
	coalesce(module_version, ''), coalesce(module_format_version, ''), coalesce(module_kit_version, ''),
	coalesce(task_id, ''), coalesce(seed, 0), instance, choice_id, lesson_run_id, segment, previous_attempt_id,
	last_action_at, close_reason, wrong_answers, step_techniques_at IS NOT NULL,
	requested_techniques_at IS NOT NULL, leading_question_at IS NOT NULL, correct_words_given, paper_key, paper_reading`

func scanAttempt(row pgx.Row) (Attempt, error) {
	var a Attempt
	var seed int64
	err := row.Scan(&a.ID, &a.Paper, &a.PackageDigest, &a.ModuleID, &a.ModuleVersion, &a.FormatVersion, &a.ModuleKit,
		&a.TaskID, &seed, &a.Instance, &a.ChoiceID, &a.LessonRunID, &a.Segment, &a.PreviousID, &a.LastActionAt,
		&a.CloseReason, &a.WrongAnswers, &a.StepTechniques, &a.RequestedTechniques, &a.LeadingQuestion,
		&a.CorrectWordsGiven, &a.PaperKey, &a.PaperReading)
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
// by it before it holds the attempt. One of a task from paper is ErrPaper.
func AttemptTask(ctx context.Context, db Querier, profileID, attemptID string) (digest, taskID string, err error) {
	var d, t *string
	err = db.QueryRow(ctx, `SELECT package_digest, task_id FROM attempts WHERE profile_id = $1 AND id = $2`,
		profileID, attemptID).Scan(&d, &t)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", "", ErrNotFound
	case err != nil:
		return "", "", fmt.Errorf("store: read the attempt's task: %w", err)
	case d == nil:
		return "", "", ErrPaper
	}
	return *d, *t, nil
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
	// LessonRunID and Segment are the run and the place of the segment of an
	// attempt of a lesson's task.
	LessonRunID *string
	Segment     *int
}

// HoldOpenAttempt is the profile's open attempt of a task of a module, its row
// held FOR UPDATE in tx, and whether it has one: the open attempts of a page
// from paper stay beside it.
func HoldOpenAttempt(ctx context.Context, tx pgx.Tx, profileID string) (Attempt, bool, error) {
	a, err := scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptColumns+` FROM attempts
		WHERE profile_id = $1 AND closed_at IS NULL AND kind = 'module' FOR UPDATE`, profileID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Attempt{}, false, nil
	case err != nil:
		return Attempt{}, false, fmt.Errorf("store: read the open attempt: %w", err)
	}
	return a, true, nil
}

// BeginAttempt begins a at at, in tx, once the profile's open attempt is
// closed: the profile has one open attempt of a module. A choice that has begun
// an attempt already is ErrChoiceBegun.
func BeginAttempt(ctx context.Context, tx pgx.Tx, profileID string, a NewAttempt, at time.Time) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `INSERT INTO attempts (profile_id, package_digest, module_id, module_version,
			module_format_version, module_kit_version, task_id, seed, instance, choice_id, previous_attempt_id,
			lesson_run_id, segment, started_at, last_action_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $14) RETURNING id`,
		profileID, a.PackageDigest, a.ModuleID, a.ModuleVersion, a.FormatVersion, a.ModuleKit, a.TaskID,
		int64(a.Seed), a.Instance, a.ChoiceID, a.PreviousID, a.LessonRunID, a.Segment, at).Scan(&id)
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "attempts_choice_id_key":
		return "", ErrChoiceBegun
	case err != nil:
		return "", fmt.Errorf("store: begin the attempt: %w", err)
	}
	return id, nil
}

// BeginPaperAttempts closes "left", at at, in tx, the profile's open attempts
// of tasks from paper of another page than pageID, and begins n attempts of
// tasks from paper on it, one of each, their IDs in order. An attempt of paper
// gives no evidence, so the close settles none. Its caller holds the account's
// row FOR NO KEY UPDATE (HoldAccount), so that two beginnings of one profile go
// one after the other and the second sees the first's attempts.
func BeginPaperAttempts(ctx context.Context, tx pgx.Tx, profileID, pageID string, n int, at time.Time) ([]string, error) {
	if _, err := tx.Exec(ctx, `UPDATE attempts SET close_reason = 'left', closed_at = $2
		WHERE profile_id = $1 AND closed_at IS NULL AND kind = 'paper' AND paper_page IS DISTINCT FROM $3`,
		profileID, at, pageID); err != nil {
		return nil, fmt.Errorf("store: close the attempts of the page before: %w", err)
	}
	// One statement, so that the order of the IDs is the order of the tasks.
	rows, err := tx.Query(ctx, `INSERT INTO attempts (profile_id, kind, paper_page, started_at, last_action_at)
		SELECT $1, 'paper', $4, $2, $2 FROM generate_series(1, $3) AS t(n) ORDER BY t.n RETURNING id`,
		profileID, at, n, pageID)
	if err != nil {
		return nil, fmt.Errorf("store: begin the attempts of the page: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("store: begin the attempts of the page: %w", err)
	}
	return ids, nil
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

// SetPaperKey keeps the key of the profile's open attempt of a task from paper,
// the message Key of novatrail.paper.v1, until the attempt closes. An attempt
// that is not of paper, or closed, keeps none.
func SetPaperKey(ctx context.Context, tx pgx.Tx, profileID, attemptID string, key []byte) error {
	tag, err := tx.Exec(ctx, `UPDATE attempts SET paper_key = $3
		WHERE profile_id = $1 AND id = $2 AND kind = 'paper' AND closed_at IS NULL`, profileID, attemptID, key)
	if err != nil {
		return fmt.Errorf("store: keep the key of the attempt: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("store: no open attempt of paper to keep the key of")
	}
	return nil
}

// SetPaperReading keeps the reading of the statement of the profile's open
// attempt of a task from paper, the message Reading of novatrail.paper.v1,
// until the attempt closes. An attempt that is not of paper, or closed, keeps
// none.
func SetPaperReading(ctx context.Context, tx pgx.Tx, profileID, attemptID string, reading []byte) error {
	tag, err := tx.Exec(ctx, `UPDATE attempts SET paper_reading = $3
		WHERE profile_id = $1 AND id = $2 AND kind = 'paper' AND closed_at IS NULL`, profileID, attemptID, reading)
	if err != nil {
		return fmt.Errorf("store: keep the reading of the attempt: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("store: no open attempt of paper to keep the reading of")
	}
	return nil
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

// How a submission of an essay ends, and the source of its words, as the schema
// writes them.
const (
	EssayFeedback   = "feedback"
	EssayUnreadable = "unreadable"
	EssayUnchecked  = "unchecked"
	// The marks of a criterion.
	MarkMet         = "met"
	MarkNotMet      = "not_met"
	MarkNotAssessed = "not_assessed"
	// SourceNoFeedback is the phrase "no feedback this time" in place of words
	// that failed a check; the model's words are SourceModel.
	SourceNoFeedback = "no_feedback"
)

// Essay is how a submission of an essay ended: its end, and beside feedback the
// mark of each criterion by its number in the rubric, from 1, and the source of
// the words. Neither the text read nor the words are in it.
type Essay struct {
	End    string
	Marks  []string
	Source *string
}

// EssaySubmission is a submission of an essay as the server keeps it: when it
// came, and how it ended.
type EssaySubmission struct {
	AttemptID   string
	SubmittedAt time.Time
	Essay
}

// SubmitEssay records a submission of an essay of the attempt, by its answer —
// without the text read and the words of the feedback — and how it ended, as
// the attempt's next, whose last action is at. It gives the submission's ID.
func SubmitEssay(ctx context.Context, tx pgx.Tx, profileID, attemptID string, answer []byte, e Essay, at time.Time) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `INSERT INTO submissions (profile_id, attempt_id, number, submitted_at, answer, essay_end, marks, words_source)
		SELECT $1, $2, coalesce(max(number), 0) + 1, $3, $4, $5, $6, $7 FROM submissions WHERE profile_id = $1 AND attempt_id = $2
		RETURNING id`, profileID, attemptID, at, answer, e.End, e.Marks, e.Source).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("store: record the essay: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE attempts SET last_action_at = $3 WHERE profile_id = $1 AND id = $2`,
		profileID, attemptID, at); err != nil {
		return "", fmt.Errorf("store: count the essay: %w", err)
	}
	return id, nil
}

// EssaySubmissions is the submissions of the profile's attempts of attemptIDs
// that are essays, by attempt and in order.
func EssaySubmissions(ctx context.Context, db Lister, profileID string, attemptIDs []string) ([]EssaySubmission, error) {
	rows, err := db.Query(ctx, `SELECT attempt_id, submitted_at, essay_end, marks, words_source FROM submissions
		WHERE profile_id = $1 AND attempt_id = ANY($2::uuid[]) AND essay_end IS NOT NULL ORDER BY attempt_id, number`,
		profileID, attemptIDs)
	if err != nil {
		return nil, fmt.Errorf("store: read the essays: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (EssaySubmission, error) {
		var e EssaySubmission
		err := r.Scan(&e.AttemptID, &e.SubmittedAt, &e.End, &e.Marks, &e.Source)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("store: read the essays: %w", err)
	}
	return out, nil
}

// Submitted says whether the profile's attempt has a submission, however it
// ended: what "Done" asks of an attempt with a rubric.
func Submitted(ctx context.Context, db Querier, profileID, attemptID string) (bool, error) {
	var submitted bool
	if err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM submissions WHERE profile_id = $1 AND attempt_id = $2)`,
		profileID, attemptID).Scan(&submitted); err != nil {
		return false, fmt.Errorf("store: read whether the attempt has a submission: %w", err)
	}
	return submitted, nil
}

// LastSubmission is the answer of the attempt's last submission with a
// verdict, and whether it has one.
func LastSubmission(ctx context.Context, db Querier, profileID, attemptID string) ([]byte, bool, error) {
	var answer []byte
	err := db.QueryRow(ctx, `SELECT answer FROM submissions WHERE profile_id = $1 AND attempt_id = $2 AND essay_end IS NULL
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

// openAttempts is the open attempt of a module of each of the account's
// profiles that has one, by the profile's ID.
func openAttempts(ctx context.Context, tx pgx.Tx, accountID string) (map[string]OpenAttempt, error) {
	rows, err := tx.Query(ctx, `SELECT a.profile_id, a.id, a.lesson_run_id, a.segment FROM attempts a
		JOIN profiles p ON p.id = a.profile_id WHERE p.account_id = $1 AND a.closed_at IS NULL AND a.kind = 'module'`, accountID)
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
