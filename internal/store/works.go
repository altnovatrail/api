package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrStorageOff is a work put while the storage of the profile's works is off
// (R113 spec).
var ErrStorageOff = errors.New("store: the storage of the profile's works is off")

// The kinds of a stored work, by the attempt's task (contract, WorkKind).
const (
	WorkHandwriting = "handwriting"
	WorkEssay       = "essay"
	WorkDrawing     = "drawing"
)

// Work is a stored work of a profile (R70): the attempt whose work it is, what
// it is, its size as the contract counts it, when api kept it, and, where it is
// read whole, its bytes — the contract's StoredWork in binary protobuf, which
// the store does not read.
type Work struct {
	AttemptID string
	Kind      string
	SizeBytes int64
	StoredAt  time.Time
	Bytes     []byte
}

// PutWork keeps w in tx, in the place of the work kept of its attempt before,
// while the storage of the profile's works is on: ErrStorageOff when it is off,
// ErrNoProfile when the account has no such profile, ErrNotFound when the
// attempt is not the profile's. The profile's row is held FOR SHARE, which the
// UPDATE of SwitchWorkStorageOff waits for and waits on: a work put before the
// switch is erased by it, and one after it is refused. FOR KEY SHARE would not
// do: that UPDATE takes FOR NO KEY UPDATE, which does not conflict with it.
func PutWork(ctx context.Context, tx pgx.Tx, accountID, profileID string, w Work) error {
	var on bool
	err := tx.QueryRow(ctx, `SELECT work_storage FROM profiles WHERE id = $1 AND account_id = $2 FOR SHARE`,
		profileID, accountID).Scan(&on)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNoProfile
	case err != nil:
		return fmt.Errorf("store: hold the profile: %w", err)
	case !on:
		return ErrStorageOff
	}
	_, err = tx.Exec(ctx, `INSERT INTO works (profile_id, attempt_id, kind, size_bytes, work, stored_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (profile_id, attempt_id) DO UPDATE
		SET kind = EXCLUDED.kind, size_bytes = EXCLUDED.size_bytes, work = EXCLUDED.work, stored_at = EXCLUDED.stored_at`,
		profileID, w.AttemptID, w.Kind, w.SizeBytes, w.Bytes, w.StoredAt)
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation:
		return ErrNotFound
	case err != nil:
		return fmt.Errorf("store: keep the work: %w", err)
	}
	return nil
}

// ListWorks is the profile's stored works, newest first, without their bytes.
func ListWorks(ctx context.Context, db Lister, profileID string) ([]Work, error) {
	rows, err := db.Query(ctx, `SELECT attempt_id, kind, size_bytes, stored_at FROM works WHERE profile_id = $1
		ORDER BY stored_at DESC, attempt_id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("store: list the works: %w", err)
	}
	works, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Work, error) {
		var w Work
		err := r.Scan(&w.AttemptID, &w.Kind, &w.SizeBytes, &w.StoredAt)
		return w, err
	})
	if err != nil {
		return nil, fmt.Errorf("store: list the works: %w", err)
	}
	return works, nil
}

// GetWork is the profile's stored work of the attempt, with its bytes, or
// ErrNotFound.
func GetWork(ctx context.Context, db Querier, profileID, attemptID string) (Work, error) {
	w := Work{AttemptID: attemptID}
	err := db.QueryRow(ctx, `SELECT kind, size_bytes, stored_at, work FROM works WHERE profile_id = $1 AND attempt_id = $2`,
		profileID, attemptID).Scan(&w.Kind, &w.SizeBytes, &w.StoredAt, &w.Bytes)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Work{}, ErrNotFound
	case err != nil:
		return Work{}, fmt.Errorf("store: read the work: %w", err)
	}
	return w, nil
}

// KeptWorkKind is the kind of the profile's stored work of the attempt, or
// ErrNotFound when none is kept.
func KeptWorkKind(ctx context.Context, db Querier, profileID, attemptID string) (string, error) {
	var kind string
	err := db.QueryRow(ctx, `SELECT kind FROM works WHERE profile_id = $1 AND attempt_id = $2`,
		profileID, attemptID).Scan(&kind)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", ErrNotFound
	case err != nil:
		return "", fmt.Errorf("store: read the work's kind: %w", err)
	}
	return kind, nil
}

// DeleteWork erases the profile's stored work of the attempt, when one is kept.
func DeleteWork(ctx context.Context, db Querier, profileID, attemptID string) error {
	if _, err := db.Exec(ctx, `DELETE FROM works WHERE profile_id = $1 AND attempt_id = $2`, profileID, attemptID); err != nil {
		return fmt.Errorf("store: erase the work: %w", err)
	}
	return nil
}

// WorkDecider is who gave the consent of the account's profile, and so decides
// the storage of its works (R113 spec), read without a lock, or ErrNoProfile:
// a profile has a consent always. A lock on the consent would wait in another
// order than the deletion of the profile, which holds the profile first.
func WorkDecider(ctx context.Context, db Querier, accountID, profileID string) (string, error) {
	var giver string
	err := db.QueryRow(ctx, `SELECT giver FROM consents WHERE profile_id = $1 AND account_id = $2`,
		profileID, accountID).Scan(&giver)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", ErrNoProfile
	case err != nil:
		return "", fmt.Errorf("store: read the consent's giver: %w", err)
	}
	return giver, nil
}
