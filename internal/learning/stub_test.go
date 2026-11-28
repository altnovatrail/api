package learning_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	"github.com/google/uuid"

	"github.com/altnovatrail/api/internal/admission"
	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/legal"
	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/tutorstub"
)

// newStubWorld is the world of the dev mode: the gate of the stub, which admits
// an adult's handwriting and words at the author's stage with no key of the
// platform, and the stub, which reads and words in place of tutor.
func newStubWorld(t *testing.T) (*world, *admission.Gate) {
	t.Helper()
	g := stubGate(t)
	return worldOf(t, g, []learning.Option{learning.WithTutor(tutorstub.New(g))}), g
}

// stubGate is the gate of the dev mode, as just dev builds it: the facts for an
// adult, the author's stage and the stub's adapter.
func stubGate(t *testing.T) *admission.Gate {
	t.Helper()
	return stubGateOf(t, adultFacts)
}

// childFacts are the facts that open the reading of handwriting by Anthropic's
// models on platform AI for a child under 13 too, as dev-check declares them to
// its server on the stub, which sends nothing to a provider.
const childFacts = `anthropic:
  data_processing_agreement: [adult, under_13]
  minimal_retention: [adult, under_13]
  minor_safeguards: [under_13]
`

// stubGateOf is the gate of the dev mode on the facts given, with the variables
// more declares.
func stubGateOf(t *testing.T, given string, more ...string) *admission.Gate {
	t.Helper()
	facts := filepath.Join(t.TempDir(), "facts.yaml")
	if err := os.WriteFile(facts, []byte(given), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.AdmissionFrom(append([]string{"API_TUTOR_STUB=1", "API_ADMISSION_RUNG=author", "API_ADMISSION_FACTS_FILE=" + facts, "API_READING_MODE=pair"}, more...))
	if err != nil {
		t.Fatal(err)
	}
	g, _, err := admission.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// The path of the check, the hint and the words runs on the stub, with no
// provider: a square crop is illegible, a tall crop reads a wrong value, the
// rung's words are the stub's and pass the checks, "explain differently" gives
// other words, and a wide crop reads the right value and solves the task. Each
// call spends what tutor expects it to cost, the reserve it took, and "explain
// differently", the hint without the work that tutor expects no price of, keeps
// the value of its kind as spent: nothing is left in reserve. The log names the
// model stub.
func TestThePathRunsOnTheStub(t *testing.T) {
	t.Parallel()

	w, g := newStubWorld(t)
	l := w.learner(t)
	a := w.begin(t, l, l.adult, "apples").GetAttemptId()
	submit := func(width, height int) *v1.SubmitWorkResponse {
		t.Helper()
		res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a,
			IdempotencyKey: uuid.NewString(), Work: sheet(pngOf(t, width, height))})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := submit(10, 10); verdicts(res) != "unreadable[unreadable illegible]" || res.GetWrongAnswers() != 0 {
		t.Fatalf("a square crop = %s, %d wrong; want illegible, no wrong answer", verdicts(res), res.GetWrongAnswers())
	}
	if res := submit(10, 40); !strings.HasPrefix(verdicts(res), "not_yet") || res.GetWrongAnswers() != 1 {
		t.Fatalf("a tall crop = %s, %d wrong; want not yet, one wrong", verdicts(res), res.GetWrongAnswers())
	}
	wrong := int32(1)
	words, err := w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong, Work: sheet(pngOf(t, 10, 40))})
	if err != nil {
		t.Fatal(err)
	}
	if words.GetWords() == "" || words.GetWords() == trapAdvice || words.GetOutcome() != nil {
		t.Errorf("the rung's words = %v, want the stub's question, not the fallback", words)
	}
	other, err := w.explain(l, l.adult, a, 1)
	if err != nil {
		t.Fatal(err)
	}
	if other.GetWords() == "" || other.GetWords() == words.GetWords() || other.GetOutcome() != nil {
		t.Errorf("explained differently = %v, want other words of the stub than %q", other, words.GetWords())
	}
	if res := submit(40, 10); verdicts(res) != "correct[correct 7]" || res.GetSolved() == nil {
		t.Errorf("a wide crop = %s, solved %v; want correct and solved", verdicts(res), res.GetSolved())
	}

	adult := tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategoryAdult}
	sheetPrice := quota.USD(g.Price(tutor.CallSheet, adult).ExpectedUSD)
	wordsPrice := quota.USD(g.Price(tutor.CallWords, adult).ExpectedUSD)
	hintReserve := quota.Reserve(tutor.CallHint, g.Price(tutor.CallHint, adult))
	want := quota.Counter{Spent: 3*sheetPrice + wordsPrice + hintReserve}
	if profile, account := w.counted(t, l.accountID, l.adult); sheetPrice == 0 || wordsPrice == 0 || profile != want || account != want {
		t.Errorf("the counters = %+v, %+v; want %+v, the expected prices, and nothing in reserve", profile, account, want)
	}
	log := w.log.String()
	if !strings.Contains(log, `"model":"stub"`) || strings.Contains(log, "words_check") || strings.Contains(log, "claude-") {
		t.Errorf("the log = %s, want the stub's usage, no words turned down and no model of Claude", log)
	}
}

// On an own key the stub ends each call of AI by how the key ends, which api
// hands it beside the route: the reading, the words of a rung, "explain
// differently" and the words after a correct answer are refused — the key
// refused, or a limit with its cause —, while a key of no such ending is read
// and worded. Nothing of the quota of platform AI is spent, and the log holds
// no key.
func TestTheStubRefusesAnOwnKeyByItsEnding(t *testing.T) {
	t.Parallel()

	w, _ := newStubWorld(t)
	l := w.learner(t)
	a := w.begin(t, l, l.adult, "apples").GetAttemptId()
	key := func(ending string) *v1.OwnKey {
		return &v1.OwnKey{Provider: "anthropic", Key: "sk-ant-synthetic" + ending}
	}
	submit := func(width, height int, own *v1.OwnKey) *v1.SubmitWorkResponse {
		t.Helper()
		res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a,
			IdempotencyKey: uuid.NewString(), Work: sheet(pngOf(t, width, height)), OwnKey: own})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	before := time.Now().UTC()
	limited := func(name string, o *v1.Outcome, want v1.LimitCause) {
		t.Helper()
		// The ceiling of the tier alone says when access returns: 00:00 UTC on the
		// first of the next month, or of the month after, should the month turn
		// while the test runs.
		returns := o.GetOwnKeyLimit().GetAccessReturns()
		timed := want == v1.LimitCause_LIMIT_CAUSE_TIER
		next := time.Date(before.Year(), before.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		if at := returns.AsTime(); timed && (returns == nil || !at.Equal(next) && !at.Equal(next.AddDate(0, 1, 0))) {
			t.Errorf("%s: access returns %v, want 00:00 UTC on the first of the next month", name, returns)
		}
		if o.GetCause() != v1.Cause_CAUSE_LIMIT_REACHED || o.GetOwnKeyLimit() == nil || o.GetOwnKeyLimit().GetCause() != want || !timed && returns != nil {
			t.Errorf("%s = %v, want the limit reached with the cause %v", name, o, want)
		}
	}
	res := submit(40, 10, key(tutorstub.KeyLimitTier))
	limited("a reading on -limit-tier", res.GetOutcome(), v1.LimitCause_LIMIT_CAUSE_TIER)
	if res.GetWrongAnswers() != 0 || res.GetSolved() != nil {
		t.Errorf("a reading on -limit-tier = %d wrong, solved %v; want nothing checked", res.GetWrongAnswers(), res.GetSolved())
	}
	if o := submit(40, 10, key(tutorstub.KeyRefused)).GetOutcome(); o.GetCause() != v1.Cause_CAUSE_KEY_REFUSED || o.GetOwnKeyLimit() != nil {
		t.Errorf("a reading on -refused = %v, want the key refused", o)
	}
	if res := submit(10, 40, key("")); !strings.HasPrefix(verdicts(res), "not_yet") || res.GetWrongAnswers() != 1 {
		t.Fatalf("a tall crop on a key of no ending = %s, %d wrong; want not yet, one wrong", verdicts(res), res.GetWrongAnswers())
	}
	wrong := int32(1)
	words, err := w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong, Work: sheet(pngOf(t, 10, 40)), OwnKey: key(tutorstub.KeyLimitBalance)})
	if err != nil {
		t.Fatal(err)
	}
	limited("the rung's words on -limit-balance", words.GetOutcome(), v1.LimitCause_LIMIT_CAUSE_BALANCE)
	// The stub reads the slot's first trap value, whose trap's advice is the fallback.
	if words.GetWords() != trapAdvice {
		t.Errorf("the rung's words on -limit-balance = %q, want the fallback text", words.GetWords())
	}
	other, err := w.attempts.ExplainDifferently(l.ctx, &v1.ExplainDifferentlyRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong, OwnKey: key(tutorstub.KeyLimit)})
	if err != nil {
		t.Fatal(err)
	}
	limited("explained differently on -limit", other.GetOutcome(), v1.LimitCause_LIMIT_CAUSE_UNSPECIFIED)
	if res := submit(40, 10, key("")); res.GetSolved() == nil {
		t.Fatalf("a wide crop on a key of no ending = %s, want solved", verdicts(res))
	}
	praise, err := w.attempts.GetCorrectAnswerWords(l.ctx, &v1.GetCorrectAnswerWordsRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), Work: sheet(pngOf(t, 40, 10)), OwnKey: key(tutorstub.KeyLimitPayment)})
	if err != nil {
		t.Fatal(err)
	}
	limited("the words after a correct answer on -limit-payment", praise.GetOutcome(), v1.LimitCause_LIMIT_CAUSE_PAYMENT)
	if praise.GetWords() != "" {
		t.Errorf("the words after a correct answer on -limit-payment = %q, want none", praise.GetWords())
	}

	if profile, account := w.counted(t, l.accountID, l.adult); profile != (quota.Counter{}) || account != (quota.Counter{}) {
		t.Errorf("the counters = %+v, %+v; want nothing of platform AI spent on an own key", profile, account)
	}
	if log := w.log.String(); strings.Contains(log, "sk-ant-synthetic") {
		t.Errorf("the log = %s, want no own key", log)
	}
}

// Off the stub the gate's adapter drops the own key's value: tutor takes the key
// from the route alone, and an ending of the stub's refuses nothing there. On
// the stub's gate, whose adapter refuses every call of a model, the reading, the
// words of a rung and the words after a correct answer on such a key go to
// tutor's call and fail as AI does, with no limit and no key refused.
func TestTheGateHearsNoEndingOfAnOwnKey(t *testing.T) {
	t.Parallel()

	g := stubGate(t)
	rs := &readers{}
	w := worldOf(t, g, []learning.Option{learning.WithReader(rs.read), learning.WithTexts(legal.Spec())})
	w.clock.move(onPolicy3.Sub(time.Now()))
	l := w.learner(t)
	a := w.begin(t, l, l.adult, "apples").GetAttemptId()
	key := func(ending string) *v1.OwnKey {
		return &v1.OwnKey{Provider: "anthropic", Key: "sk-ant-synthetic" + ending}
	}
	failed := func(name string, o *v1.Outcome) {
		t.Helper()
		if o.GetCause() != v1.Cause_CAUSE_AI_FAILURE || o.GetOwnKeyLimit() != nil {
			t.Errorf("%s on the gate = %v, want a failure of AI, not the stub's refusal", name, o)
		}
	}
	submit := func(own *v1.OwnKey) *v1.SubmitWorkResponse {
		t.Helper()
		res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a,
			IdempotencyKey: uuid.NewString(), Work: sheet(pngOf(t, 40, 10)), OwnKey: own})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	// The reading goes through the gate's adapter, with the value it drops.
	rs.next = func(ctx context.Context, r tutor.SheetReading) tutor.SheetRead {
		return learning.GateTutor(g).ReadSheet(ctx, r, formatv0.Task{}, key(tutorstub.KeyLimitTier).GetKey())
	}
	failed("a reading on -limit-tier", submit(key(tutorstub.KeyLimitTier)).GetOutcome())
	rs.give(answered(read("6"), read("6")))
	if res := submit(key("")); res.GetWrongAnswers() != 1 {
		t.Fatalf("a reading of 6 = %s, %d wrong; want one wrong", verdicts(res), res.GetWrongAnswers())
	}
	wrong := int32(1)
	words, err := w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong, Work: sheet(pngOf(t, 10, 40)), OwnKey: key(tutorstub.KeyLimitBalance)})
	if err != nil {
		t.Fatal(err)
	}
	failed("the rung's words on -limit-balance", words.GetOutcome())
	rs.give(answered(read("7"), read("7")))
	if res := submit(key("")); res.GetSolved() == nil {
		t.Fatalf("a reading of 7 = %s, want solved", verdicts(res))
	}
	praise, err := w.attempts.GetCorrectAnswerWords(l.ctx, &v1.GetCorrectAnswerWordsRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), Work: sheet(pngOf(t, 40, 10)), OwnKey: key(tutorstub.KeyLimitPayment)})
	if err != nil {
		t.Fatal(err)
	}
	failed("the words after a correct answer on -limit-payment", praise.GetOutcome())
	if log := w.log.String(); strings.Count(log, `"ai_outcome":"failed"`) != 3 {
		t.Errorf("the log = %s, want three calls of AI that failed", log)
	}
}

// An essay runs on the stub: the stub admits its reading and its assessment
// where the gate admits a sheet's, so story begins for an adult and enters the
// pool, but not for a child, whose sheet the facts of an adult do not admit. On
// an own key the stub refuses the reading by the key's ending; a square crop
// reads illegible and a tall one blank, each giving the assessment's reserve
// back; a wide one reads the stub's text, assessed every criterion met but the
// last, with the stub's feedback, which passes the leak check and the rule of
// feedback. "Done" then closes the attempt with story's criteria. Each reading
// keeps tutor's expected price of the reading as spent, and the assessment its
// own, the prices of the entries admitted without an eval run. The log holds
// neither the text nor the feedback, nor the key.
func TestAnEssayRunsOnTheStub(t *testing.T) {
	t.Parallel()

	w, g := newStubWorld(t)
	l := w.learner(t)
	pool, _, err := w.service.Candidates(w.current, formatv0.AgeCategoryAdult, "")
	if err != nil || !slices.Contains(pool, module+"/task/story") {
		t.Errorf("the pool = %v, %v; want story in it", pool, err)
	}
	_, err = w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: l.child, IdempotencyKey: uuid.NewString(),
		Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: w.choice(t, l.child, "story")}})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED)

	a := w.begin(t, l, l.adult, "story").GetAttemptId()
	submit := func(width, height int, own *v1.OwnKey) *v1.SubmitEssayResponse {
		t.Helper()
		res, err := w.attempts.SubmitEssay(l.ctx, &v1.SubmitEssayRequest{ProfileId: l.adult, AttemptId: a,
			IdempotencyKey: uuid.NewString(), Crop: pngOf(t, width, height), OwnKey: own})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	refusedKey := "sk-ant-synthetic" + tutorstub.KeyRefused
	if res := submit(40, 10, &v1.OwnKey{Provider: "anthropic", Key: refusedKey}); res.GetEnd() != v1.SubmissionEnd_SUBMISSION_END_UNCHECKED ||
		res.GetOutcome().GetCause() != v1.Cause_CAUSE_KEY_REFUSED || res.Text != nil {
		t.Errorf("an essay on -refused = %v, want unchecked, the key refused", res)
	}
	for _, tc := range []struct {
		width, height int
		reason        string
	}{{10, 10, "illegible"}, {10, 40, "blank"}} {
		if res := submit(tc.width, tc.height, nil); res.GetEnd() != v1.SubmissionEnd_SUBMISSION_END_UNREADABLE || res.GetReason() != tc.reason {
			t.Errorf("an essay of %d × %d = %v, want unreadable, %s", tc.width, tc.height, res, tc.reason)
		}
	}
	res := submit(40, 10, nil)
	text, feedback := res.GetText(), res.GetFeedback()
	if res.GetEnd() != v1.SubmissionEnd_SUBMISSION_END_FEEDBACK || text == "" || feedback == "" {
		t.Fatalf("a wide essay = %v, want feedback with the text read and the stub's words", res)
	}
	rows := w.essayRows(t, a)
	if len(rows) != 4 || !slices.Equal(rows[3].marks, []string{"met", "not_met"}) || deref(rows[3].source) != "model" {
		t.Errorf("the submissions kept are %+v, want four, the last met but its last criterion, by the model's words", rows)
	}
	done, err := w.done(l, l.adult, a)
	if err != nil || !slices.Equal(done.GetSolved().GetCriteria(), storyCriteria) || w.closeOf(t, a) != "done" {
		t.Errorf("Done = %v, %v; want story's criteria and the attempt closed done", done, err)
	}

	adult := tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategoryAdult}
	readPrice, assessPrice := g.Price(tutor.CallEssay, adult), g.Price(tutor.CallRubric, adult)
	reading, assessing := quota.Reserve(tutor.CallEssay, readPrice), quota.Reserve(tutor.CallRubric, assessPrice)
	want := quota.Counter{Spent: 3*reading + assessing}
	if profile, account := w.counted(t, l.accountID, l.adult); !readPrice.Expected || !assessPrice.Expected ||
		reading != quota.USD(readPrice.ExpectedUSD) || assessing != quota.USD(assessPrice.ExpectedUSD) || profile != want || account != want {
		t.Errorf("the counters = %+v, %+v by %d and %d; want tutor's expected prices, three readings and an assessment on platform AI kept as spent, %+v",
			profile, account, reading, assessing, want)
	}
	if log := w.log.String(); !strings.Contains(log, `"model":"stub"`) || strings.Contains(log, text) || strings.Contains(log, feedback) ||
		strings.Contains(log, refusedKey) {
		t.Errorf("the log = %s, want the stub's usage and neither the text, the feedback nor the own key", log)
	}
}

// At the stage full the stub admits no essay, as tutor admits there none of the
// entries admitted without an eval run, and a workbook page, which no task is
// solved on, begins at no stage: neither table nor story begins, nor enters the
// pool.
func TestTheStubAtTheStageFullReadsNoWorkbookAndNoEssay(t *testing.T) {
	t.Parallel()

	g := stubGateOf(t, childFacts, "API_ADMISSION_RUNG=full")
	w := worldOf(t, g, []learning.Option{learning.WithTutor(tutorstub.New(g))})
	l := w.learner(t)
	pool, _, err := w.service.Candidates(w.current, formatv0.AgeCategoryAdult, "")
	if err != nil || slices.Contains(pool, module+"/task/table") || slices.Contains(pool, module+"/task/story") {
		t.Errorf("the pool at the stage full = %v, %v; want neither table nor story", pool, err)
	}
	for _, task := range []string{"table", "story"} {
		_, err := w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString(),
			Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: w.choice(t, l.adult, task)}})
		t.Run(task, func(t *testing.T) { refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED) })
	}
}

// In the mode single the stub reads a sheet by one reader, as tutor's Haiku
// alone: module-kit gives the verdict on the one observation, each reading
// spends what tutor expects of the one reader, less than the pair, on one line
// of usage, and nothing is left in reserve.
func TestThePathRunsOnTheStubByOneReader(t *testing.T) {
	t.Parallel()

	g := stubGateOf(t, adultFacts, "API_READING_MODE=single")
	w := worldOf(t, g, []learning.Option{learning.WithTutor(tutorstub.New(g))})
	l := w.learner(t)
	a := w.begin(t, l, l.adult, "apples").GetAttemptId()
	submit := func(width, height int) *v1.SubmitWorkResponse {
		t.Helper()
		res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a,
			IdempotencyKey: uuid.NewString(), Work: sheet(pngOf(t, width, height))})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := submit(10, 10); verdicts(res) != "unreadable[unreadable illegible]" || res.GetOutcome() != nil {
		t.Fatalf("a square crop = %s, %v; want illegible", verdicts(res), res.GetOutcome())
	}
	if res := submit(10, 40); !strings.HasPrefix(verdicts(res), "not_yet") || res.GetWrongAnswers() != 1 {
		t.Fatalf("a tall crop = %s, %d wrong; want not yet, one wrong", verdicts(res), res.GetWrongAnswers())
	}
	if res := submit(40, 10); verdicts(res) != "correct[correct 7]" || res.GetSolved() == nil {
		t.Errorf("a wide crop = %s, solved %v; want correct and solved", verdicts(res), res.GetSolved())
	}

	adult := tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategoryAdult}
	one := quota.USD(g.Price(tutor.CallSheet, adult).ExpectedUSD)
	two := quota.USD(stubGate(t).Price(tutor.CallSheet, adult).ExpectedUSD)
	want := quota.Counter{Spent: 3 * one}
	if profile, account := w.counted(t, l.accountID, l.adult); one == 0 || one >= two || profile != want || account != want {
		t.Errorf("the counters = %+v, %+v; want %+v, the one reader's price, less than the pair's %d", profile, account, want, two)
	}
	if n := strings.Count(w.log.String(), `"message":"ai_usage"`); n != 3 {
		t.Errorf("the log has %d lines of usage, want one for each of the three readings", n)
	}
}
