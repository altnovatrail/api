package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is what a read runs on: a pool, a connection or a transaction.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Account is what a request knows of the account of its token (R04).
type Account struct {
	// ID is the account's own ID, the subject of the rights.
	ID string
	// Deleting says the account is marked "deleting" (R21).
	Deleting bool
	// LastRequestOn is the date, by UTC, of the account's last request; zero if
	// none was written yet.
	LastRequestOn time.Time
}

// TouchedOn says whether the date of the last request is already the date of
// now by UTC, so that TouchAccount would write nothing.
func (a Account) TouchedOn(now time.Time) bool { return TodayByUTC(a.LastRequestOn, now) }

// AccountByUID is the account of the sign-in service's UID, or ErrNoAccount.
func AccountByUID(ctx context.Context, db Querier, uid string) (Account, error) {
	var account Account
	var deleting, lastRequest *time.Time
	err := db.QueryRow(ctx, `SELECT id, deleting_at, last_request_on FROM accounts WHERE auth_uid = $1`, uid).
		Scan(&account.ID, &deleting, &lastRequest)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Account{}, ErrNoAccount
	case err != nil:
		return Account{}, fmt.Errorf("store: find the account: %w", err)
	}
	account.Deleting = deleting != nil
	if lastRequest != nil {
		account.LastRequestOn = *lastRequest
	}
	return account, nil
}

// TouchAccount writes today, by UTC, as the date of the account's last request,
// once a day at most: the date the account is deleted by after 12 months with
// no request (R05, R33). A plain UPDATE takes FOR NO KEY UPDATE, which the
// writes under the account, by FOR KEY SHARE, do not wait for.
func TouchAccount(ctx context.Context, db Querier, accountID string) error {
	_, err := db.Exec(ctx, `UPDATE accounts SET last_request_on = (now() AT TIME ZONE 'UTC')::date
		WHERE id = $1 AND last_request_on IS DISTINCT FROM (now() AT TIME ZONE 'UTC')::date`, accountID)
	if err != nil {
		return fmt.Errorf("store: touch the account: %w", err)
	}
	return nil
}

// ProfileOf says whether the account has the profile, and the date of the
// profile's last activity. It is read after the right to the profile, never
// before (architecture 3.1): a tuple whose profile is gone — dangling, R21 —
// gives a right to nothing.
func ProfileOf(ctx context.Context, db Querier, accountID, profileID string) (bool, time.Time, error) {
	var activeOn time.Time
	err := db.QueryRow(ctx, `SELECT active_on FROM profiles WHERE id = $1 AND account_id = $2`,
		profileID, accountID).Scan(&activeOn)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, time.Time{}, nil
	case err != nil:
		return false, time.Time{}, fmt.Errorf("store: find the profile: %w", err)
	}
	return true, activeOn, nil
}

// TodayByUTC says whether on is the date of now by UTC: the date written once a
// day, of an account or a profile, that needs no writing again.
func TodayByUTC(on, now time.Time) bool {
	year, month, day := now.UTC().Date()
	onYear, onMonth, onDay := on.Date()
	return !on.IsZero() && year == onYear && month == onMonth && day == onDay
}

// ConsentGiven says whether the profile's consent is confirmed: what a learning
// request of E3 asks before it does anything (architecture 3.1, step 5; П-1).
// A profile has one consent at most, and a consent that waits has no profile.
func ConsentGiven(ctx context.Context, db Querier, profileID string) (bool, error) {
	var given bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM consents WHERE profile_id = $1 AND state = 'confirmed')`,
		profileID).Scan(&given)
	if err != nil {
		return false, fmt.Errorf("store: find the consent: %w", err)
	}
	return given, nil
}
