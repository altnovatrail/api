package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrNoConsent is a consent for a child that is not the account's.
var ErrNoConsent = errors.New("store: no such consent")

// The states of a consent.
const (
	ConsentWaiting   = "waiting"
	ConsentConfirmed = "confirmed"
)

// Consent is a consent for a child, as its profile is created by it.
type Consent struct {
	ChildConsent
	// ProfileID is the profile created by it, or empty while there is none.
	ProfileID string
}

// WaitingConsents is how many consents for a child wait under the account with
// no profile: those the limit counts (contract, CAUSE_TOO_MANY_CONSENTS). It
// takes no lock: its caller holds the account's row FOR NO KEY UPDATE, by
// HoldAccount, as every request under a key does, so that two requests beside
// each other count one after the other (R35, R36).
func WaitingConsents(ctx context.Context, tx pgx.Tx, accountID string) (int, error) {
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM consents
		WHERE account_id = $1 AND subject = 'child' AND state = 'waiting' AND profile_id IS NULL`, accountID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count the consents waiting: %w", err)
	}
	return n, nil
}

// ConsentFor is the account's consent for a child, held FOR UPDATE in tx, or
// ErrNoConsent.
func ConsentFor(ctx context.Context, tx pgx.Tx, accountID, consentID string) (Consent, error) {
	return consentOf(ctx, tx, `id = $1 AND account_id = $2 AND subject = 'child'`, consentID, accountID)
}

// CreateChildProfile makes the profile of a confirmed consent, which ConsentFor
// held, of its category and country, binds the consent to it, and makes the row
// of its skill state. The date the age was given is the date the consent was
// asked for, by UTC.
func CreateChildProfile(ctx context.Context, tx pgx.Tx, accountID string, c Consent, p Profile) (Profile, error) {
	created, err := scanProfile(tx.QueryRow(ctx, `INSERT INTO profiles (account_id, owner, pseudonym, avatar,
			age_category, country, age_stated_on, voicing_enabled, voicing_speed, default_mode, interface_language,
			grade, ask_before_showing, active_on)
		SELECT $1, false, $2, $3, age_category, country, (requested_at AT TIME ZONE 'UTC')::date, $4, $5, $6, $7, $8, $9,
			(now() AT TIME ZONE 'UTC')::date
		FROM consents WHERE id = $10
		RETURNING `+profileColumns,
		accountID, p.Pseudonym, p.Avatar, p.VoicingEnabled, p.VoicingSpeed, p.DefaultMode, p.InterfaceLanguage,
		p.Grade, p.AskBeforeShowing, c.ID))
	if err != nil {
		return Profile{}, fmt.Errorf("store: create the child's profile: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE consents SET profile_id = $2 WHERE id = $1`, c.ID, created.ID); err != nil {
		return Profile{}, fmt.Errorf("store: bind the consent: %w", err)
	}
	if err := startSkillState(ctx, tx, created.ID); err != nil {
		return Profile{}, err
	}
	return created, nil
}
