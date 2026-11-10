package account

import (
	"context"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/countries"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// The age a child's category changes at (privacy 3.1 spec; R36). While the EU
// and the United Kingdom are closed until the DPIA (R119 spec), the countries
// taken have 13 as the age of digital consent: from_13_to_consent is empty, and
// the table of ages by country comes from spec's epic E4.
const (
	consentAge = 13
	adultAge   = 18
)

// waitingConsentsMax is how many consents for a child may wait under an
// account: a family needs a few, not hundreds, and each request with a new key
// makes a row and a key's record (the 2026-11-09 amendment to R98 spec; R36).
// The slow fingerprint of a request is worked out before the count, so the limit
// bounds the rows, not that work. The number is api's. Until spec's epic E4 gives
// the contract its cause, a request over it is refused as invalid_argument with
// no Refusal, and nothing but the deletion of the account takes a consent out of
// waiting: E4 brings the confirmation and the removal of one that waits.
const waitingConsentsMax = 10

// category is the age category of a child of age in years.
func category(age int32) string {
	if age < consentAge {
		return "under_13"
	}
	return "from_consent_to_18"
}

// RequestChildConsent records a consent for a child that waits, with the
// category worked out of the age and the country; the age in years lives in this
// request's memory alone (privacy 3.3 spec). A repeat under its key is the same
// consent, at the limit too, since it is answered before the consents are
// counted; the key's fingerprint is slow, as the request carries the age.
func (s *Service) RequestChildConsent(ctx context.Context, req *v1.RequestChildConsentRequest) (*v1.RequestChildConsentResponse, error) {
	if req.AgeYears == nil || req.GetAgeYears() < 0 || req.GetAgeYears() >= adultAge {
		return nil, rpc.Malformed("age_years")
	}
	if !countries.Valid(req.GetCountry()) {
		return nil, rpc.Malformed("country")
	}
	category := category(req.GetAgeYears())
	return keyedOnce(ctx, s, apiv1connect.AccountServiceRequestChildConsentProcedure, req,
		func(tx pgx.Tx, accountID string, _ time.Time) (*v1.RequestChildConsentResponse, error) {
			if err := accepted(ctx, tx, req.GetCountry()); err != nil {
				return nil, err
			}
			waiting, err := store.WaitingConsents(ctx, tx, accountID)
			if err != nil {
				return nil, err
			}
			if waiting >= waitingConsentsMax {
				return nil, connect.NewError(connect.CodeInvalidArgument, "too many consents waiting")
			}
			c, err := store.CreateChildConsent(ctx, tx, accountID, category, req.GetCountry())
			if err != nil {
				return nil, err
			}
			return &v1.RequestChildConsentResponse{Consent: childConsent(c)}, nil
		})
}

// CreateChildProfile creates a child's profile by its consent, once it is
// confirmed, of its category and country, and its tuple before the COMMIT. A
// consent with its profile already is CAUSE_ALREADY_EXISTS before its state is
// looked at, so that a repeat is told the profile is there.
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
		case c.State != store.ConsentConfirmed:
			return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)
		}
		p = newProfile(c.AgeCategory, c.Country)
		if err := apply(&p, req.GetSettings(), true); err != nil {
			return err
		}
		if p, err = store.CreateChildProfile(ctx, tx, caller.AccountID, c, p); err != nil {
			return err
		}
		return s.rights.WriteOwner(ctx, caller.AccountID, p.ID)
	})
	if err != nil {
		return nil, refusedProfile(ctx, err)
	}
	return &v1.CreateChildProfileResponse{Profile: profile(p)}, nil
}

func notFound(ctx context.Context) error {
	return rpc.Refuse(ctx, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
}

func childConsent(c store.ChildConsent) *v1.ChildConsent {
	return &v1.ChildConsent{ConsentId: c.ID, AgeCategory: c.AgeCategory, Country: c.Country, State: consentStates[c.State]}
}
