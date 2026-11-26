package learning_test

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	paperv1 "github.com/altnovatrail/spec/contracts/novatrail/paper/v1"
	"github.com/altnovatrail/tutor"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/admission"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/tutorstub"
)

// counting is the stub of tutor that counts the readings of a statement it is
// asked for.
type counting struct {
	*scripted
	statements atomic.Int32
}

func (c *counting) ReadStatement(ctx context.Context, p tutor.PaperTask, own string) tutor.StatementRead {
	c.statements.Add(1)
	return c.scripted.ReadStatement(ctx, p, own)
}

// recognise asks for the statement of the attempt's crop of shape.
func (w *world) recognise(t *testing.T, l learner, attempt string, shape [2]int, key string) (*v1.ReadStatementResponse, error) {
	return w.attempts.ReadStatement(l.ctx, &v1.ReadStatementRequest{ProfileId: l.adult, AttemptId: attempt, IdempotencyKey: key,
		Statement: photoOf(t, shape[0], shape[1])})
}

// paperReading is the reading of the statement the attempt keeps, or nil when
// it keeps none.
func (w *world) paperReading(t *testing.T, attempt string) *paperv1.Reading {
	t.Helper()
	var kept []byte
	if err := w.pool.QueryRow(t.Context(), `SELECT paper_reading FROM attempts WHERE id = $1`, attempt).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept == nil {
		return nil
	}
	r := &paperv1.Reading{}
	if err := proto.Unmarshal(kept, r); err != nil {
		t.Fatal(err)
	}
	return r
}

// countingWorld is a world on the stub whose readings of a statement are
// counted.
func countingWorld(t *testing.T) (*world, *counting, *admission.Gate) {
	t.Helper()
	g := stubGate(t)
	c := &counting{scripted: &scripted{Stub: tutorstub.New(g)}}
	return worldOf(t, g, []learning.Option{learning.WithTutor(c)}), c, g
}

// "Recognise" reads the statement of the crop once: the answer is its
// statements, never a formula, and the attempt keeps the reading, so that a
// repeat and a later press answer with no AI. The Check takes nothing of it
// but its count of tasks, and reads no statement of its own; the attempt's
// close erases the reading with the Check.
func TestRecogniseKeepsTheReadingForTheCheck(t *testing.T) {
	t.Parallel()

	w, c, g := countingWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	key := uuid.NewString()
	res, err := w.recognise(t, l, a, formulaStatement, key)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.GetStatements(), []string{"37 + 48 ="}) || res.GetRepeated() || res.GetOutcome() != nil {
		t.Errorf("Recognise = %v, want the statement read, not repeated", res)
	}
	if r := w.paperReading(t, a); len(r.GetTasks()) != 1 || r.GetTasks()[0].GetFormula() != "37+48" || w.paperCheck(t, a) != nil {
		t.Errorf("the reading kept = %v, the Check %v; want the task with its formula, and no Check", r, w.paperCheck(t, a))
	}
	stating, checking := reserveOf(g, tutor.CallStatement), reserveOf(g, tutor.CallChecking)
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != stating || profile.Reserved != 0 {
		t.Errorf("the counter = %+v, want the statement's call spent", profile)
	}
	for name, k := range map[string]string{"the repeat": key, "a new press": uuid.NewString()} {
		again, err := w.recognise(t, l, a, formulaStatement, k)
		if err != nil || !proto.Equal(again, &v1.ReadStatementResponse{Statements: []string{"37 + 48 ="}, Repeated: true}) {
			t.Errorf("%s = %v (%v), want the statements kept, repeated", name, again, err)
		}
	}
	if n := c.statements.Load(); n != 1 {
		t.Errorf("the statement was read %d times, want once", n)
	}

	if s := slotOf(t, w.submitPaper(t, l, a, wrongWork)); s.GetVerdict() != "not_yet" || s.GetValue() != "851" {
		t.Errorf("the Check = %v, want not_yet", s)
	}
	if n := c.statements.Load(); n != 1 {
		t.Errorf("the statement was read %d times after the Check, want once", n)
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != stating+checking || profile.Reserved != 0 {
		t.Errorf("the counter = %+v, want the Check alone more, %d", profile, stating+checking)
	}
	again, err := w.recognise(t, l, a, formulaStatement, uuid.NewString())
	if err != nil || !proto.Equal(again, &v1.ReadStatementResponse{Statements: []string{"37 + 48 ="}, Repeated: true}) {
		t.Errorf("Recognise after the Check = %v (%v), want its statements, repeated", again, err)
	}

	w.submitPaper(t, l, a, rightWork)
	if w.closeOf(t, a) != "correct" || w.paperReading(t, a) != nil || w.paperCheck(t, a) != nil {
		t.Errorf("the attempt closed %q, its reading %v, its Check %v; want correct, and neither", w.closeOf(t, a), w.paperReading(t, a), w.paperCheck(t, a))
	}
	_, err = w.recognise(t, l, a, formulaStatement, uuid.NewString())
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED)
}

// A reading of several tasks keeps them all; "Recognise" after a Check with no
// reading kept reads the statement.
func TestRecogniseOfSeveralTasksKeepsThemAll(t *testing.T) {
	t.Parallel()

	w, c, _ := countingWorld(t)
	l := w.learner(t)
	page := w.beginPage(t, l, l.adult, 2)
	w.submitPaper(t, l, page[0], wrongWork)
	res, err := w.recognise(t, l, page[0], severalStatement, uuid.NewString())
	want := []string{"37 + 48 =", "40 + 45 =", "50 + 36 ="}
	if err != nil || !slices.Equal(res.GetStatements(), want) || res.GetRepeated() || c.statements.Load() != 1 {
		t.Errorf("Recognise after a Check = %v (%v), want its three statements read", res, err)
	}
	res, err = w.recognise(t, l, page[1], severalStatement, uuid.NewString())
	if err != nil || !slices.Equal(res.GetStatements(), want) || res.GetRepeated() {
		t.Errorf("Recognise of several = %v (%v), want three statements", res, err)
	}
	if r := w.paperReading(t, page[1]); len(r.GetTasks()) != 3 {
		t.Errorf("the reading kept = %v, want three tasks", r)
	}
}

// A reading that gives no statement is an outcome with nothing kept, its
// reserve settled, and the next press asks again; a repeat gets the outcome.
// A crop on which the reading found no task keeps a reading of none, and the
// Check from it is told one task, with no reading of the statement.
func TestRecogniseThatGivesNoReadingKeepsNothing(t *testing.T) {
	t.Parallel()

	w, c, _ := countingWorld(t)
	l := w.learner(t)
	page := w.beginPage(t, l, l.adult, 2)
	c.mu.Lock()
	c.statement = &tutor.StatementRead{Outcome: tutor.Outcome{Kind: tutor.Failed}}
	c.mu.Unlock()
	key := uuid.NewString()
	for _, k := range []string{key, key} {
		res, err := w.recognise(t, l, page[0], formulaStatement, k)
		if err != nil || res.GetOutcome().GetCause() != v1.Cause_CAUSE_AI_FAILURE || len(res.GetStatements()) != 0 {
			t.Errorf("a failed reading = %v (%v), want the failure of AI and no statement", res, err)
		}
	}
	if w.paperReading(t, page[0]) != nil || c.statements.Load() != 1 {
		t.Errorf("the reading kept = %v after %d calls, want none after one", w.paperReading(t, page[0]), c.statements.Load())
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Reserved != 0 {
		t.Errorf("the counter = %+v, want nothing left in reserve", profile)
	}
	own := &v1.OwnKey{Provider: "anthropic", Key: "sk-own" + tutorstub.KeyRefused}
	c.mu.Lock()
	c.statement = nil
	c.mu.Unlock()
	res, err := w.attempts.ReadStatement(l.ctx, &v1.ReadStatementRequest{ProfileId: l.adult, AttemptId: page[0], IdempotencyKey: uuid.NewString(),
		Statement: photoOf(t, formulaStatement[0], formulaStatement[1]), OwnKey: own})
	if err != nil || res.GetOutcome().GetCause() != v1.Cause_CAUSE_KEY_REFUSED || w.paperReading(t, page[0]) != nil {
		t.Errorf("a refused own key = %v (%v), want the key refused and no reading", res, err)
	}
	if res, err := w.recognise(t, l, page[0], formulaStatement, uuid.NewString()); err != nil || len(res.GetStatements()) != 1 {
		t.Errorf("the next press = %v (%v), want the statement read", res, err)
	}

	c.mu.Lock()
	c.statement = &tutor.StatementRead{Outcome: tutor.Outcome{Kind: tutor.Answered}}
	c.mu.Unlock()
	res, err = w.recognise(t, l, page[1], formulaStatement, uuid.NewString())
	if err != nil || len(res.GetStatements()) != 0 || res.GetOutcome() != nil {
		t.Errorf("a crop of no task = %v (%v), want no statement and no outcome", res, err)
	}
	if r := w.paperReading(t, page[1]); r == nil || len(r.GetTasks()) != 0 {
		t.Errorf("the reading kept = %v, want one of no task", r)
	}
	calls := c.statements.Load()
	check, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: page[1], IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, 90, 30))})
	if err != nil || check.GetSolved() == nil || len(check.GetItems()) != 1 || c.statements.Load() != calls {
		t.Errorf("the Check of a reading of no task = %v (%v), want one task solved with no reading of the statement", check, err)
	}
}

// What "Recognise" refuses before any call: an attempt of a module, a crop the
// contract does not take, a quota that does not hold the call, platform AI
// closed by its limit, and an own key of a child; nothing is spent.
func TestRecogniseRefusesBeforeAnyCall(t *testing.T) {
	t.Parallel()

	w, c, _ := countingWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	m := w.begin(t, l, l.adult, "apples").GetAttemptId()
	if _, err := w.recognise(t, l, m, formulaStatement, uuid.NewString()); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("a task of a module = %v, want invalid_argument", err)
	}
	if _, err := w.attempts.ReadStatement(l.ctx, &v1.ReadStatementRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Statement: []byte("no picture")}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("a crop that is no picture = %v, want invalid_argument", err)
	}
	day := w.spend(t, l.accountID, 3_000_000)
	_, err := w.recognise(t, l, a, formulaStatement, uuid.NewString())
	if r := refusedAs(t, err, connect.CodeResourceExhausted, v1.Cause_CAUSE_QUOTA); !r.GetNextFamilyDay().AsTime().Equal(day.End) {
		t.Errorf("the quota's refusal = %v, want the next day", r)
	}
	w.exec(t, `UPDATE account_quota SET spent_micro_usd = 0 WHERE account_id = $1`, l.accountID)
	w.exec(t, `UPDATE platform_ai SET closed_at = now()`)
	_, err = w.recognise(t, l, a, formulaStatement, uuid.NewString())
	refusedAs(t, err, connect.CodeResourceExhausted, v1.Cause_CAUSE_PLATFORM_LIMIT)
	_, err = w.attempts.ReadStatement(l.ctx, &v1.ReadStatementRequest{ProfileId: l.child, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Statement: photoOf(t, formulaStatement[0], formulaStatement[1]), OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "sk-own"}})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE)
	if c.statements.Load() != 0 || w.paperReading(t, a) != nil {
		t.Errorf("%d readings, the reading %v; want none", c.statements.Load(), w.paperReading(t, a))
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != 0 || profile.Reserved != 0 {
		t.Errorf("the counter = %+v, want nothing spent", profile)
	}
}
