package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// AccountRef is an account the daily job works on: its ID and its UID at the
// sign-in service.
type AccountRef struct {
	ID  string
	UID string
}

// ProfileRef is a profile the daily job works on, with its account.
type ProfileRef struct {
	AccountID string
	ID        string
}

// MarkedBefore is up to limit accounts marked "deleting" before before, whose ID
// comes after after — empty for the first page —, by ID: the marks a request left unfinished, which the daily
// job finishes (R21). The job pages by the last ID it saw, so that an account it
// fails on is not chosen again in the same pass.
func MarkedBefore(ctx context.Context, db Lister, before time.Time, after string, limit int) ([]AccountRef, error) {
	return accountRefs(ctx, db, `SELECT id, auth_uid FROM accounts
		WHERE deleting_at < $1 AND id > $2 ORDER BY id LIMIT $3`, before, from(after), limit)
}

// IdleAccounts is up to limit accounts not marked whose last request was before
// before, whose ID comes after after, by ID: those the daily job deletes after 12
// months without a request (П-9). An account with no date of a request yet is
// none of them.
func IdleAccounts(ctx context.Context, db Lister, before time.Time, after string, limit int) ([]AccountRef, error) {
	return accountRefs(ctx, db, `SELECT id, auth_uid FROM accounts
		WHERE deleting_at IS NULL AND last_request_on < $1 AND id > $2 ORDER BY id LIMIT $3`,
		dateOf(before), from(after), limit)
}

func accountRefs(ctx context.Context, db Lister, sql string, args ...any) ([]AccountRef, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("store: choose the accounts: %w", err)
	}
	refs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AccountRef, error) {
		var ref AccountRef
		err := row.Scan(&ref.ID, &ref.UID)
		return ref, err
	})
	if err != nil {
		return nil, fmt.Errorf("store: choose the accounts: %w", err)
	}
	return refs, nil
}

// MarkIdle marks the account "deleting" in tx, as MarkDeleting does, only if it
// still had no request since before, and says whether it did. The term is read
// under the row's lock: a request that wrote today's date while the job chose the
// account keeps it, since FOR UPDATE waits for that write and reads the row
// again (R21; П-9).
func MarkIdle(ctx context.Context, tx pgx.Tx, accountID string, before time.Time) (bool, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM accounts
		WHERE id = $1 AND deleting_at IS NULL AND last_request_on < $2 FOR UPDATE`, accountID, dateOf(before)).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("store: lock the account: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE accounts SET deleting_at = now() WHERE id = $1`, id); err != nil {
		return false, fmt.Errorf("store: mark the account: %w", err)
	}
	return true, nil
}

// IdleProfiles is up to limit profiles of accounts not marked, active last before
// before, whose ID comes after after, by ID: those the daily job deletes after 12
// months without activity (П-9).
func IdleProfiles(ctx context.Context, db Lister, before time.Time, after string, limit int) ([]ProfileRef, error) {
	rows, err := db.Query(ctx, `SELECT p.account_id, p.id FROM profiles p JOIN accounts a ON a.id = p.account_id
		WHERE a.deleting_at IS NULL AND p.active_on < $1 AND p.id > $2 ORDER BY p.id LIMIT $3`,
		dateOf(before), from(after), limit)
	if err != nil {
		return nil, fmt.Errorf("store: choose the profiles: %w", err)
	}
	refs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ProfileRef, error) {
		var ref ProfileRef
		err := row.Scan(&ref.AccountID, &ref.ID)
		return ref, err
	})
	if err != nil {
		return nil, fmt.Errorf("store: choose the profiles: %w", err)
	}
	return refs, nil
}

// ProfileIdle holds the account's profile FOR UPDATE in tx if it was still active
// last before before, and says whether it was: a request that touched it today
// while the job chose it keeps it (П-9).
func ProfileIdle(ctx context.Context, tx pgx.Tx, accountID, profileID string, before time.Time) (bool, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM profiles WHERE id = $1 AND account_id = $2 AND active_on < $3 FOR UPDATE`,
		profileID, accountID, dateOf(before)).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("store: hold the profile: %w", err)
	}
	return true, nil
}

// HaveAccounts is the UIDs, of uids, that have an account, marked or not: the
// daily job removes the users of the sign-in service that have none (R21).
func HaveAccounts(ctx context.Context, db Lister, uids []string) (map[string]bool, error) {
	rows, err := db.Query(ctx, `SELECT auth_uid FROM accounts WHERE auth_uid = ANY($1)`, uids)
	if err != nil {
		return nil, fmt.Errorf("store: find the accounts: %w", err)
	}
	have := map[string]bool{}
	uid := ""
	_, err = pgx.ForEachRow(rows, []any{&uid}, func() error {
		have[uid] = true
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store: find the accounts: %w", err)
	}
	return have, nil
}

// ProfilesThere is the profiles, of ids, that are there, each with its account:
// a tuple whose profile is not, or is another account's, is dangling (R21). An
// ID that is no UUID as the database writes one names no row, and is not asked.
func ProfilesThere(ctx context.Context, db Lister, ids []string) (map[string]string, error) {
	canonical := make([]string, 0, len(ids))
	for _, id := range ids {
		if CanonicalID(id) {
			canonical = append(canonical, id)
		}
	}
	rows, err := db.Query(ctx, `SELECT id::text, account_id::text FROM profiles WHERE id = ANY($1::uuid[])`, canonical)
	if err != nil {
		return nil, fmt.Errorf("store: find the profiles: %w", err)
	}
	there := map[string]string{}
	var id, account string
	_, err = pgx.ForEachRow(rows, []any{&id, &account}, func() error {
		there[id] = account
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store: find the profiles: %w", err)
	}
	return there, nil
}

// dropBatch is how many rows one statement of a term takes — a DELETE, or the
// UPDATE that closes what was abandoned — so that no statement holds the table
// long.
const dropBatch = 1000

// DropKeys deletes the records of idempotency keys made before before, a batch at
// a time, and says how many went: a key is kept for a day (R17). A stop between
// batches keeps what went.
func DropKeys(ctx context.Context, db Querier, before time.Time) (int64, error) {
	return inBatches(ctx, db, `DELETE FROM idempotency_keys WHERE ctid IN (
		SELECT ctid FROM idempotency_keys WHERE created_at < $1 LIMIT $2)`, before, "delete the keys' records")
}

// DropConsentRecords deletes the minimal records of consents for children whose
// account or profile was deleted before before, a batch at a time, and says how
// many went: a record is kept 5 years after the deletion and then erased (R117
// spec; R21). A stop between batches keeps what went.
func DropConsentRecords(ctx context.Context, db Querier, before time.Time) (int64, error) {
	return inBatches(ctx, db, `DELETE FROM child_consent_records WHERE id IN (
		SELECT id FROM child_consent_records WHERE deleted_at < $1 LIMIT $2)`, before, "delete the consents' minimal records")
}

// CloseAbandonedAttempts closes "abandoned", at at, the open attempts with no
// action since before, a batch at a time, and says how many it closed (R10). One
// clock judges the term and dates the close: the pass's. An attempt a request
// holds is skipped, and the next pass finds it if it is still idle; the term is
// checked again on the row the UPDATE takes, so an action that came meanwhile
// keeps the attempt open. The close moves no date of the profile's activity.
func CloseAbandonedAttempts(ctx context.Context, db Querier, before, at time.Time) (int64, error) {
	return inBatches(ctx, db, `UPDATE attempts SET close_reason = 'abandoned', closed_at = $3
		WHERE id IN (SELECT id FROM attempts WHERE closed_at IS NULL AND last_action_at < $1
			LIMIT $2 FOR UPDATE SKIP LOCKED)
		AND closed_at IS NULL AND last_action_at < $1`, before, "close the abandoned attempts", at)
}

// EndAbandonedRuns ends "abandoned", at at, the open lesson runs with no action
// since before, a batch at a time, as CloseAbandonedAttempts closes attempts (R13).
func EndAbandonedRuns(ctx context.Context, db Querier, before, at time.Time) (int64, error) {
	return inBatches(ctx, db, `UPDATE lesson_runs SET end_reason = 'abandoned', ended_at = $3
		WHERE id IN (SELECT id FROM lesson_runs WHERE ended_at IS NULL AND last_action_at < $1
			LIMIT $2 FOR UPDATE SKIP LOCKED)
		AND ended_at IS NULL AND last_action_at < $1`, before, "end the abandoned lesson runs", at)
}

// inBatches runs sql, a statement on the rows older than its $1, a batch of $2
// at a time, with the rest of its arguments after them, until a batch is short.
func inBatches(ctx context.Context, db Querier, sql string, before time.Time, what string, rest ...any) (int64, error) {
	var dropped int64
	for {
		tag, err := db.Exec(ctx, sql, append([]any{before, dropBatch}, rest...)...)
		if err != nil {
			return dropped, fmt.Errorf("store: %s: %w", what, err)
		}
		dropped += tag.RowsAffected()
		if tag.RowsAffected() < dropBatch {
			return dropped, nil
		}
	}
}

// dateOf is the date of t by UTC, as the dates of requests and activity are
// written.
func dateOf(t time.Time) time.Time {
	year, month, day := t.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// from is where a page starts: after the ID after, or from the first ID. Pages go
// by the primary key, as uuid compares.
func from(after string) string {
	if after == "" {
		return "00000000-0000-0000-0000-000000000000"
	}
	return after
}
