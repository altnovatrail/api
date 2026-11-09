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

// CreateChildConsent records a consent for a child that waits, under the
// account, with the age category and the country alone: no pseudonym, no other
// field of the profile, and not the age in years (privacy 3.3 spec; R36).
func CreateChildConsent(ctx context.Context, tx pgx.Tx, accountID, category, country string) (ChildConsent, error) {
	c := ChildConsent{AgeCategory: category, Country: country, State: ConsentWaiting}
	err := tx.QueryRow(ctx, `INSERT INTO consents (account_id, subject, state, age_category, country)
		VALUES ($1, 'child', 'waiting', $2, $3) RETURNING id`, accountID, category, country).Scan(&c.ID)
	if err != nil {
		return ChildConsent{}, fmt.Errorf("store: record the consent: %w", err)
	}
	return c, nil
}

// WaitingConsents is how many consents for a child wait under the account. It
// takes no lock: its caller holds the account's row FOR NO KEY UPDATE, by
// HoldAccount, as every request under a key does, so that two requests beside
// each other count one after the other (R35, R36).
func WaitingConsents(ctx context.Context, tx pgx.Tx, accountID string) (int, error) {
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM consents
		WHERE account_id = $1 AND subject = 'child' AND state = 'waiting'`, accountID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count the consents waiting: %w", err)
	}
	return n, nil
}

// ConsentFor is the account's consent for a child, held FOR UPDATE in tx, or
// ErrNoConsent.
func ConsentFor(ctx context.Context, tx pgx.Tx, accountID, consentID string) (Consent, error) {
	var c Consent
	var profileID *string
	err := tx.QueryRow(ctx, `SELECT id, age_category, country, state, profile_id FROM consents
		WHERE id = $1 AND account_id = $2 AND subject = 'child' FOR UPDATE`, consentID, accountID).
		Scan(&c.ID, &c.AgeCategory, &c.Country, &c.State, &profileID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Consent{}, ErrNoConsent
	case err != nil:
		return Consent{}, fmt.Errorf("store: hold the consent: %w", err)
	}
	if profileID != nil {
		c.ProfileID = *profileID
	}
	return c, nil
}

// CreateChildProfile makes the profile of a confirmed consent, which ConsentFor
// held, of its category and country, and binds the consent to it. The date the
// age was given is the date the consent was asked for, by UTC.
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
	return created, nil
}
