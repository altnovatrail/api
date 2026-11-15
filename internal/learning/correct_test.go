package learning_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/tutor"
	"github.com/altnovatrail/tutor/words"
	"github.com/google/uuid"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/legal"
	"github.com/altnovatrail/api/internal/quota"
)

// praise is the words after a correct answer of the tests: a praise of a step
// and a question on the technique, last, as words.Correct takes them.
const praise = "You put the two numbers together well. Which way of counting helped you here?"

// praisers are the tellers of the words after a correct answer of a test: what
// they give next, and what they were given.
type praisers struct {
	mu    sync.Mutex
	next  func(ctx context.Context, c tutor.CorrectRequest) tutor.CorrectAnswer
	given []tutor.CorrectRequest
}

func (ps *praisers) tell(ctx context.Context, c tutor.CorrectRequest) tutor.CorrectAnswer {
	ps.mu.Lock()
	ps.given = append(ps.given, c)
	next := ps.next
	ps.mu.Unlock()
	return next(ctx, c)
}

// say makes the words text, from a model whose usage is recorded.
func (ps *praisers) say(text string) {
	ps.give(tutor.CorrectAnswer{Words: text, Outcome: tutor.Outcome{Kind: tutor.Answered,
		Usage: []tutor.Usage{{Provider: "anthropic", Model: "claude-opus-5-5", InputTokens: 1300, OutputTokens: 40}}}})
}

func (ps *praisers) give(got tutor.CorrectAnswer) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.next = func(context.Context, tutor.CorrectRequest) tutor.CorrectAnswer { return got }
}

func (ps *praisers) count() int {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return len(ps.given)
}

func (ps *praisers) last() tutor.CorrectRequest {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.given[len(ps.given)-1]
}

// newPraiseWorld is the world whose words after a correct answer are ps and
// whose handwriting rs reads, admitted for every learner, where every call is
// expected to cost $0.02, on the day of at: its clock is there before its first
// learner.
func newPraiseWorld(t *testing.T, ps *praisers, rs *readers, at time.Time) *world {
	t.Helper()
	// The texts are the tag's, as serve gives them to the attempts.
	w := worldOf(t, gate(t, adultFacts, config.RungAuthor), []learning.Option{learning.WithCorrect(ps.tell), learning.WithTexts(legal.Spec()),
		learning.WithReader(rs.read), learning.WithPrice(func(tutor.Call, tutor.Route) tutor.CallPrice {
			return tutor.CallPrice{ExpectedUSD: expected.Dollars(), Expected: true}
		})})
	w.clock.move(at.Sub(time.Now()))
	return w
}

// The days around the one version 3 of the privacy policy is in force from,
// 2026-11-27 (spec/legal/legal.yaml).
var (
	beforePolicy3 = time.Date(2026, 11, 26, 23, 30, 0, 0, time.UTC)
	onPolicy3     = time.Date(2026, 11, 27, 0, 30, 0, 0, time.UTC)
)

// praised asks the words after a correct answer of the attempt, with the work.
func (w *world) praised(l learner, profile, attempt string, work *v1.Work) (*v1.GetCorrectAnswerWordsResponse, error) {
	return w.attempts.GetCorrectAnswerWords(l.ctx, &v1.GetCorrectAnswerWordsRequest{ProfileId: profile, AttemptId: attempt,
		IdempotencyKey: uuid.NewString(), Work: work})
}

// correctOn is an attempt of the adult on apples that a correct reading of its
// work closed.
func (w *world) correctOn(t *testing.T, l learner, rs *readers) string {
	t.Helper()
	rs.give(answered(read("7"), read("7")))
	a := w.begin(t, l, l.adult, "apples").GetAttemptId()
	res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), Work: sheet(crop(t))})
	if err != nil || res.GetSolved() == nil {
		t.Fatalf("the correct reading = %v (%v)", res, err)
	}
	return a
}

// wordsGiven says whether the attempt records its words after a correct answer
// given.
func (w *world) wordsGiven(t *testing.T, attempt string) bool {
	t.Helper()
	var given bool
	if err := w.pool.QueryRow(t.Context(), `SELECT correct_words_given FROM attempts WHERE id = $1`, attempt).Scan(&given); err != nil {
		t.Fatal(err)
	}
	return given
}

// Before the day version 3 of the privacy policy is in force the words after a
// correct answer are "not admitted" on the route to a provider, with no call and
// nothing spent; from that day, UTC, they come and spend the call's price. The
// call has the open view of the task, its key and solution, its techniques by
// their titles, the module's language and the adult's style, and no crop when a
// tap checked the task. The words reach the learner once: a repeat gets none,
// another request "no more words", and the attempt records them given. The log
// holds no words.
func TestTheWordsAfterACorrectAnswerGoFromTheDayOfThePolicy(t *testing.T) {
	t.Parallel()

	ps, rs := &praisers{}, &readers{}
	ps.say(praise)
	w := newPraiseWorld(t, ps, rs, beforePolicy3)
	l := w.learner(t)
	a := w.begin(t, l, l.adult, "pick").GetAttemptId()
	w.wrongs(t, l, l.adult, a, []*v1.TappedChoice{tap(1, 1, "three and four")})
	res, err := w.praised(l, l.adult, a, nil)
	if err != nil || res.Words != nil || res.GetOutcome().GetCause() != v1.Cause_CAUSE_NOT_ADMITTED || ps.count() != 0 {
		t.Errorf("the words on 2026-11-26 = %v (%v), called %d; want none, not admitted, no call", res, err, ps.count())
	}
	if profile, account := w.counted(t, l.accountID, l.adult); profile != (quota.Counter{}) || account != profile {
		t.Errorf("the counters on 2026-11-26 = %+v, %+v; want nothing spent", profile, account)
	}

	w.clock.move(onPolicy3.Sub(beforePolicy3))
	req := &v1.GetCorrectAnswerWordsRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString()}
	res, err = w.attempts.GetCorrectAnswerWords(l.ctx, req)
	if err != nil || res.GetWords() != praise || res.GetOutcome() != nil || res.GetRepeated() || ps.count() != 1 {
		t.Fatalf("the words on 2026-11-27 = %v (%v), called %d; want the praise", res, err, ps.count())
	}
	called := ps.last()
	if called.Task == nil || called.Crop != nil || called.Key != "three and four" || called.Language != "en" || called.Style != "an adult" ||
		strings.Join(called.Solution, "|") != "3 + 4 = 7, and 7 is more than 5." || strings.Join(called.Techniques, "|") != "Count on" ||
		called.Route.AI != admissionv0.AiRoutePlatformAi {
		t.Errorf("the call = %+v, want the task's key, solution and technique, en, an adult, no crop, platform AI", called)
	}
	if profile, account := w.counted(t, l.accountID, l.adult); profile.Spent == 0 || profile.Reserved != 0 || account != profile {
		t.Errorf("the counters on 2026-11-27 = %+v, %+v; want the call spent", profile, account)
	}
	if !w.wordsGiven(t, a) {
		t.Error("the attempt does not record its words given")
	}
	again, err := w.attempts.GetCorrectAnswerWords(l.ctx, req)
	if err != nil || again.Words != nil || !again.GetRepeated() || ps.count() != 1 {
		t.Errorf("a repeat = %v (%v), want one without words, and no call", again, err)
	}
	_, err = w.praised(l, l.adult, a, nil)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_MORE_WORDS)
	if log := w.log.String(); strings.Contains(log, "put the two numbers") || !strings.Contains(log, `"message":"ai_usage"`) {
		t.Errorf("the log = %s, want a line of the call and no words", log)
	}
}

// The work goes to the call of the words after a correct answer of a task AI
// read, and must: none, or a crop that is no PNG, is malformed. Words that leak
// the key, break the form of words.Correct or are empty, and a call that gives
// none, leave the answer without words — the check in the request's line, the
// failure as the outcome — and the right stays, until words pass.
func TestTheWordsAfterACorrectAnswerThatFailGiveNone(t *testing.T) {
	t.Parallel()

	ps, rs := &praisers{}, &readers{}
	w := newPraiseWorld(t, ps, rs, onPolicy3)
	l := w.learner(t)
	a := w.correctOn(t, l, rs)
	ps.say(praise)
	_, err := w.praised(l, l.adult, a, nil)
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	_, err = w.praised(l, l.adult, a, sheet([]byte("no png")))
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	if ps.count() != 0 {
		t.Fatalf("a malformed work called %d times", ps.count())
	}
	for _, tc := range []struct {
		name, said, check string
		failed            bool
	}{
		{name: "a leak", said: "You found 7 apples. How did you count them?", check: "leak"},
		{name: "a form", said: "Well done, you counted on from three.", check: "form"},
		{name: "empty words", said: " ", check: "empty"},
		{name: "a failure", failed: true},
	} {
		if tc.failed {
			ps.give(tutor.CorrectAnswer{Outcome: tutor.Outcome{Kind: tutor.Failed}})
		} else {
			ps.say(tc.said)
		}
		before := len(w.log.String())
		res, err := w.praised(l, l.adult, a, sheet(crop(t)))
		switch {
		case err != nil || res.Words != nil:
			t.Errorf("%s = %v (%v), want no words", tc.name, res, err)
		case tc.failed && res.GetOutcome().GetCause() != v1.Cause_CAUSE_AI_FAILURE:
			t.Errorf("%s = %v, want the failure of AI", tc.name, res)
		case !tc.failed && (res.GetOutcome() != nil || !strings.Contains(w.log.String()[before:], `"words_check":"`+tc.check+`"`)):
			t.Errorf("%s = %v, the line %s; want no outcome and the check %s", tc.name, res, w.log.String()[before:], tc.check)
		}
		if strings.TrimSpace(tc.said) != "" && strings.Contains(w.log.String(), tc.said) {
			t.Errorf("the log holds the words of %s", tc.name)
		}
	}
	if w.wordsGiven(t, a) {
		t.Error("the attempt records words given that failed")
	}
	ps.say(praise)
	res, err := w.praised(l, l.adult, a, sheet(crop(t)))
	if err != nil || res.GetWords() != praise || len(ps.last().Crop) == 0 || ps.last().Key != "7" {
		t.Errorf("the words with the work = %v (%v), crop %d bytes, key %q; want the praise, the crop and the key 7",
			res, err, len(ps.last().Crop), ps.last().Key)
	}
}

// A spent quota makes no call of the words after a correct answer: no words,
// the outcome "quota" with when the next family day begins. An own key spends
// no quota; a child's is refused.
func TestTheWordsAfterACorrectAnswerOnTheQuotaAndAnOwnKey(t *testing.T) {
	t.Parallel()

	ps, rs := &praisers{}, &readers{}
	ps.say(praise)
	w := newPraiseWorld(t, ps, rs, onPolicy3)
	l := w.learner(t)
	a := w.correctOn(t, l, rs)
	day := w.spend(t, l.accountID, 1_500_000)
	res, err := w.praised(l, l.adult, a, sheet(crop(t)))
	if err != nil || res.Words != nil || res.GetOutcome().GetCause() != v1.Cause_CAUSE_QUOTA ||
		!res.GetOutcome().GetNextFamilyDay().AsTime().Equal(day.End) || ps.count() != 0 {
		t.Errorf("the words on a spent quota = %v (%v), called %d; want none, the quota and the next day, no call", res, err, ps.count())
	}
	profileBefore, _ := w.counted(t, l.accountID, l.adult)
	res, err = w.attempts.GetCorrectAnswerWords(l.ctx, &v1.GetCorrectAnswerWordsRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), Work: sheet(crop(t)), OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "sk-own"}})
	if err != nil || res.GetWords() != praise || ps.last().Route.AI != admissionv0.AiRouteOwnKey {
		t.Errorf("the words on an own key = %v (%v), want the praise on the own key", res, err)
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile != profileBefore {
		t.Errorf("an own key spent: %+v, then %+v", profileBefore, profile)
	}
	_, err = w.attempts.GetCorrectAnswerWords(l.ctx, &v1.GetCorrectAnswerWordsRequest{ProfileId: l.child, AttemptId: a,
		IdempotencyKey: uuid.NewString(), OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "sk-own"}})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE)
}

// A request beside another that had the words given while it called, or that
// a new attempt of the profile left behind, is "no more words" after its call.
func TestTheWordsAfterACorrectAnswerBesideAnotherAreGivenOnce(t *testing.T) {
	t.Parallel()

	ps, rs := &praisers{}, &readers{}
	w := newPraiseWorld(t, ps, rs, onPolicy3)
	l := w.learner(t)
	for _, beside := range []func(attempt string){
		func(attempt string) {
			w.exec(t, `UPDATE attempts SET correct_words_given = true WHERE id = $1`, attempt)
		},
		func(string) { w.begin(t, l, l.adult, "pick") },
	} {
		a := w.correctOn(t, l, rs)
		ps.mu.Lock()
		ps.next = func(context.Context, tutor.CorrectRequest) tutor.CorrectAnswer {
			beside(a)
			return tutor.CorrectAnswer{Words: praise, Outcome: tutor.Outcome{Kind: tutor.Answered}}
		}
		ps.mu.Unlock()
		_, err := w.praised(l, l.adult, a, sheet(crop(t)))
		refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_MORE_WORDS)
	}
}

// The stub words the praise after a correct answer before the day of the
// policy too: nothing goes to a provider (R66). Its words pass words.Correct.
func TestTheStubWordsAfterACorrectAnswerWhateverTheDay(t *testing.T) {
	t.Parallel()

	w, _ := newStubWorld(t)
	w.clock.move(beforePolicy3.Sub(time.Now()))
	l := w.learner(t)
	a := w.begin(t, l, l.adult, "apples").GetAttemptId()
	wide := sheet(pngOf(t, 40, 10))
	if res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), Work: wide}); err != nil || res.GetSolved() == nil {
		t.Fatalf("a wide crop = %v (%v), want the task solved", res, err)
	}
	res, err := w.praised(l, l.adult, a, wide)
	if err != nil || res.GetWords() == "" || res.GetOutcome() != nil {
		t.Fatalf("the stub's words on 2026-11-26 = %v (%v), want words", res, err)
	}
	if breaches := words.Correct(res.GetWords(), "en", ""); len(breaches) > 0 {
		t.Errorf("the stub's words %q break %v", res.GetWords(), breaches)
	}
}

// twice is the answer of a request and of its repeat under the same key.
func twice[R any](t *testing.T, ask func() (R, error)) [2]R {
	t.Helper()
	var answers [2]R
	for i := range answers {
		var err error
		if answers[i], err = ask(); err != nil {
			t.Fatal(err)
		}
	}
	return answers
}

// A limit of the own key names which limit ran out, by the reason tutor gives,
// beside CAUSE_LIMIT_REACHED in every answer whose call may meet it — the
// reading of a submission, the words of a rung, "explain differently" and the
// words after a correct answer —, and never when access returns; a repeat under
// the key gets the same. A reason tutor does not name, or none, is unnamed.
func TestAnOwnKeysLimitNamesWhichLimitRanOut(t *testing.T) {
	t.Parallel()

	ps, rs, ts := &praisers{}, &readers{}, &tellers{}
	w := worldOf(t, gate(t, adultFacts, config.RungAuthor), []learning.Option{learning.WithCorrect(ps.tell),
		learning.WithTexts(legal.Spec()), learning.WithReader(rs.read), learning.WithWords(ts.tell)})
	w.clock.move(onPolicy3.Sub(time.Now()))
	l := w.learner(t)
	own := &v1.OwnKey{Provider: "anthropic", Key: "sk-own"}
	wrong := int32(1)
	for _, tc := range []struct {
		reason tutor.LimitReason
		want   v1.LimitCause
	}{
		{"", v1.LimitCause_LIMIT_CAUSE_UNSPECIFIED},
		{tutor.LimitOwner, v1.LimitCause_LIMIT_CAUSE_LIMIT},
		{tutor.LimitTier, v1.LimitCause_LIMIT_CAUSE_TIER},
		{tutor.LimitBalance, v1.LimitCause_LIMIT_CAUSE_BALANCE},
		{tutor.LimitBilling, v1.LimitCause_LIMIT_CAUSE_PAYMENT},
		{"rate", v1.LimitCause_LIMIT_CAUSE_UNSPECIFIED},
	} {
		limited := tutor.Outcome{Kind: tutor.LimitReached, Limit: tc.reason}
		t.Run(string(tc.reason), func(t *testing.T) {
			// limitOf checks the outcome of an answer and of its repeat: the field is
			// there with no reason too.
			limitOf := func(name string, answers ...*v1.Outcome) {
				t.Helper()
				for _, o := range answers {
					limit := o.GetOwnKeyLimit()
					if limit == nil {
						t.Errorf("%s = %v, want own_key_limit", name, o)
						continue
					}
					if o.GetCause() != v1.Cause_CAUSE_LIMIT_REACHED || limit.GetCause() != tc.want || limit.GetAccessReturns() != nil {
						t.Errorf("%s = %v, want the limit reached, %v, and no time access returns", name, o, tc.want)
					}
				}
			}

			a := w.begin(t, l, l.adult, "apples").GetAttemptId()
			rs.give(tutor.SheetRead{Outcome: limited})
			submit := &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
				Work: sheet(crop(t)), OwnKey: own}
			s := twice(t, func() (*v1.SubmitWorkResponse, error) { return w.attempts.SubmitWork(l.ctx, submit) })
			limitOf("SubmitWork", s[0].GetOutcome(), s[1].GetOutcome())

			b := w.begin(t, l, l.adult, "pick").GetAttemptId()
			w.wrongs(t, l, l.adult, b, []*v1.TappedChoice{tap(1, 1, "five")})
			ts.give(tutor.WordsAnswer{Outcome: limited})
			rung := &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: b, IdempotencyKey: uuid.NewString(),
				WrongAnswers: &wrong, OwnKey: own}
			r := twice(t, func() (*v1.GetRungWordsResponse, error) { return w.attempts.GetRungWords(l.ctx, rung) })
			limitOf("GetRungWords", r[0].GetOutcome(), r[1].GetOutcome())
			explain := &v1.ExplainDifferentlyRequest{ProfileId: l.adult, AttemptId: b, IdempotencyKey: uuid.NewString(),
				WrongAnswers: &wrong, OwnKey: own}
			e := twice(t, func() (*v1.ExplainDifferentlyResponse, error) { return w.attempts.ExplainDifferently(l.ctx, explain) })
			limitOf("ExplainDifferently", e[0].GetOutcome(), e[1].GetOutcome())

			c := w.correctOn(t, l, rs)
			ps.give(tutor.CorrectAnswer{Outcome: limited})
			praise := &v1.GetCorrectAnswerWordsRequest{ProfileId: l.adult, AttemptId: c, IdempotencyKey: uuid.NewString(),
				Work: sheet(crop(t)), OwnKey: own}
			p := twice(t, func() (*v1.GetCorrectAnswerWordsResponse, error) {
				return w.attempts.GetCorrectAnswerWords(l.ctx, praise)
			})
			limitOf("GetCorrectAnswerWords", p[0].GetOutcome(), p[1].GetOutcome())
			if given := w.wordsGiven(t, c); !p[1].GetRepeated() || given {
				t.Errorf("the repeat of the words = %v, given %t; want a repeat and the right kept", p[1], given)
			}
		})
	}
}
