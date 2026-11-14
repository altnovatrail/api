package account

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/consentage"
	"github.com/altnovatrail/api/internal/legal"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// youngestAge and oldestAge bound the age in years of a child: from 0 to 17
// (contract, RequestChildConsent).
const (
	youngestAge = 0
	oldestAge   = 17
)

// waitingConsentsMax is how many consents for a child may wait under an
// account with no profile: a family needs a few, not hundreds, and each request
// with a new key makes a row and a key's record (R36; contract,
// CAUSE_TOO_MANY_CONSENTS). The slow fingerprint of a request is worked out
// before the count, so the limit bounds the rows, not that work. The number is
// api's.
const waitingConsentsMax = 10

// The scan of a form (contract, SendConsentForm): up to 10 MiB, a photo or a PDF.
const maxForm = 10 << 20

var formTypes = map[string]bool{"image/jpeg": true, "image/png": true, "application/pdf": true}

// codeTries is how many codes of a request are drawn before the request fails:
// 32^10 codes leave a second draw all but never needed.
const codeTries = 4

// crockford is the alphabet of Crockford's base32, which leaves out I, L, O
// and U.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// requestCode is a code of a parent's request: ten characters of Crockford's
// base32 in two groups of five, ABCDE-FGHJK (contract, ChildConsent).
func requestCode() string {
	var b [10]byte
	_, _ = rand.Read(b[:])
	code := make([]byte, 0, 11)
	for i, c := range b {
		if i == 5 {
			code = append(code, '-')
		}
		code = append(code, crockford[c%32])
	}
	return string(code)
}

// RequestChildConsent records a consent for a child that waits, with the
// category worked out of the age and the country by the table of the age of
// digital consent; the age in years lives in this request's memory alone
// (privacy 3.3 spec; R134 spec). Below the age of the country the consent is a
// parent's, by a code of its request; from it, the learner's own. The request
// names the current version of parent-consent whoever gives it, on the day of
// its time in UTC. A repeat under its key is the same consent, at the limit and
// past the day of a new version too, since the limit and the texts are checked
// after the key.
func (s *Service) RequestChildConsent(ctx context.Context, req *v1.RequestChildConsentRequest) (*v1.RequestChildConsentResponse, error) {
	if req.AgeYears == nil || req.GetAgeYears() < youngestAge || req.GetAgeYears() > oldestAge {
		return nil, rpc.Malformed("age_years")
	}
	if !s.ages.Country(req.GetCountry()) {
		return nil, rpc.Malformed("country")
	}
	return keyedOnce(ctx, s, apiv1connect.AccountServiceRequestChildConsentProcedure, req, true,
		func(tx pgx.Tx, accountID string, now time.Time) (*v1.RequestChildConsentResponse, error) {
			if !s.texts.On(now).NamesOne(req.GetParentConsentText(), legal.ParentConsent) {
				return nil, textNotCurrent(ctx)
			}
			if err := accepted(ctx, tx, req.GetCountry()); err != nil {
				return nil, err
			}
			waiting, err := store.WaitingConsents(ctx, tx, accountID)
			if err != nil {
				return nil, err
			}
			if waiting >= waitingConsentsMax {
				return nil, rpc.Refuse(ctx, connect.CodeResourceExhausted, v1.Cause_CAUSE_TOO_MANY_CONSENTS)
			}
			category, applied := s.ages.Category(int(req.GetAgeYears()), req.GetCountry(), now)
			c := store.NewConsent{AgeCategory: category, Country: req.GetCountry(), Applied: applied, Giver: store.GiverLearner}
			if consentage.ParentConsents(category) {
				c.Giver, c.TextVersion = store.GiverParent, textVersion(req.GetParentConsentText())
			}
			made, err := s.askConsent(ctx, tx, accountID, c)
			if err != nil {
				return nil, err
			}
			return &v1.RequestChildConsentResponse{Consent: childConsent(made)}, nil
		})
}

// askConsent records the consent c, with a code of its own when it is a
// parent's: a code another waiting consent has is drawn again.
func (s *Service) askConsent(ctx context.Context, tx pgx.Tx, accountID string, c store.NewConsent) (store.ChildConsent, error) {
	for range codeTries {
		if c.Giver == store.GiverParent {
			c.RequestCode = requestCode()
		}
		made, err := store.AskChildConsent(ctx, tx, accountID, c)
		if !errors.Is(err, store.ErrCodeTaken) {
			return made, err
		}
	}
	return store.ChildConsent{}, errors.New("account: no code of a request was free")
}

// textVersion is the version a reference names, or none.
func textVersion(ref *v1.LegalTextRef) *int32 {
	if ref == nil {
		return nil
	}
	v := ref.GetVersion()
	return &v
}

// CreateChildProfile creates a child's profile by its consent, of its category
// and country, and its tuple before the COMMIT: a parent's consent once it is
// confirmed; the learner's own at once, and the profile waits for it (contract,
// CreateChildProfile). A consent with its profile already is
// CAUSE_ALREADY_EXISTS before its state is looked at, so that a repeat is told
// the profile is there.
func (s *Service) CreateChildProfile(ctx context.Context, req *v1.CreateChildProfileRequest) (*v1.CreateChildProfileResponse, error) {
	consentID := req.GetConsentId()
	if !store.CanonicalID(consentID) {
		return nil, notFound(ctx)
	}
	caller, _ := rpc.CallerFrom(ctx)
	var p store.Profile
	err := store.WriteUnderAccount(ctx, s.db, caller.AccountID, func(tx pgx.Tx) error {
		c, err := store.ConsentFor(ctx, tx, caller.AccountID, consentID)
		switch {
		case err != nil:
			return err
		case c.ProfileID != "":
			return store.ErrProfileExists
		case c.State != store.ConsentConfirmed && c.Giver != store.GiverLearner:
			return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)
		}
		p = newProfile(c.AgeCategory, c.Country)
		if err := apply(&p, req.GetSettings(), true); err != nil {
			return err
		}
		if p, err = store.CreateChildProfile(ctx, tx, caller.AccountID, c, p); err != nil {
			return err
		}
		if p, err = store.ProfileWithConsent(ctx, tx, caller.AccountID, p.ID); err != nil {
			return err
		}
		return s.rights.WriteOwner(ctx, caller.AccountID, p.ID)
	})
	if err != nil {
		return nil, refusedProfile(ctx, err)
	}
	return &v1.CreateChildProfileResponse{Profile: s.profile(p)}, nil
}

// SendConsentForm keeps the scan of the signed form of a parent's consent that
// waits — the account's own, or one a profile waits for — in the operator's
// store, apart from the database, where the operator checks it against the code
// and confirms the consent (R116 spec). A new scan takes the place of the one
// before. With no store configured, the request is refused unavailable. A
// repeat under its key gives the same consent, with the form it sent; the key's
// fingerprint is fast, as the scan carries no PIN and no age.
func (s *Service) SendConsentForm(ctx context.Context, req *v1.SendConsentFormRequest) (*v1.SendConsentFormResponse, error) {
	switch {
	case !store.CanonicalID(req.GetConsentId()):
		return nil, notFound(ctx)
	case len(req.GetForm()) == 0 || len(req.GetForm()) > maxForm:
		return nil, rpc.Malformed("form")
	case !formTypes[req.GetMediaType()]:
		return nil, rpc.Malformed("media_type")
	case s.forms == nil:
		return nil, connect.NewError(connect.CodeUnavailable, "no store of the forms is configured")
	}
	caller, _ := rpc.CallerFrom(ctx)
	object, err := s.sendScan(ctx, caller.AccountID, req)
	if err != nil {
		return nil, refusedProfile(ctx, err)
	}
	return keyedOnce(ctx, s, apiv1connect.AccountServiceSendConsentFormProcedure, req, false,
		func(tx pgx.Tx, accountID string, now time.Time) (*v1.SendConsentFormResponse, error) {
			c, err := store.ConsentFor(ctx, tx, accountID, req.GetConsentId())
			switch {
			case err != nil:
				return nil, err
			case c.Giver != store.GiverParent:
				return nil, store.ErrNoConsent
			case c.State == store.ConsentConfirmed:
				return nil, alreadyExists(ctx)
			}
			if object == "" {
				return nil, errors.New("account: the consent waited for its form only after its scan was refused")
			}
			if err := store.SendForm(ctx, tx, c, object); err != nil {
				return nil, err
			}
			c.FormState = store.FormSent
			return &v1.SendConsentFormResponse{Consent: childConsent(c.ChildConsent)}, nil
		})
}

// sendScan puts the scan of the request in the store of the forms, out of every
// transaction, at an object of its own, which it pends first: a scan whose
// request fails after it went is erased by the daily job, and no upload holds
// the account. A consent that takes no form gives no object, and the request
// under its key answers as its record or its refusal says.
func (s *Service) sendScan(ctx context.Context, accountID string, req *v1.SendConsentFormRequest) (string, error) {
	var suffix [8]byte
	_, _ = rand.Read(suffix[:])
	object := req.GetConsentId() + "-" + hex.EncodeToString(suffix[:])
	sendable := false
	err := store.WriteUnderAccount(ctx, s.db, accountID, func(tx pgx.Tx) error {
		c, err := store.ConsentFor(ctx, tx, accountID, req.GetConsentId())
		switch {
		case errors.Is(err, store.ErrNoConsent):
			return nil
		case err != nil:
			return err
		case c.Giver != store.GiverParent || c.State != store.ConsentWaiting:
			return nil
		}
		sendable = true
		return store.PendForm(ctx, tx, object, req.GetMediaType(), s.now())
	})
	if err != nil || !sendable {
		return "", err
	}
	if err := s.forms.Put(ctx, object, req.GetForm(), req.GetMediaType()); err != nil {
		return "", err
	}
	return object, nil
}

// CancelChildConsent takes back a consent for a child that has no profile: one
// that waits is erased with its form — the daily job erases the scan —, one
// confirmed leaves its minimal record and its form for their term (R117 spec).
// A consent not there is CAUSE_NOT_FOUND, which is the answer to a repeat too.
func (s *Service) CancelChildConsent(ctx context.Context, req *v1.CancelChildConsentRequest) (*v1.CancelChildConsentResponse, error) {
	if !store.CanonicalID(req.GetConsentId()) {
		return nil, notFound(ctx)
	}
	caller, _ := rpc.CallerFrom(ctx)
	err := store.WriteUnderAccount(ctx, s.db, caller.AccountID, func(tx pgx.Tx) error {
		cancelled, err := store.CancelChildConsent(ctx, tx, caller.AccountID, req.GetConsentId())
		if err == nil && !cancelled {
			return store.ErrNoConsent
		}
		return err
	})
	if err != nil {
		return nil, refusedProfile(ctx, err)
	}
	return &v1.CancelChildConsentResponse{}, nil
}

// GiveOwnConsent gives the learner's own consent at their profile, to the
// current version of teen-consent: a profile below the age of digital consent,
// whose consent an adult gives, is CAUSE_ADULT_NEEDED. A consent given already
// is the answer, as a repeat's.
func (s *Service) GiveOwnConsent(ctx context.Context, req *v1.GiveOwnConsentRequest) (*v1.GiveOwnConsentResponse, error) {
	caller, _ := rpc.CallerFrom(ctx)
	var p store.Profile
	err := store.WriteUnderAccount(ctx, s.db, caller.AccountID, func(tx pgx.Tx) error {
		c, err := s.consentOf(ctx, tx, caller.AccountID, req.GetProfileId())
		switch {
		case err != nil:
			return err
		case c.Giver != store.GiverLearner:
			return adultNeeded(ctx)
		case c.State == store.ConsentWaiting && !s.texts.On(s.now()).NamesOne(req.GetText(), legal.TeenConsent):
			return textNotCurrent(ctx)
		case c.State == store.ConsentWaiting:
			if _, err := store.GiveOwnConsent(ctx, tx, c.ProfileID, textVersion(req.GetText())); err != nil {
				return err
			}
		}
		p, err = store.ProfileWithConsent(ctx, tx, caller.AccountID, c.ProfileID)
		return err
	})
	if err != nil {
		return nil, refusedProfile(ctx, err)
	}
	return &v1.GiveOwnConsentResponse{Profile: s.profile(p)}, nil
}

// WithdrawOwnConsent withdraws the learner's own consent — given, or asked of a
// learner who reached the age while a parent's stood (R141 spec) — and deletes
// their profile and all of it, as DeleteProfile does, leaving the consent's
// minimal record and the problem reports (R117, R132 spec). A consent an adult
// gives is CAUSE_ADULT_NEEDED; a profile gone, a repeat's, CAUSE_NO_PROFILE.
func (s *Service) WithdrawOwnConsent(ctx context.Context, req *v1.WithdrawOwnConsentRequest) (*v1.WithdrawOwnConsentResponse, error) {
	caller, _ := rpc.CallerFrom(ctx)
	err := store.WriteUnderAccount(ctx, s.db, caller.AccountID, func(tx pgx.Tx) error {
		c, err := s.consentOf(ctx, tx, caller.AccountID, req.GetProfileId())
		if err == nil && c.Giver != store.GiverLearner {
			return adultNeeded(ctx)
		}
		return err
	})
	if err != nil {
		return nil, refusedProfile(ctx, err)
	}
	if err := deleted(ctx, s.deleter.Profile(ctx, caller.AccountID, req.GetProfileId())); err != nil {
		return nil, err
	}
	return &v1.WithdrawOwnConsentResponse{}, nil
}

// RestateAge gives the age of a profile again — after CAUSE_AGE_NEEDED, at the
// yearly check, or with a new country — and works out its category anew by the
// table, keeping the age nowhere (privacy 3.3 spec; R134 spec). A category whose
// consent another gives leaves the profile waiting for it: the learner's own,
// or a parent's by a new code; the consent before is kept as its minimal
// record. The owner's profile gives its country alone. A repeat under its key is
// the same profile; the fingerprint is slow, as the request carries the age.
func (s *Service) RestateAge(ctx context.Context, req *v1.RestateAgeRequest) (*v1.RestateAgeResponse, error) {
	if req.AgeYears != nil && (req.GetAgeYears() < youngestAge || req.GetAgeYears() > oldestAge) {
		return nil, rpc.Malformed("age_years")
	}
	if !s.ages.Country(req.GetCountry()) {
		return nil, rpc.Malformed("country")
	}
	return keyedOnce(ctx, s, apiv1connect.AccountServiceRestateAgeProcedure, req, true,
		func(tx pgx.Tx, accountID string, now time.Time) (*v1.RestateAgeResponse, error) {
			if !s.texts.On(now).NamesOne(req.GetParentConsentText(), legal.ParentConsent) {
				return nil, textNotCurrent(ctx)
			}
			c, err := s.consentOf(ctx, tx, accountID, req.GetProfileId())
			if err != nil {
				return nil, err
			}
			if err := accepted(ctx, tx, req.GetCountry()); err != nil {
				return nil, err
			}
			r := store.Restated{AgeCategory: consentage.Adult, Country: req.GetCountry()}
			switch {
			case c.Giver == store.GiverOwner && req.AgeYears != nil:
				return nil, rpc.Malformed("age_years")
			case c.Giver == store.GiverOwner:
			case req.AgeYears == nil:
				return nil, rpc.Malformed("age_years")
			default:
				r.AgeCategory, r.Applied = s.ages.Category(int(req.GetAgeYears()), req.GetCountry(), now)
				giver := store.GiverLearner
				if consentage.ParentConsents(r.AgeCategory) {
					giver = store.GiverParent
				}
				if giver != c.Giver || c.State == store.ConsentWaiting && giver == store.GiverParent {
					r.Giver = giver
				}
				if giver == store.GiverParent {
					r.TextVersion = textVersion(req.GetParentConsentText())
				}
			}
			if err := s.restate(ctx, tx, c, r); err != nil {
				return nil, err
			}
			p, err := store.ProfileWithConsent(ctx, tx, accountID, c.ProfileID)
			if err != nil {
				return nil, err
			}
			return &v1.RestateAgeResponse{Profile: s.profile(p)}, nil
		})
}

// restate writes r, with a code of its own when it asks a parent's consent.
func (s *Service) restate(ctx context.Context, tx pgx.Tx, c store.Consent, r store.Restated) error {
	for range codeTries {
		if r.Giver == store.GiverParent {
			r.RequestCode = requestCode()
		}
		if err := store.RestateAge(ctx, tx, c, r); !errors.Is(err, store.ErrCodeTaken) {
			return err
		}
	}
	return errors.New("account: no code of a request was free")
}

// SetWorkStorage switches the storage of the learner's works on the server on
// or off (R113, R127 spec): an adult with can_manage, as the action ADULT, or,
// at a profile whose learner consents themselves, the learner alone, as the
// action LEARNING — the other is CAUSE_LEARNER_DECIDES or CAUSE_ADULT_NEEDED.
// Switching on names the current version of storage-consent and needs the
// profile's consent; switching off records when, even while it waits. A learner
// who switches on what an adult did gives their own consent in its place (R141
// spec). A state that holds already is the answer, as a repeat's.
func (s *Service) SetWorkStorage(ctx context.Context, req *v1.SetWorkStorageRequest) (*v1.SetWorkStorageResponse, error) {
	action := req.GetAction()
	if action != v1.Action_ACTION_ADULT && action != v1.Action_ACTION_LEARNING {
		return nil, rpc.Malformed("action")
	}
	caller, _ := rpc.CallerFrom(ctx)
	if action == v1.Action_ACTION_ADULT {
		can, err := s.rights.Can(ctx, caller.AccountID, authz.CanManage, req.GetProfileId())
		switch {
		case err != nil:
			return nil, rpc.Failed(ctx, err)
		case !can:
			return nil, rpc.Refuse(ctx, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
		}
	}
	var p store.Profile
	err := store.WriteUnderAccount(ctx, s.db, caller.AccountID, func(tx pgx.Tx) error {
		c, err := s.consentOf(ctx, tx, caller.AccountID, req.GetProfileId())
		if err != nil {
			return err
		}
		learnerDecides := c.Giver == store.GiverLearner
		switch {
		case learnerDecides && action == v1.Action_ACTION_ADULT:
			return rpc.Refuse(ctx, connect.CodePermissionDenied, v1.Cause_CAUSE_LEARNER_DECIDES)
		case !learnerDecides && action == v1.Action_ACTION_LEARNING:
			return adultNeeded(ctx)
		}
		giver, on, err := store.WorkStorageOn(ctx, tx, c.ProfileID)
		if err != nil {
			return err
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		switch {
		case !req.GetEnabled() && on:
			err = store.SwitchWorkStorageOff(ctx, tx, c.ProfileID, now)
		case !req.GetEnabled():
		case s.need(c, now) != v1.ConsentNeed_CONSENT_NEED_NONE:
			return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)
		case on && giver == c.Giver:
		case !s.texts.On(now).NamesOne(req.GetText(), legal.StorageConsent):
			return textNotCurrent(ctx)
		default:
			err = store.SwitchWorkStorageOn(ctx, tx, c.ProfileID, c.Giver, textVersion(req.GetText()), now)
		}
		if err != nil {
			return err
		}
		p, err = store.ProfileWithConsent(ctx, tx, caller.AccountID, c.ProfileID)
		return err
	})
	if err != nil {
		return nil, refusedProfile(ctx, err)
	}
	return &v1.SetWorkStorageResponse{Profile: s.profile(p)}, nil
}

// consentOf is the consent of the account's profile, held, or ErrNoProfile: a
// profile has a consent always.
func (s *Service) consentOf(ctx context.Context, tx pgx.Tx, accountID, profileID string) (store.Consent, error) {
	if !store.CanonicalID(profileID) {
		return store.Consent{}, store.ErrNoProfile
	}
	c, err := store.ConsentOfProfile(ctx, tx, accountID, profileID)
	if errors.Is(err, store.ErrNoConsent) {
		return store.Consent{}, store.ErrNoProfile
	}
	return c, err
}

// need is what the profile of the consent c needs before its learning at now: a
// parent's form, the learner's own consent, the age given again, or nothing.
func (s *Service) need(c store.Consent, now time.Time) v1.ConsentNeed {
	switch {
	case c.State == store.ConsentWaiting && c.Giver == store.GiverParent:
		return v1.ConsentNeed_CONSENT_NEED_PARENT
	case c.State == store.ConsentWaiting:
		return v1.ConsentNeed_CONSENT_NEED_OWN
	case s.ages.AskAgain(c.AgeCategory, c.Applied, c.Country, now):
		return v1.ConsentNeed_CONSENT_NEED_AGE
	}
	return v1.ConsentNeed_CONSENT_NEED_NONE
}

func notFound(ctx context.Context) error {
	return rpc.Refuse(ctx, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
}

func textNotCurrent(ctx context.Context) error {
	return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_TEXT_NOT_CURRENT)
}

func adultNeeded(ctx context.Context) error {
	return rpc.Refuse(ctx, connect.CodePermissionDenied, v1.Cause_CAUSE_ADULT_NEEDED)
}

// givers is who gives a consent, in the contract's words.
var givers = map[string]v1.ConsentGiver{
	store.GiverOwner:   v1.ConsentGiver_CONSENT_GIVER_OWNER,
	store.GiverParent:  v1.ConsentGiver_CONSENT_GIVER_PARENT,
	store.GiverLearner: v1.ConsentGiver_CONSENT_GIVER_LEARNER,
}

// formStates is the state of a parent's form, in the contract's words.
var formStates = map[string]v1.FormState{
	store.FormNotSent:     v1.FormState_FORM_STATE_NOT_SENT,
	store.FormSent:        v1.FormState_FORM_STATE_SENT,
	store.FormNotAccepted: v1.FormState_FORM_STATE_NOT_ACCEPTED,
}

func childConsent(c store.ChildConsent) *v1.ChildConsent {
	return &v1.ChildConsent{ConsentId: c.ID, AgeCategory: c.AgeCategory, Country: c.Country, State: consentStates[c.State],
		Giver: givers[c.Giver], RequestCode: c.RequestCode, Form: formStates[c.FormState]}
}
