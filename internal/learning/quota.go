package learning

import (
	"context"

	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/tutor"
	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// familyDay is the caller's family day at the request's time, on a request that
// goes to platform AI by route, once it sent the offset of its device's clock,
// if any (R11; the amendment of 2026-11-07 to it): the account keeps the
// offset, when valid, for the days to come, and the day that holds now, in the
// first transaction of the request, which holds the account's row. An own key
// spends no quota and sends no offset: its route keeps nothing, and has no day.
func (s *Service) familyDay(ctx context.Context, tx pgx.Tx, route tutor.Route, offset *int32) (quota.Day, error) {
	if route.AI != admissionv0.AiRoutePlatformAi {
		return quota.Day{}, nil
	}
	caller, _ := rpc.CallerFrom(ctx)
	return store.FamilyDay(ctx, tx, caller.AccountID, offset, s.clock())
}
