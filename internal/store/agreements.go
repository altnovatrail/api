package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Who gives a consent (R116 spec): the owner of the account, of their own
// profile; a parent, by a signed form; or the learner, from the age of digital
// consent of their country.
const (
	GiverOwner   = "owner"
	GiverParent  = "parent"
	GiverLearner = "learner"
)

// The states of the form of a parent's consent.
const (
	FormNotSent     = "not_sent"
	FormSent        = "sent"
	FormNotAccepted = "not_accepted"
)

// How a consent for a child was checked, as its minimal record keeps it (R117
// spec): a parent's by the signed form, the learner's by their own consent.
const (
	ConsentBySignedForm = "signed_form"
	ConsentByLearner    = "own_consent"
)

// ErrCodeTaken is a code of a request that another waiting consent has: the
// caller draws another.
var ErrCodeTaken = errors.New("store: the code of the request is taken")

// NewConsent is a consent for a child as it is asked for: the category of the
// learner, the country, the age of the country it was given by, who gives it,
// and, for a parent's, the code of its request and the version of parent-consent.
type NewConsent struct {
	AgeCategory string
	Country     string
	Applied     int
	Giver       string
	RequestCode string
	TextVersion *int32
}

// AskChildConsent records a consent for a child that waits, under the account:
// with no pseudonym, no other field of the profile, and not the age in years
// (privacy 3.3 spec; R36). A code of a request another waiting consent has is
// ErrCodeTaken, and tx can go on: the insert runs in a savepoint of its own.
func AskChildConsent(ctx context.Context, tx pgx.Tx, accountID string, c NewConsent) (ChildConsent, error) {
	out := ChildConsent{AgeCategory: c.AgeCategory, Country: c.Country, State: ConsentWaiting, Giver: c.Giver,
		RequestCode: c.RequestCode}
	var code, form *string
	if c.Giver == GiverParent {
		code, form, out.FormState = &c.RequestCode, ptr(FormNotSent), FormNotSent
	}
	err := pgx.BeginFunc(ctx, tx, func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `INSERT INTO consents (account_id, subject, state, age_category, country, giver,
				request_code, form_state, applied, text_version)
			VALUES ($1, 'child', 'waiting', $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
			accountID, c.AgeCategory, c.Country, c.Giver, code, form, c.Applied, c.TextVersion).Scan(&out.ID)
	})
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "consents_request_code":
		return ChildConsent{}, ErrCodeTaken
	case err != nil:
		return ChildConsent{}, fmt.Errorf("store: record the consent: %w", err)
	}
	return out, nil
}

func ptr[T any](v T) *T { return &v }

// consentColumns are the columns of a Consent, in scanConsent's order.
const consentColumns = `id, age_category, country, state, profile_id, giver, coalesce(request_code, ''),
	coalesce(form_state, ''), coalesce(applied, 0)`

func scanConsent(row pgx.Row) (Consent, error) {
	var c Consent
	var profileID *string
	err := row.Scan(&c.ID, &c.AgeCategory, &c.Country, &c.State, &profileID, &c.Giver, &c.RequestCode, &c.FormState,
		&c.Applied)
	if profileID != nil {
		c.ProfileID = *profileID
	}
	return c, err
}

// consentOf is the one consent the query gives, held FOR UPDATE, or
// ErrNoConsent.
func consentOf(ctx context.Context, tx pgx.Tx, sql string, args ...any) (Consent, error) {
	c, err := scanConsent(tx.QueryRow(ctx, `SELECT `+consentColumns+` FROM consents WHERE `+sql+` FOR UPDATE`, args...))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Consent{}, ErrNoConsent
	case err != nil:
		return Consent{}, fmt.Errorf("store: hold the consent: %w", err)
	}
	return c, nil
}

// ConsentOfProfile is the consent of the account's profile, held FOR UPDATE in
// tx, or ErrNoConsent.
func ConsentOfProfile(ctx context.Context, tx pgx.Tx, accountID, profileID string) (Consent, error) {
	return consentOf(ctx, tx, `account_id = $1 AND profile_id = $2`, accountID, profileID)
}

// CancelChildConsent takes back the account's consent for a child with no
// profile, and says whether there was one: one that waits is erased, with its
// form; one confirmed leaves its minimal record and its form for their term
// (R117 spec; contract, CancelChildConsent). Neither counts toward the limit
// after.
func CancelChildConsent(ctx context.Context, tx pgx.Tx, accountID, consentID string) (bool, error) {
	if _, err := tx.Exec(ctx, fmt.Sprintf(minimalRecords, `id = $1 AND account_id = $2 AND profile_id IS NULL`),
		consentID, accountID); err != nil {
		return false, fmt.Errorf("store: keep the consent's record: %w", err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM consents WHERE id = $1 AND account_id = $2 AND subject = 'child'
		AND profile_id IS NULL`, consentID, accountID)
	if err != nil {
		return false, fmt.Errorf("store: take back the consent: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// PendForm records the object a scan is about to go to, before it goes: a row
// of no consent, erased by the daily job today, so that a scan whose request
// failed after it went is erased too, and none lies in the store unknown to the
// database.
func PendForm(ctx context.Context, db Querier, object, mediaType string, at time.Time) error {
	if _, err := db.Exec(ctx, `INSERT INTO consent_forms (object, media_type, sent_at, keep_until)
		VALUES ($1, $2, $3, ($3 AT TIME ZONE 'UTC')::date)`, object, mediaType, at); err != nil {
		return fmt.Errorf("store: pend the form: %w", err)
	}
	return nil
}

// SendForm makes the scan at object, pended, the form of the consent c, a
// parent's that waits, held, and the form sent; the scan it sent before is no
// longer its form, and goes today (R116 spec).
func SendForm(ctx context.Context, tx pgx.Tx, c Consent, object string) error {
	if _, err := tx.Exec(ctx, `UPDATE consent_forms SET consent_id = NULL, keep_until = (now() AT TIME ZONE 'UTC')::date
		WHERE consent_id = $1`, c.ID); err != nil {
		return fmt.Errorf("store: let the form before go: %w", err)
	}
	tag, err := tx.Exec(ctx, `UPDATE consent_forms SET consent_id = $2, keep_until = NULL WHERE object = $1 AND consent_id IS NULL`,
		object, c.ID)
	switch {
	case err != nil:
		return fmt.Errorf("store: record the form: %w", err)
	case tag.RowsAffected() == 0:
		return fmt.Errorf("store: no form pended at %s", object)
	}
	if _, err := tx.Exec(ctx, `UPDATE consents SET form_state = 'sent' WHERE id = $1`, c.ID); err != nil {
		return fmt.Errorf("store: send the form: %w", err)
	}
	return nil
}

// GiveOwnConsent confirms the learner's own consent of the profile, which waits,
// to the version of teen-consent, and says whether it waited.
func GiveOwnConsent(ctx context.Context, tx pgx.Tx, profileID string, textVersion *int32) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE consents SET state = 'confirmed', confirmed_at = now(), method = $2, text_version = $3
		WHERE profile_id = $1 AND giver = 'learner' AND state = 'waiting'`, profileID, ConsentByLearner, textVersion)
	if err != nil {
		return false, fmt.Errorf("store: give the consent: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// Restated is what a profile's age given again sets: the category and the
// country, the age of the country it was given by, and the consent it needs
// after — the one it has, kept, or a new one, which waits: the learner's, or a
// parent's by a new code.
type Restated struct {
	AgeCategory string
	Country     string
	Applied     int
	// Giver is who gives the consent the profile needs after; empty keeps the
	// one it has.
	Giver       string
	RequestCode string
	TextVersion *int32
}

// RestateAge writes the age given again of the profile c is the consent of,
// held: its category, country and the day the age was given, today; and, when
// r names a new giver, a new consent in the place of c, which waits. A code
// another waiting consent has is ErrCodeTaken, in a savepoint of its own, and
// the caller asks again with c: what was done before it holds.
func RestateAge(ctx context.Context, tx pgx.Tx, c Consent, r Restated) error {
	if _, err := tx.Exec(ctx, `UPDATE profiles SET age_category = $2, country = $3,
		age_stated_on = (now() AT TIME ZONE 'UTC')::date WHERE id = $1`, c.ProfileID, r.AgeCategory, r.Country); err != nil {
		return fmt.Errorf("store: give the age again: %w", err)
	}
	if r.Giver == "" {
		if _, err := tx.Exec(ctx, `UPDATE consents SET age_category = $2, country = $3, applied = nullif($4, 0) WHERE id = $1`,
			c.ID, r.AgeCategory, r.Country, r.Applied); err != nil {
			return fmt.Errorf("store: write the consent: %w", err)
		}
		return nil
	}
	// The consent before goes as a deletion takes it: its minimal record, when
	// it was confirmed, and the term of its scan — 5 years, or none for one that
	// waited — by the trigger. The new one is a consent of its own, with an ID of
	// its own, that its minimal record and its scan are kept by (R117 spec).
	if _, err := tx.Exec(ctx, fmt.Sprintf(minimalRecords, `id = $1`), c.ID); err != nil {
		return fmt.Errorf("store: keep the consent's record: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM consents WHERE id = $1`, c.ID); err != nil {
		return fmt.Errorf("store: take the consent before away: %w", err)
	}
	var code, form *string
	if r.Giver == GiverParent {
		code, form = &r.RequestCode, ptr(FormNotSent)
	}
	err := pgx.BeginFunc(ctx, tx, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `INSERT INTO consents (account_id, profile_id, subject, state, age_category, country, giver,
				request_code, form_state, applied, text_version)
			SELECT account_id, id, 'child', 'waiting', $2, $3, $5, $6, $7, nullif($4, 0), $8 FROM profiles WHERE id = $1`,
			c.ProfileID, r.AgeCategory, r.Country, r.Applied, r.Giver, code, form, r.TextVersion)
		return err
	})
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "consents_request_code":
		return ErrCodeTaken
	case err != nil:
		return fmt.Errorf("store: ask the new consent: %w", err)
	}
	return nil
}

// WorkStorageOn is the open record of the storage of the profile's works: who
// gave its consent, and whether it is on.
func WorkStorageOn(ctx context.Context, tx pgx.Tx, profileID string) (giver string, on bool, err error) {
	err = tx.QueryRow(ctx, `SELECT giver FROM work_storage_consents WHERE profile_id = $1 AND off_at IS NULL
		ORDER BY on_at DESC LIMIT 1`, profileID).Scan(&giver)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("store: read the storage of works: %w", err)
	}
	return giver, true, nil
}

// SwitchWorkStorageOn switches the storage of the profile's works on at at, by
// the consent of giver to the version of storage-consent: a record of its own,
// in the place of an open one of another giver (R113 spec; R141 spec).
func SwitchWorkStorageOn(ctx context.Context, tx pgx.Tx, profileID, giver string, textVersion *int32, at time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE work_storage_consents SET off_at = $2 WHERE profile_id = $1 AND off_at IS NULL`,
		profileID, at); err != nil {
		return fmt.Errorf("store: close the record of the storage: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO work_storage_consents (profile_id, giver, text_version, on_at)
		VALUES ($1, $2, $3, $4)`, profileID, giver, textVersion, at); err != nil {
		return fmt.Errorf("store: record the storage: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE profiles SET work_storage = true WHERE id = $1`, profileID); err != nil {
		return fmt.Errorf("store: switch the storage on: %w", err)
	}
	return nil
}

// SwitchWorkStorageOff switches the storage of the profile's works off at at,
// records when, and erases every work the server keeps of the profile, in tx:
// one step (R113 spec; R70). The works go after the profile's row is updated,
// which waits for a PutWork holding it FOR SHARE: the DELETE then sees the work
// that PutWork kept. In the other order the DELETE would lock the rows that
// PutWork waits to update while the UPDATE waits for PutWork.
func SwitchWorkStorageOff(ctx context.Context, tx pgx.Tx, profileID string, at time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE work_storage_consents SET off_at = $2 WHERE profile_id = $1 AND off_at IS NULL`,
		profileID, at); err != nil {
		return fmt.Errorf("store: record the storage off: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE profiles SET work_storage = false WHERE id = $1`, profileID); err != nil {
		return fmt.Errorf("store: switch the storage off: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM works WHERE profile_id = $1`, profileID); err != nil {
		return fmt.Errorf("store: erase the works: %w", err)
	}
	return nil
}

// TextRef is a version of a legal text.
type TextRef struct {
	ID      string
	Version int32
}

// AcceptTexts records the texts the owner accepted with the account, at at.
func AcceptTexts(ctx context.Context, tx pgx.Tx, accountID string, texts []TextRef, at time.Time) error {
	for _, t := range texts {
		if _, err := tx.Exec(ctx, `INSERT INTO account_texts (account_id, text_id, version, accepted_at)
			VALUES ($1, $2, $3, $4)`, accountID, t.ID, t.Version, at); err != nil {
			return fmt.Errorf("store: record the texts accepted: %w", err)
		}
	}
	return nil
}

// Report is a problem report of an adult on a reply of the tutor (R123 spec).
type Report struct {
	ID        string
	AttemptID string
	Reply     string
	Rung      *int16
	Words     string
	Kind      string
	Text      string
	CreatedAt time.Time
}

// WriteReport records the report of the account's profile, at its CreatedAt, and
// gives its ID.
func WriteReport(ctx context.Context, tx pgx.Tx, accountID, profileID string, r Report) (string, error) {
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO problem_reports (account_id, profile_id, attempt_id, reply, rung, words,
			kind, text, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		accountID, profileID, r.AttemptID, r.Reply, r.Rung, r.Words, r.Kind, r.Text, r.CreatedAt).Scan(&id); err != nil {
		return "", fmt.Errorf("store: record the report: %w", err)
	}
	return id, nil
}

// AttemptOfProfile says whether the attempt, open or closed, is the profile's.
func AttemptOfProfile(ctx context.Context, db Querier, profileID, attemptID string) (bool, error) {
	var there bool
	if err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM attempts WHERE id = $1 AND profile_id = $2)`,
		attemptID, profileID).Scan(&there); err != nil {
		return false, fmt.Errorf("store: find the attempt: %w", err)
	}
	return there, nil
}
