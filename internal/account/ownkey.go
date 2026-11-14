package account

import (
	"context"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"

	"github.com/altnovatrail/api/internal/admission"
	"github.com/altnovatrail/api/internal/consentage"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// OwnKeys is what tells the providers open to an own key at an age category:
// admission.Gate, which asks tutor's choice (R72).
type OwnKeys interface {
	OwnKeyProviders(category formatv0.AgeCategory) []admission.Provider
}

// WithOwnKeys is the gate the own key's requests ask; with none, they are
// refused unimplemented.
func WithOwnKeys(gate OwnKeys) Option { return func(s *Service) { s.ownKeys = gate } }

// ListOwnKeyProviders names the providers of the admission table, in its order,
// whose own key tutor admits at the profile's category for one kind of call
// api makes at least, by code and name as the table writes them (R72; R84,
// R96 spec): none at a category not adult, with no refusal. The checks have
// held can_manage; a profile gone since is CAUSE_NO_PROFILE.
func (s *Service) ListOwnKeyProviders(ctx context.Context, req *v1.ListOwnKeyProvidersRequest) (*v1.ListOwnKeyProvidersResponse, error) {
	if s.ownKeys == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, "no gate is configured")
	}
	caller, _ := rpc.CallerFrom(ctx)
	// The category alone, in one query: the request is an adult's, which no
	// consent of the profile's refuses (contract, ListOwnKeyProviders).
	l, err := store.LearnerOf(ctx, s.db, caller.AccountID, req.GetProfileId())
	if err != nil {
		return nil, refusedProfile(ctx, err)
	}
	out := &v1.ListOwnKeyProvidersResponse{}
	if l.AgeCategory != consentage.Adult {
		// The table opens no own key below 18 (R122 spec), and the contract
		// gives none whatever it says.
		return out, nil
	}
	for _, provider := range s.ownKeys.OwnKeyProviders(formatv0.AgeCategoryAdult) {
		out.Providers = append(out.Providers, &v1.OwnKeyProvider{Provider: string(provider.Code), Name: provider.Name})
	}
	return out, nil
}
