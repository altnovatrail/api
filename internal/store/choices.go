package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// NewChoice is an engine's choice as it is written (R06, R50; R107 spec): what
// it gives — a task, a lesson or nothing, with the outcome "not admitted" or
// none —, its reason code, the skills it handed out and the package it was made
// on, and the input a replay needs — the skill chosen on the skill map, the age
// category and the grade of the profile, whether the pool was limited to the
// tasks a tap checks, the versions, the release and the window, the number of
// the last evidence read, and the digests of the graphs —, with the close or the
// end of a run whose next it is, when it is one.
type NewChoice struct {
	Kind        string
	ModuleID    string
	TaskID      string
	LessonID    string
	NotAdmitted bool
	// Closed is why the AI that checks the tasks was closed, beside nothing to give
	// when it left a task out: a word of NothingCauses, with NextDay beside the
	// quota (R59).
	Closed        string
	NextDay       *time.Time
	ReasonCode    int
	IssuedSkills  []string
	PackageDigest string
	MapSkill      string
	AgeCategory   string
	Grade         *int
	PoolLimited   bool
	Mark          Mark
	EngineVersion string
	LastSeq       int64
	Digests       []string
	EngineDigests []string
	// AfterAttemptID and AfterLessonRunID name the close of an attempt or the end
	// of a run that gave the choice as what comes next.
	AfterAttemptID   *string
	AfterLessonRunID *string
}

// WriteChoice writes the profile's choice c, made at at, and gives its ID.
func WriteChoice(ctx context.Context, tx pgx.Tx, profileID string, c NewChoice, at time.Time) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `INSERT INTO engine_choices (profile_id, kind, chosen_at, module_id, task_id, lesson_id,
			reason_code, issued_skills, package_digest, map_skill, age_category, grade, pool_limited, engine_version,
			learner_model_version, modules_release, format_window, last_seq, digests, engine_digests,
			after_attempt_id, after_lesson_run_id, not_admitted, nothing_cause, next_family_day)
		VALUES ($1, $2, $3, nullif($4, ''), nullif($5, ''), nullif($6, ''), $7, $8, nullif($9, ''), nullif($10, ''), $11, $12,
			$13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, nullif($24, ''), $25) RETURNING id`,
		profileID, c.Kind, at, c.ModuleID, c.TaskID, c.LessonID, c.ReasonCode, c.IssuedSkills, c.PackageDigest,
		c.MapSkill, c.AgeCategory, c.Grade, c.PoolLimited, c.EngineVersion, c.Mark.LearnerModel, c.Mark.Release,
		c.Mark.Window, c.LastSeq, c.Digests, c.EngineDigests, c.AfterAttemptID, c.AfterLessonRunID, c.NotAdmitted, c.Closed, c.NextDay).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("store: write the choice: %w", err)
	}
	return id, nil
}

// MadeChoice is a choice written: its ID and kind, and, beside nothing to give,
// whether its answer said the AI was closed — and why, by a word of
// NothingCauses, with when the next family day begins beside the quota.
type MadeChoice struct {
	ID          string
	Kind        string
	NotAdmitted bool
	Closed      string
	NextDay     *time.Time
}

// The causes of a choice of nothing to give whose pool the closed AI narrowed.
const (
	NothingNotAdmitted   = "not_admitted"
	NothingQuota         = "quota"
	NothingPlatformLimit = "platform_limit"
)

// ChoiceAfterAttempt is the choice the close of the profile's attempt gave as
// what comes next, and whether it gave one: a repeat of the request that closed
// it gives it again.
func ChoiceAfterAttempt(ctx context.Context, db Querier, profileID, attemptID string) (MadeChoice, bool, error) {
	return madeChoice(ctx, db, `after_attempt_id`, profileID, attemptID)
}

// ChoiceAfterRun is the choice the end of the profile's lesson run gave, and
// whether it gave one.
func ChoiceAfterRun(ctx context.Context, db Querier, profileID, runID string) (MadeChoice, bool, error) {
	return madeChoice(ctx, db, `after_lesson_run_id`, profileID, runID)
}

func madeChoice(ctx context.Context, db Querier, column, profileID, id string) (MadeChoice, bool, error) {
	var c MadeChoice
	var closed *string
	err := db.QueryRow(ctx, `SELECT id, kind, coalesce(not_admitted, false), nothing_cause, next_family_day FROM engine_choices
		WHERE profile_id = $1 AND `+column+` = $2`, profileID, id).Scan(&c.ID, &c.Kind, &c.NotAdmitted, &closed, &c.NextDay)
	c.Closed = deref(closed)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return MadeChoice{}, false, nil
	case err != nil:
		return MadeChoice{}, false, fmt.Errorf("store: read the choice given: %w", err)
	}
	return c, true, nil
}

// HeldMapSkill is the skill chosen on the skill map that the profile's last
// choice holds, empty when it holds none: a choice from the map holds until the
// learner asks for something else (R107 spec).
func HeldMapSkill(ctx context.Context, db Querier, profileID string) (string, error) {
	var skill *string
	err := db.QueryRow(ctx, `SELECT map_skill FROM engine_choices WHERE profile_id = $1 AND reason_code IS NOT NULL
		ORDER BY chosen_at DESC, id DESC LIMIT 1`, profileID).Scan(&skill)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("store: read the skill held: %w", err)
	}
	return deref(skill), nil
}

// Issued is when a skill was last handed out by a choice, by its full ID.
type Issued struct {
	Skill string
	At    time.Time
}

// IssuedSkills is when each skill the profile's choices handed out was last
// handed out: a lesson hands out each of its skills (R104 spec).
func IssuedSkills(ctx context.Context, db Lister, profileID string) ([]Issued, error) {
	rows, err := db.Query(ctx, `SELECT s, max(c.chosen_at) FROM engine_choices c, unnest(c.issued_skills) s
		WHERE c.profile_id = $1 GROUP BY s ORDER BY s`, profileID)
	if err != nil {
		return nil, fmt.Errorf("store: read the skills handed out: %w", err)
	}
	issued, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Issued])
	if err != nil {
		return nil, fmt.Errorf("store: read the skills handed out: %w", err)
	}
	return issued, nil
}

// ModuleChoices is how many choices of a module the profile had, a lesson's
// counted once.
type ModuleChoices struct {
	ModuleID string
	Choices  int64
}

// ChoicesByModule is how many choices the profile had of each module.
func ChoicesByModule(ctx context.Context, db Lister, profileID string) ([]ModuleChoices, error) {
	rows, err := db.Query(ctx, `SELECT module_id, count(*) FROM engine_choices
		WHERE profile_id = $1 AND module_id IS NOT NULL GROUP BY module_id ORDER BY module_id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("store: read the choices of the modules: %w", err)
	}
	counts, err := pgx.CollectRows(rows, pgx.RowToStructByPos[ModuleChoices])
	if err != nil {
		return nil, fmt.Errorf("store: read the choices of the modules: %w", err)
	}
	return counts, nil
}

// Solution is when the solution of a task of a module last went to the
// profile's device: an attempt of it closed correct or with the solution shown
// (R82 spec).
type Solution struct {
	ModuleID string
	TaskID   string
	At       time.Time
}

// SolutionsSent is when the solution of each of the profile's tasks last went
// to its device.
func SolutionsSent(ctx context.Context, db Lister, profileID string) ([]Solution, error) {
	rows, err := db.Query(ctx, `SELECT module_id, task_id, max(closed_at) FROM attempts
		WHERE profile_id = $1 AND close_reason IN ('correct', 'solution_shown')
		GROUP BY module_id, task_id ORDER BY module_id, task_id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("store: read the solutions sent: %w", err)
	}
	solutions, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Solution])
	if err != nil {
		return nil, fmt.Errorf("store: read the solutions sent: %w", err)
	}
	return solutions, nil
}

// ClosedAttempt is a closed attempt of the profile as the engine's input reads
// it: what it was of, how and when it closed, the full ID of the lesson of its
// run, the reason code of the choice that began it — of its run's, for a
// segment; none for an attempt begun again or by a choice written before the
// codes —, and its evidence, when the journal has it.
type ClosedAttempt struct {
	ID            string
	PackageDigest string
	ModuleID      string
	TaskID        string
	CloseReason   string
	ClosedAt      time.Time
	LessonID      *string
	ReasonCode    *int
	Evidence      []byte
}

// ClosedAttempts is the profile's closed attempts, the last closed first.
func ClosedAttempts(ctx context.Context, db Lister, profileID string) ([]ClosedAttempt, error) {
	rows, err := db.Query(ctx, `SELECT a.id, a.package_digest, a.module_id, a.task_id, a.close_reason, a.closed_at,
			r.lesson_id, c.reason_code, e.evidence
		FROM attempts a
		LEFT JOIN lesson_runs r ON r.profile_id = a.profile_id AND r.id = a.lesson_run_id
		LEFT JOIN engine_choices c ON c.profile_id = a.profile_id AND c.id = coalesce(a.choice_id, r.choice_id)
		LEFT JOIN evidence e ON e.profile_id = a.profile_id AND e.attempt_id = a.id
		WHERE a.profile_id = $1 AND a.closed_at IS NOT NULL ORDER BY a.closed_at DESC, a.id DESC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("store: read the closed attempts: %w", err)
	}
	closed, err := pgx.CollectRows(rows, pgx.RowToStructByPos[ClosedAttempt])
	if err != nil {
		return nil, fmt.Errorf("store: read the closed attempts: %w", err)
	}
	return closed, nil
}

// Run is a lesson run of the profile as the engine's input reads it: its
// lesson, its package, and how and when it ended, while it has.
type Run struct {
	LessonID      string
	ModuleID      string
	PackageDigest string
	EndReason     *string
	EndedAt       *time.Time
}

// ProfileRuns is the profile's lesson runs, in the order they began.
func ProfileRuns(ctx context.Context, db Lister, profileID string) ([]Run, error) {
	rows, err := db.Query(ctx, `SELECT lesson_id, module_id, package_digest, end_reason, ended_at FROM lesson_runs
		WHERE profile_id = $1 ORDER BY started_at, id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("store: read the lesson runs: %w", err)
	}
	runs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Run])
	if err != nil {
		return nil, fmt.Errorf("store: read the lesson runs: %w", err)
	}
	return runs, nil
}
