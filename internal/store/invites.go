package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The refusals of the creation of an account (R04; contract, CreateAccount).
var (
	// ErrAccountExists is a user who has an account already.
	ErrAccountExists = errors.New("store: the account exists")
	// ErrCodeNotAccepted is a code that is not there, or was redeemed.
	ErrCodeNotAccepted = errors.New("store: the code is not accepted")
)

// CreateInvite keeps a new invite code by its SHA-256 alone, with the tariff it
// passes to the account that redeems it (R04, R11).
func CreateInvite(ctx context.Context, db Querier, codeSHA256 []byte, tariff string) error {
	if _, err := db.Exec(ctx, `INSERT INTO invite_codes (code_sha256, tariff) VALUES ($1, $2)`, codeSHA256, tariff); err != nil {
		return fmt.Errorf("store: keep the code: %w", err)
	}
	return nil
}

// CreateAccount creates the account of the sign-in service's UID by the code of
// codeSHA256, with the owner's statement that they are 18 or older dated now,
// and returns its ID (R04; architecture 3.1, steps 2 and 3). It is one
// transaction, in the order of the contract: an account the UID has already is
// ErrAccountExists — or ErrAccountDeleting, when it is marked — before the code
// is looked at, so that a repeat is not refused for the code it redeemed; then a
// code not there or redeemed is ErrCodeNotAccepted, and no account is left. The
// code is bound to the account, and its tariff passes to it, and the texts the
// owner accepted are recorded with it (R136 spec).
//
// Two creations race safely. Two by one UID: the second waits on the UID's
// unique index and finds the first's account. Two for one code: the second waits
// on the code's row and finds it redeemed. Either way, a first one that rolls
// back leaves the second to go on.
func CreateAccount(ctx context.Context, db Beginner, uid string, codeSHA256 []byte, texts []TextRef) (string, error) {
	var id string
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO accounts (auth_uid, adult_stated_at, last_request_on)
			VALUES ($1, now(), (now() AT TIME ZONE 'UTC')::date)
			ON CONFLICT (auth_uid) DO NOTHING RETURNING id`, uid).Scan(&id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return existing(ctx, tx, uid)
		case err != nil:
			return fmt.Errorf("store: create the account: %w", err)
		}
		// The code is redeemed and bound, and its tariff given to the account, in one
		// statement: no row is a code not there or redeemed.
		var tariff string
		err = tx.QueryRow(ctx, `WITH code AS (
				UPDATE invite_codes SET redeemed_at = now(), account_id = $2
				WHERE code_sha256 = $1 AND redeemed_at IS NULL RETURNING tariff)
			UPDATE accounts SET tariff = code.tariff FROM code WHERE accounts.id = $2 RETURNING code.tariff`,
			codeSHA256, id).Scan(&tariff)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrCodeNotAccepted
		case err != nil:
			return fmt.Errorf("store: redeem the code: %w", err)
		}
		return AcceptTexts(ctx, tx, id, texts, time.Now())
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// existing is the refusal of a UID that has an account already. One whose row
// went between the two statements was deleted, as a marked one is being.
func existing(ctx context.Context, tx pgx.Tx, uid string) error {
	var deleting *time.Time
	err := tx.QueryRow(ctx, `SELECT deleting_at FROM accounts WHERE auth_uid = $1`, uid).Scan(&deleting)
	switch {
	case errors.Is(err, pgx.ErrNoRows), err == nil && deleting != nil:
		return ErrAccountDeleting
	case err != nil:
		return fmt.Errorf("store: find the account: %w", err)
	}
	return ErrAccountExists
}
