package learning

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/tutor"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// The quota of platform AI on the calls of AI (R11, R12, R58): a request that
// calls platform AI takes a reserve of the call's expected price on the
// counters of its profile and its account before the call, in its first
// transaction, and settles it by what the call cost after it, in a transaction
// of its own. A reserve that does not fit makes no call: the answer's outcome
// is the quota, with when the next family day begins. An own key spends no
// quota.

// settleTimeout bounds the transaction that settles a reserve after its call:
// it goes on a context the request's end does not cut short, so that a call
// that went is settled even past the request's deadline.
const settleTimeout = 5 * time.Second

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

// reserve is what a request took of the quota before its call of AI: the
// charge, when it reserved one, or the outcome of the call that does not go,
// when the quota does not hold it. The zero reserve is a call on an own key,
// which takes none.
type reserve struct {
	charge store.Charge
	// taken says the charge is reserved, and is settled after the call.
	taken bool
	// refused is the outcome of the call that does not go.
	refused *v1.Outcome
}

// reserveFor reserves the call on platform AI by route, for the profile, on the
// family day, in tx (R12): its expected price by tutor, or the value of its
// kind when tutor has none. Platform AI that the limit of its provider closed
// makes no call, with the outcome "platform limit" (R59). The quota of the
// profile or the account that does not hold it refuses it with the outcome
// "quota" and when the next family day begins; an account whose tariff has no
// caps, with "not admitted": platform AI is closed to it by default (R11).
// Either is noted in the request's log, with the cap and no ID.
func (s *Service) reserveFor(ctx context.Context, tx pgx.Tx, profileID string, route tutor.Route, call tutor.Call, day quota.Day) (reserve, error) {
	if route.AI != admissionv0.AiRoutePlatformAi {
		return reserve{}, nil
	}
	if limit, err := platformLimit(ctx, tx); err != nil || limit != nil {
		return reserve{refused: limit}, err
	}
	caller, _ := rpc.CallerFrom(ctx)
	c := store.Charge{AccountID: caller.AccountID, ProfileID: profileID, Day: day,
		Amount: quota.Reserve(call, s.price(call, route), 1)}
	spent, err := store.ReserveQuota(ctx, tx, c)
	switch {
	case err != nil:
		return reserve{}, err
	case spent == quota.Fits:
		return reserve{charge: c, taken: true}, nil
	}
	rpc.NoteQuotaSpent(ctx, string(spent))
	return reserve{charge: c, refused: quotaOutcome(spent, day)}, nil
}

// quotaOutcome is the outcome of a call the quota does not hold: "quota" with
// when the next family day begins, or "not admitted" on an account whose tariff
// has no caps, which no next day opens.
func quotaOutcome(spent quota.Spent, day quota.Day) *v1.Outcome {
	if spent == quota.NoTariff {
		return &v1.Outcome{Cause: v1.Cause_CAUSE_NOT_ADMITTED}
	}
	return &v1.Outcome{Cause: v1.Cause_CAUSE_QUOTA, NextFamilyDay: timestamppb.New(day.End)}
}

// closedRefusal is the refusal of a request that platform AI is closed to, as
// the outcome o says (contract, Cause): "quota", resource_exhausted, with when
// the next family day begins; "platform limit", resource_exhausted; "not
// admitted", failed_precondition.
func closedRefusal(ctx context.Context, o *v1.Outcome) error {
	switch o.GetCause() {
	case v1.Cause_CAUSE_QUOTA:
		return rpc.Refuse(ctx, connect.CodeResourceExhausted, v1.Cause_CAUSE_QUOTA, func(r *v1.Refusal) {
			r.Data = &v1.Refusal_NextFamilyDay{NextFamilyDay: o.GetNextFamilyDay()}
		})
	case v1.Cause_CAUSE_PLATFORM_LIMIT:
		return rpc.Refuse(ctx, connect.CodeResourceExhausted, v1.Cause_CAUSE_PLATFORM_LIMIT)
	}
	return rpc.Refuse(ctx, connect.CodeFailedPrecondition, o.GetCause())
}

// refusal is closedRefusal of a call the quota or the flag did not let go, which
// keeps what the request wrote before it: the family day.
func refusal(ctx context.Context, o *v1.Outcome) error { return rpc.Keep(closedRefusal(ctx, o)) }

// afterCall settles the reserve r by the outcome of its call, in a transaction
// of its own on a context the request's end does not cut short: the call went,
// and what it cost counts whatever the request comes to after it. A settlement
// that fails leaves the reserve spent on its day, as a process that fell does
// (R12), and the request's line names the failure. A refusal of the provider by
// its limit on the platform's key closes platform AI to all, until the operator
// opens it again, and the first such refusal is a line of the log (R11, R59).
func (s *Service) afterCall(ctx context.Context, r reserve, o tutor.Outcome) {
	if !r.taken {
		return
	}
	done, cancel := context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
	defer cancel()
	settled := store.SettleQuota(done, s.db, r.charge, cost(r.charge.Amount, o))
	var closed error
	if o.Kind == tutor.LimitReached {
		var set bool
		if set, closed = store.ClosePlatformAI(done, s.db, s.clock()); set {
			rpc.NotePlatformLimit(ctx)
		}
	}
	if err := errors.Join(settled, closed); err != nil {
		// The line names both failures, when both failed.
		rpc.NoteFailure(ctx, err)
	}
}

// closedTo is why platform AI is closed to a new task of the profile that it
// would check, read in q at the request's time, or none when it is open (R11):
// the limit of its provider; what is left of the family day of the profile or
// the account, which does not hold typical — with when the next day begins —;
// or a tariff with no caps. The day is the one the account keeps at now,
// written by the requests on platform AI: a beginning sends no offset (R97
// spec). It asks what reserveFor asks, writing nothing.
func closedTo(ctx context.Context, q store.Querier, profileID string, typical quota.Micro, now time.Time) (*v1.Outcome, error) {
	if limit, err := platformLimit(ctx, q); err != nil || limit != nil {
		return limit, err
	}
	caller, _ := rpc.CallerFrom(ctx)
	day, err := store.Today(ctx, q, caller.AccountID, now)
	if err != nil {
		return nil, err
	}
	spent, err := store.QuotaFits(ctx, q, store.Charge{AccountID: caller.AccountID, ProfileID: profileID, Day: day, Amount: typical})
	if err != nil || spent == quota.Fits {
		return nil, err
	}
	return quotaOutcome(spent, day), nil
}

// platformLimit is the outcome "platform limit" while the limit of its provider
// keeps platform AI closed to all, until the operator opens it (R59), read in q
// on every request: one instance closes it for the others at once.
func platformLimit(ctx context.Context, q store.Querier) (*v1.Outcome, error) {
	closed, err := store.PlatformAIClosed(ctx, q)
	if err != nil || !closed {
		return nil, err
	}
	return &v1.Outcome{Cause: v1.Cause_CAUSE_PLATFORM_LIMIT}, nil
}

// cost is what a call that reserved reserved cost by its outcome, by the table
// of R12: no call went — the admission refused it before any model — nothing; a
// call with no record of usage at all, its reserve — tutor tells no call that
// came back with nothing from one that never went, but by "not admitted" —;
// records that are each the whole bill by the prices, their sum, which is that
// of the reader that answered when the other was refused; and a record at its
// worst — of an answer cut short before its tokens were counted — the reserve,
// or the sum of the others when it is more: a failure of the platform eats no
// learner's day.
func cost(reserved quota.Micro, o tutor.Outcome) quota.Micro {
	if o.Kind == tutor.NotAdmitted {
		return 0
	}
	if len(o.Usage) == 0 {
		return reserved
	}
	var sum quota.Micro
	exact := true
	for _, u := range o.Usage {
		if u.Billed && u.Priced {
			sum += quota.USD(u.USD)
		} else {
			exact = false
		}
	}
	if exact {
		return sum
	}
	return max(reserved, sum)
}
