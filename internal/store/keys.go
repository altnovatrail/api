package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/pin"
)

// The states of a record of an idempotency key.
const (
	// KeyGoing is a request that is still going.
	KeyGoing = "going"
	// KeyDone is a request done, whose answer the record holds.
	KeyDone = "done"
)

// KeyRecord is what is recorded of a request under its idempotency key (R17,
// R35): its method, its fingerprint, its state and, once done, its answer — or,
// for an answer that holds what the learner sent, the submission it recorded.
type KeyRecord struct {
	Method       string
	Fingerprint  string
	State        string
	Response     []byte
	SubmissionID string
	// Age is how long ago the record was made, by the database's clock: a record
	// still going past the longest a request may run is of a request that fell.
	Age time.Duration
}

// KeyTarget is what a request done under a key acted on, and what its record
// holds of the answer: the profile and the attempt it acted on, when it did, and
// either the answer or the submission whose answer a repeat gets (architecture
// 3.2).
type KeyTarget struct {
	ProfileID    string
	AttemptID    string
	SubmissionID string
	Response     []byte
}

// RecordOf is the record of the account's key, and whether there is one.
func RecordOf(ctx context.Context, tx pgx.Tx, accountID, key string) (KeyRecord, bool, error) {
	var r KeyRecord
	var ageMicros int64
	err := tx.QueryRow(ctx, `SELECT method, fingerprint, state, response, coalesce(submission_id::text, ''),
			(extract(epoch FROM now() - created_at) * 1000000)::bigint
		FROM idempotency_keys WHERE account_id = $1 AND key = $2`, accountID, key).
		Scan(&r.Method, &r.Fingerprint, &r.State, &r.Response, &r.SubmissionID, &ageMicros)
	r.Age = time.Duration(ageMicros) * time.Microsecond
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return KeyRecord{}, false, nil
	case err != nil:
		return KeyRecord{}, false, fmt.Errorf("store: read the key's record: %w", err)
	}
	return r, true, nil
}

// RecordDone records a request done under the account's key, with its target,
// in the transaction of the action, so that the action and its record commit
// together or not at all. A request on the account itself names no profile.
func RecordDone(ctx context.Context, tx pgx.Tx, accountID, key, method, fingerprint string, target KeyTarget) error {
	_, err := tx.Exec(ctx, `INSERT INTO idempotency_keys (account_id, key, method, fingerprint, state, response,
			profile_id, attempt_id, submission_id)
		VALUES ($1, $2, $3, $4, 'done', $5, nullif($6, '')::uuid, nullif($7, '')::uuid, nullif($8, '')::uuid)`,
		accountID, key, method, fingerprint, target.Response, target.ProfileID, target.AttemptID, target.SubmissionID)
	if err != nil {
		return fmt.Errorf("store: record the key: %w", err)
	}
	return nil
}

// HoldAccount takes the account's row FOR NO KEY UPDATE in tx: what a request
// under an idempotency key holds first, so that a repeat beside it waits and
// finds its record. The lock conflicts with itself and not with FOR KEY SHARE,
// so the other writes under the account do not wait for it (R29, R35).
func HoldAccount(ctx context.Context, tx pgx.Tx, accountID string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id = $1 FOR NO KEY UPDATE`, accountID).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNoAccount
	case err != nil:
		return fmt.Errorf("store: hold the account: %w", err)
	}
	return nil
}

// HoldPin takes the account's row FOR NO KEY UPDATE and reads its PIN. The lock
// conflicts with itself and not with FOR KEY SHARE: the requests of the PIN of
// one account — from two devices, or a repeat beside its first — go one after
// the other, and its count loses no wrong PIN, while the other writes under the
// account do not wait for them (R29).
func HoldPin(ctx context.Context, tx pgx.Tx, accountID string) (pin.State, error) {
	var hash *string
	var failed []time.Time
	var locked *time.Time
	err := tx.QueryRow(ctx, `SELECT pin_hash, pin_failed_at, pin_locked_until
		FROM accounts WHERE id = $1 FOR NO KEY UPDATE`, accountID).Scan(&hash, &failed, &locked)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return pin.State{}, ErrNoAccount
	case err != nil:
		return pin.State{}, fmt.Errorf("store: hold the PIN: %w", err)
	}
	state := pin.State{Failed: failed}
	if hash != nil {
		state.Hash = *hash
	}
	if locked != nil {
		state.LockedUntil = *locked
	}
	return state, nil
}

// WritePin writes the account's PIN as state has it: after an action that
// left one set, which every action of the PIN does.
func WritePin(ctx context.Context, tx pgx.Tx, accountID string, state pin.State) error {
	failed := state.Failed
	if failed == nil {
		failed = []time.Time{}
	}
	_, err := tx.Exec(ctx, `UPDATE accounts SET pin_hash = $2, pin_failed_at = $3, pin_locked_until = $4 WHERE id = $1`,
		accountID, state.Hash, failed, nullableTime(state.LockedUntil))
	if err != nil {
		return fmt.Errorf("store: write the PIN: %w", err)
	}
	return nil
}

func nullableTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// RecordGoing records a request under the account's key as going, with what it
// acts on, and commits with the checks before its call of AI: a repeat beside
// it is "still going" until it is done, or until it outlives the longest a
// request may run (R17). It gives when the record was made, by the database's
// clock: the mark by which the request finishes or drops its own record, and
// never a repeat's that took its place.
func RecordGoing(ctx context.Context, tx pgx.Tx, accountID, key, method, fingerprint string, target KeyTarget) (time.Time, error) {
	var made time.Time
	err := tx.QueryRow(ctx, `INSERT INTO idempotency_keys (account_id, key, method, fingerprint, state, profile_id, attempt_id)
		VALUES ($1, $2, $3, $4, 'going', nullif($5, '')::uuid, nullif($6, '')::uuid) RETURNING created_at`,
		accountID, key, method, fingerprint, target.ProfileID, target.AttemptID).Scan(&made)
	if err != nil {
		return time.Time{}, fmt.Errorf("store: record the key going: %w", err)
	}
	return made, nil
}

// ErrRecordGone is a record going that is no longer there, or no longer this
// request's, when its request comes to finish: a repeat found it fallen and went
// again.
var ErrRecordGone = errors.New("store: the key's record going is gone")

// FinishRecord turns the account's record going, the one made at made, into
// done with its target, in the transaction of what the request writes after its
// call.
func FinishRecord(ctx context.Context, tx pgx.Tx, accountID, key string, made time.Time, target KeyTarget) error {
	tag, err := tx.Exec(ctx, `UPDATE idempotency_keys SET state = 'done', response = $4,
			profile_id = nullif($5, '')::uuid, attempt_id = nullif($6, '')::uuid, submission_id = nullif($7, '')::uuid
		WHERE account_id = $1 AND key = $2 AND created_at = $3 AND state = 'going'`,
		accountID, key, made, target.Response, target.ProfileID, target.AttemptID, target.SubmissionID)
	switch {
	case err != nil:
		return fmt.Errorf("store: finish the key's record: %w", err)
	case tag.RowsAffected() == 0:
		return ErrRecordGone
	}
	return nil
}

// DropRecord deletes the account's record going made at made — of a request
// refused after its call, which records nothing — or, with made zero, whichever
// is going: of a request that fell, which a repeat runs again.
func DropRecord(ctx context.Context, tx pgx.Tx, accountID, key string, made time.Time) error {
	if _, err := tx.Exec(ctx, `DELETE FROM idempotency_keys
		WHERE account_id = $1 AND key = $2 AND state = 'going' AND ($3::timestamptz IS NULL OR created_at = $3)`,
		accountID, key, nullableTime(made)); err != nil {
		return fmt.Errorf("store: drop the key's record: %w", err)
	}
	return nil
}
