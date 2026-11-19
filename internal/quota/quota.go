// Package quota is the quota of platform AI (R11, R12, R55): its unit, the
// tariffs and their caps for a family day, the typical tasks a new task of
// handwriting must find room for, and the reserve of a kind of call when tutor
// gives no expected price.
//
// The values are the configuration of api, and a pull request changes them, as
// R11 has it: they are product, not deployment, and a deployment that set its
// own would make two platforms of one release without a word in the log. The
// first values are those of the tariff of the closed slice, $0.50 a profile and
// $1.50 an account.
package quota

import (
	"math"
	"slices"

	"github.com/altnovatrail/tutor"
)

// Micro is an amount of US dollars by tutor's estimate, in whole micro-dollars,
// as the export writes the counters (exportv4.QuotaCounter): a sum of them adds
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
	// to see the quota run out on a run with app: room for a typical task and a
	// few checks after it, and no more (R55).
	Probe = "probe"
	// DevCheck is the tariff of the account of dev-check on the dev projects,
	// tenfold the closed slice's, so that the runs of a chain of tasks in one
	// family day all find room. It is off a deployment alone: there the commands
	// refuse it, and api reads it as a tariff with no caps (R55).
	DevCheck = "dev_check"
)

// tariff is a row of the table of tariffs: its name, its caps, and whether it is
// the dev projects' alone.
type tariff struct {
	name string
	caps Caps
	dev  bool
}

// tariffs are the tariffs, in the order a command lists them.
var tariffs = []tariff{
	{ClosedSlice, Caps{Profile: 500_000, Account: 1_500_000}, false},
	{Probe, Caps{Profile: 80_000, Account: 100_000}, false},
	{DevCheck, Caps{Profile: 5_000_000, Account: 15_000_000}, true},
}

// lookup is the tariff of the table by its name, and false for one it does not
// hold.
func lookup(name string) (tariff, bool) {
	i := slices.IndexFunc(tariffs, func(t tariff) bool { return t.name == name })
	if i < 0 {
		return tariff{}, false
	}
	return tariffs[i], true
}

// Tariffs is every tariff, by its name, in the order of the table.
func Tariffs() []string {
	names := make([]string, len(tariffs))
	for i, t := range tariffs {
		names[i] = t.name
	}
	return names
}

// Known says whether name is one of Tariffs.
func Known(name string) bool {
	_, known := lookup(name)
	return known
}

// DevOnly says whether the tariff name is the dev projects' alone, which neither an
// invite code nor an account of a deployment may have.
func DevOnly(name string) bool {
	t, _ := lookup(name)
	return t.dev
}

// CapsOf is the caps of the tariff, and false for an account with no tariff or a
// tariff with no caps: platform AI is closed to it, by default, as the function
// of admission closes a route it does not know (R11). A tariff of the dev
// projects alone has its caps only offDeployment, so that, deployed or not told
// otherwise, an account that got it all the same opens no spending of the
// platform's.
func CapsOf(name string, offDeployment bool) (Caps, bool) {
	t, known := lookup(name)
	if !known || t.dev && !offDeployment {
		return Caps{}, false
	}
	return t.caps, t.caps.Profile > 0 && t.caps.Account > 0
}

// The typical tasks (R11, R55). A new task that platform AI checks begins only
// when what is left of the day holds one, so that the learner does not begin
// work nobody can read today.
const (
	// TypicalHandwriting is a task of handwriting: two checks and the words of a
	// rung and of praise, ≈ $0.05 by tutor's expected prices (Tutor.Price), with
	// room for checks dearer than expected, as the first app measured them.
	TypicalHandwriting Micro = 60_000
	// TypicalRubric is a task with a rubric: two readings of the essay with their
	// assessment, the first submission and one rewrite, ≈ $0.06 by tutor's price
	// of an essay.
	TypicalRubric Micro = 60_000
)

// wordsReserve is the words' value, which the hint without the work shares.
const wordsReserve Micro = 18_200

// reserves are what a call of a kind reserves when tutor's price of it has no
// expected tokens (R12, R55): the sheet by the pair as the first app measured
// it, the words by tutor's expected price, the hint without the work as the
// words — the same prompt and model with no picture, so no dearer, and tutor
// expects no tokens of it (R106 tutor) —, the essay's reading and assessment
// half each of $0.030, tutor's price of an essay, and a page of a workbook by
// each of its crops, as a sheet. The finding of the tasks on the photo of a
// page, admitted with no run (R134 tutor), by its model, Opus 5.5 at $4 and $20
// per million tokens, on the largest photo, 4 784 tokens, its text and 1 500
// tokens written. The calls of a task from paper, admitted with no run (R135
// tutor), at their bounds, by Haiku 5.5 at $0.10 and $0.50 per million tokens:
// the reading of the statement and the solution each on the largest photo, its
// text and the most tokens it may write, 4 000 and 16 000; the reading of the
// work as a sheet's, by the pair. The words after a correct answer have no value
// here: tutor expects their tokens (R113 tutor, R76).
var reserves = map[tutor.Call]Micro{
	tutor.CallSheet:     14_000,
	tutor.CallWords:     wordsReserve,
	tutor.CallHint:      wordsReserve,
	tutor.CallEssay:     15_000,
	tutor.CallRubric:    15_000,
	tutor.CallWorkbook:  14_000,
	tutor.CallFinding:   52_000,
	tutor.CallStatement: 2_600,
	tutor.CallSolving:   8_600,
	tutor.CallPaper:     14_000,
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

// Spent is why a reserve does not fit, in the words of the log's line of an
// exhausted quota: the cap of the profile or of the account has too little
// left, or the account has no tariff with caps. Fits is none: it fits.
type Spent string

// The reasons a reserve does not fit.
const (
	Fits       Spent = ""
	ProfileCap Spent = "profile"
	AccountCap Spent = "account"
	NoTariff   Spent = "tariff"
)

// Counter is what a counter of a family day holds: spent, and in reserve.
type Counter struct {
	Spent, Reserved Micro
}

// Share is the share of the cap limit that spent takes, in whole percent rounded
// down, as the share of the quota gives it (R11):
// 100 is the cap reached, and spending past it is 100 too, as is any of no cap.
func Share(spent, limit Micro) int32 {
	if limit <= 0 || spent >= limit {
		return 100
	}
	return int32(max(spent, 0) * 100 / limit)
}

// Reached says whether what the profile or its account has spent has reached
// its cap of caps: no request of the profile on platform AI goes until the next
// family day.
func Reached(caps Caps, profile, account Micro) bool {
	return profile >= caps.Profile || account >= caps.Account
}

// Room says whether a reserve of amount fits under caps beside the counters of
// the profile and of its account: on each, what is spent, what is in reserve
// and amount together are no more than its cap (R12). The profile's cap is
// asked first.
func Room(caps Caps, profile, account Counter, amount Micro) Spent {
	switch {
	case profile.Spent+profile.Reserved+amount > caps.Profile:
		return ProfileCap
	case account.Spent+account.Reserved+amount > caps.Account:
		return AccountCap
	}
	return Fits
}
