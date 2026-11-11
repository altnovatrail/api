package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// View is the account as GetAccount gives it (contract, Account): whether its
// PIN is set, its profiles with their open attempts, and its consents for a
// child with no profile yet.
type View struct {
	// ID is the account's ID.
	ID string
	// PinSet says the PIN of the section for adults is set.
	PinSet bool
	// Profiles are the account's profiles: the owner's first, then by when they
	// were created.
	Profiles []Profile
	// ChildConsents are the consents for a child that have no profile, by when
	// they were asked for.
	ChildConsents []ChildConsent
}

// Profile is a profile's row, with the contract's codes as the schema keeps
// them.
type Profile struct {
	ID                string
	Owner             bool
	Pseudonym         string
	Avatar            string
	AgeCategory       string
	Country           string
	VoicingEnabled    bool
	VoicingSpeed      string
	DefaultMode       string
	InterfaceLanguage *string
	Grade             *int16
	AskBeforeShowing  bool
	// WorkStorage says the storage of the learner's works on the server is on
	// (R113 spec).
	WorkStorage bool
	// AgeStatedOn is the day the age was last given, by UTC: a year after it, the
	// category is refined (privacy 3.3 spec).
	AgeStatedOn *time.Time
	// Consent is the profile's consent, which AccountView and ProfileWithConsent
	// read.
	Consent Consent
	// OpenAttempt is the profile's open attempt, which the view of the account
	// alone reads: nil when it has none.
	OpenAttempt *OpenAttempt
	// OpenLessonRuns are the profile's open lesson runs, one at most for each
	// lesson, which the view of the account alone reads.
	OpenLessonRuns []OpenLessonRun
}

// ChildConsent is a consent for a child that waits for its profile.
type ChildConsent struct {
	ID          string
	AgeCategory string
	Country     string
	// State is "waiting" or "confirmed".
	State string
	// Giver is who gives it: GiverOwner, GiverParent or GiverLearner.
	Giver string
	// RequestCode and FormState are a parent's: the code of the request and the
	// state of the form.
	RequestCode string
	FormState   string
	// Applied is the age of digital consent of the country on the day the age was
	// given, by which the category was; 0 for the owner's.
	Applied int
}

// AccountView reads the account in one snapshot, so that its profiles and its
// consents are of one moment: a device erases the profiles the list does not
// hold (contract, GetAccount). An account that is not there is ErrNoAccount, and
// one marked "deleting" ErrAccountDeleting.
func AccountView(ctx context.Context, db TxBeginner, accountID string) (View, error) {
	view := View{ID: accountID}
	snapshot := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	err := pgx.BeginTxFunc(ctx, db, snapshot, func(tx pgx.Tx) error {
		var deleting *time.Time
		err := tx.QueryRow(ctx, `SELECT pin_hash IS NOT NULL, deleting_at FROM accounts WHERE id = $1`, accountID).
			Scan(&view.PinSet, &deleting)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrNoAccount
		case err != nil:
			return fmt.Errorf("store: read the account: %w", err)
		case deleting != nil:
			return ErrAccountDeleting
		}
		if view.Profiles, err = profiles(ctx, tx, accountID); err != nil {
			return err
		}
		open, err := openAttempts(ctx, tx, accountID)
		if err != nil {
			return err
		}
		runs, err := openLessonRuns(ctx, tx, accountID)
		if err != nil {
			return err
		}
		consents, err := profileConsents(ctx, tx, accountID)
		if err != nil {
			return err
		}
		for i, p := range view.Profiles {
			view.Profiles[i].Consent = consents[p.ID]
			if a, ok := open[p.ID]; ok {
				view.Profiles[i].OpenAttempt = &a
			}
			view.Profiles[i].OpenLessonRuns = runs[p.ID]
		}
		view.ChildConsents, err = childConsents(ctx, tx, accountID)
		return err
	})
	if err != nil {
		return View{}, err
	}
	return view, nil
}

func profiles(ctx context.Context, tx pgx.Tx, accountID string) ([]Profile, error) {
	rows, err := tx.Query(ctx, `SELECT `+profileColumns+`
		FROM profiles WHERE account_id = $1 ORDER BY owner DESC, created_at, id`, accountID)
	if err != nil {
		return nil, fmt.Errorf("store: read the profiles: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Profile, error) { return scanProfile(row) })
	if err != nil {
		return nil, fmt.Errorf("store: read the profiles: %w", err)
	}
	return list, nil
}

func childConsents(ctx context.Context, tx pgx.Tx, accountID string) ([]ChildConsent, error) {
	rows, err := tx.Query(ctx, `SELECT `+consentColumns+` FROM consents
		WHERE account_id = $1 AND subject = 'child' AND profile_id IS NULL ORDER BY requested_at, id`, accountID)
	if err != nil {
		return nil, fmt.Errorf("store: read the consents: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ChildConsent, error) {
		c, err := scanConsent(row)
		return c.ChildConsent, err
	})
	if err != nil {
		return nil, fmt.Errorf("store: read the consents: %w", err)
	}
	return list, nil
}

// profileConsents is the consent of each of the account's profiles, by the
// profile's ID.
func profileConsents(ctx context.Context, db Lister, accountID string) (map[string]Consent, error) {
	rows, err := db.Query(ctx, `SELECT `+consentColumns+` FROM consents WHERE account_id = $1 AND profile_id IS NOT NULL`,
		accountID)
	if err != nil {
		return nil, fmt.Errorf("store: read the consents of the profiles: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Consent, error) { return scanConsent(row) })
	if err != nil {
		return nil, fmt.Errorf("store: read the consents of the profiles: %w", err)
	}
	by := map[string]Consent{}
	for _, c := range list {
		by[c.ProfileID] = c
	}
	return by, nil
}

// ProfileWithConsent is the account's profile with its consent, as an answer
// gives it, or ErrNoProfile.
func ProfileWithConsent(ctx context.Context, db Querier, accountID, profileID string) (Profile, error) {
	p, err := scanProfile(db.QueryRow(ctx, `SELECT `+profileColumns+` FROM profiles WHERE id = $1 AND account_id = $2`,
		profileID, accountID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Profile{}, ErrNoProfile
	case err != nil:
		return Profile{}, fmt.Errorf("store: read the profile: %w", err)
	}
	c, err := scanConsent(db.QueryRow(ctx, `SELECT `+consentColumns+` FROM consents WHERE profile_id = $1`, profileID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return Profile{}, fmt.Errorf("store: read the profile's consent: %w", err)
	default:
		p.Consent = c
	}
	return p, nil
}
