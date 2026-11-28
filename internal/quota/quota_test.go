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

// The tariff of the closed slice caps a profile at $1.00 and an account at
// $3.00, and the probe at a few cents with room for a typical task; an account
// with no tariff, or one the table does not know, has no caps, and platform AI
// is closed to it.
func TestATariffHasItsCapsAndNoneIsClosed(t *testing.T) {
	t.Parallel()

	for _, off := range []bool{false, true} {
		if caps, ok := quota.CapsOf(quota.ClosedSlice, off); !ok || caps.Profile != 1_000_000 || caps.Account != 3_000_000 {
			t.Errorf("the caps of the closed slice, off a deployment %v = %+v, %v", off, caps, ok)
		}
		probe, ok := quota.CapsOf(quota.Probe, off)
		if !ok || probe.Profile < quota.TypicalHandwriting || probe.Account < probe.Profile || probe.Account > 150_000 {
			t.Errorf("the caps of the probe, off a deployment %v = %+v, %v; want a few cents with room for a typical task", off, probe, ok)
		}
		for _, tariff := range []string{"", "plus", "Closed_Slice"} {
			if caps, ok := quota.CapsOf(tariff, off); ok || caps != (quota.Caps{}) {
				t.Errorf("CapsOf(%q, %v) = %+v, %v; want none", tariff, off, caps, ok)
			}
			if quota.Known(tariff) || quota.DevOnly(tariff) {
				t.Errorf("Known(%q) or DevOnly(%q) = true", tariff, tariff)
			}
		}
	}
	names := quota.Tariffs()
	if len(names) != 3 || names[0] != quota.ClosedSlice || !quota.Known(quota.Probe) || !quota.Known(quota.DevCheck) {
		t.Errorf("Tariffs = %v", names)
	}
	if quota.DevOnly(quota.ClosedSlice) || quota.DevOnly(quota.Probe) {
		t.Error("DevOnly of the closed slice or the probe = true")
	}
}

// The tariff of dev-check caps a profile at $5.00 and an account at $15.00,
// tenfold the closed slice, off a deployment; deployed, or not told it is off
// one, it has no caps, and platform AI is closed to an account that got it all
// the same.
func TestTheTariffOfDevCheckIsTheDevProjectsAlone(t *testing.T) {
	t.Parallel()

	if !quota.DevOnly(quota.DevCheck) {
		t.Error("DevOnly(dev_check) = false")
	}
	if caps, ok := quota.CapsOf(quota.DevCheck, true); !ok || caps.Profile != 5_000_000 || caps.Account != 15_000_000 {
		t.Errorf("the caps of dev_check off a deployment = %+v, %v; want $5.00 and $15.00", caps, ok)
	}
	if caps, ok := quota.CapsOf(quota.DevCheck, false); ok || caps != (quota.Caps{}) {
		t.Errorf("the caps of dev_check deployed, or not told = %+v, %v; want none", caps, ok)
	}
}

// A call reserves tutor's expected price, rounded up, when tutor has one; else
// the value of its kind.
func TestACallReservesTheExpectedPriceOrItsKindsValue(t *testing.T) {
	t.Parallel()

	expected := tutor.CallPrice{ExpectedUSD: 0.0093424, Expected: true, WorstUSD: 0.7, Worst: true}
	for _, tc := range []struct {
		name  string
		call  tutor.Call
		price tutor.CallPrice
		want  quota.Micro
	}{
		{"a sheet tutor prices", tutor.CallSheet, expected, 9343},
		{"a sheet tutor does not price", tutor.CallSheet, tutor.CallPrice{}, 14_000},
		{"words tutor does not price", tutor.CallWords, tutor.CallPrice{Expected: true}, 18_200},
		{"a hint without the work, worst alone", tutor.CallHint, tutor.CallPrice{WorstUSD: 0.512, Worst: true}, 18_200},
		{"the words after a correct answer tutor prices", tutor.CallCorrect,
			tutor.CallPrice{ExpectedUSD: 0.013524, Expected: true, WorstUSD: 0.684872, Worst: true}, 13_524},
		{"an essay", tutor.CallEssay, tutor.CallPrice{}, 15_000},
		{"a rubric", tutor.CallRubric, tutor.CallPrice{}, 15_000},
	} {
		if got := quota.Reserve(tc.call, tc.price); got != tc.want {
			t.Errorf("%s: Reserve = %d, want %d", tc.name, got, tc.want)
		}
	}
	if quota.TypicalHandwriting <= 2*quota.Reserve(tutor.CallSheet, expected) || quota.TypicalRubric <= 0 {
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

// A share is whole percent rounded down, and a cap reached, spending past it or
// no cap at all is 100; either cap reached closes the quota.
func TestAShareIsWholePercentDownToTheCap(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		spent, limit quota.Micro
		want         int32
	}{
		{0, 80_000, 0},
		{799, 80_000, 0},
		{800, 80_000, 1},
		{79_999, 80_000, 99},
		{80_000, 80_000, 100},
		{90_000, 80_000, 100},
		{-1, 80_000, 0},
		{0, 0, 100},
	} {
		if got := quota.Share(tc.spent, tc.limit); got != tc.want {
			t.Errorf("Share(%d, %d) = %d, want %d", tc.spent, tc.limit, got, tc.want)
		}
	}
	caps := quota.Caps{Profile: 80_000, Account: 100_000}
	for _, tc := range []struct {
		profile, account quota.Micro
		want             bool
	}{
		{79_999, 99_999, false},
		{80_000, 0, true},
		{0, 100_000, true},
	} {
		if got := quota.Reached(caps, tc.profile, tc.account); got != tc.want {
			t.Errorf("Reached(%d, %d) = %v, want %v", tc.profile, tc.account, got, tc.want)
		}
	}
}
