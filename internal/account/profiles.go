package account

import (
	"context"
	"errors"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/countries"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// errCountry is a country the deployment does not take, found in the
// transaction and answered after it.
var errCountry = errors.New("account: the country is not accepted")

// CreateOwnerProfile creates the owner's profile, of the category adult by the
// owner's statement, with its consent, and its tuple before the COMMIT (R04,
// R36; architecture 3.1). A profile of the owner there already comes before the
// country, so that a repeat is told it is there.
func (s *Service) CreateOwnerProfile(ctx context.Context, req *v1.CreateOwnerProfileRequest) (*v1.CreateOwnerProfileResponse, error) {
	if !countries.Valid(req.GetCountry()) {
		return nil, malformed("country")
	}
	p := newProfile("adult", req.GetCountry())
	if err := apply(&p, req.GetSettings(), true); err != nil {
		return nil, err
	}
	caller, _ := rpc.CallerFrom(ctx)
	err := store.WriteUnderAccount(ctx, s.db, caller.AccountID, func(tx pgx.Tx) error {
		if err := ownerProfileAbsent(ctx, tx, caller.AccountID); err != nil {
			return err
		}
		if err := accepted(ctx, tx, p.Country); err != nil {
			return err
		}
		created, err := store.CreateOwnerProfile(ctx, tx, caller.AccountID, p)
		if err != nil {
			return err
		}
		p = created
		return s.rights.WriteOwner(ctx, caller.AccountID, created.ID)
	})
	if err != nil {
		return nil, refusedProfile(ctx, err)
	}
	return &v1.CreateOwnerProfileResponse{Profile: profile(p)}, nil
}

// UpdateProfile changes the settings sent, and leaves the others (contract,
// ProfileSettings). The checks have held the right can_manage.
func (s *Service) UpdateProfile(ctx context.Context, req *v1.UpdateProfileRequest) (*v1.UpdateProfileResponse, error) {
	p, err := s.change(ctx, req.GetProfileId(), req.GetSettings())
	if err != nil {
		return nil, err
	}
	return &v1.UpdateProfileResponse{Profile: profile(p)}, nil
}

// SetVoicing sets the voicing alone, from the screen of a task: the checks have
// held the right can_learn, which the learner has as well as the adult.
func (s *Service) SetVoicing(ctx context.Context, req *v1.SetVoicingRequest) (*v1.SetVoicingResponse, error) {
	p, err := s.change(ctx, req.GetProfileId(), &v1.ProfileSettings{Voicing: req.GetVoicing()})
	if err != nil {
		return nil, err
	}
	return &v1.SetVoicingResponse{Profile: profile(p)}, nil
}

// change applies settings to the account's profile, held FOR UPDATE under the
// account.
func (s *Service) change(ctx context.Context, profileID string, settings *v1.ProfileSettings) (store.Profile, error) {
	caller, _ := rpc.CallerFrom(ctx)
	var p store.Profile
	err := store.WriteUnderAccount(ctx, s.db, caller.AccountID, func(tx pgx.Tx) error {
		var err error
		if p, err = store.ProfileFor(ctx, tx, caller.AccountID, profileID); err != nil {
			return err
		}
		if err := apply(&p, settings, false); err != nil {
			return err
		}
		return store.WriteProfile(ctx, tx, p)
	})
	if err != nil {
		return store.Profile{}, refusedProfile(ctx, err)
	}
	return p, nil
}

// ownerProfileAbsent refuses an account whose owner has a profile already.
func ownerProfileAbsent(ctx context.Context, tx pgx.Tx, accountID string) error {
	has, err := store.HasOwnerProfile(ctx, tx, accountID)
	switch {
	case err != nil:
		return err
	case has:
		return store.ErrProfileExists
	}
	return nil
}

// accepted refuses a country the deployment does not take (R118, R119 spec).
func accepted(ctx context.Context, tx pgx.Tx, country string) error {
	ok, err := store.CountryAccepted(ctx, tx, country)
	switch {
	case err != nil:
		return err
	case !ok:
		return errCountry
	}
	return nil
}

// refusedProfile answers what a request on a profile met: a refusal of the
// contract, a form refused, or a failure.
func refusedProfile(ctx context.Context, err error) error {
	var refusal *connect.Error
	switch {
	case errors.As(err, &refusal):
		return err
	case errors.Is(err, errCountry):
		return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_COUNTRY_NOT_ACCEPTED)
	case errors.Is(err, store.ErrProfileExists):
		return alreadyExists(ctx)
	case errors.Is(err, store.ErrNoProfile):
		return rpc.Refuse(ctx, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
	case errors.Is(err, store.ErrNoConsent):
		return notFound(ctx)
	}
	return refused(ctx, err)
}
