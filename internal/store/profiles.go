package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The refusals of a profile (contract, AccountService).
var (
	// ErrProfileExists is a profile there already: the owner's second, or the
	// profile of a consent that has one.
	ErrProfileExists = errors.New("store: the profile exists")
	// ErrNoProfile is a profile that is not the account's.
	ErrNoProfile = errors.New("store: no such profile")
)

// The ways a consent was given (R36). The owner's own is the statement that
// they are 18 or older, dated with the account (R118 spec).
const ConsentByOwnerStatement = "owner_statement"

// profileColumns are the columns of a Profile, in scanProfile's order.
const profileColumns = `id, owner, pseudonym, avatar, age_category, country, voicing_enabled,
	voicing_speed, default_mode, interface_language, grade, ask_before_showing`

func scanProfile(row pgx.Row) (Profile, error) {
	var p Profile
	err := row.Scan(&p.ID, &p.Owner, &p.Pseudonym, &p.Avatar, &p.AgeCategory, &p.Country, &p.VoicingEnabled,
		&p.VoicingSpeed, &p.DefaultMode, &p.InterfaceLanguage, &p.Grade, &p.AskBeforeShowing)
	return p, err
}

// CreateOwnerProfile makes the owner's profile in tx, of the category adult by
// the owner's statement, with its consent: confirmed now, given by that
// statement (R36; architecture 3.1). The date the age was given is the date of
// the statement, and every date is by UTC, as TouchProfile writes it. The account has one owner's profile: a second is
// ErrProfileExists.
func CreateOwnerProfile(ctx context.Context, tx pgx.Tx, accountID string, p Profile) (Profile, error) {
	p.Owner, p.AgeCategory = true, "adult"
	created, err := scanProfile(tx.QueryRow(ctx, `INSERT INTO profiles (account_id, owner, pseudonym, avatar,
			age_category, country, age_stated_on, voicing_enabled, voicing_speed, default_mode, interface_language,
			grade, ask_before_showing, active_on)
		SELECT $1, true, $2, $3, 'adult', $4, (adult_stated_at AT TIME ZONE 'UTC')::date, $5, $6, $7, $8, $9, false,
			(now() AT TIME ZONE 'UTC')::date
		FROM accounts WHERE id = $1
		RETURNING `+profileColumns,
		accountID, p.Pseudonym, p.Avatar, p.Country, p.VoicingEnabled, p.VoicingSpeed, p.DefaultMode,
		p.InterfaceLanguage, p.Grade))
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "profiles_one_owner":
		return Profile{}, ErrProfileExists
	case errors.Is(err, pgx.ErrNoRows):
		return Profile{}, ErrNoAccount
	case err != nil:
		return Profile{}, fmt.Errorf("store: create the owner's profile: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO consents (account_id, profile_id, subject, state, age_category, country,
			method, confirmed_at)
		VALUES ($1, $2, 'adult', 'confirmed', 'adult', $3, $4, now())`,
		accountID, created.ID, created.Country, ConsentByOwnerStatement); err != nil {
		return Profile{}, fmt.Errorf("store: record the owner's consent: %w", err)
	}
	return created, nil
}

// ProfileFor is the account's profile, held FOR UPDATE in tx for a change, or
// ErrNoProfile.
func ProfileFor(ctx context.Context, tx pgx.Tx, accountID, profileID string) (Profile, error) {
	p, err := scanProfile(tx.QueryRow(ctx, `SELECT `+profileColumns+` FROM profiles
		WHERE id = $1 AND account_id = $2 FOR UPDATE`, profileID, accountID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Profile{}, ErrNoProfile
	case err != nil:
		return Profile{}, fmt.Errorf("store: hold the profile: %w", err)
	}
	return p, nil
}

// WriteProfile writes the settings of p, the profile ProfileFor held: what an
// adult changes, and nothing the profile was created with.
func WriteProfile(ctx context.Context, tx pgx.Tx, p Profile) error {
	_, err := tx.Exec(ctx, `UPDATE profiles SET pseudonym = $2, avatar = $3, voicing_enabled = $4, voicing_speed = $5,
			default_mode = $6, interface_language = $7, grade = $8, ask_before_showing = $9
		WHERE id = $1`,
		p.ID, p.Pseudonym, p.Avatar, p.VoicingEnabled, p.VoicingSpeed, p.DefaultMode, p.InterfaceLanguage, p.Grade,
		p.AskBeforeShowing)
	if err != nil {
		return fmt.Errorf("store: write the profile: %w", err)
	}
	return nil
}

// TouchProfile writes today, by UTC, as the date of the profile's last
// activity, once a day at most: the date a profile is deleted by after 12 months
// with none (П-9; R36), as TouchAccount writes the account's.
func TouchProfile(ctx context.Context, db Querier, profileID string) error {
	_, err := db.Exec(ctx, `UPDATE profiles SET active_on = (now() AT TIME ZONE 'UTC')::date
		WHERE id = $1 AND active_on IS DISTINCT FROM (now() AT TIME ZONE 'UTC')::date`, profileID)
	if err != nil {
		return fmt.Errorf("store: touch the profile: %w", err)
	}
	return nil
}

// HasOwnerProfile says whether the account has the owner's profile.
func HasOwnerProfile(ctx context.Context, tx pgx.Tx, accountID string) (bool, error) {
	var has bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM profiles WHERE account_id = $1 AND owner)`, accountID).Scan(&has)
	if err != nil {
		return false, fmt.Errorf("store: find the owner's profile: %w", err)
	}
	return has, nil
}
