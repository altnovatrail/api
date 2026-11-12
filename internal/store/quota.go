package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/quota"
)

// The quota of platform AI as the database keeps it (R11, R12; architecture
// 3.6): the account's family day — its offset, the beginning and the end of its
// current day.

// clockOf is the family day the account keeps, as read in q.
func clockOf(ctx context.Context, q Querier, accountID string) (quota.Clock, error) {
	var c quota.Clock
	var start, end *time.Time
	err := q.QueryRow(ctx, `SELECT utc_offset_minutes, day_starts_at, day_ends_at FROM accounts WHERE id = $1`, accountID).
		Scan(&c.Offset, &start, &end)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return quota.Clock{}, ErrNoAccount
	case err != nil:
		return quota.Clock{}, fmt.Errorf("store: read the family day: %w", err)
	}
	if start != nil && end != nil {
		c.Day = &quota.Day{Start: start.UTC(), End: end.UTC()}
	}
	return c, nil
}

// FamilyDay is the account's family day at now, once a request on platform AI
// sent the offset reported, if any (R11): the offset, when valid, kept for the
// days to come, and the day that holds now, kept once the day before is over.
// It writes in tx, which holds the account's row already, as a request under a
// key does (HoldAccount), and only what changed. The offset is no field of any
// line of the log.
func FamilyDay(ctx context.Context, tx pgx.Tx, accountID string, reported *int32, now time.Time) (quota.Day, error) {
	kept, err := clockOf(ctx, tx, accountID)
	if err != nil {
		return quota.Day{}, err
	}
	c, changed := kept.Sent(reported, now)
	if changed {
		if _, err := tx.Exec(ctx, `UPDATE accounts SET utc_offset_minutes = $2, day_starts_at = $3, day_ends_at = $4
			WHERE id = $1`, accountID, c.Offset, c.Day.Start, c.Day.End); err != nil {
			return quota.Day{}, fmt.Errorf("store: keep the family day: %w", err)
		}
	}
	return *c.Day, nil
}

// Today is the account's family day at now as it keeps it, writing nothing: for
// a request that sends no offset, such as the beginning of an attempt (R97
// spec; the amendment of 2026-11-07 to R11).
func Today(ctx context.Context, q Querier, accountID string, now time.Time) (quota.Day, error) {
	c, err := clockOf(ctx, q, accountID)
	if err != nil {
		return quota.Day{}, err
	}
	return c.Today(now), nil
}
