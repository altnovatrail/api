package learning_test

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/rpc"
)

// open opens the lesson of a new engine's choice on the profile.
func (w *world) open(t *testing.T, l learner, profile, lesson string) *v1.LessonRun {
	t.Helper()
	res, err := w.learning.OpenLesson(l.ctx, &v1.OpenLessonRequest{ProfileId: profile, ChoiceId: w.lessonChoice(t, profile, lesson)})
	if err != nil {
		t.Fatalf("OpenLesson %s: %v", lesson, err)
	}
	return res.GetRun()
}

// segment begins an attempt on the segment at place of the run.
func (w *world) segment(l learner, profile, run string, place int32) (*v1.AttemptStart, error) {
	res, err := w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: profile, IdempotencyKey: uuid.NewString(),
		Start: &v1.BeginAttemptRequest_Segment{Segment: &v1.SegmentPlace{LessonRunId: run, Segment: place}}})
	return res.GetAttempt(), err
}

// run is the open run again, by its ID.
func (w *world) run(l learner, profile, run string) (*v1.LessonRun, error) {
	res, err := w.learning.GetLessonRun(l.ctx, &v1.GetLessonRunRequest{ProfileId: profile, LessonRunId: run})
	return res.GetRun(), err
}

// complete is "lesson done" on the run.
func (w *world) complete(l learner, profile, run string) (*v1.CompleteLessonResponse, error) {
	return w.learning.CompleteLesson(l.ctx, &v1.CompleteLessonRequest{ProfileId: profile, LessonRunId: run})
}

// endOf is how the run ended, empty while it is open.
func (w *world) endOf(t *testing.T, run string) string {
	t.Helper()
	var reason *string
	if err := w.pool.QueryRow(t.Context(), `SELECT end_reason FROM lesson_runs WHERE id = $1`, run).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason == nil {
		return ""
	}
	return *reason
}

// endedAs fails the test unless err is "run ended" by reason.
func endedAs(t *testing.T, err error, reason v1.RunEndReason) {
	t.Helper()
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_RUN_ENDED); r.GetRunEnd().GetReason() != reason ||
		r.GetRunEnd().GetChoice() != nil {
		t.Errorf("the run's end = %v, want %v with no choice", r.GetRunEnd(), reason)
	}
}

// A choice of a lesson opens one run on the package of the current version,
// with the open view of the lesson whole: opened again — by the same choice or
// another, at once or later, or by its ID — it is the same run, and the list of
// profiles names it. A new run that cannot begin is refused.
func TestAChoiceOfALessonOpensOneRun(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	choice := w.lessonChoice(t, l.child, "basics")
	res, err := w.learning.OpenLesson(l.ctx, &v1.OpenLessonRequest{ProfileId: l.child, ChoiceId: choice})
	if err != nil {
		t.Fatal(err)
	}
	run := res.GetRun()
	if run.GetLessonRunId() == "" || run.GetPackageDigest() != string(w.current.Digest) || len(run.GetEndedSegments()) != 0 ||
		run.GetOpenAttempt() != nil || run.GetLesson().GetModuleFormatVersion() != "0.4" {
		t.Errorf("the run = %v", run)
	}
	if view := string(run.GetLesson().GetJson()); !strings.Contains(view, "Choosing and adding") || !strings.Contains(view, `"task":"pairs"`) ||
		strings.Contains(view, "comparing") {
		t.Errorf("the open view = %s, want the lesson whole without its skills", view)
	}
	again, err := w.learning.OpenLesson(l.ctx, &v1.OpenLessonRequest{ProfileId: l.child, ChoiceId: choice})
	if err != nil || again.GetRun().GetLessonRunId() != run.GetLessonRunId() {
		t.Errorf("opened again = %v (%v), want the same run", again.GetRun(), err)
	}
	if other := w.open(t, l, l.child, "basics"); other.GetLessonRunId() != run.GetLessonRunId() {
		t.Errorf("opened by another choice = %v, want the same run", other)
	}
	if byID, err := w.run(l, l.child, run.GetLessonRunId()); err != nil || byID.GetLessonRunId() != run.GetLessonRunId() {
		t.Errorf("GetLessonRun = %v (%v)", byID, err)
	}
	account, err := w.accounts.GetAccount(l.ctx, &v1.GetAccountRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range account.GetAccount().GetProfiles() {
		runs := p.GetOpenLessonRuns()
		switch {
		case p.GetProfileId() == l.child && (len(runs) != 1 || runs[0].GetLessonRunId() != run.GetLessonRunId() ||
			runs[0].GetLesson() != "example/choices/lesson/basics" || runs[0].GetPackageDigest() != string(w.current.Digest)):
			t.Errorf("the child's open runs = %v", runs)
		case p.GetProfileId() != l.child && len(runs) != 0:
			t.Errorf("the open runs of %s = %v, want none", p.GetProfileId(), runs)
		}
	}

	// Two openings at once give the one run.
	var wg sync.WaitGroup
	ids := make([]string, 2)
	for i := range ids {
		wg.Go(func() {
			res, err := w.learning.OpenLesson(l.ctx, &v1.OpenLessonRequest{ProfileId: l.adult, ChoiceId: w.lessonChoice(t, l.adult, "grown")})
			if err != nil {
				t.Errorf("an opening at once: %v", err)
			}
			ids[i] = res.GetRun().GetLessonRunId()
		})
	}
	wg.Wait()
	if ids[0] == "" || ids[0] != ids[1] {
		t.Errorf("two openings at once = %v, want one run", ids)
	}

	teen := w.child(t, l, "from_13_to_consent", true)
	w.exec(t, `INSERT INTO connected_modules (profile_id, module_id) VALUES ($1, $2)`, teen, module)
	other := w.id(t, `INSERT INTO engine_choices (profile_id, kind, module_id, lesson_id) VALUES ($1, 'lesson', 'example/other', 'basics')
		RETURNING id`, l.child)
	for name, tc := range map[string]struct {
		profile, choice string
		code            connect.Code
		cause           v1.Cause
	}{
		"a choice of a task":         {l.child, w.choice(t, l.child, "pick"), connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED},
		"a choice not there":         {l.child, uuid.NewString(), connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND},
		"a choice that is no ID":     {l.child, "x", connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND},
		"a module not connected":     {l.child, other, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND},
		"a lesson for adults":        {l.child, w.lessonChoice(t, l.child, "grown"), connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_SUITABLE},
		"no such lesson":             {l.child, w.lessonChoice(t, l.child, "gone"), connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION},
		"no version for the learner": {teen, w.lessonChoice(t, teen, "basics"), connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION},
	} {
		_, err := w.learning.OpenLesson(l.ctx, &v1.OpenLessonRequest{ProfileId: tc.profile, ChoiceId: tc.choice})
		t.Run(name, func(t *testing.T) { refusedAs(t, err, tc.code, tc.cause) })
	}
	_, err = w.run(l, l.child, uuid.NewString())
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	_, err = w.run(l, l.adult, run.GetLessonRunId())
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	_, err = w.complete(l, l.child, "x")
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
}

// The task segments of a lesson begin in order, on the run's package: each
// once those before it have ended, and its open attempt as it is. An attempt
// closed by a correct answer or the solution shown ends its segment, and its
// answer names the next segment, or that none is left; one left does not.
// "Lesson done" ends the run "passed" once every task segment has ended, and
// says so again; the choice of an ended run gets "run ended", and a new choice
// a new run.
func TestTheSegmentsOfALessonGoInOrder(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	choice := w.lessonChoice(t, l.child, "basics")
	res, err := w.learning.OpenLesson(l.ctx, &v1.OpenLessonRequest{ProfileId: l.child, ChoiceId: choice})
	if err != nil {
		t.Fatal(err)
	}
	run := res.GetRun().GetLessonRunId()
	unavailable := func(err error, ended ...int32) {
		t.Helper()
		if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_SEGMENT_UNAVAILABLE); !slices.Equal(r.GetEndedSegments().GetPlaces(), ended) {
			t.Errorf("the ended segments = %v, want %v", r.GetEndedSegments().GetPlaces(), ended)
		}
	}
	_, err = w.segment(l, l.child, run, 4)
	unavailable(err)
	pick, err := w.segment(l, l.child, run, 2)
	if err != nil {
		t.Fatal(err)
	}
	if pick.GetSegment().GetLessonRunId() != run || pick.GetSegment().GetSegment() != 2 || pick.GetPackageDigest() != string(w.current.Digest) {
		t.Errorf("the segment's attempt = %v", pick)
	}
	if same, err := w.segment(l, l.child, run, 2); err != nil || same.GetAttemptId() != pick.GetAttemptId() {
		t.Errorf("the segment begun again = %v (%v), want its open attempt", same, err)
	}
	open, err := w.run(l, l.child, run)
	if err != nil || open.GetOpenAttempt().GetAttemptId() != pick.GetAttemptId() || open.GetOpenAttempt().GetSegment().GetSegment() != 2 {
		t.Errorf("the run = %v (%v), want the segment's open attempt", open, err)
	}
	w.wrongs(t, l, l.child, pick.GetAttemptId(), []*v1.TappedChoice{tap(1, 1, "five")})
	right := &v1.SubmitWorkRequest{ProfileId: l.child, AttemptId: pick.GetAttemptId(), IdempotencyKey: uuid.NewString(),
		TappedChoices: []*v1.TappedChoice{tap(1, 1, "three and four")}}
	solved, err := w.attempts.SubmitWork(l.ctx, right)
	if err != nil || solved.GetSolved() == nil || solved.GetNext().GetSegment() != 3 {
		t.Fatalf("the correct answer = %v (%v), want the next segment 3", solved, err)
	}
	if again, err := w.attempts.SubmitWork(l.ctx, right); err != nil || again.GetNext().GetSegment() != 3 {
		t.Errorf("a repeat = %v (%v), want the next segment again", again, err)
	}
	if open, err := w.run(l, l.child, run); err != nil || !slices.Equal(open.GetEndedSegments(), []int32{2}) || open.GetOpenAttempt() != nil {
		t.Errorf("the run = %v (%v), want segment 2 ended", open, err)
	}
	_, err = w.segment(l, l.child, run, 2)
	unavailable(err, 2)
	_, err = w.complete(l, l.child, run)
	if r := refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_SEGMENTS_NOT_ENDED); !slices.Equal(r.GetEndedSegments().GetPlaces(), []int32{2}) {
		t.Errorf("the refusal = %v", r)
	}

	// An attempt left does not end its segment: the learner begins it again.
	pairs, err := w.segment(l, l.child, run, 4)
	if err != nil {
		t.Fatal(err)
	}
	w.begin(t, l, l.child, "greater")
	if got := w.closeOf(t, pairs.GetAttemptId()); got != "left" {
		t.Errorf("the segment's attempt is closed %q, want left", got)
	}
	pairs, err = w.segment(l, l.child, run, 4)
	if err != nil || pairs.GetAttemptId() == "" {
		t.Fatalf("the segment begun again = %v (%v)", pairs, err)
	}
	solved, err = w.submit(l, l.child, pairs.GetAttemptId(), tap(1, 1, "21"), tap(2, 1, "30"))
	if err != nil || solved.GetNext().GetSegment() != 5 {
		t.Fatalf("the correct answer = %v (%v), want the next segment 5", solved, err)
	}
	done, err := w.complete(l, l.child, run)
	if err != nil || done.GetEnd().GetReason() != v1.RunEndReason_RUN_END_REASON_PASSED {
		t.Fatalf("lesson done = %v (%v)", done, err)
	}
	if again, err := w.complete(l, l.child, run); err != nil || again.GetEnd().GetReason() != v1.RunEndReason_RUN_END_REASON_PASSED {
		t.Errorf("lesson done again = %v (%v), want the same", again, err)
	}
	_, err = w.run(l, l.child, run)
	endedAs(t, err, v1.RunEndReason_RUN_END_REASON_PASSED)
	_, err = w.learning.OpenLesson(l.ctx, &v1.OpenLessonRequest{ProfileId: l.child, ChoiceId: choice})
	endedAs(t, err, v1.RunEndReason_RUN_END_REASON_PASSED)
	_, err = w.segment(l, l.child, run, 4)
	endedAs(t, err, v1.RunEndReason_RUN_END_REASON_PASSED)
	if next := w.open(t, l, l.child, "basics"); next.GetLessonRunId() == run {
		t.Errorf("a new choice = %v, want a new run", next)
	}

	// A last segment that is a task: no segment is left after it, and the solution shown ends it too.
	grown := w.open(t, l, l.adult, "grown").GetLessonRunId()
	a, err := w.segment(l, l.adult, grown, 1)
	if err != nil {
		t.Fatal(err)
	}
	shown, err := w.attempts.ShowSolution(l.ctx, &v1.ShowSolutionRequest{ProfileId: l.adult, AttemptId: a.GetAttemptId(),
		IdempotencyKey: uuid.NewString(), Action: v1.Action_ACTION_LEARNING})
	if err != nil || !shown.GetNext().GetSegmentsEnded() {
		t.Errorf("the solution shown = %v (%v), want no segment left", shown, err)
	}
	if done, err := w.complete(l, l.adult, grown); err != nil || done.GetEnd().GetReason() != v1.RunEndReason_RUN_END_REASON_PASSED {
		t.Errorf("lesson done = %v (%v)", done, err)
	}

	// A segment this code cannot begin is "not admitted", and the run stays open.
	essay := w.open(t, l, l.adult, "essay").GetLessonRunId()
	_, err = w.segment(l, l.adult, essay, 2)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED)
	if got := w.endOf(t, essay); got != "" {
		t.Errorf("the run ended %q, want open", got)
	}
}

// api ends a run by itself, at a request on it, and the request gets "run
// ended" with how: "unchecked" on a package the image no longer gives, "not
// suitable" once the module is disconnected — with its open attempt closed so
// —, and "abandoned" after 30 days with no request on the run or its attempts,
// whose actions keep it open.
func TestARunEndsByItself(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	withdrawn := w.id(t, `INSERT INTO lesson_runs (profile_id, lesson_id, module_id, module_version, package_digest, choice_id)
		VALUES ($1, 'example/choices/lesson/basics', $2, '0.9.0', $3, $4) RETURNING id`,
		l.child, module, string(w.earlier.Digest), w.lessonChoice(t, l.child, "basics"))
	_, err := w.run(l, l.child, withdrawn)
	endedAs(t, err, v1.RunEndReason_RUN_END_REASON_UNCHECKED)
	if got := w.endOf(t, withdrawn); got != "unchecked" {
		t.Errorf("the run ended %q, want unchecked", got)
	}
	unknown := w.id(t, `INSERT INTO lesson_runs (profile_id, lesson_id, module_id, module_version, package_digest, choice_id)
		VALUES ($1, 'example/choices/lesson/basics', $2, '0.8.0', $3, $4) RETURNING id`,
		l.child, module, "sha256:"+strings.Repeat("0", 64), w.lessonChoice(t, l.child, "basics"))
	_, err = w.complete(l, l.child, unknown)
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_PACKAGE_UNKNOWN)
	if got := w.endOf(t, unknown); got != "" {
		t.Errorf("the run on an unknown package ended %q, want nothing written", got)
	}
	w.exec(t, `UPDATE lesson_runs SET end_reason = 'abandoned', ended_at = now() WHERE id = $1`, unknown)

	run := w.open(t, l, l.child, "basics").GetLessonRunId()
	a, err := w.segment(l, l.child, run, 2)
	if err != nil {
		t.Fatal(err)
	}
	w.exec(t, `DELETE FROM connected_modules WHERE profile_id = $1`, l.child)
	_, err = w.segment(l, l.child, run, 2)
	endedAs(t, err, v1.RunEndReason_RUN_END_REASON_NOT_SUITABLE)
	if got := w.closeOf(t, a.GetAttemptId()); got != "not_suitable" {
		t.Errorf("the run's attempt is closed %q, want not_suitable", got)
	}
	w.exec(t, `INSERT INTO connected_modules (profile_id, module_id) VALUES ($1, $2)`, l.child, module)

	// The actions on its attempts keep a run open; with none for 30 days it is abandoned.
	run = w.open(t, l, l.child, "basics").GetLessonRunId()
	w.clock.move(20 * 24 * time.Hour)
	b, err := w.segment(l, l.child, run, 2)
	if err != nil {
		t.Fatal(err)
	}
	w.exec(t, `UPDATE lesson_runs SET last_action_at = last_action_at - interval '20 days' WHERE id = $1`, run)
	w.clock.move(20 * 24 * time.Hour)
	if _, err := w.attempts.GetHint(l.ctx, &v1.GetHintRequest{ProfileId: l.child, AttemptId: b.GetAttemptId()}); err == nil {
		t.Fatal("the hint on an attempt idle for 20 days was given")
	}
	if _, err := w.run(l, l.child, run); err != nil {
		t.Errorf("the run whose attempt acted 20 days ago = %v, want open", err)
	}
	w.clock.move(31 * 24 * time.Hour)
	_, err = w.complete(l, l.child, run)
	endedAs(t, err, v1.RunEndReason_RUN_END_REASON_ABANDONED)
}

// A database that fails is unavailable, at each step of a lesson.
func TestALessonOnADatabaseThatFailsIsUnavailable(t *testing.T) {
	t.Parallel()

	for _, failOn := range []string{
		"FROM lesson_runs WHERE", "INSERT INTO lesson_runs", "SET end_reason", "SET last_action_at = $3",
		"SELECT DISTINCT segment", "lesson_run_id = $2 AND closed_at IS NULL", "SELECT lesson_id FROM lesson_runs",
		"FROM engine_choices", "FOR UPDATE OF r",
	} {
		t.Run(failOn, func(t *testing.T) {
			t.Parallel()

			w := newWorld(t, func(p *pgxpool.Pool) rpc.DB { return &failing{Pool: p, failOn: failOn} })
			l := w.learner(t)
			var errs []error
			note := func(_ any, err error) { errs = append(errs, err) }
			res, err := w.learning.OpenLesson(l.ctx, &v1.OpenLessonRequest{ProfileId: l.adult, ChoiceId: w.lessonChoice(t, l.adult, "grown")})
			errs = append(errs, err)
			run := res.GetRun().GetLessonRunId()
			if run == "" {
				run = w.id(t, `INSERT INTO lesson_runs (profile_id, lesson_id, module_id, module_version, package_digest, choice_id)
					VALUES ($1, 'example/choices/lesson/grown', $2, '1.0.0', $3, $4) RETURNING id`,
					l.adult, module, string(w.current.Digest), w.lessonChoice(t, l.adult, "grown"))
			}
			note(w.run(l, l.adult, run))
			a, err := w.segment(l, l.adult, run, 1)
			errs = append(errs, err)
			if a.GetAttemptId() != "" {
				note(w.submit(l, l.adult, a.GetAttemptId(), tap(1, 1, "a dozen")))
			}
			note(w.complete(l, l.adult, run))
			w.exec(t, `DELETE FROM connected_modules WHERE profile_id = $1`, l.adult)
			note(w.run(l, l.adult, run))
			unavailable := false
			for _, err := range errs {
				if connect.CodeOf(err) == connect.CodeUnavailable {
					unavailable = true
				}
			}
			if !unavailable {
				t.Errorf("no request was unavailable: %v", errs)
			}
		})
	}
}

// A segment whose open attempt lapsed begins a new attempt in the same request,
// and a new choice of a lesson whose open run another choice opened and the
// check ends opens a run of its own.
func TestALapseDoesNotStopALesson(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	run := w.open(t, l, l.child, "basics").GetLessonRunId()
	old, err := w.segment(l, l.child, run, 2)
	if err != nil {
		t.Fatal(err)
	}
	w.clock.move(8 * 24 * time.Hour)
	fresh, err := w.segment(l, l.child, run, 2)
	if err != nil || fresh.GetAttemptId() == "" || fresh.GetAttemptId() == old.GetAttemptId() {
		t.Fatalf("the segment after its attempt lapsed = %v (%v), want a new attempt", fresh, err)
	}
	if got := w.closeOf(t, old.GetAttemptId()); got != "abandoned" {
		t.Errorf("the lapsed attempt is closed %q, want abandoned", got)
	}
	w.clock.move(31 * 24 * time.Hour)
	next := w.open(t, l, l.child, "basics")
	if next.GetLessonRunId() == run {
		t.Errorf("a new choice after the run lapsed = %v, want a new run", next)
	}
	if got := w.endOf(t, run); got != "abandoned" {
		t.Errorf("the lapsed run ended %q, want abandoned", got)
	}
}
