package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// minimalRecords keeps, of every confirmed consent for a child that the rows
// matched by cond go with, the minimal record: its ID, when it was given, how it
// was checked and when it was deleted — no ID of the account or the profile, no
// pseudonym, category or country (П-14; R21). A record kept already stays as it
// is, so that a repeat of the deletion keeps no second one.
const minimalRecords = `INSERT INTO child_consent_records (id, consented_at, method)
	SELECT id, confirmed_at, coalesce(method, '') FROM consents
	WHERE subject = 'child' AND state = 'confirmed' AND %s
	ON CONFLICT (id) DO NOTHING`

// DeleteProfileRows deletes, in the transaction that holds the profile, all that
// the database keeps of it (R21; architecture 3.10): its consent — for a child
// into its minimal record, an adult's whole — its records of idempotency keys,
// and the profile. What epic E3 keeps of a profile goes with it by its keys.
func DeleteProfileRows(ctx context.Context, tx pgx.Tx, profileID string) error {
	if _, err := tx.Exec(ctx, fmt.Sprintf(minimalRecords, `profile_id = $1`), profileID); err != nil {
		return fmt.Errorf("store: keep the consent's record: %w", err)
	}
	// The consent and the records of keys go by their keys, ON DELETE CASCADE.
	if _, err := tx.Exec(ctx, `DELETE FROM profiles WHERE id = $1`, profileID); err != nil {
		return fmt.Errorf("store: delete the profile: %w", err)
	}
	return nil
}

// ClearAccount deletes, in the transaction that marked the account "deleting",
// all the database keeps of it but its ID, its UID and the time of the mark
// (R21; architecture 3.10, step 1): every profile as DeleteProfileRows deletes
// one, the consents that wait and the adults', the records of its keys, its
// fields, and the binding of its invite code — the code stays redeemed.
func ClearAccount(ctx context.Context, tx pgx.Tx, accountID string) error {
	for _, step := range []struct {
		what, sql string
	}{
		// Every confirmed consent for a child, with its profile or still waiting for
		// it to be made, leaves its record; one that waits leaves none.
		{"keep the consents' records", fmt.Sprintf(minimalRecords, `account_id = $1`)},
		{"delete the profiles", `DELETE FROM profiles WHERE account_id = $1`},
		{"delete the consents", `DELETE FROM consents WHERE account_id = $1`},
		{"delete the keys' records", `DELETE FROM idempotency_keys WHERE account_id = $1`},
		{"clear the account", `UPDATE accounts SET adult_stated_at = NULL, pin_hash = NULL, pin_failures = NULL,
			pin_failures_since = NULL, pin_locked_until = NULL, pin_failed_at = '{}', tariff = NULL,
			utc_offset_minutes = NULL, day_ends_at = NULL, last_request_on = NULL WHERE id = $1`},
		{"unbind the code", `UPDATE invite_codes SET account_id = NULL WHERE account_id = $1`},
	} {
		if _, err := tx.Exec(ctx, step.sql, accountID); err != nil {
			return fmt.Errorf("store: %s: %w", step.what, err)
		}
	}
	return nil
}

// DeleteAccountRow is the last step of the deletion of an account (R21;
// architecture 3.10, step 4): in one transaction, it clears again what a row
// written late may have left, and deletes the account's row. An account that is
// not there is deleted already.
func DeleteAccountRow(ctx context.Context, db Beginner, accountID string) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		err := MarkDeleting(ctx, tx, accountID)
		switch {
		case errors.Is(err, ErrNoAccount):
			return nil
		case err != nil:
			return err
		}
		if err := ClearAccount(ctx, tx, accountID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, accountID); err != nil {
			return fmt.Errorf("store: delete the account: %w", err)
		}
		return nil
	})
}
