package learning_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/tutor"
	"github.com/google/uuid"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/quota"
)

// familyDayOf is the offset and the end of the day the account keeps.
func (w *world) familyDayOf(t *testing.T, accountID string) (*int32, *time.Time) {
	t.Helper()
	var offset *int32
	var end *time.Time
	if err := w.pool.QueryRow(t.Context(), `SELECT utc_offset_minutes, day_ends_at FROM accounts WHERE id = $1`, accountID).
		Scan(&offset, &end); err != nil {
		t.Fatal(err)
	}
	return offset, end
}

// A request on platform AI that sends the offset of its device's clock gives
// the account its family day by it, which ends at a midnight of that clock; a
// request by an own key keeps nothing of its offset, and no line of the log
// names an offset (R11, R12).
func TestARequestOnPlatformAIGivesTheAccountItsFamilyDay(t *testing.T) {
	t.Parallel()

	ts := &tellers{}
	w := newWordsWorld(t, ts, nil)
	l := w.learner(t)
	ts.say("Which of the two numbers has more tens?", nil)
	trapped := []*v1.TappedChoice{tap(1, 1, "12"), tap(2, 1, "30")}

	own := w.begin(t, l, l.adult, "pairs").GetAttemptId()
	w.wrongs(t, l, l.adult, own, trapped)
	wrong, east := int32(1), int32(120)
	if _, err := w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: own,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong, OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "k"},
		ClockOffsetMinutes: &east}); err != nil {
		t.Fatal(err)
	}
	if offset, end := w.familyDayOf(t, l.accountID); offset != nil || end != nil {
		t.Errorf("a request by an own key kept %v, %v", offset, end)
	}

	a := w.begin(t, l, l.child, "pairs").GetAttemptId()
	w.wrongs(t, l, l.child, a, trapped)
	west := int32(-300)
	if _, err := w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.child, AttemptId: a,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong, ClockOffsetMinutes: &west}); err != nil {
		t.Fatal(err)
	}
	offset, end := w.familyDayOf(t, l.accountID)
	if offset == nil || *offset != west || end == nil || !end.After(w.clock.now()) || end.Add(-5*time.Hour).UTC().Hour() != 0 {
		t.Errorf("the account keeps %v, %v; want −300 and a midnight at −5 h ahead", offset, end)
	}
	if log := w.log.String(); strings.Contains(log, "offset") || strings.Contains(log, "-300") {
		t.Errorf("the log names an offset: %s", log)
	}
}

// expected is the price every call of a quota world is expected to cost: $0.02.
const expected quota.Micro = 20_000

// newQuotaWorld is the world whose words are ts and whose handwriting rs reads,
// admitted for every learner, where every call is expected to cost $0.02.
func newQuotaWorld(t *testing.T, ts *tellers, rs *readers) *world {
	t.Helper()
	return worldOf(t, gate(t, adultFacts, config.RungAuthor), []learning.Option{learning.WithWords(ts.tell),
		learning.WithReader(rs.read), learning.WithPrice(func(tutor.Call, tutor.Route) tutor.CallPrice {
			return tutor.CallPrice{ExpectedUSD: expected.Dollars(), Expected: true}
		})})
}

// counted is what the counters of the quota hold, of the profile and of the
// account, over every day.
func (w *world) counted(t *testing.T, accountID, profileID string) (profile, account quota.Counter) {
	t.Helper()
	for _, read := range []struct {
		sql  string
		id   string
		into *quota.Counter
	}{
		{`SELECT coalesce(sum(spent_micro_usd), 0), coalesce(sum(reserved_micro_usd), 0) FROM profile_quota WHERE profile_id = $1`, profileID, &profile},
		{`SELECT coalesce(sum(spent_micro_usd), 0), coalesce(sum(reserved_micro_usd), 0) FROM account_quota WHERE account_id = $1`, accountID, &account},
	} {
		if err := w.pool.QueryRow(t.Context(), read.sql, read.id).Scan(&read.into.Spent, &read.into.Reserved); err != nil {
			t.Fatal(err)
		}
	}
	return profile, account
}

// spend gives the account a family day that holds now, with spent on its
// counter, as the requests of the day would have: the day it gives.
func (w *world) spend(t *testing.T, accountID string, spent quota.Micro) quota.Day {
	t.Helper()
	now := w.clock.now().UTC().Truncate(time.Microsecond)
	day := quota.Day{Start: now.Add(-time.Hour), End: now.Add(23 * time.Hour)}
	w.exec(t, `UPDATE accounts SET utc_offset_minutes = 0, day_starts_at = $2, day_ends_at = $3 WHERE id = $1`, accountID, day.Start, day.End)
	w.exec(t, `INSERT INTO account_quota (account_id, day_start, day_end, spent_micro_usd) VALUES ($1, $2, $3, $4)
		ON CONFLICT (account_id, day_start) DO UPDATE SET spent_micro_usd = EXCLUDED.spent_micro_usd`, accountID, day.Start, day.End, int64(spent))
	return day
}

// A call of platform AI reserves its expected price on the counters of the
// profile and the account before it goes, and settles it after by what it cost
// (R12): an exact record its price, one at its worst the reserve, none at all the
// reserve, and no call — the admission refused it — nothing. The line of each
// model's usage says what it cost and whether that is its whole bill.
func TestACallReservesItsPriceAndSettlesWhatItCost(t *testing.T) {
	t.Parallel()

	ts, rs := &tellers{}, &readers{}
	w := newQuotaWorld(t, ts, rs)
	l := w.learner(t)
	trapped := []*v1.TappedChoice{tap(1, 1, "12"), tap(2, 1, "30")}
	opus := tutor.Usage{Provider: "anthropic", Model: "claude-opus-5-5", InputTokens: 2300, OutputTokens: 400}
	exact, worst := opus, opus
	exact.Billed, exact.Priced, exact.USD = true, true, 0.0151
	worst.Priced, worst.USD = true, 0.68
	var spent quota.Micro
	for _, tc := range []struct {
		name    string
		outcome tutor.Outcome
		cost    quota.Micro
	}{
		{"an exact record", tutor.Outcome{Kind: tutor.Answered, Usage: []tutor.Usage{exact}}, 15_100},
		{"a record at its worst", tutor.Outcome{Kind: tutor.Failed, Usage: []tutor.Usage{worst}}, expected},
		{"no record at all", tutor.Outcome{Kind: tutor.Failed}, expected},
		{"no call", tutor.Outcome{Kind: tutor.NotAdmitted}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var during quota.Counter
			ts.mu.Lock()
			ts.next = func(context.Context, tutor.WordsRequest) tutor.WordsAnswer {
				during, _ = w.counted(t, l.accountID, l.child)
				return tutor.WordsAnswer{Question: "Which of the two numbers has more tens?", Outcome: tc.outcome}
			}
			ts.mu.Unlock()
			a := w.begin(t, l, l.child, "pairs").GetAttemptId()
			w.wrongs(t, l, l.child, a, trapped)
			if _, err := w.rungWords(l, l.child, a, 1); err != nil {
				t.Fatal(err)
			}
			if during != (quota.Counter{Spent: spent, Reserved: expected}) {
				t.Errorf("the profile's counter during the call = %+v, want the reserve of $0.02", during)
			}
			spent += tc.cost
			profile, account := w.counted(t, l.accountID, l.child)
			if profile != (quota.Counter{Spent: spent}) || account != profile {
				t.Errorf("the counters after = %+v, %+v; want %d spent and nothing in reserve", profile, account, spent)
			}
		})
	}
	if log := w.log.String(); !strings.Contains(log, `"cost_micro_usd":15100,"billed":true`) ||
		!strings.Contains(log, `"cost_micro_usd":680000,"billed":false`) {
		t.Errorf("the log = %s, want each model's cost and whether it is its bill", log)
	}
}

// A reading by two readers reserves both together and settles the sum of
// their records; when one reader was refused, the record of the one that
// answered alone.
func TestAReadingSettlesTheReadersThatAnswered(t *testing.T) {
	t.Parallel()

	ts, rs := &tellers{}, &readers{}
	w := newQuotaWorld(t, ts, rs)
	l := w.learner(t)
	both := answered(read("6"), read("6"))
	both.Outcome.Usage[0].Billed, both.Outcome.Usage[0].Priced, both.Outcome.Usage[0].USD = true, true, 0.0083
	both.Outcome.Usage[1].Billed, both.Outcome.Usage[1].Priced, both.Outcome.Usage[1].USD = true, true, 0.0003
	one := tutor.SheetRead{Outcome: tutor.Outcome{Kind: tutor.LimitReached, Usage: both.Outcome.Usage[:1]}}
	var spent quota.Micro
	for _, tc := range []struct {
		read tutor.SheetRead
		cost quota.Micro
	}{{both, 8600}, {one, 8300}} {
		rs.give(tc.read)
		a := w.begin(t, l, l.adult, "apples").GetAttemptId()
		if _, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a,
			IdempotencyKey: uuid.NewString(), Work: sheet(crop(t))}); err != nil {
			t.Fatal(err)
		}
		spent += tc.cost
		if profile, account := w.counted(t, l.accountID, l.adult); profile != (quota.Counter{Spent: spent}) || account != profile {
			t.Errorf("the counters = %+v, %+v; want %d spent", profile, account, spent)
		}
	}
}

// A quota spent makes no call: the reading leaves its slots unchecked with the
// outcome "quota" and when the next family day begins, and the wrong answers do
// not grow; the words of a rung are its fallback text with the same outcome;
// and "explain differently" is refused so, and its right stays for the next
// day. Each is a line of its own in the log, with the cap it met and no ID.
func TestASpentQuotaMakesNoCall(t *testing.T) {
	t.Parallel()

	ts, rs := &tellers{}, &readers{}
	w := newQuotaWorld(t, ts, rs)
	l := w.learner(t)
	ts.say("Which of the two numbers has more tens?", nil)
	trapped := []*v1.TappedChoice{tap(1, 1, "12"), tap(2, 1, "30")}
	// The attempt of handwriting begins with room for a typical task, and waits
	// once the quota is spent.
	sheetAttempt := w.begin(t, l, l.adult, "apples").GetAttemptId()
	day := w.spend(t, l.accountID, 1_500_000)
	calls := ts.count()

	rs.give(answered(read("7"), read("7")))
	res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: sheetAttempt,
		IdempotencyKey: uuid.NewString(), Work: sheet(crop(t))})
	if err != nil {
		t.Fatal(err)
	}
	if res.GetOutcome().GetCause() != v1.Cause_CAUSE_QUOTA || !res.GetOutcome().GetNextFamilyDay().AsTime().Equal(day.End) ||
		res.GetWrongAnswers() != 0 || res.GetSolved() != nil || res.GetItems()[0].GetAnswerSlots()[0].GetVerdict() != "unchecked" || rs.count() != 0 {
		t.Errorf("a reading on a spent quota = %v, read %d times; want unchecked, the quota and the next day", res, rs.count())
	}

	b := w.begin(t, l, l.child, "pairs").GetAttemptId()
	w.wrongs(t, l, l.child, b, trapped)
	words, err := w.rungWords(l, l.child, b, 1)
	if err != nil || words.GetWords() != trapText || words.GetOutcome().GetCause() != v1.Cause_CAUSE_QUOTA ||
		!words.GetOutcome().GetNextFamilyDay().AsTime().Equal(day.End) || ts.count() != calls {
		t.Errorf("the words on a spent quota = %v (%v); want the fallback text, the quota and the next day, and no call", words, err)
	}

	_, err = w.explain(l, l.child, b, 1)
	refusal := refusedAs(t, err, connect.CodeResourceExhausted, v1.Cause_CAUSE_QUOTA)
	if !refusal.GetNextFamilyDay().AsTime().Equal(day.End) || ts.count() != calls {
		t.Errorf("explain differently on a spent quota = %v, %v; want the quota and the next day, and no call", err, refusal)
	}
	if log := w.log.String(); strings.Count(log, `"message":"quota_exhausted"`) != 3 || !strings.Contains(log, `"quota_cap":"account"`) ||
		strings.Contains(log, l.accountID) || strings.Contains(log, l.child) {
		t.Errorf("the log = %s, want three lines of the quota spent, with the cap and no ID", log)
	}

	// A new task of handwriting does not begin; the engine gives what a tap checks.
	_, err = w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString(),
		Start: &v1.BeginAttemptRequest_PreviousAttemptId{PreviousAttemptId: sheetAttempt}})
	if r := refusedAs(t, err, connect.CodeResourceExhausted, v1.Cause_CAUSE_QUOTA); !r.GetNextFamilyDay().AsTime().Equal(day.End) {
		t.Errorf("the beginning's refusal = %v, want the next day", r)
	}
	if open, err := w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: l.adult, AttemptId: sheetAttempt}); err != nil || open == nil {
		t.Errorf("the attempt that waits = %v (%v), want it open", open, err)
	}
	choice, err := w.learning.ChooseNext(l.ctx, &v1.ChooseNextRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString()})
	var limited bool
	if err == nil {
		err = w.pool.QueryRow(t.Context(), `SELECT pool_limited FROM engine_choices WHERE profile_id = $1 ORDER BY chosen_at DESC LIMIT 1`, l.adult).Scan(&limited)
	}
	if err != nil || !limited || choice.GetChoice().GetKind() == v1.ChoiceKind_CHOICE_KIND_UNSPECIFIED {
		t.Errorf("the choice on a spent quota = %v (%v), limited %v; want a pool of taps", choice, err, limited)
	}

	// The next day the right is there still.
	w.exec(t, `UPDATE account_quota SET spent_micro_usd = 0 WHERE account_id = $1`, l.accountID)
	if other, err := w.explain(l, l.child, b, 1); err != nil || ts.count() != calls+1 || other.GetWords() == "" {
		t.Errorf("explain differently with the quota back = %v (%v), called %d times", other, err, ts.count()-calls)
	}
}

// An own key spends no quota; a repeat under the key reserves nothing again;
// and an account whose tariff has no caps has platform AI closed: "not
// admitted", with no next day.
func TestAnOwnKeyARepeatAndNoTariff(t *testing.T) {
	t.Parallel()

	ts, rs := &tellers{}, &readers{}
	w := newQuotaWorld(t, ts, rs)
	l := w.learner(t)
	ts.give(tutor.WordsAnswer{Question: "Which of the two numbers has more tens?", Outcome: tutor.Outcome{Kind: tutor.Answered,
		Usage: []tutor.Usage{{Provider: "anthropic", Model: "claude-opus-5-5", Billed: true, Priced: true, USD: 0.015}}}})
	trapped := []*v1.TappedChoice{tap(1, 1, "12"), tap(2, 1, "30")}

	a := w.begin(t, l, l.adult, "pairs").GetAttemptId()
	w.wrongs(t, l, l.adult, a, trapped)
	wrong := int32(1)
	if _, err := w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong, OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "k"}}); err != nil {
		t.Fatal(err)
	}
	if profile, account := w.counted(t, l.accountID, l.adult); profile != (quota.Counter{}) || account != profile {
		t.Errorf("an own key spent %+v, %+v", profile, account)
	}

	b := w.begin(t, l, l.child, "pairs").GetAttemptId()
	w.wrongs(t, l, l.child, b, trapped)
	req := &v1.GetRungWordsRequest{ProfileId: l.child, AttemptId: b, IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong}
	for range 2 {
		if _, err := w.attempts.GetRungWords(l.ctx, req); err != nil {
			t.Fatal(err)
		}
	}
	if profile, _ := w.counted(t, l.accountID, l.child); profile != (quota.Counter{Spent: 15_000}) {
		t.Errorf("a request and its repeat spent %+v, want one call", profile)
	}

	w.exec(t, `UPDATE accounts SET tariff = NULL WHERE id = $1`, l.accountID)
	c := w.begin(t, l, l.child, "pairs").GetAttemptId()
	w.wrongs(t, l, l.child, c, trapped)
	words, err := w.rungWords(l, l.child, c, 1)
	if err != nil || words.GetOutcome().GetCause() != v1.Cause_CAUSE_NOT_ADMITTED || words.GetOutcome().GetNextFamilyDay() != nil {
		t.Errorf("the words of an account with no tariff = %v (%v); want not admitted", words, err)
	}
	if !strings.Contains(w.log.String(), `"quota_cap":"tariff"`) {
		t.Error("the log does not say the tariff had no caps")
	}
	_, err = w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString(),
		Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: w.choice(t, l.adult, "apples")}})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED)
}

// A refusal of the provider by its limit on the platform's key closes platform
// AI to all, and says so once in the log: then no call goes — the words are the
// fallback text with the outcome "platform limit", "explain differently" and a
// new task of handwriting are refused so, and the engine's pool is of taps —
// until the operator opens it again. An own key goes on.
func TestThePlatformsLimitClosesPlatformAIToAll(t *testing.T) {
	t.Parallel()

	ts, rs := &tellers{}, &readers{}
	w := newQuotaWorld(t, ts, rs)
	l := w.learner(t)
	trapped := []*v1.TappedChoice{tap(1, 1, "12"), tap(2, 1, "30")}
	sheetAttempt := w.begin(t, l, l.adult, "apples").GetAttemptId()
	ts.give(tutor.WordsAnswer{Outcome: tutor.Outcome{Kind: tutor.LimitReached}})
	a := w.begin(t, l, l.child, "pairs").GetAttemptId()
	w.wrongs(t, l, l.child, a, trapped)
	if words, err := w.rungWords(l, l.child, a, 1); err != nil || words.GetOutcome().GetCause() != v1.Cause_CAUSE_PLATFORM_LIMIT {
		t.Fatalf("the words the provider refused = %v (%v)", words, err)
	}
	if log := w.log.String(); strings.Count(log, `"message":"platform_limit"`) != 1 || !strings.Contains(log, `"severity":"WARNING","message":"platform_limit"`) {
		t.Errorf("the log = %s, want one warning of the platform's limit", log)
	}
	calls := ts.count()

	ts.say("Which of the two numbers has more tens?", nil)
	b := w.begin(t, l, l.child, "pairs").GetAttemptId()
	w.wrongs(t, l, l.child, b, trapped)
	words, err := w.rungWords(l, l.child, b, 1)
	if err != nil || words.GetOutcome().GetCause() != v1.Cause_CAUSE_PLATFORM_LIMIT || words.GetWords() != trapText || ts.count() != calls {
		t.Errorf("the words with platform AI closed = %v (%v); want the fallback text and no call", words, err)
	}
	_, err = w.explain(l, l.child, b, 1)
	refusedAs(t, err, connect.CodeResourceExhausted, v1.Cause_CAUSE_PLATFORM_LIMIT)
	rs.give(answered(read("7"), read("7")))
	res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: sheetAttempt,
		IdempotencyKey: uuid.NewString(), Work: sheet(crop(t))})
	if err != nil || res.GetOutcome().GetCause() != v1.Cause_CAUSE_PLATFORM_LIMIT || rs.count() != 0 {
		t.Errorf("a reading with platform AI closed = %v (%v), read %d times", res, err, rs.count())
	}
	// An own key is not platform AI.
	res, err = w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: sheetAttempt,
		IdempotencyKey: uuid.NewString(), Work: sheet(crop(t)), OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "k"}})
	if err != nil || res.GetSolved() == nil || rs.count() != 1 {
		t.Errorf("a reading by an own key = %v (%v)", res, err)
	}
	_, err = w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString(),
		Start: &v1.BeginAttemptRequest_PreviousAttemptId{PreviousAttemptId: sheetAttempt}})
	refusedAs(t, err, connect.CodeResourceExhausted, v1.Cause_CAUSE_PLATFORM_LIMIT)

	w.exec(t, `UPDATE platform_ai SET closed_at = NULL`)
	if again := w.begin(t, l, l.adult, "apples"); again.GetAttemptId() == "" {
		t.Error("a new task of handwriting did not begin once platform AI was opened")
	}
}
