package learning_test

import (
	"slices"
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/rpc"
)

// storyCriteria is the texts of the criteria of story's rubric, in order.
var storyCriteria = []string{"The story shares all the sweets.", "Every friend gets as many sweets as the others."}

// done is "Done" on the profile's attempt.
func (w *world) done(l learner, profile, attempt string) (*v1.DoneResponse, error) {
	return w.attempts.Done(l.ctx, &v1.DoneRequest{ProfileId: profile, AttemptId: attempt})
}

// choices is how many engine's choices the profile has.
func (w *world) choices(t *testing.T, profile string) int {
	t.Helper()
	var n int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM engine_choices WHERE profile_id = $1`, profile).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// "Done" ends an attempt with a rubric once it has a submission, however that
// ended (R14): before one it is "no submission"; after an empty sheet,
// "unreadable", the attempt is closed "done" with no evidence, and the answer
// is the task solved — the criteria of its rubric, without the marks, and no
// keys or solution, which story has none of (R87 spec) — and, outside a
// lesson, the engine's choice. A repeat gets the same answer and asks the
// engine nothing; the attempt takes no submission more.
func TestDoneEndsAnEssayOnceSubmitted(t *testing.T) {
	t.Parallel()

	es := &essays{}
	es.give(essayUnread(observationv1.Unreadable_UNREADABLE_BLANK), assessment(kindWords, yes, yes))
	w := newEssayWorld(t, es)
	l := w.learner(t)
	attempt := w.begin(t, l, l.adult, "story").GetAttemptId()
	_, err := w.done(l, l.adult, attempt)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_SUBMISSION)
	if res, err := w.submitEssay(t, l, attempt, nil); err != nil || res.GetEnd() != v1.SubmissionEnd_SUBMISSION_END_UNREADABLE {
		t.Fatalf("SubmitEssay = %v, %v; want unreadable", res, err)
	}
	res, err := w.done(l, l.adult, attempt)
	if err != nil {
		t.Fatal(err)
	}
	solved := res.GetSolved()
	if !slices.Equal(solved.GetCriteria(), storyCriteria) || len(solved.GetKeys()) != 0 || len(solved.GetSolution()) != 0 ||
		solved.GetModuleFormatVersion() != "0.4" {
		t.Errorf("the task solved = %v; want story's criteria alone, of format 0.4", solved)
	}
	if c := res.GetNext().GetChoice(); c.GetKind() == v1.ChoiceKind_CHOICE_KIND_UNSPECIFIED {
		t.Errorf("what comes next = %v, want the engine's choice", res.GetNext())
	}
	if got := w.closeOf(t, attempt); got != "done" {
		t.Errorf("the attempt is closed %q, want done", got)
	}
	var evidence int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM evidence WHERE attempt_id = $1`, attempt).Scan(&evidence); err != nil || evidence != 0 {
		t.Errorf("the attempt done has %d evidence (%v), want none", evidence, err)
	}

	chosen := w.choices(t, l.adult)
	again, err := w.done(l, l.adult, attempt)
	if err != nil || !proto.Equal(again, res) {
		t.Errorf("the repeat = %v, %v; want %v", again, err, res)
	}
	if now := w.choices(t, l.adult); now != chosen {
		t.Errorf("the repeat made %d choices, want none", now-chosen)
	}
	// A close that gave no choice — the engine could not make one — repeats with
	// none, as its first answer had none.
	w.exec(t, `DELETE FROM engine_choices WHERE after_attempt_id = $1`, attempt)
	if again, err := w.done(l, l.adult, attempt); err != nil || again.Next != nil || !slices.Equal(again.GetSolved().GetCriteria(), storyCriteria) {
		t.Errorf("the repeat of a close with no choice = %v, %v; want the task solved and nothing next", again, err)
	}
	_, err = w.submitEssay(t, l, attempt, nil)
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED); r.GetCloseReason() != v1.CloseReason_CLOSE_REASON_DONE {
		t.Errorf("a submission after Done = %v, want closed done", r)
	}
}

// The segment of a task with a rubric begins where both its calls are admitted
// and ends once its attempt is closed "done" (R13): before, the lesson's
// segments have not all ended; after, no segment is left, and the lesson is
// passed.
func TestASegmentWithARubricEndsByDone(t *testing.T) {
	t.Parallel()

	es := &essays{}
	es.give(essayText(essay), assessment(kindWords, yes, no))
	w := newEssayWorld(t, es)
	l := w.learner(t)
	run := w.open(t, l, l.adult, "essay").GetLessonRunId()
	a, err := w.segment(l, l.adult, run, 2)
	if err != nil {
		t.Fatalf("the segment of story = %v, want it begun", err)
	}
	if _, err := w.submitEssay(t, l, a.GetAttemptId(), nil); err != nil {
		t.Fatal(err)
	}
	_, err = w.complete(l, l.adult, run)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_SEGMENTS_NOT_ENDED)
	res, err := w.done(l, l.adult, a.GetAttemptId())
	if err != nil || !res.GetNext().GetSegmentsEnded() || !slices.Equal(res.GetSolved().GetCriteria(), storyCriteria) {
		t.Fatalf("Done on the segment = %v, %v; want story solved and no segment left", res, err)
	}
	if got, err := w.run(l, l.adult, run); err != nil || !slices.Equal(got.GetEndedSegments(), []int32{2}) {
		t.Errorf("the run = %v, %v; want segment 2 ended", got, err)
	}
	if passed, err := w.complete(l, l.adult, run); err != nil || passed.GetEnd().GetReason() != v1.RunEndReason_RUN_END_REASON_PASSED {
		t.Errorf("lesson done = %v, %v; want passed", passed, err)
	}
}

// The engine gets a task with a rubric and its lesson where both its calls are
// admitted, and AI that reads the work is admitted by the two together: the
// pool of a learner for whom only an essay's calls are admitted is not limited
// to the tasks a tap checks, and one for whom only the reading is, is (R50,
// R82).
func TestTheEngineGetsATaskWithARubricByBothItsCalls(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		calls   []tutor.Call
		takes   bool
		limited bool
	}{
		"an essay's two calls": {[]tutor.Call{tutor.CallEssay, tutor.CallRubric}, true, false},
		"the reading alone":    {[]tutor.Call{tutor.CallEssay}, false, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			w := worldOf(t, gate(t, adultFacts, config.RungAuthor), []learning.Option{
				learning.WithAdmits(func(call tutor.Call) bool { return slices.Contains(tc.calls, call) })})
			pool, lessons, err := w.service.Candidates(w.current, formatv0.AgeCategoryAdult, "")
			// Of the adult's lessons, basics and grown a tap checks whole, and essay holds story.
			if err != nil || slices.Contains(pool, module+"/task/story") != tc.takes || (lessons == 3) != tc.takes {
				t.Errorf("the pool = %v and %d lessons, %v; want story and its lesson in them %v", pool, lessons, err, tc.takes)
			}
			l := w.learner(t)
			c, err := w.chooseNext(l, l.adult)
			if err != nil {
				t.Fatal(err)
			}
			id := c.GetChoiceId()
			if id == "" {
				id = w.id(t, `SELECT id FROM engine_choices WHERE profile_id = $1 ORDER BY chosen_at DESC LIMIT 1`, l.adult)
			}
			if limited := w.choiceRow(t, id).poolLimited; limited == nil || *limited != tc.limited {
				t.Errorf("the pool is limited %v, want %v", limited, tc.limited)
			}
		})
	}
}

// "Done" again on an attempt closed "done" on a package the index no longer
// knows is "package unknown", and one on a database that fails is unavailable.
func TestDoneThatCannotAnswerIsRefused(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	gone := w.attempt(t, l.adult, unknown, "story")
	w.exec(t, `UPDATE attempts SET close_reason = 'done', closed_at = now() WHERE id = $1`, gone)
	_, err := w.done(l, l.adult, gone)
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_PACKAGE_UNKNOWN)

	for _, failOn := range []string{"FROM submissions WHERE profile_id = $1 AND attempt_id = $2)", "SET close_reason"} {
		t.Run(failOn, func(t *testing.T) {
			t.Parallel()

			w := worldOf(t, gate(t, adultFacts, config.RungAuthor), []learning.Option{
				learning.WithAdmits(func(tutor.Call) bool { return true })},
				func(p *pgxpool.Pool) rpc.DB { return &failing{Pool: p, failOn: failOn} })
			l := w.learner(t)
			story := w.attempt(t, l.adult, string(w.current.Digest), "story")
			w.exec(t, `INSERT INTO submissions (profile_id, attempt_id, number, submitted_at, answer, essay_end)
				VALUES ($1, $2, 1, now(), '', 'unreadable')`, l.adult, story)
			_, err := w.done(l, l.adult, story)
			refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
		})
	}
}
