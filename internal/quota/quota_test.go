package quota_test

import (
	"math"
	"slices"
	"testing"

	"github.com/altnovatrail/tutor"

	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/quota"
)

// Dollars are whole micro-dollars rounded up, so that no call goes for free;
// nothing below zero, and no number, are none.
func TestDollarsAreMicroDollarsRoundedUp(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		usd  float64
		want quota.Micro
	}{
		{0.5, 500_000}, {1.5, 1_500_000}, {0.0093424, 9343}, {0.018164, 18_164}, {0.0000001, 1},
		{0, 0}, {-0.2, 0}, {math.NaN(), 0},
	} {
		if got := quota.USD(tc.usd); got != tc.want {
			t.Errorf("USD(%v) = %d, want %d", tc.usd, got, tc.want)
		}
	}
	if d := quota.Micro(14_000).Dollars(); d != 0.014 {
		t.Errorf("Dollars = %v, want 0.014", d)
	}
}

// The tariff of the closed slice caps a profile at $0.50 and an account at
// $1.50 (R11), and the probe at a few cents with room for a typical task; an
// account with no tariff, or one the table does not know, has no caps, and
// platform AI is closed to it.
func TestATariffHasItsCapsAndNoneIsClosed(t *testing.T) {
	t.Parallel()

	if caps, ok := quota.CapsOf(quota.ClosedSlice); !ok || caps.Profile != 500_000 || caps.Account != 1_500_000 {
		t.Errorf("the caps of the closed slice = %+v, %v", caps, ok)
	}
	probe, ok := quota.CapsOf(quota.Probe)
	if !ok || probe.Profile < quota.TypicalHandwriting || probe.Account < probe.Profile || probe.Account > 150_000 {
		t.Errorf("the caps of the probe = %+v, %v; want a few cents with room for a typical task", probe, ok)
	}
	for _, tariff := range []string{"", "plus", "Closed_Slice"} {
		if caps, ok := quota.CapsOf(tariff); ok || caps != (quota.Caps{}) {
			t.Errorf("CapsOf(%q) = %+v, %v; want none", tariff, caps, ok)
		}
		if quota.Known(tariff) {
			t.Errorf("Known(%q) = true", tariff)
		}
	}
	if names := quota.Tariffs(); len(names) != 2 || names[0] != quota.ClosedSlice || !quota.Known(quota.Probe) {
		t.Errorf("Tariffs = %v", names)
	}
}

// A call reserves tutor's expected price, rounded up, when tutor has one; else
// the value of its kind, and a page of a workbook by its crops (R12).
func TestACallReservesTheExpectedPriceOrItsKindsValue(t *testing.T) {
	t.Parallel()

	expected := tutor.CallPrice{ExpectedUSD: 0.0093424, Expected: true, WorstUSD: 0.7, Worst: true}
	for _, tc := range []struct {
		name  string
		call  tutor.Call
		price tutor.CallPrice
		crops int
		want  quota.Micro
	}{
		{"a sheet tutor prices", tutor.CallSheet, expected, 1, 9343},
		{"a sheet tutor does not price", tutor.CallSheet, tutor.CallPrice{}, 1, 14_000},
		{"words tutor does not price", tutor.CallWords, tutor.CallPrice{Expected: true}, 0, 18_200},
		{"an essay", tutor.CallEssay, tutor.CallPrice{}, 1, 15_000},
		{"a rubric", tutor.CallRubric, tutor.CallPrice{}, 1, 15_000},
		{"a page of three crops", tutor.CallWorkbook, tutor.CallPrice{}, 3, 42_000},
	} {
		if got := quota.Reserve(tc.call, tc.price, tc.crops); got != tc.want {
			t.Errorf("%s: Reserve = %d, want %d", tc.name, got, tc.want)
		}
	}
	if quota.TypicalHandwriting <= 2*quota.Reserve(tutor.CallSheet, expected, 1) || quota.TypicalRubric <= 0 {
		t.Errorf("a typical task (%d, %d) holds less than its checks", quota.TypicalHandwriting, quota.TypicalRubric)
	}
}

// The words of the log's cap are those of the reasons a reserve does not fit.
func TestTheLogNamesTheCapsByTheirWords(t *testing.T) {
	t.Parallel()

	want := []string{string(quota.ProfileCap), string(quota.AccountCap), string(quota.NoTariff)}
	if !slices.Equal(logging.QuotaCaps, want) {
		t.Errorf("logging.QuotaCaps = %v, want %v", logging.QuotaCaps, want)
	}
}

// A reserve fits under both caps, and is refused by the profile's first.
func TestRoomAsksTheProfileFirst(t *testing.T) {
	t.Parallel()

	caps := quota.Caps{Profile: 100, Account: 150}
	for _, tc := range []struct {
		profile, account quota.Counter
		amount           quota.Micro
		want             quota.Spent
	}{
		{quota.Counter{}, quota.Counter{}, 100, quota.Fits},
		{quota.Counter{Spent: 50, Reserved: 10}, quota.Counter{Spent: 50, Reserved: 10}, 41, quota.ProfileCap},
		{quota.Counter{Spent: 50}, quota.Counter{Spent: 120}, 40, quota.AccountCap},
		{quota.Counter{Spent: 90}, quota.Counter{Spent: 140}, 20, quota.ProfileCap},
	} {
		if got := quota.Room(caps, tc.profile, tc.account, tc.amount); got != tc.want {
			t.Errorf("Room(%+v, %+v, %d) = %q, want %q", tc.profile, tc.account, tc.amount, got, tc.want)
		}
	}
}
