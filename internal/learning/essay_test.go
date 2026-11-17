package learning_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/tutor"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/quota"
)

// essays is the reader and the assessor of a test's essays: what they give
// next, what they were given, and what the reader does while it reads.
type essays struct {
	mu          sync.Mutex
	read        tutor.EssayRead
	assessed    tutor.RubricAssessed
	reads       []tutor.EssayReading
	assessments []tutor.RubricAssessment
	// during runs while the essay is read, between the request's transactions.
	during func()
}

func (es *essays) readEssay(_ context.Context, r tutor.EssayReading) tutor.EssayRead {
	es.mu.Lock()
	es.reads = append(es.reads, r)
	got, during := es.read, es.during
	es.mu.Unlock()
	if during != nil {
		during()
	}
	return got
}

func (es *essays) assess(_ context.Context, r tutor.RubricAssessment) tutor.RubricAssessed {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.assessments = append(es.assessments, r)
	return es.assessed
}

// give is what the reader and the assessor give next.
func (es *essays) give(read tutor.EssayRead, assessed tutor.RubricAssessed) {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.read, es.assessed = read, assessed
}

// meanwhile is what the reader does while it reads, from now on.
func (es *essays) meanwhile(during func()) {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.during = during
}

// calls is how many essays were read and assessed.
func (es *essays) calls() (reads, assessments int) {
	es.mu.Lock()
	defer es.mu.Unlock()
	return len(es.reads), len(es.assessments)
}

// essayText is the reading of the essay of story, its one slot bound sheet.
func essayText(text string) tutor.EssayRead {
	return tutor.EssayRead{Observation: &observationv1.Observation{AnswerSlots: []*observationv1.AnswerSlotObservation{{
		AnswerSlot: &observationv1.AnswerSlotPlace{Item: 1, Slot: 1},
		Read:       &observationv1.AnswerSlotObservation_Value{Value: text},
	}}}, Outcome: tutor.Outcome{Kind: tutor.Answered,
		Usage: []tutor.Usage{{Provider: "anthropic", Model: "claude-opus-5-5", Billed: true, Priced: true, USD: 0.004}}}}
}

// essayUnread is the reading of the essay of story that cannot be read, for
// reason.
func essayUnread(reason observationv1.Unreadable) tutor.EssayRead {
	return tutor.EssayRead{Observation: &observationv1.Observation{AnswerSlots: []*observationv1.AnswerSlotObservation{{
		AnswerSlot: &observationv1.AnswerSlotPlace{Item: 1, Slot: 1},
		Read:       &observationv1.AnswerSlotObservation_Unreadable{Unreadable: reason},
	}}}, Outcome: tutor.Outcome{Kind: tutor.Answered,
		Usage: []tutor.Usage{{Provider: "anthropic", Model: "claude-opus-5-5", Billed: true, Priced: true, USD: 0.004}}}}
}

// assessment is an assessment of story's two criteria, nil for one with no
// assessment, with the feedback.
func assessment(feedback string, met ...*bool) tutor.RubricAssessed {
	got := tutor.RubricAssessed{Feedback: feedback, Outcome: tutor.Outcome{Kind: tutor.Answered,
		Usage: []tutor.Usage{{Provider: "anthropic", Model: "claude-opus-5-5", Billed: true, Priced: true, USD: 0.006}}}}
	for _, m := range met {
		got.Criteria = append(got.Criteria, tutor.CriterionMark{Evidence: "the learner's own words", Met: m})
	}
	return got
}

var (
	yes, no = new(bool), new(bool)
	// essay is what the test's learner wrote, as it was read.
	essay = "I give each friend two sweets and keep none for me."
	// kindWords is feedback that keeps the rule of feedback.
	kindWords = "Your story shares every sweet. Next time, say how many friends there are."
)

func init() { *yes = true }

// newEssayWorld is the world of a deployment that admits the reading of an
// adult's handwriting on a sheet, and, by the test's own admission, the reading
// of an essay and its assessment too, by es, for want of a provider. opts are more of the world's options.
func newEssayWorld(t *testing.T, es *essays, opts ...learning.Option) *world {
	t.Helper()
	return worldOf(t, gate(t, adultFacts, config.RungAuthor), append([]learning.Option{
		learning.WithAdmits(func(tutor.Call) bool { return true }), learning.WithEssay(es.readEssay, es.assess)}, opts...))
}

// submitEssay sends the crop of story's sheet on the attempt, on platform AI
// or by own.
func (w *world) submitEssay(t *testing.T, l learner, attempt string, own *v1.OwnKey) (*v1.SubmitEssayResponse, error) {
	t.Helper()
	return w.attempts.SubmitEssay(l.ctx, &v1.SubmitEssayRequest{ProfileId: l.adult, AttemptId: attempt,
		IdempotencyKey: uuid.NewString(), Crop: pngOf(t, 40, 30), OwnKey: own})
}

// essayRow is a submission of an essay as the database keeps it.
type essayRow struct {
	end, source *string
	marks       []string
	answer      []byte
	observed    bool
}

// essayRows is the submissions of the attempt, in order.
func (w *world) essayRows(t *testing.T, attempt string) []essayRow {
	t.Helper()
	rows, err := w.pool.Query(t.Context(), `SELECT essay_end, words_source, marks, answer, observation IS NOT NULL
		FROM submissions WHERE attempt_id = $1 ORDER BY number`, attempt)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []essayRow
	for rows.Next() {
		var r essayRow
		if err := rows.Scan(&r.end, &r.source, &r.marks, &r.answer, &r.observed); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// deref is the string p points to, or "-".
func deref(p *string) string {
	if p == nil {
		return "-"
	}
	return *p
}

// An essay is read and its text assessed, and the submission ends feedback,
// unreadable or unchecked (R14): feedback with the text and the model's words
// once they keep the rule of feedback, and without the words otherwise; the
// marks are kept by the numbers of the criteria with the source of the words,
// never the text or the words. Each submission goes on the same attempt, which
// stays open for rewriting, and the open attempt says how each ended.
func TestAnEssayEndsInFeedbackUnreadableOrUnchecked(t *testing.T) {
	t.Parallel()

	es := &essays{}
	w := newEssayWorld(t, es)
	l := w.learner(t)
	attempt := w.begin(t, l, l.adult, "story").GetAttemptId()
	own := &v1.OwnKey{Provider: "anthropic", Key: "sk-own"}
	var kept []string
	for _, tc := range []struct {
		name     string
		read     tutor.EssayRead
		assessed tutor.RubricAssessed
		own      *v1.OwnKey
		end      v1.SubmissionEnd
		text     bool
		feedback bool
		reason   string
		cause    v1.Cause
		// kept is the end, the marks and the source the row keeps.
		kept  string
		check string
	}{
		{"feedback", essayText(essay), assessment(kindWords, yes, no), nil, v1.SubmissionEnd_SUBMISSION_END_FEEDBACK,
			true, true, "", v1.Cause_CAUSE_UNSPECIFIED, "feedback [met not_met] model", ""},
		{"a criterion not assessed", essayText(essay), assessment(kindWords, nil, yes), nil, v1.SubmissionEnd_SUBMISSION_END_FEEDBACK,
			true, true, "", v1.Cause_CAUSE_UNSPECIFIED, "feedback [not_assessed met] model", ""},
		{"a mark missing", essayText(essay), assessment(kindWords, no), nil, v1.SubmissionEnd_SUBMISSION_END_FEEDBACK,
			true, true, "", v1.Cause_CAUSE_UNSPECIFIED, "feedback [not_met not_assessed] model", ""},
		{"feedback too long", essayText(essay), assessment("It is good. It is kind. It is fair. It is short.", yes, yes), nil,
			v1.SubmissionEnd_SUBMISSION_END_FEEDBACK, true, false, "", v1.Cause_CAUSE_UNSPECIFIED, "feedback [met met] no_feedback", "form"},
		{"a quote not of the essay", essayText(essay), assessment(`You wrote "all my sweets go to my dog".`, yes, yes), nil,
			v1.SubmissionEnd_SUBMISSION_END_FEEDBACK, true, false, "", v1.Cause_CAUSE_UNSPECIFIED, "feedback [met met] no_feedback", "form"},
		{"no words", essayText(essay), assessment(" ", yes, yes), nil, v1.SubmissionEnd_SUBMISSION_END_FEEDBACK,
			true, false, "", v1.Cause_CAUSE_UNSPECIFIED, "feedback [met met] no_feedback", "empty"},
		{"no criterion assessed", essayText(essay), assessment(kindWords, nil, nil), nil, v1.SubmissionEnd_SUBMISSION_END_UNCHECKED,
			true, false, "", v1.Cause_CAUSE_UNSPECIFIED, "unchecked [] -", ""},
		{"the assessment failed", essayText(essay), tutor.RubricAssessed{Outcome: tutor.Outcome{Kind: tutor.Failed}}, nil,
			v1.SubmissionEnd_SUBMISSION_END_UNCHECKED, true, false, "", v1.Cause_CAUSE_AI_FAILURE, "unchecked [] -", ""},
		{"the own key's limit", essayText(essay), tutor.RubricAssessed{Outcome: tutor.Outcome{Kind: tutor.LimitReached, Limit: tutor.LimitBalance}},
			own, v1.SubmissionEnd_SUBMISSION_END_UNCHECKED, true, false, "", v1.Cause_CAUSE_LIMIT_REACHED, "unchecked [] -", ""},
		{"the own key's tier", tutor.EssayRead{Outcome: tutor.Outcome{Kind: tutor.LimitReached, Limit: tutor.LimitTier, AccessReturns: nextMonth}},
			assessment(kindWords, yes, yes), own, v1.SubmissionEnd_SUBMISSION_END_UNCHECKED, false, false, "", v1.Cause_CAUSE_LIMIT_REACHED, "unchecked [] -", ""},
		{"a blank sheet", essayUnread(observationv1.Unreadable_UNREADABLE_BLANK), assessment(kindWords, yes, yes), nil,
			v1.SubmissionEnd_SUBMISSION_END_UNREADABLE, false, false, "blank", v1.Cause_CAUSE_UNSPECIFIED, "unreadable [] -", ""},
		{"an illegible sheet", essayUnread(observationv1.Unreadable_UNREADABLE_ILLEGIBLE), assessment(kindWords, yes, yes), nil,
			v1.SubmissionEnd_SUBMISSION_END_UNREADABLE, false, false, "illegible", v1.Cause_CAUSE_UNSPECIFIED, "unreadable [] -", ""},
		{"the reading not admitted", tutor.EssayRead{Outcome: tutor.Outcome{Kind: tutor.NotAdmitted}}, assessment(kindWords, yes, yes), nil,
			v1.SubmissionEnd_SUBMISSION_END_UNCHECKED, false, false, "", v1.Cause_CAUSE_NOT_ADMITTED, "unchecked [] -", ""},
		{"the own key refused", tutor.EssayRead{Outcome: tutor.Outcome{Kind: tutor.KeyRefused}}, assessment(kindWords, yes, yes), own,
			v1.SubmissionEnd_SUBMISSION_END_UNCHECKED, false, false, "", v1.Cause_CAUSE_KEY_REFUSED, "unchecked [] -", ""},
		{"a reading of no slot", tutor.EssayRead{Outcome: tutor.Outcome{Kind: tutor.Answered}}, assessment(kindWords, yes, yes), nil,
			v1.SubmissionEnd_SUBMISSION_END_UNCHECKED, false, false, "", v1.Cause_CAUSE_AI_FAILURE, "unchecked [] -", ""},
		{"a reading of another slot", tutor.EssayRead{Observation: &observationv1.Observation{AnswerSlots: []*observationv1.AnswerSlotObservation{{
			AnswerSlot: &observationv1.AnswerSlotPlace{Item: 2, Slot: 1}, Read: &observationv1.AnswerSlotObservation_Value{Value: essay}}}},
			Outcome: tutor.Outcome{Kind: tutor.Answered}}, assessment(kindWords, yes, yes), nil,
			v1.SubmissionEnd_SUBMISSION_END_UNCHECKED, false, false, "", v1.Cause_CAUSE_AI_FAILURE, "unchecked [] -", ""},
		{"a reading of no text", essayText(" \n"), assessment(kindWords, yes, yes), nil,
			v1.SubmissionEnd_SUBMISSION_END_UNCHECKED, false, false, "", v1.Cause_CAUSE_AI_FAILURE, "unchecked [] -", ""},
	} {
		es.give(tc.read, tc.assessed)
		kept = append(kept, tc.kept)
		reads, assessments := es.calls()
		before := len(w.log.String())
		res, err := w.submitEssay(t, l, attempt, tc.own)
		t.Run(tc.name, func(t *testing.T) {
			if err != nil {
				t.Fatal(err)
			}
			if res.GetEnd() != tc.end || (res.Text != nil) != tc.text || (res.Feedback != nil) != tc.feedback ||
				res.GetReason() != tc.reason || res.GetOutcome().GetCause() != tc.cause || res.GetRepeated() ||
				res.GetClosed() != v1.CloseReason_CLOSE_REASON_UNSPECIFIED {
				t.Errorf("SubmitEssay = %v; want %v, text %v, feedback %v, reason %q, %v", res, tc.end, tc.text, tc.feedback, tc.reason, tc.cause)
			}
			if tc.text && res.GetText() != essay || tc.feedback && res.GetFeedback() != kindWords {
				t.Errorf("SubmitEssay gave %q and %q, want the text read and the model's words", res.GetText(), res.GetFeedback())
			}
			// The balance is the assessment's limit, with no time, and the tier the
			// reading's, with when access returns (R80).
			if limit := res.GetOutcome().GetOwnKeyLimit(); tc.cause == v1.Cause_CAUSE_LIMIT_REACHED &&
				!(limit.GetCause() == v1.LimitCause_LIMIT_CAUSE_BALANCE && returnsAt(limit, time.Time{}) ||
					limit.GetCause() == v1.LimitCause_LIMIT_CAUSE_TIER && returnsAt(limit, nextMonth)) {
				t.Errorf("the own key's limit = %v, want the balance with no time or the tier with %v", res.GetOutcome(), nextMonth)
			}
			nowReads, nowAssessments := es.calls()
			if want := assessments + btoi(tc.text); nowReads != reads+1 || nowAssessments != want {
				t.Errorf("%d readings and %d assessments, want 1 and %d", nowReads-reads, nowAssessments-assessments, want-assessments)
			}
			line := w.log.String()[before:]
			if tc.check != "" && !strings.Contains(line, `"words_check":"`+tc.check+`"`) {
				t.Errorf("the line %s names no check %s", line, tc.check)
			}
			if strings.Contains(line, essay) || strings.Contains(line, "sweets go to my dog") || strings.Contains(line, "Next time") {
				t.Errorf("the log holds the essay or the feedback: %s", line)
			}
		})
	}
	if res := w.closeOf(t, attempt); res != "" {
		t.Errorf("the attempt is closed %q, want it open for rewriting", res)
	}
	rows := w.essayRows(t, attempt)
	open, err := w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: l.adult, AttemptId: attempt})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(kept) || len(open.GetAttempt().GetEssaySubmissions()) != len(rows) || len(open.GetAttempt().GetItems()) != 0 {
		t.Fatalf("the open attempt = %v, want %d essays and no verdict", open.GetAttempt(), len(rows))
	}
	for i, r := range rows {
		var answer v1.SubmitEssayResponse
		if err := proto.Unmarshal(r.answer, &answer); err != nil {
			t.Fatal(err)
		}
		if got := deref(r.end) + " [" + strings.Join(r.marks, " ") + "] " + deref(r.source); got != kept[i] {
			t.Errorf("submission %d is kept as %s, want %s", i+1, got, kept[i])
		}
		if answer.Text != nil || answer.Feedback != nil || r.observed || bytes.Contains(r.answer, []byte(essay)) {
			t.Errorf("submission %d keeps %v, observed %v: want neither the text nor the words", i+1, &answer, r.observed)
		}
		if e := open.GetAttempt().GetEssaySubmissions()[i]; e.GetEnd() != answer.GetEnd() || e.GetSubmittedAt() == nil {
			t.Errorf("the open attempt's essay %d = %v, want %v", i+1, e, answer.GetEnd())
		}
	}
	// The reader got the crop, the open view of story and the route; the
	// assessment the text, the statement, the criteria, the style and the
	// language, and no picture.
	first, assessed := es.reads[0], es.assessments[0]
	if !bytes.Equal(first.Crop, pngOf(t, 40, 30)) || first.Task.ID != "story" || first.Route.AI != admissionv0.AiRoutePlatformAi ||
		first.Route.Category != admissionv0.AgeCategoryAdult {
		t.Errorf("the reader was given %s by %v", first.Task.ID, first.Route)
	}
	if assessed.Text != essay || !strings.HasPrefix(assessed.Topic, "Tell in a few sentences") ||
		!slices.Equal(assessed.Criteria, []string{"The story shares all the sweets.", "Every friend gets as many sweets as the others."}) ||
		assessed.Style != "an adult" || assessed.Language != "en" {
		t.Errorf("the assessment was given %+v", assessed)
	}
	if own := es.reads[8]; own.Route.AI != admissionv0.AiRouteOwnKey || own.Route.Own.Provider() != "anthropic" {
		t.Errorf("the own key's reading went by %v", own.Route)
	}
}

// btoi is 1 for true.
func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// A repeat under the key calls nothing and gets how the submission ended,
// repeated, without the text read and the words of the feedback, which are
// nowhere on the server.
func TestAnEssayRepeatsByItsKeyWithoutItsText(t *testing.T) {
	t.Parallel()

	es := &essays{}
	es.give(essayText(essay), assessment(kindWords, yes, no))
	w := newEssayWorld(t, es)
	l := w.learner(t)
	attempt := w.begin(t, l, l.adult, "story").GetAttemptId()
	req := &v1.SubmitEssayRequest{ProfileId: l.adult, AttemptId: attempt, IdempotencyKey: uuid.NewString(), Crop: pngOf(t, 40, 30)}
	first, err := w.attempts.SubmitEssay(l.ctx, req)
	if err != nil || first.GetFeedback() != kindWords {
		t.Fatalf("SubmitEssay = %v, %v", first, err)
	}
	again, err := w.attempts.SubmitEssay(l.ctx, req)
	if err != nil || !again.GetRepeated() || again.Text != nil || again.Feedback != nil ||
		again.GetEnd() != v1.SubmissionEnd_SUBMISSION_END_FEEDBACK {
		t.Errorf("the repeat = %v, %v; want feedback repeated, without the text and the words", again, err)
	}
	if reads, assessments := es.calls(); reads != 1 || assessments != 1 {
		t.Errorf("the repeat called %d readings and %d assessments, want none more", reads-1, assessments-1)
	}
	var records int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM idempotency_keys WHERE key = $1 AND response IS NULL
		AND submission_id IS NOT NULL`, req.GetIdempotencyKey()).Scan(&records); err != nil || records != 1 {
		t.Errorf("the key's records naming the submission = %d (%v), want 1", records, err)
	}
	// The same key on SubmitWork is another method's.
	_, err = w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: attempt, IdempotencyKey: req.GetIdempotencyKey()})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_ALREADY_USED)
}

// An attempt another device closed while the essay was read is no refusal: the
// answer from the request's memory has the feedback and names the close,
// nothing is recorded, and the repeat gets the same, without the text and the
// words.
func TestAnEssayWhoseAttemptClosedMeanwhileAnswersFromMemory(t *testing.T) {
	t.Parallel()

	es := &essays{}
	es.give(essayText(essay), assessment(kindWords, yes, yes))
	w := newEssayWorld(t, es)
	l := w.learner(t)
	attempt := w.begin(t, l, l.adult, "story").GetAttemptId()
	es.meanwhile(func() { w.begin(t, l, l.adult, "pick") })
	req := &v1.SubmitEssayRequest{ProfileId: l.adult, AttemptId: attempt, IdempotencyKey: uuid.NewString(), Crop: pngOf(t, 40, 30)}
	res, err := w.attempts.SubmitEssay(l.ctx, req)
	if err != nil || res.GetClosed() != v1.CloseReason_CLOSE_REASON_LEFT || res.GetFeedback() != kindWords || res.GetText() != essay ||
		res.GetEnd() != v1.SubmissionEnd_SUBMISSION_END_FEEDBACK {
		t.Fatalf("SubmitEssay on an attempt closed meanwhile = %v, %v; want the feedback and the close", res, err)
	}
	if rows := w.essayRows(t, attempt); len(rows) != 0 {
		t.Errorf("the attempt closed meanwhile keeps %d submissions, want none", len(rows))
	}
	es.meanwhile(nil)
	again, err := w.attempts.SubmitEssay(l.ctx, req)
	if err != nil || !again.GetRepeated() || again.GetClosed() != v1.CloseReason_CLOSE_REASON_LEFT || again.Text != nil || again.Feedback != nil {
		t.Errorf("the repeat = %v, %v; want the close repeated, without the text and the words", again, err)
	}
	// A new submission on the closed attempt is refused before any call.
	reads, _ := es.calls()
	_, err = w.submitEssay(t, l, attempt, nil)
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED); r.GetCloseReason() != v1.CloseReason_CLOSE_REASON_LEFT {
		t.Errorf("the refusal = %v, want closed left", r)
	}
	if now, _ := es.calls(); now != reads {
		t.Errorf("a submission to a closed attempt read %d essays, want none", now-reads)
	}
}

// A submission of an essay that cannot go is refused before any call: a task
// with a verdict, a crop that is no PNG, and an own key for a child.
func TestAnEssayThatCannotGoIsRefused(t *testing.T) {
	t.Parallel()

	es := &essays{}
	es.give(essayText(essay), assessment(kindWords, yes, yes))
	w := newEssayWorld(t, es)
	l := w.learner(t)
	pick := w.begin(t, l, l.adult, "pick").GetAttemptId()
	_, err := w.submitEssay(t, l, pick, nil)
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	story := w.begin(t, l, l.adult, "story").GetAttemptId()
	for name, crop := range map[string][]byte{"no crop": nil, "no PNG": {1, 2, 3}, "too large": pngOf(t, 4097, 2)} {
		_, err := w.attempts.SubmitEssay(l.ctx, &v1.SubmitEssayRequest{ProfileId: l.adult, AttemptId: story,
			IdempotencyKey: uuid.NewString(), Crop: crop})
		t.Run(name, func(t *testing.T) { refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED) })
	}
	_, err = w.attempts.SubmitEssay(l.ctx, &v1.SubmitEssayRequest{ProfileId: l.child, AttemptId: story,
		IdempotencyKey: uuid.NewString(), Crop: pngOf(t, 4, 4), OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "sk-own"}})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE)
	_, err = w.attempts.SubmitEssay(l.ctx, &v1.SubmitEssayRequest{ProfileId: l.adult, AttemptId: uuid.NewString(),
		IdempotencyKey: uuid.NewString(), Crop: pngOf(t, 4, 4)})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	if reads, _ := es.calls(); reads != 0 {
		t.Errorf("%d essays read of those refused, want none", reads)
	}
}

// The reading of an essay reserves with its assessment, by the values of
// internal/quota when tutor has no price (R12, R82): an essay read and assessed
// settles both by what they cost, one that cannot be read gives the
// assessment's part back, and a day whose quota does not hold both calls
// nothing and ends unchecked with "quota".
func TestAnEssayReservesItsReadingWithItsAssessment(t *testing.T) {
	t.Parallel()

	es := &essays{}
	w := newEssayWorld(t, es, learning.WithPrice(func(tutor.Call, tutor.Route) tutor.CallPrice { return tutor.CallPrice{} }))
	l := w.learner(t)
	attempt := w.begin(t, l, l.adult, "story").GetAttemptId()
	es.give(essayText(essay), assessment(kindWords, yes, yes))
	if _, err := w.submitEssay(t, l, attempt, nil); err != nil {
		t.Fatal(err)
	}
	if profile, account := w.counted(t, l.accountID, l.adult); profile != (quota.Counter{Spent: 10_000}) || account != profile {
		t.Errorf("an essay read and assessed counted %+v and %+v, want 10,000 spent and nothing reserved", profile, account)
	}
	es.give(essayUnread(observationv1.Unreadable_UNREADABLE_BLANK), assessment(kindWords, yes, yes))
	if _, err := w.submitEssay(t, l, attempt, nil); err != nil {
		t.Fatal(err)
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile != (quota.Counter{Spent: 14_000}) {
		t.Errorf("an essay that cannot be read counted %+v, want the reading's 4,000 more and the assessment's reserve back", profile)
	}
	// An own key spends no quota.
	es.give(essayText(essay), assessment(kindWords, yes, yes))
	if _, err := w.submitEssay(t, l, attempt, &v1.OwnKey{Provider: "anthropic", Key: "sk-own"}); err != nil {
		t.Fatal(err)
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile != (quota.Counter{Spent: 14_000}) {
		t.Errorf("an essay on an own key counted %+v, want nothing more", profile)
	}
	// What is left of the account's day holds the reading but not both calls.
	w.spend(t, l.accountID, quota.Micro(1_500_000-20_000))
	reads, _ := es.calls()
	res, err := w.submitEssay(t, l, attempt, nil)
	if err != nil || res.GetEnd() != v1.SubmissionEnd_SUBMISSION_END_UNCHECKED || res.GetOutcome().GetCause() != v1.Cause_CAUSE_QUOTA ||
		res.GetOutcome().GetNextFamilyDay() == nil {
		t.Errorf("an essay the quota does not hold = %v, %v; want unchecked with the quota", res, err)
	}
	if now, _ := es.calls(); now != reads {
		t.Errorf("an essay the quota does not hold read %d essays, want none", now-reads)
	}
	// A new task with a rubric begins only with room for a typical one.
	_, err = w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString(),
		Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: w.choice(t, l.adult, "story")}})
	refusedAs(t, err, connect.CodeResourceExhausted, v1.Cause_CAUSE_QUOTA)
}

// An essay whose reading left too little of the request's time for its
// assessment is "unchecked", a failure of AI, with no assessment and the
// assessment's reserve given back: a call cut short would spend it.
func TestAnEssayReadTooLateIsNotAssessed(t *testing.T) {
	t.Parallel()

	es := &essays{}
	w := newEssayWorld(t, es)
	l := w.learner(t)
	attempt := w.begin(t, l, l.adult, "story").GetAttemptId()
	es.give(essayText(essay), assessment(kindWords, yes, yes))
	es.meanwhile(func() { time.Sleep(time.Second) })
	ctx, cancel := context.WithTimeout(l.ctx, 6*time.Second+500*time.Millisecond)
	defer cancel()
	res, err := w.attempts.SubmitEssay(ctx, &v1.SubmitEssayRequest{ProfileId: l.adult, AttemptId: attempt,
		IdempotencyKey: uuid.NewString(), Crop: pngOf(t, 40, 30)})
	if err != nil || res.GetEnd() != v1.SubmissionEnd_SUBMISSION_END_UNCHECKED || res.GetOutcome().GetCause() != v1.Cause_CAUSE_AI_FAILURE ||
		res.GetText() != essay {
		t.Fatalf("an essay read too late = %v, %v; want unchecked by a failure of AI, with its text", res, err)
	}
	if _, assessments := es.calls(); assessments != 0 {
		t.Errorf("an essay read too late was assessed %d times, want none", assessments)
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile != (quota.Counter{Spent: 4_000}) {
		t.Errorf("an essay read too late counted %+v, want the reading's 4,000 and the assessment's reserve back", profile)
	}
}

// A task with a rubric begins where both its calls are admitted, and not where
// only one is.
func TestATaskWithARubricBeginsWhereBothItsCallsAreAdmitted(t *testing.T) {
	t.Parallel()

	for name, admits := range map[string]func(tutor.Call) bool{
		"the reading alone":    func(c tutor.Call) bool { return c != tutor.CallRubric },
		"the assessment alone": func(c tutor.Call) bool { return c != tutor.CallEssay },
	} {
		w := worldOf(t, gate(t, adultFacts, config.RungAuthor), []learning.Option{learning.WithAdmits(admits)})
		l := w.learner(t)
		_, err := w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString(),
			Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: w.choice(t, l.adult, "story")}})
		t.Run(name, func(t *testing.T) { refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED) })
	}
}

// The progress gives each submission of an essay, how it ended, and the marks
// of its feedback once the attempt is closed: an adult reads them then (R15).
func TestTheProgressGivesTheMarksOfAClosedEssay(t *testing.T) {
	t.Parallel()

	es := &essays{}
	es.give(essayText(essay), assessment(kindWords, yes, no))
	w := newEssayWorld(t, es)
	l := w.learner(t)
	attempt := w.begin(t, l, l.adult, "story").GetAttemptId()
	if _, err := w.submitEssay(t, l, attempt, nil); err != nil {
		t.Fatal(err)
	}
	marksOf := func() (int, []v1.CriterionMark) {
		t.Helper()
		res, err := w.progress.GetProgress(l.ctx, &v1.GetProgressRequest{ProfileId: l.adult})
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range res.GetAttempts() {
			if a.GetTask() == module+"/task/story" {
				if len(a.GetEssaySubmissions()) == 0 {
					return 0, nil
				}
				return len(a.GetEssaySubmissions()), a.GetEssaySubmissions()[0].GetCriterionMarks()
			}
		}
		return 0, nil
	}
	if n, marks := marksOf(); n != 1 || len(marks) != 0 {
		t.Errorf("the open essay's progress = %d submissions, marks %v; want one without marks", n, marks)
	}
	w.begin(t, l, l.adult, "pick")
	want := []v1.CriterionMark{v1.CriterionMark_CRITERION_MARK_MET, v1.CriterionMark_CRITERION_MARK_NOT_MET}
	if n, marks := marksOf(); n != 1 || !slices.Equal(marks, want) {
		t.Errorf("the closed essay's progress = %d submissions, marks %v; want one with %v", n, marks, want)
	}
}
