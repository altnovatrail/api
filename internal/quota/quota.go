// Package quota is the quota of platform AI (R11, R12; architecture 3.6): its
// unit, the tariffs and their caps for a family day, the typical tasks a new
// task of handwriting must find room for, and the reserve of a kind of call
// when tutor gives no expected price.
//
// The values are the configuration of api, and a pull request changes them, as
// R11 has it: they are product, not deployment, and a deployment that set its
// own would make two platforms of one release without a word in the log. The
// first values are those of the tariff of the closed slice, $0.50 a profile and
// $1.50 an account; the estimates behind them are in architecture 3.6.
package quota

import (
	"math"
	"slices"

	"github.com/altnovatrail/tutor"
)

// Micro is an amount of US dollars by tutor's estimate, in whole micro-dollars,
// as the export writes the counters (exportv1.QuotaCounter): a sum of them adds
// up the same in every order, which a float does not.
type Micro int64

// USD is d dollars in micro-dollars, rounded up: a quota errs on the side of
// spending, never of letting a call through for free. Below zero, or not a
// number, is none.
func USD(d float64) Micro {
	if !(d > 0) {
		return 0
	}
	return Micro(math.Ceil(d*1e6 - 1e-6))
}

// Dollars is m in dollars, for a test or a line that writes it as a number.
func (m Micro) Dollars() float64 { return float64(m) / 1e6 }

// Caps are the caps of a tariff for a family day: of each profile, which keeps
// the children of one account fair to each other, and of the account, which
// holds what the account spends (R11).
type Caps struct {
	Profile, Account Micro
}

// The tariffs. An account gets its tariff with its invite code (R04, R34), and
// the operator changes it by api tariff.
const (
	// ClosedSlice is the tariff of the closed slice, a code's by default.
	ClosedSlice = "closed_slice"
	// Probe is a tariff of a few cents a day, which the operator gives an account
	// to see the quota run out on a run with app (T52): room for a typical task
	// and a few checks after it, and no more.
	Probe = "probe"
)

// tariffs are the caps of each tariff, in the order a command lists them.
var tariffs = []struct {
	name string
	caps Caps
}{
	{ClosedSlice, Caps{Profile: 500_000, Account: 1_500_000}},
	{Probe, Caps{Profile: 80_000, Account: 100_000}},
}

// Tariffs is every tariff, by its name, in the order of the table.
func Tariffs() []string {
	names := make([]string, len(tariffs))
	for i, t := range tariffs {
		names[i] = t.name
	}
	return names
}

// Known says whether tariff is one of Tariffs.
func Known(tariff string) bool { return slices.Contains(Tariffs(), tariff) }

// CapsOf is the caps of the tariff, and false for an account with no tariff or a
// tariff with no caps: platform AI is closed to it, by default, as the function
// of admission closes a route it does not know (R11).
func CapsOf(tariff string) (Caps, bool) {
	for _, t := range tariffs {
		if t.name == tariff {
			return t.caps, t.caps.Profile > 0 && t.caps.Account > 0
		}
	}
	return Caps{}, false
}

// The typical tasks (R11; the amendment of 2026-11-02 to it). A new task that
// platform AI checks begins only when what is left of the day holds one, so
// that the learner does not begin work nobody can read today.
const (
	// TypicalHandwriting is a task of handwriting: two checks and the words of a
	// rung and of praise. By tutor v0.1.24's expected prices, $0.0093 a check by
	// the pair and $0.0182 the words (Tutor.Price), it is $0.055; the first app
	// measured $0.014 a check (T42), and this is $0.06.
	TypicalHandwriting Micro = 60_000
	// TypicalRubric is a task with a rubric: two readings of the essay with their
	// assessment, the first submission and one rewrite, ≈ $0.06 by the price of
	// an essay at tutor (architecture 3.4 tutor).
	TypicalRubric Micro = 60_000
)

// reserves are what a call of a kind reserves when tutor's price of it has no
// expected tokens (В13; R12): the sheet by the pair at the first app's measure
// (T42), the words by tutor's v0.1.24, the essay's reading and assessment half
// each of $0.030 (architecture 3.4 tutor), and a page of a workbook by each of
// its crops, as a sheet.
var reserves = map[tutor.Call]Micro{
	tutor.CallSheet:    14_000,
	tutor.CallWords:    18_200,
	tutor.CallEssay:    15_000,
	tutor.CallRubric:   15_000,
	tutor.CallWorkbook: 14_000,
}

// Reserve is what a call of the kind reserves before it goes: tutor's expected
// price, when it has one, or the value of the kind here. A page of a workbook
// reserves by its crops, crops of them; any other call, crops = 1.
func Reserve(call tutor.Call, price tutor.CallPrice, crops int) Micro {
	if price.Expected && price.ExpectedUSD > 0 {
		return USD(price.ExpectedUSD)
	}
	return reserves[call] * Micro(max(crops, 1))
}
