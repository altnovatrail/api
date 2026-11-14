package account

import (
	"context"
	"errors"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"

	"github.com/altnovatrail/api/internal/admission"
	"github.com/altnovatrail/api/internal/consentage"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// OwnKeys is what tells the providers open to an own key at an age category,
// and checks a key: admission.Gate, which asks tutor's choice and tutor's
// check (R72, R73).
type OwnKeys interface {
	OwnKeyProviders(category formatv0.AgeCategory) []admission.Provider
	CheckKey(ctx context.Context, category formatv0.AgeCategory, provider, key string) (tutor.KeyCheck, error)
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

// CheckOwnKey checks the owner's own key at its provider through tutor, by a
// request with nothing of a learner's, when the key is connected (R73; R84
// spec, R96 tutor). A key with no provider or no key is invalid_argument, as
// on the requests of an attempt, and a profile not of the category adult is
// CAUSE_KEY_NOT_FOR_PROFILE, both with no call; a provider closed to the key
// at the profile is PROVIDER_CLOSED with no call, as ListOwnKeyProviders
// leaves it out. A provider that gave no answer is no refusal: the check is
// OWN_KEY_CHECK_UNSPECIFIED with the outcome CAUSE_AI_FAILURE. The key lives
// in the request's memory alone: the log line names how the check ended.
func (s *Service) CheckOwnKey(ctx context.Context, req *v1.CheckOwnKeyRequest) (*v1.CheckOwnKeyResponse, error) {
	if s.ownKeys == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, "no gate is configured")
	}
	caller, _ := rpc.CallerFrom(ctx)
	l, err := store.LearnerOf(ctx, s.db, caller.AccountID, req.GetProfileId())
	if err != nil {
		return nil, refusedProfile(ctx, err)
	}
	own := req.GetOwnKey()
	if own.GetProvider() == "" || own.GetKey() == "" {
		return nil, rpc.Refuse(ctx, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	}
	if l.AgeCategory != consentage.Adult {
		return nil, rpc.Refuse(ctx, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE)
	}
	check, err := s.ownKeys.CheckKey(ctx, formatv0.AgeCategoryAdult, own.GetProvider(), own.GetKey())
	if errors.Is(err, admission.ErrTooLittleTime) {
		// The client's deadline, not the provider, left no time, as on the
		// requests of an attempt.
		return nil, connect.NewError(connect.CodeDeadlineExceeded, "too little time to check the key")
	}
	// Otherwise the error says which model failed how, and the answer says it
	// all: the log keeps the end alone.
	out, word := checked(check)
	rpc.NoteKeyCheck(ctx, word)
	if c := out.GetOutcome().GetCause(); c != v1.Cause_CAUSE_UNSPECIFIED {
		rpc.NoteOutcome(ctx, c)
	}
	return out, nil
}

// checked is the answer of a check that ended so, and its word in the log: an
// end tutor does not name is a provider that gave no answer.
func checked(check tutor.KeyCheck) (*v1.CheckOwnKeyResponse, string) {
	switch check {
	case tutor.KeyAccepted:
		return &v1.CheckOwnKeyResponse{Check: v1.OwnKeyCheck_OWN_KEY_CHECK_ACCEPTED}, logging.KeyCheckAccepted
	case tutor.KeyWrong:
		return &v1.CheckOwnKeyResponse{Check: v1.OwnKeyCheck_OWN_KEY_CHECK_INVALID}, logging.KeyCheckInvalid
	case tutor.KeyNoRights:
		return &v1.CheckOwnKeyResponse{Check: v1.OwnKeyCheck_OWN_KEY_CHECK_NO_PERMISSION}, logging.KeyCheckNoPermission
	case tutor.KeyProviderClosed:
		return &v1.CheckOwnKeyResponse{Check: v1.OwnKeyCheck_OWN_KEY_CHECK_PROVIDER_CLOSED}, logging.KeyCheckProviderClosed
	}
	return &v1.CheckOwnKeyResponse{Check: v1.OwnKeyCheck_OWN_KEY_CHECK_UNSPECIFIED,
		Outcome: &v1.Outcome{Cause: v1.Cause_CAUSE_AI_FAILURE}}, logging.KeyCheckUnanswered
}
