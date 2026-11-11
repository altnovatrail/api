package account

import (
	"context"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/export"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// snapshot is the transaction an export reads in: one moment of the database,
// written by nothing (R135 spec).
var snapshot = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}

// ExportProfile gives api's part of the export of a profile, api.zip by spec's
// part export/ (R52; R135 spec): an adult's action with can_manage — the action
// unspecified is one —, or, at a profile whose learner consents themselves, the
// learner's own with can_learn, which neither a consent that waits nor an age
// to give again refuses; at any other profile the learner's is
// CAUSE_ADULT_NEEDED (R132 spec).
func (s *Service) ExportProfile(ctx context.Context, req *v1.ExportProfileRequest) (*v1.ExportProfileResponse, error) {
	if s.export == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, "no export is configured")
	}
	caller, _ := rpc.CallerFrom(ctx)
	learning := req.GetAction() == v1.Action_ACTION_LEARNING
	if !learning {
		can, err := s.rights.Can(ctx, caller.AccountID, authz.CanManage, req.GetProfileId())
		switch {
		case err != nil:
			return nil, rpc.Failed(ctx, err)
		case !can:
			return nil, rpc.Refuse(ctx, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
		}
	}
	var part []byte
	err := pgx.BeginTxFunc(ctx, s.db, snapshot, func(tx pgx.Tx) error {
		if learning {
			// Read, not held: the snapshot writes nothing.
			p, err := store.ProfileWithConsent(ctx, tx, caller.AccountID, req.GetProfileId())
			switch {
			case err != nil:
				return err
			case p.Consent.Giver != store.GiverLearner:
				return adultNeeded(ctx)
			}
		}
		var err error
		part, err = s.export.Profile(ctx, tx, caller.AccountID, req.GetProfileId())
		return err
	})
	if err != nil {
		return nil, refusedProfile(ctx, err)
	}
	return &v1.ExportProfileResponse{Part: part, ExportFormatVersion: export.Version}, nil
}

// ExportAccount gives api's part of the export of the account and every
// profile of it, api.zip by spec's part export/ (R52; R135 spec).
func (s *Service) ExportAccount(ctx context.Context, _ *v1.ExportAccountRequest) (*v1.ExportAccountResponse, error) {
	if s.export == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, "no export is configured")
	}
	caller, _ := rpc.CallerFrom(ctx)
	var part []byte
	err := pgx.BeginTxFunc(ctx, s.db, snapshot, func(tx pgx.Tx) error {
		var err error
		part, err = s.export.Account(ctx, tx, caller.AccountID)
		return err
	})
	if err != nil {
		return nil, refused(ctx, err)
	}
	return &v1.ExportAccountResponse{Part: part, ExportFormatVersion: export.Version}, nil
}
