package account

import (
	"context"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// GetQuota is the share of the family day's quota of platform AI that the
// profile has spent, and beside an adult's action the account's too, never a
// sum in dollars (the amendment of 2026-11-12 to R11; Д229 spec). A learning
// action holds can_learn, which the checks asked, and the profile's consent and
// age, as every learning request; an adult's, can_view, which the handler asks —
// the checks before it hold can_learn alone, as for the skill map. The share is
// of the spending settled, without the reserves of calls still going, on the
// day the account keeps: the request carries no offset, and writes nothing.
func (s *Service) GetQuota(ctx context.Context, req *v1.GetQuotaRequest) (*v1.GetQuotaResponse, error) {
	caller, _ := rpc.CallerFrom(ctx)
	profileID := req.GetProfileId()
	adult := false
	switch req.GetAction() {
	case v1.Action_ACTION_LEARNING:
		if _, err := rpc.Learner(ctx, s.db, profileID); err != nil {
			return nil, err
		}
	case v1.Action_ACTION_ADULT:
		can, err := s.rights.Can(ctx, caller.AccountID, authz.CanView, profileID)
		switch {
		case err != nil:
			return nil, rpc.Failed(ctx, err)
		case !can:
			return nil, connect.NewError(connect.CodePermissionDenied, "")
		}
		adult = true
	default:
		return nil, rpc.Malformed("action")
	}
	var settled store.Settled
	err := pgx.BeginTxFunc(ctx, s.db, snapshot, func(tx pgx.Tx) error {
		var err error
		settled, err = store.QuotaSettled(ctx, tx, caller.AccountID, profileID, s.now())
		return err
	})
	if err != nil {
		return nil, refusedProfile(ctx, err)
	}
	return &v1.GetQuotaResponse{Share: share(settled, adult)}, nil
}

// share is the share of what was settled in the contract's words: the
// account's beside an adult's action alone, so that a child never sees what the
// family spends, and no share when the tariff has no caps.
func share(s store.Settled, adult bool) *v1.QuotaShare {
	out := &v1.QuotaShare{NextFamilyDay: timestamppb.New(s.Day.End)}
	if !s.Capped {
		return out
	}
	out.ProfilePercent = proto.Int32(quota.Share(s.Profile, s.Caps.Profile))
	if adult {
		out.AccountPercent = proto.Int32(quota.Share(s.Account, s.Caps.Account))
	}
	out.CapReached = quota.Reached(s.Caps, s.Profile, s.Account)
	return out
}
