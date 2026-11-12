package quota_test

import (
	"math"
	"testing"

	"github.com/altnovatrail/tutor"

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
