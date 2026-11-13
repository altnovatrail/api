package account_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/rpc"
)

// family is an account of the tariff probe — a profile $0.08, the account
// $0.10 — with the owner's profile and a child's, and the family day it keeps.
type family struct {
	ctx                   context.Context
	account, owner, child string
	day                   struct{ start, end time.Time }
}

func (w *world) family(t *testing.T) family {
	t.Helper()
	w.allow(t, "US")
	f := family{ctx: w.owner(t, "uid-owner")}
	f.owner = w.ownerProfile(t, f.ctx).GetProfileId()
	consent, err := w.askConsent(t, f.ctx, "key", 8, "US")
	if err != nil {
		t.Fatal(err)
	}
	w.confirm(t, consent.GetConsentId())
	child, err := w.client.CreateChildProfile(f.ctx, &v1.CreateChildProfileRequest{ConsentId: consent.GetConsentId(),
		Settings: &v1.ProfileSettings{Pseudonym: ptr("Fox")}})
	if err != nil {
		t.Fatal(err)
	}
	f.child = child.GetProfile().GetProfileId()
	// A day by an offset of −5 hours that holds the world's now, as a request on
	// platform AI kept it.
	now := w.clock.Now().UTC()
	f.day.end = now.Truncate(24 * time.Hour).Add(29 * time.Hour)
	f.day.start = f.day.end.Add(-24 * time.Hour)
	if now.Before(f.day.start) {
		f.day.start, f.day.end = f.day.start.Add(-24*time.Hour), f.day.start
	}
	if err := w.pool.QueryRow(t.Context(), `UPDATE accounts SET tariff = $2, utc_offset_minutes = -300,
		day_starts_at = $3, day_ends_at = $4 FROM profiles p WHERE p.id = $1 AND accounts.id = p.account_id RETURNING accounts.id`,
		f.owner, quota.Probe, f.day.start, f.day.end).Scan(&f.account); err != nil {
		t.Fatal(err)
	}
	return f
}

// spend sets the counter of the family day of the table's owner id: spent and
// in reserve.
func (w *world) spend(t *testing.T, f family, table, column, id string, spent, reserved quota.Micro) {
	t.Helper()
	if _, err := w.pool.Exec(t.Context(), `INSERT INTO `+table+` (`+column+`, day_start, day_end, spent_micro_usd, reserved_micro_usd)
		VALUES ($1, $2, $3, $4, $5) ON CONFLICT (`+column+`, day_start)
		DO UPDATE SET spent_micro_usd = EXCLUDED.spent_micro_usd, reserved_micro_usd = EXCLUDED.reserved_micro_usd`,
		id, f.day.start, f.day.end, int64(spent), int64(reserved)); err != nil {
		t.Fatal(err)
	}
}

func (w *world) share(t *testing.T, f family, profileID string, action v1.Action) *v1.QuotaShare {
	t.Helper()
	res, err := w.client.GetQuota(f.ctx, &v1.GetQuotaRequest{ProfileId: profileID, Action: action})
	if err != nil {
		t.Fatalf("GetQuota of %s = %v", action, err)
	}
	return res.GetShare()
}

// A learner sees the share of their own profile alone, and an adult the
// account's beside it: whole percent of the settled spending, rounded down,
// with the reserves of calls still going left out, and the end of the day the
// account keeps (the amendment of 2026-11-12 to R11).
func TestTheShareOfTheQuotaIsTheProfilesAndTheAdultsTheAccountsToo(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	f := w.family(t)
	next := timestamppb.New(f.day.end)
	// A day with no counter yet has spent nothing.
	want := &v1.QuotaShare{ProfilePercent: proto.Int32(0), NextFamilyDay: next}
	if got := w.share(t, f, f.child, v1.Action_ACTION_LEARNING); !proto.Equal(got, want) {
		t.Errorf("the share of a day with no counter = %v\nwant %v", got, want)
	}
	// The child spent $0.021 of $0.08, the account $0.0599 of $0.10; the
	// reserves of calls still going are no part of it.
	w.spend(t, f, "profile_quota", "profile_id", f.child, 21_000, 50_000)
	w.spend(t, f, "account_quota", "account_id", f.account, 59_900, 50_000)
	want = &v1.QuotaShare{ProfilePercent: proto.Int32(26), NextFamilyDay: next}
	if got := w.share(t, f, f.child, v1.Action_ACTION_LEARNING); !proto.Equal(got, want) {
		t.Errorf("the learner's share = %v\nwant %v", got, want)
	}
	want = &v1.QuotaShare{ProfilePercent: proto.Int32(26), AccountPercent: proto.Int32(59), NextFamilyDay: next}
	if got := w.share(t, f, f.child, v1.Action_ACTION_ADULT); !proto.Equal(got, want) {
		t.Errorf("the adult's share = %v\nwant %v", got, want)
	}
	// The counters of another day are not this one's.
	if _, err := w.pool.Exec(t.Context(), `UPDATE profile_quota SET day_start = day_start - interval '1 day'`); err != nil {
		t.Fatal(err)
	}
	if got := w.share(t, f, f.child, v1.Action_ACTION_LEARNING).GetProfilePercent(); got != 0 {
		t.Errorf("the share by yesterday's counter = %d, want 0", got)
	}
	// The day after the one kept is the next, by the offset kept, and writes nothing.
	w.clock.advance(f.day.end.Sub(w.clock.Now()))
	got := w.share(t, f, f.owner, v1.Action_ACTION_ADULT)
	if !got.GetNextFamilyDay().AsTime().Equal(f.day.end.Add(24*time.Hour)) || got.GetAccountPercent() != 0 {
		t.Errorf("the share of the next day = %v, want its end and nothing spent", got)
	}
	var end time.Time
	if err := w.pool.QueryRow(t.Context(), `SELECT day_ends_at FROM accounts WHERE id = $1`, f.account).Scan(&end); err != nil || !end.Equal(f.day.end) {
		t.Errorf("the day kept = %v, %v; want it unwritten", end, err)
	}
}

// A cap reached is 100 and cap_reached, and so is spending past it; the
// account's cap reached closes the quota to a learner whose own share is below
// it, with no share of the account beside a learning action.
func TestACapReachedIsAHundredAndClosesTheQuota(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	f := w.family(t)
	next := timestamppb.New(f.day.end)
	w.spend(t, f, "profile_quota", "profile_id", f.child, 80_000, 0)
	w.spend(t, f, "account_quota", "account_id", f.account, 80_000, 0)
	want := &v1.QuotaShare{ProfilePercent: proto.Int32(100), AccountPercent: proto.Int32(80), CapReached: true, NextFamilyDay: next}
	if got := w.share(t, f, f.child, v1.Action_ACTION_ADULT); !proto.Equal(got, want) {
		t.Errorf("the profile's cap reached = %v\nwant %v", got, want)
	}
	w.spend(t, f, "profile_quota", "profile_id", f.owner, 30_000, 0)
	w.spend(t, f, "account_quota", "account_id", f.account, 110_000, 0)
	want = &v1.QuotaShare{ProfilePercent: proto.Int32(37), CapReached: true, NextFamilyDay: next}
	if got := w.share(t, f, f.owner, v1.Action_ACTION_LEARNING); !proto.Equal(got, want) {
		t.Errorf("the account's cap passed, to a learner = %v\nwant %v", got, want)
	}
	if got := w.share(t, f, f.owner, v1.Action_ACTION_ADULT).GetAccountPercent(); got != 100 {
		t.Errorf("the account's share past its cap = %d, want 100", got)
	}
}

// A tariff with no caps, or no tariff, has platform AI closed and no share: the
// end of the day alone, and no cap reached.
func TestATariffWithNoCapsHasNoShare(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	f := w.family(t)
	w.spend(t, f, "profile_quota", "profile_id", f.child, 90_000, 0)
	want := &v1.QuotaShare{NextFamilyDay: timestamppb.New(f.day.end)}
	for _, tariff := range []any{"plus", nil} {
		if _, err := w.pool.Exec(t.Context(), `UPDATE accounts SET tariff = $2 WHERE id = $1`, f.account, tariff); err != nil {
			t.Fatal(err)
		}
		for _, action := range []v1.Action{v1.Action_ACTION_LEARNING, v1.Action_ACTION_ADULT} {
			if got := w.share(t, f, f.child, action); !proto.Equal(got, want) {
				t.Errorf("the share of the tariff %v to %s = %v\nwant %v", tariff, action, got, want)
			}
		}
	}
}

// snapshotFails is the database whose snapshots do not begin.
type snapshotFails struct{ *pgxpool.Pool }

func (snapshotFails) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) { return nil, errOwner }

// The share is refused as the contract has it: an action unspecified, a
// learning action of a profile with no consent, an adult's with no can_view; the
// checks tell no profile before the handler, and a failure is a failure.
func TestTheShareOfTheQuotaIsRefusedAsTheContractHasIt(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	f := w.family(t)
	ask := func(profileID string, action v1.Action) error {
		_, err := w.client.GetQuota(f.ctx, &v1.GetQuotaRequest{ProfileId: profileID, Action: action})
		return err
	}
	refusedAs(t, ask(f.child, v1.Action_ACTION_UNSPECIFIED), connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	refusedAs(t, ask("00000000-0000-0000-0000-000000000000", v1.Action_ACTION_ADULT), connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
	w.denyView.Store(true)
	refusedAs(t, ask(f.child, v1.Action_ACTION_ADULT), connect.CodePermissionDenied, v1.Cause_CAUSE_UNSPECIFIED)
	w.denyView.Store(false)
	w.viewFails.Store(true)
	refusedAs(t, ask(f.child, v1.Action_ACTION_ADULT), connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
	w.viewFails.Store(false)
	if _, err := w.pool.Exec(t.Context(), `UPDATE consents SET state = 'waiting', confirmed_at = NULL WHERE profile_id = $1`, f.child); err != nil {
		t.Fatal(err)
	}
	refusedAs(t, ask(f.child, v1.Action_ACTION_LEARNING), connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)
	// An adult's view asks for no consent of the learner.
	if err := ask(f.child, v1.Action_ACTION_ADULT); err != nil {
		t.Errorf("the adult's share of a profile with no consent = %v", err)
	}

	broken := newWorld(t, func(pool *pgxpool.Pool) rpc.DB { return snapshotFails{pool} })
	g := broken.family(t)
	_, err := broken.client.GetQuota(g.ctx, &v1.GetQuotaRequest{ProfileId: g.owner, Action: v1.Action_ACTION_LEARNING})
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
}
