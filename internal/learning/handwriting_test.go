package learning_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
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
	"github.com/altnovatrail/api/internal/idem"
	"github.com/altnovatrail/api/internal/learning"
)

// crop is the PNG of a crop of a sheet: small, grey and synthetic, with bytes
// no other value of a test holds.
func crop(t *testing.T) []byte {
	t.Helper()
	return pngOf(t, 13, 7)
}

// pngOf is a grey PNG of width by height, its pixels in a pattern of their own.
func pngOf(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, width, height))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 37)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// sheet is the work of a handwriting sheet with its crop.
func sheet(crop []byte) *v1.Work {
	return &v1.Work{Surface: &v1.Work_Sheet{Sheet: &v1.SheetWork{Crop: crop}}}
}

// read is the observation of a reader of apples' one slot: value read, or
// unreadable for a reason.
func read(value string) *observationv1.Observation {
	return &observationv1.Observation{AnswerSlots: []*observationv1.AnswerSlotObservation{{
		AnswerSlot: &observationv1.AnswerSlotPlace{Item: 1, Slot: 1},
		Read:       &observationv1.AnswerSlotObservation_Value{Value: value},
	}}}
}

// answered is a reading of the two readers, with the usage of each.
func answered(first, second *observationv1.Observation) tutor.SheetRead {
	return tutor.SheetRead{Observations: []*observationv1.Observation{first, second}, Outcome: tutor.Outcome{Kind: tutor.Answered,
		Usage: []tutor.Usage{
			{Provider: "anthropic", Model: "claude-opus-5-5", InputTokens: 1200, OutputTokens: 40, ThinkingTokens: 300},
			{Provider: "anthropic", Model: "claude-haiku-5-5", InputTokens: 1200, OutputTokens: 35},
		}}}
}

// readers are the readers of a test: what they give next, what they were given,
// and how many times they read.
type readers struct {
	mu    sync.Mutex
	next  func(ctx context.Context, r tutor.SheetReading) tutor.SheetRead
	given []tutor.SheetReading
}

func (rs *readers) read(ctx context.Context, r tutor.SheetReading) tutor.SheetRead {
	rs.mu.Lock()
	rs.given = append(rs.given, r)
	next := rs.next
	rs.mu.Unlock()
	return next(ctx, r)
}

// give makes the readers give got, whatever they read.
func (rs *readers) give(got tutor.SheetRead) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.next = func(context.Context, tutor.SheetReading) tutor.SheetRead { return got }
}

func (rs *readers) count() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return len(rs.given)
}

// newReadingWorld is the world of a deployment that admits the reading of an
// adult's handwriting, read by rs.
func newReadingWorld(t *testing.T, rs *readers) *world {
	t.Helper()
	return worldOf(t, gate(t, adultFacts, config.RungAuthor), []learning.Option{learning.WithReader(rs.read)})
}

// A handwriting sheet is read by two readers and its verdict given by
// module-kit on what they agree: the answer read is correct or not yet, two
// readings that differ are unreadable, and a call that gives no reading leaves
// the slot unchecked with its outcome. The submission keeps the joined
// observation; the crop is kept nowhere and written to no log.
func TestAHandwritingSheetIsReadByTwoReaders(t *testing.T) {
	t.Parallel()

	rs := &readers{}
	w := newReadingWorld(t, rs)
	l := w.learner(t)
	png := crop(t)
	limited := tutor.SheetRead{Outcome: tutor.Outcome{Kind: tutor.LimitReached}}
	for _, tc := range []struct {
		name    string
		got     tutor.SheetRead
		own     *v1.OwnKey
		verdict string
		wrong   int32
		cause   v1.Cause
	}{
		{"not yet", answered(read("6"), read("6")), nil, "not_yet[not_yet 6]", 1, v1.Cause_CAUSE_UNSPECIFIED},
		{"two readings that differ", answered(read("6"), read("7")), nil, "unreadable[unreadable ambiguous]", 0, v1.Cause_CAUSE_UNSPECIFIED},
		{"AI failed", tutor.SheetRead{Outcome: tutor.Outcome{Kind: tutor.Failed}}, nil, "unchecked[unchecked ]", 0, v1.Cause_CAUSE_AI_FAILURE},
		{"not admitted", tutor.SheetRead{Outcome: tutor.Outcome{Kind: tutor.NotAdmitted}}, nil, "unchecked[unchecked ]", 0, v1.Cause_CAUSE_NOT_ADMITTED},
		{"the platform's limit", limited, nil, "unchecked[unchecked ]", 0, v1.Cause_CAUSE_PLATFORM_LIMIT},
		{"the own key's limit", limited, &v1.OwnKey{Provider: "anthropic", Key: "sk-own"}, "unchecked[unchecked ]", 0, v1.Cause_CAUSE_LIMIT_REACHED},
		{"a key refused", tutor.SheetRead{Outcome: tutor.Outcome{Kind: tutor.KeyRefused}}, &v1.OwnKey{Provider: "anthropic", Key: "sk-own"},
			"unchecked[unchecked ]", 0, v1.Cause_CAUSE_KEY_REFUSED},
		{"one reader alone", tutor.SheetRead{Observations: []*observationv1.Observation{read("7")}, Outcome: tutor.Outcome{Kind: tutor.Answered}},
			nil, "unchecked[unchecked ]", 0, v1.Cause_CAUSE_AI_FAILURE},
		{"a reading of no slot", answered(&observationv1.Observation{}, read("7")), nil, "unchecked[unchecked ]", 0, v1.Cause_CAUSE_AI_FAILURE},
		{"correct", answered(read("7"), read("7")), nil, "correct[correct 7]", 0, v1.Cause_CAUSE_UNSPECIFIED},
	} {
		attempt := w.begin(t, l, l.adult, "apples").GetAttemptId()
		rs.give(tc.got)
		res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: attempt,
			IdempotencyKey: uuid.NewString(), Work: sheet(png), OwnKey: tc.own})
		t.Run(tc.name, func(t *testing.T) {
			if err != nil {
				t.Fatal(err)
			}
			if got := verdicts(res); got != tc.verdict || res.GetWrongAnswers() != tc.wrong || res.GetOutcome().GetCause() != tc.cause {
				t.Errorf("SubmitWork = %s, %d wrong, %v; want %s, %d, %v", got, res.GetWrongAnswers(), res.GetOutcome().GetCause(),
					tc.verdict, tc.wrong, tc.cause)
			}
		})
	}
	// The readers got the crop as it came, the open view of the task and the
	// route: an own key with its provider, the adult's category.
	last := rs.given[len(rs.given)-1]
	if !bytes.Equal(last.Crop, png) || last.Task.ID != "apples" || last.Route.Category != admissionv0.AgeCategoryAdult ||
		last.Route.AI != admissionv0.AiRoutePlatformAi || last.Route.Own != nil {
		t.Errorf("the readers were given %v, want the crop, apples and platform AI for an adult", last.Route)
	}
	own := rs.given[5]
	if own.Route.AI != admissionv0.AiRouteOwnKey || own.Route.Own.Provider() != "anthropic" {
		t.Errorf("the own key's reading went by %v, want the own key of anthropic", own.Route)
	}
	// The last attempt is solved and closed "correct"; its submission keeps the
	// value both read.
	var observed []byte
	if err := w.pool.QueryRow(t.Context(), `SELECT observation FROM submissions ORDER BY submitted_at DESC, number DESC LIMIT 1`).
		Scan(&observed); err != nil {
		t.Fatal(err)
	}
	o := &observationv1.Observation{}
	if err := proto.Unmarshal(observed, o); err != nil || len(o.GetAnswerSlots()) != 1 || o.GetAnswerSlots()[0].GetValue() != "7" {
		t.Errorf("the submission keeps %v (%v), want the joined value 7", o, err)
	}
	// Neither the crop nor what was read is in the database or the log.
	var leaked int
	if err := w.pool.QueryRow(t.Context(), `SELECT
			(SELECT count(*) FROM submissions WHERE position($1::bytea IN answer) > 0 OR position($1::bytea IN coalesce(observation, ''::bytea)) > 0)
			+ (SELECT count(*) FROM idempotency_keys WHERE position($1::bytea IN coalesce(response, ''::bytea)) > 0)`, png).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	log := w.log.String()
	if leaked != 0 || strings.Contains(log, base64.StdEncoding.EncodeToString(png)) || strings.Contains(log, "sk-own") {
		t.Errorf("the crop is in %d rows, or the crop or the own key is in the log", leaked)
	}
	if !strings.Contains(log, `"ai_outcome":"failed"`) {
		t.Errorf("the log has no line of a call that no model answered: %s", log)
	}
	if !strings.Contains(log, `"message":"ai_usage"`) || !strings.Contains(log, `"model":"claude-opus-5-5"`) ||
		!strings.Contains(log, `"outcome_cause":"CAUSE_AI_FAILURE"`) {
		t.Errorf("the log has no usage of the readers or no cause of an outcome: %s", log)
	}
}

// A repeat under the key gets the answer recorded without a reading; one while
// the reading goes is still going; and one after a request that fell, whose
// record going outlived the longest a request may run, goes again.
func TestASubmissionUnderItsKeyIsReadOnce(t *testing.T) {
	t.Parallel()

	rs := &readers{}
	w := newReadingWorld(t, rs)
	l := w.learner(t)
	attempt := w.begin(t, l, l.adult, "apples").GetAttemptId()
	req := &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: attempt, IdempotencyKey: uuid.NewString(), Work: sheet(crop(t))}

	reading, release := make(chan struct{}), make(chan struct{})
	rs.next = func(context.Context, tutor.SheetReading) tutor.SheetRead {
		close(reading)
		<-release
		return answered(read("6"), read("6"))
	}
	first := make(chan error, 1)
	go func() {
		_, err := w.attempts.SubmitWork(l.ctx, req)
		first <- err
	}()
	<-reading
	_, err := w.attempts.SubmitWork(l.ctx, req)
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_STILL_GOING)
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	again, err := w.attempts.SubmitWork(l.ctx, req)
	if err != nil || verdicts(again) != "not_yet[not_yet 6]" || rs.count() != 1 {
		t.Errorf("the repeat = %v, %v after %d readings; want the answer recorded after one", again, err, rs.count())
	}

	// A request that fell left its record going a minute ago.
	fell := &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: attempt, IdempotencyKey: uuid.NewString(), Work: sheet(crop(t))}
	w.exec(t, `INSERT INTO idempotency_keys (account_id, key, method, fingerprint, state, created_at)
		VALUES ($1, $2, '/novatrail.api.v1.AttemptService/SubmitWork', $3, 'going', now() - interval '1 minute')`,
		l.accountID, fell.GetIdempotencyKey(), idem.Fingerprint(fell, false))
	rs.give(answered(read("7"), read("7")))
	res, err := w.attempts.SubmitWork(l.ctx, fell)
	if err != nil || verdicts(res) != "correct[correct 7]" {
		t.Errorf("the repeat of a request that fell = %v, %v; want it read again", res, err)
	}
}

// An attempt that another device closed while AI read is "closed": nothing is
// written, and the key records nothing, so that a repeat is checked again.
func TestAnAttemptClosedWhileAIReadsIsClosed(t *testing.T) {
	t.Parallel()

	rs := &readers{}
	w := newReadingWorld(t, rs)
	l := w.learner(t)
	attempt := w.begin(t, l, l.adult, "apples").GetAttemptId()
	rs.next = func(context.Context, tutor.SheetReading) tutor.SheetRead {
		w.begin(t, l, l.adult, "pick")
		return answered(read("7"), read("7"))
	}
	req := &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: attempt, IdempotencyKey: uuid.NewString(), Work: sheet(crop(t))}
	_, err := w.attempts.SubmitWork(l.ctx, req)
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED); r.GetCloseReason() != v1.CloseReason_CLOSE_REASON_LEFT {
		t.Errorf("the refusal = %v, want closed left", r)
	}
	var submissions, records int
	if err := w.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM submissions WHERE attempt_id = $1),
			(SELECT count(*) FROM idempotency_keys WHERE key = $2)`, attempt, req.GetIdempotencyKey()).Scan(&submissions, &records); err != nil {
		t.Fatal(err)
	}
	if submissions != 0 || records != 0 {
		t.Errorf("a submission to a closed attempt wrote %d submissions and %d records, want none", submissions, records)
	}
}

// The reading ends before the request's deadline, with time left to write what
// came of it: a reading cut short is a failure of AI, not a request that fell.
func TestAReadingEndsBeforeTheRequestsDeadline(t *testing.T) {
	t.Parallel()

	rs := &readers{next: func(ctx context.Context, _ tutor.SheetReading) tutor.SheetRead {
		<-ctx.Done()
		return tutor.SheetRead{Outcome: tutor.Outcome{Kind: tutor.Failed, Err: ctx.Err()}}
	}}
	w := newReadingWorld(t, rs)
	l := w.learner(t)
	attempt := w.begin(t, l, l.adult, "apples").GetAttemptId()
	// The test's server cuts a request at 10 s; the client asks for 7, and the
	// reading ends 3 s before it.
	ctx, cancel := context.WithTimeout(l.ctx, 7*time.Second)
	defer cancel()
	start := time.Now()
	res, err := w.attempts.SubmitWork(ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: attempt,
		IdempotencyKey: uuid.NewString(), Work: sheet(crop(t))})
	if err != nil || res.GetOutcome().GetCause() != v1.Cause_CAUSE_AI_FAILURE || time.Since(start) > 6*time.Second {
		t.Errorf("a reading cut short = %v, %v after %v; want a failure of AI in time to write it", res, err, time.Since(start))
	}
}

// A submission with too little time left to read and write is refused before
// anything is called or recorded: a reading cut short would stay under its key
// as a failure of AI.
func TestASubmissionWithTooLittleTimeIsRefused(t *testing.T) {
	t.Parallel()

	rs := &readers{}
	rs.give(answered(read("7"), read("7")))
	w := newReadingWorld(t, rs)
	l := w.learner(t)
	attempt := w.begin(t, l, l.adult, "apples").GetAttemptId()
	ctx, cancel := context.WithTimeout(l.ctx, 5*time.Second)
	defer cancel()
	req := &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: attempt, IdempotencyKey: uuid.NewString(), Work: sheet(crop(t))}
	_, err := w.attempts.SubmitWork(ctx, req)
	if connect.CodeOf(err) != connect.CodeDeadlineExceeded || rs.count() != 0 {
		t.Errorf("a submission with 5 s left = %v after %d readings, want deadline_exceeded before any", err, rs.count())
	}
	var records int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM idempotency_keys WHERE key = $1`, req.GetIdempotencyKey()).
		Scan(&records); err != nil || records != 0 {
		t.Errorf("the key's records = %d (%v), want none", records, err)
	}
}
