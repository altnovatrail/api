package learning_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/google/uuid"
)

// lessons is the lessons of the module for the profile.
func (w *world) lessons(t *testing.T, l learner, profile string) *v1.ListLessonsResponse {
	t.Helper()
	res, err := w.learning.ListLessons(l.ctx, &v1.ListLessonsRequest{ProfileId: profile, ModuleId: module})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// listed is the lessons as "ID state group [ended]", in order.
func listed(res *v1.ListLessonsResponse) string {
	var out []string
	for _, m := range res.GetLessons() {
		item := strings.TrimPrefix(m.GetLesson(), module+"/lesson/") + " " +
			strings.TrimPrefix(m.GetState().String(), "LESSON_STATE_") + " " + string(rune('0'+m.GetGroup()))
		if len(m.GetEndedSegments()) > 0 {
			var places []string
			for _, p := range m.GetEndedSegments() {
				places = append(places, string(rune('0'+p)))
			}
			item += " [" + strings.Join(places, ",") + "]"
		}
		out = append(out, item)
	}
	return strings.Join(out, "; ")
}

// rows is how many choices and runs the profile has, and when its runs were
// last acted on.
func (w *world) rows(t *testing.T, profile string) string {
	t.Helper()
	var out string
	if err := w.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM engine_choices WHERE profile_id = $1)::text || ' ' ||
			(SELECT count(*) FROM lesson_runs WHERE profile_id = $1)::text || ' ' ||
			coalesce((SELECT max(last_action_at)::text FROM lesson_runs WHERE profile_id = $1), '')`, profile).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The lessons of a module are those that suit the learner, in the author's
// order, each with its state — new, open with the task segments ended, passed —
// and the lesson of an open run that does not suit the learner after them, in a
// group of its own; the tile shows the open run, else the first not passed,
// else the first. The look writes nothing.
func TestTheLessonsOfAModuleAreListed(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	res := w.lessons(t, l, l.child)
	if got := listed(res); got != "basics NEW 0; essay NEW 0" || res.GetTileLesson() != module+"/lesson/basics" ||
		res.GetLessons()[0].GetTitle() != "Choosing and adding" || res.GetLessons()[0].GetSegments() != 5 {
		t.Errorf("the lessons = %s, tile %s, %v", got, res.GetTileLesson(), res.GetLessons())
	}
	if got := listed(w.lessons(t, l, l.adult)); got != "basics NEW 0; essay NEW 0; grown NEW 0" {
		t.Errorf("the adult's lessons = %s, want grown too", got)
	}

	run := w.open(t, l, l.child, "essay").GetLessonRunId()
	basics := w.open(t, l, l.child, "basics").GetLessonRunId()
	a, err := w.segment(l, l.child, basics, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.submit(l, l.child, a.GetAttemptId(), tap(1, 1, "three and four")); err != nil {
		t.Fatal(err)
	}
	before := w.rows(t, l.child)
	res = w.lessons(t, l, l.child)
	if got := listed(res); got != "basics OPEN 0 [2]; essay OPEN 0" || res.GetTileLesson() != module+"/lesson/basics" {
		t.Errorf("with two open runs = %s, tile %s; want both open, the tile on the one begun last", got, res.GetTileLesson())
	}
	if after := w.rows(t, l.child); after != before {
		t.Errorf("the look wrote: %s, then %s", before, after)
	}

	w.exec(t, `UPDATE lesson_runs SET end_reason = 'passed', ended_at = now() WHERE id = ANY($1)`, []string{run, basics})
	// A lesson that does not suit the learner, open on its package, stands last.
	w.exec(t, `INSERT INTO lesson_runs (profile_id, lesson_id, module_id, module_version, package_digest, choice_id)
		VALUES ($1, $2, $3, '1.0.0', $4, $5)`, l.child, module+"/lesson/grown", module, string(w.current.Digest),
		w.lessonChoice(t, l.child, "grown"))
	// And one on a package the image does not give is not listed.
	w.exec(t, `INSERT INTO lesson_runs (profile_id, lesson_id, module_id, module_version, package_digest, choice_id)
		VALUES ($1, $2, $3, '0.9.0', $4, $5)`, l.child, module+"/lesson/withdrawn", module, string(w.earlier.Digest),
		w.lessonChoice(t, l.child, "withdrawn"))
	res = w.lessons(t, l, l.child)
	if got := listed(res); got != "basics PASSED 0; essay PASSED 0; grown OPEN 1" || res.GetTileLesson() != module+"/lesson/grown" {
		t.Errorf("passed = %s, tile %s", got, res.GetTileLesson())
	}
	w.exec(t, `UPDATE lesson_runs SET end_reason = 'abandoned', ended_at = now() WHERE lesson_id = $1`, module+"/lesson/grown")
	if res := w.lessons(t, l, l.child); listed(res) != "basics PASSED 0; essay PASSED 0" || res.GetTileLesson() != module+"/lesson/basics" {
		t.Errorf("every lesson passed = %s, tile %s; want the first", listed(res), res.GetTileLesson())
	}
}

// The lessons of a module not connected are "not found", of one with no current
// version refused so, and of one solved on the sheet malformed.
func TestTheLessonsAreRefusedForAModuleWithoutThem(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	_, err := w.learning.ListLessons(l.ctx, &v1.ListLessonsRequest{ProfileId: l.child, ModuleId: "example/none"})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	if err := w.set.List([]formatv0.ModuleID{}); err != nil {
		t.Fatal(err)
	}
	_, err = w.learning.ListLessons(l.ctx, &v1.ListLessonsRequest{ProfileId: l.child, ModuleId: module})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION)
	w.set.Sheet(module)
	_, err = w.learning.ListLessons(l.ctx, &v1.ListLessonsRequest{ProfileId: l.child, ModuleId: module})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
}

// openChosen opens the lesson the learner chose, by its local ID.
func (w *world) openChosen(l learner, profile, lesson string) (*v1.LessonRun, error) {
	res, err := w.learning.OpenLesson(l.ctx, &v1.OpenLessonRequest{ProfileId: profile, Lesson: module + "/lesson/" + lesson})
	return res.GetRun(), err
}

// A lesson the learner chose opens a new run with a choice of its own, "lesson
// chosen", in its module, which holds what an engine's choice would; opened
// again, it gives the open run; passed, a new run. The end of its run gives the
// tasks of its module. A request that names a choice and a lesson, or neither,
// is malformed; a lesson the current version does not have, or that does not
// suit the learner, is refused.
func TestALessonTheLearnerChoseOpens(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	run, err := w.openChosen(l, l.child, "basics")
	if err != nil {
		t.Fatal(err)
	}
	var choice string
	if err := w.pool.QueryRow(t.Context(), `SELECT choice_id::text FROM lesson_runs WHERE id = $1`, run.GetLessonRunId()).Scan(&choice); err != nil {
		t.Fatal(err)
	}
	row := w.choiceRow(t, choice)
	if row.kind != "lesson" || row.reason == nil || *row.reason != 10 || w.scopeOf(t, choice) != module || row.mapSkill != nil ||
		*row.packageDigest != string(w.current.Digest) || !slices.Equal(row.issued, []string{module + "/skill/comparing", module + "/skill/adding"}) ||
		len(row.engineDigests) != 1 || row.ageCategory != "under_13" || row.engineVersion == "" || row.lastSeq == nil || !*row.poolLimited {
		t.Errorf("the row of the lesson chosen = %+v", row)
	}
	if again, err := w.openChosen(l, l.child, "basics"); err != nil || again.GetLessonRunId() != run.GetLessonRunId() {
		t.Errorf("opened again = %v, %v; want the open run", again, err)
	}
	w.exec(t, `UPDATE lesson_runs SET end_reason = 'passed', ended_at = now() WHERE id = $1`, run.GetLessonRunId())
	if next, err := w.openChosen(l, l.child, "basics"); err != nil || next.GetLessonRunId() == run.GetLessonRunId() {
		t.Errorf("a lesson passed = %v, %v; want a new run", next, err)
	}

	_, err = w.learning.OpenLesson(l.ctx, &v1.OpenLessonRequest{ProfileId: l.child})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	_, err = w.learning.OpenLesson(l.ctx, &v1.OpenLessonRequest{ProfileId: l.child, ChoiceId: uuid.NewString(), Lesson: module + "/lesson/basics"})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	for _, lesson := range []string{"basics", "/lesson/", module + "/lesson/"} {
		_, err = w.learning.OpenLesson(l.ctx, &v1.OpenLessonRequest{ProfileId: l.child, Lesson: lesson})
		refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	}
	_, err = w.learning.OpenLesson(l.ctx, &v1.OpenLessonRequest{ProfileId: l.child, Lesson: "example/none/lesson/basics"})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	_, err = w.openChosen(l, l.child, "none")
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION)
	_, err = w.openChosen(l, l.child, "grown")
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_SUITABLE)
	w.set.Sheet(module)
	_, err = w.openChosen(l, l.child, "basics")
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
}

// The end of a run of a lesson gives a task of its module, never a lesson, on
// the module's task skill; an open run that its check ends, opened by the
// lesson's ID, gives way to a new run.
func TestTheEndOfALessonGivesTheTasksOfItsModule(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	comparing := module + "/skill/comparing"
	w.exec(t, `UPDATE connected_modules SET task_skill = $2 WHERE profile_id = $1`, l.adult, comparing)
	run, err := w.openChosen(l, l.adult, "grown")
	if err != nil {
		t.Fatal(err)
	}
	a, err := w.segment(l, l.adult, run.GetLessonRunId(), 1)
	if err != nil {
		t.Fatal(err)
	}
	w.shown(t, l, l.adult, a.GetAttemptId())
	done, err := w.complete(l, l.adult, run.GetLessonRunId())
	if err != nil {
		t.Fatal(err)
	}
	c := done.GetEnd().GetChoice()
	if row := w.choiceRow(t, c.GetChoiceId()); c.GetKind() != v1.ChoiceKind_CHOICE_KIND_TASK || row.mapSkill == nil ||
		*row.mapSkill != comparing || w.scopeOf(t, c.GetChoiceId()) != module || row.afterRun == nil || *row.afterRun != run.GetLessonRunId() {
		t.Errorf("the end of the lesson = %v %+v, want a task of its module on its skill", c, row)
	}

	open, err := w.openChosen(l, l.adult, "basics")
	if err != nil {
		t.Fatal(err)
	}
	w.clock.move(31 * 24 * time.Hour)
	again, err := w.openChosen(l, l.adult, "basics")
	if err != nil || again.GetLessonRunId() == open.GetLessonRunId() {
		t.Errorf("a run abandoned = %v, %v; want a new run", again, err)
	}
	if got := w.endOf(t, open.GetLessonRunId()); got != "abandoned" {
		t.Errorf("the run left = %q, want abandoned", got)
	}
}
