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

// Charge is what a request takes of the quota of platform AI: of the profile
// and its account, on the family day of its reserve, the amount reserved.
type Charge struct {
	AccountID, ProfileID string
	Day                  quota.Day
	Amount               quota.Micro
}

// room is whether the charge c fits under the caps of the account's tariff
// beside the counters of its day, read in q, and FOR UPDATE when lock: a
// reserve holds them until it writes. A day with no counter yet has spent
// nothing. Every statement of the quota takes the counters of a
// profile before those of its account, as the deletion of an account does — its
// profiles, and theirs with them, then its own —, so that two of them never wait
// for each other.
func room(ctx context.Context, q Querier, c Charge, lock bool) (quota.Spent, error) {
	var tariff *string
	err := q.QueryRow(ctx, `SELECT tariff FROM accounts WHERE id = $1`, c.AccountID).Scan(&tariff)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", ErrNoAccount
	case err != nil:
		return "", fmt.Errorf("store: read the tariff: %w", err)
	}
	caps, ok := quota.CapsOf(deref(tariff))
	if !ok {
		return quota.NoTariff, nil
	}
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	var profile, account quota.Counter
	for _, read := range []struct {
		table, column, id string
		into              *quota.Counter
	}{
		{"profile_quota", "profile_id", c.ProfileID, &profile},
		{"account_quota", "account_id", c.AccountID, &account},
	} {
		err := q.QueryRow(ctx, `SELECT spent_micro_usd, reserved_micro_usd FROM `+read.table+`
			WHERE `+read.column+` = $1 AND day_start = $2`+suffix, read.id, c.Day.Start).Scan(&read.into.Spent, &read.into.Reserved)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("store: read the quota of the %s: %w", read.table, err)
		}
	}
	return quota.Room(caps, profile, account, c.Amount), nil
}

// QuotaFits says whether the charge c would fit, reserving nothing: what is left
// of the day holds a typical task, at the beginning of an attempt (R11).
func QuotaFits(ctx context.Context, q Querier, c Charge) (quota.Spent, error) {
	return room(ctx, q, c, false)
}

// ReserveQuota reserves the charge c on the counters of the profile and of the
// account of its day, in tx, when it fits beside what each has spent and holds
// in reserve, and says why it does not when it does not, writing nothing (R12).
// The counters of a day are made by its first reserve. tx holds the account's
// row already, as a request under a key does, so that two reserves of one
// account — two devices, a profile and its owner — go one after the other, and
// the counters are held besides against a settlement going on.
func ReserveQuota(ctx context.Context, tx pgx.Tx, c Charge) (quota.Spent, error) {
	spent, err := room(ctx, tx, c, true)
	if err != nil || spent != quota.Fits {
		return spent, err
	}
	for _, add := range []struct{ table, column, id string }{
		{"profile_quota", "profile_id", c.ProfileID},
		{"account_quota", "account_id", c.AccountID},
	} {
		if _, err := tx.Exec(ctx, `INSERT INTO `+add.table+` (`+add.column+`, day_start, day_end, reserved_micro_usd)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (`+add.column+`, day_start) DO UPDATE SET reserved_micro_usd = `+add.table+`.reserved_micro_usd + EXCLUDED.reserved_micro_usd`,
			add.id, c.Day.Start, c.Day.End, int64(c.Amount)); err != nil {
			return "", fmt.Errorf("store: reserve on the %s: %w", add.table, err)
		}
	}
	return quota.Fits, nil
}

// SettleQuota settles the reserve of the charge c, on the day of its reserve,
// by what the call cost: the reserve leaves the counters of the profile and of
// the account, and spent is added to what each has spent, in one transaction of
// its own on db (R12). A counter gone meanwhile — the profile deleted, the
// account — is no failure: there is nothing left to settle.
func SettleQuota(ctx context.Context, db Beginner, c Charge, spent quota.Micro) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		for _, settle := range []struct{ table, column, id string }{
			{"profile_quota", "profile_id", c.ProfileID},
			{"account_quota", "account_id", c.AccountID},
		} {
			if _, err := tx.Exec(ctx, `UPDATE `+settle.table+`
				SET reserved_micro_usd = greatest(reserved_micro_usd - $3, 0), spent_micro_usd = spent_micro_usd + $4
				WHERE `+settle.column+` = $1 AND day_start = $2`, settle.id, c.Day.Start, int64(c.Amount), int64(spent)); err != nil {
				return fmt.Errorf("store: settle on the %s: %w", settle.table, err)
			}
		}
		return nil
	})
}

// PlatformAIClosed says whether the flag "platform AI closed" of the deployment
// is set (R11): read on every request that would call platform AI, so that one
// set by any instance, or cleared by the operator, holds at once on all.
func PlatformAIClosed(ctx context.Context, q Querier) (bool, error) {
	var closed *time.Time
	if err := q.QueryRow(ctx, `SELECT closed_at FROM platform_ai`).Scan(&closed); err != nil {
		return false, fmt.Errorf("store: read the flag of platform AI: %w", err)
	}
	return closed != nil, nil
}

// ClosePlatformAI sets the flag "platform AI closed" at now, and says whether
// this call set it: one set already keeps its time.
func ClosePlatformAI(ctx context.Context, q Querier, now time.Time) (bool, error) {
	tag, err := q.Exec(ctx, `UPDATE platform_ai SET closed_at = $1 WHERE closed_at IS NULL`, now)
	if err != nil {
		return false, fmt.Errorf("store: close platform AI: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// OpenPlatformAI clears the flag "platform AI closed", and says whether it was
// set: the operator's command, once the provider's limit is lifted (R11).
func OpenPlatformAI(ctx context.Context, q Querier) (bool, error) {
	tag, err := q.Exec(ctx, `UPDATE platform_ai SET closed_at = NULL WHERE closed_at IS NOT NULL`)
	if err != nil {
		return false, fmt.Errorf("store: open platform AI: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// DropQuotaCounters deletes the counters of the quota whose day ended before
// before, of the profiles and of the accounts, a batch at a time, and says how
// many it deleted (R12): a counter is needed for its day and for a day after
// it, to settle the requests that went over midnight.
func DropQuotaCounters(ctx context.Context, db Querier, before time.Time) (int64, error) {
	var dropped int64
	for _, table := range []string{"profile_quota", "account_quota"} {
		n, err := inBatches(ctx, db, `DELETE FROM `+table+` WHERE ctid IN (
			SELECT ctid FROM `+table+` WHERE day_end < $1 LIMIT $2)`, before, "delete the counters of the quota")
		dropped += n
		if err != nil {
			return dropped, err
		}
	}
	return dropped, nil
}
