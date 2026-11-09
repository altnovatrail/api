package account

import (
	"context"
	"errors"

	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"

	"github.com/altnovatrail/api/internal/deletion"
	"github.com/altnovatrail/api/internal/rpc"
)

// DeleteProfile deletes the profile and all of it, by can_manage and a fresh
// sign-in, which the checks held (R20, R21). The owner's profile goes the same
// way, and the last profile leaves the account without profiles. A tuple left
// after the database is done is still "deleted": its line names the failure,
// and the profile's row is gone, so the tuple gives a right to nothing; its
// repeat deletes it (architecture 3.10).
func (s *Service) DeleteProfile(ctx context.Context, req *v1.DeleteProfileRequest) (*v1.DeleteProfileResponse, error) {
	caller, _ := rpc.CallerFrom(ctx)
	if err := deleted(ctx, s.deleter.Profile(ctx, caller.AccountID, req.GetProfileId())); err != nil {
		return nil, err
	}
	return &v1.DeleteProfileResponse{}, nil
}

// DeleteAccount deletes the account, all of it and its user at the sign-in
// service, by a fresh sign-in, which the checks held (R20, R21). Once its data
// is gone from the database the answer is "deleted", and what does not finish
// the daily job does; a repeat gets "the account is gone" from the checks.
func (s *Service) DeleteAccount(ctx context.Context, _ *v1.DeleteAccountRequest) (*v1.DeleteAccountResponse, error) {
	caller, _ := rpc.CallerFrom(ctx)
	if err := deleted(ctx, s.deleter.Account(ctx, caller.AccountID, caller.UID)); err != nil {
		return nil, err
	}
	return &v1.DeleteAccountResponse{}, nil
}

// deleted answers a deletion: done, or done in the database with the rest left
// to the job — "deleted", with the failure in the log — or a refusal.
func deleted(ctx context.Context, err error) error {
	var unfinished *deletion.Unfinished
	switch {
	case err == nil:
		return nil
	case errors.As(err, &unfinished):
		rpc.NoteFailure(ctx, unfinished)
		return nil
	}
	return refusedProfile(ctx, err)
}
