package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// HoldKeyChecks takes the account's row FOR NO KEY UPDATE and reads when each
// check of an own key it keeps was made, in the order they were counted; it may
// hold checks older than the window, which no longer count. The lock conflicts
// with itself and not with FOR KEY SHARE: the checks of one account — from two
// devices, or two tabs — are counted one after the other, and none is lost,
// while the other writes under the account do not wait for them (R78).
func HoldKeyChecks(ctx context.Context, tx pgx.Tx, accountID string) ([]time.Time, error) {
	var checked []time.Time
	err := tx.QueryRow(ctx, `SELECT key_checked_at FROM accounts WHERE id = $1 FOR NO KEY UPDATE`, accountID).Scan(&checked)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrNoAccount
	case err != nil:
		return nil, fmt.Errorf("store: hold the checks of a key: %w", err)
	}
	return checked, nil
}

// WriteKeyChecks writes when each check of an own key the account keeps was
// made, after HoldKeyChecks held its row.
func WriteKeyChecks(ctx context.Context, tx pgx.Tx, accountID string, checked []time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE accounts SET key_checked_at = $2 WHERE id = $1`, accountID, checked); err != nil {
		return fmt.Errorf("store: write the checks of a key: %w", err)
	}
	return nil
}
