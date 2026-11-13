package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Record is an attempt in the progress of a profile (contract, AttemptRecord):
// what it was of, on which package, the lesson of its run, how it closed, when
// it began and closed, and when the engine chose its task — or its run's
// lesson — and why: none for an attempt begun again, or by a choice written
// before the codes.
type Record struct {
	ID            string
	PackageDigest string
	TaskID        string
	ModuleID      string
	ModuleVersion string
	LessonID      *string
	CloseReason   *string
	WrongAnswers  int
	StartedAt     time.Time
	ClosedAt      *time.Time
	ChosenAt      *time.Time
	ReasonCode    *int
}

// Cursor is where a page of the progress goes on from: the attempt last on the
// page before, by its beginning and its ID.
type Cursor struct {
	StartedAt time.Time
	ID        string
}

// ProgressPage is up to n of the profile's attempts, newest first, after the
// cursor when it is given.
func ProgressPage(ctx context.Context, db Lister, profileID string, after *Cursor, n int) ([]Record, error) {
	var at *time.Time
	var id *string
	if after != nil {
		at, id = &after.StartedAt, &after.ID
	}
	rows, err := db.Query(ctx, `SELECT a.id, a.package_digest, a.task_id, a.module_id, a.module_version, r.lesson_id, a.close_reason,
			a.wrong_answers, a.started_at, a.closed_at, c.chosen_at, c.reason_code
		FROM attempts a LEFT JOIN lesson_runs r ON r.profile_id = a.profile_id AND r.id = a.lesson_run_id
		LEFT JOIN engine_choices c ON c.profile_id = a.profile_id AND c.id = coalesce(a.choice_id, r.choice_id)
			AND c.reason_code IS NOT NULL
		WHERE a.profile_id = $1 AND ($2::timestamptz IS NULL OR (a.started_at, a.id) < ($2, $3::uuid))
		ORDER BY a.started_at DESC, a.id DESC LIMIT $4`, profileID, at, id, n)
	if err != nil {
		return nil, fmt.Errorf("store: read the progress: %w", err)
	}
	records, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Record])
	if err != nil {
		return nil, fmt.Errorf("store: read the progress: %w", err)
	}
	return records, nil
}
