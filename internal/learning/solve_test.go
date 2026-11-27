package learning_test

import (
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/google/uuid"
)

// solveIn is Solve on the module's card: the engine's choice among its tasks
// alone, under a new key.
func (w *world) solveIn(l learner, profile, moduleID string) (*v1.EngineChoice, error) {
	res, err := w.learning.ChooseNext(l.ctx, &v1.ChooseNextRequest{ProfileId: profile, IdempotencyKey: uuid.NewString(), ModuleId: moduleID})
	return res.GetChoice(), err
}

// scopeOf is the module a choice was among, empty for all of them.
func (w *world) scopeOf(t *testing.T, choice string) string {
	t.Helper()
	var scope *string
	if err := w.pool.QueryRow(t.Context(), `SELECT scope_module FROM engine_choices WHERE id = $1`, choice).Scan(&scope); err != nil {
		t.Fatal(err)
	}
	if scope == nil {
		return ""
	}
	return *scope
}

// shown closes the attempt with its solution shown and gives what comes next.
func (w *world) shown(t *testing.T, l learner, profile, attempt string) *v1.Next {
	t.Helper()
	res, err := w.attempts.ShowSolution(l.ctx, &v1.ShowSolutionRequest{ProfileId: profile, AttemptId: attempt,
		IdempotencyKey: uuid.NewString(), Action: v1.Action_ACTION_LEARNING})
	if err != nil {
		t.Fatal(err)
	}
	return res.GetNext()
}

// Solve gives a task of the module alone, never a lesson, not even the lesson
// of an open run, which a choice among all the modules gives; on the module's
// task skill as it is now, which what comes next after the task reads again;
// and the tutor's choice when the graph no longer has the skill.
func TestSolveGivesTheTasksOfTheModuleAlone(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	w.open(t, l, l.adult, "basics")
	if c, err := w.chooseNext(l, l.adult); err != nil || c.GetKind() != v1.ChoiceKind_CHOICE_KIND_LESSON {
		t.Fatalf("a choice among all the modules = %v, %v; want the lesson of the open run", c, err)
	}
	c, err := w.solveIn(l, l.adult, module)
	if err != nil || c.GetKind() != v1.ChoiceKind_CHOICE_KIND_TASK {
		t.Fatalf("Solve = %v, %v; want a task", c, err)
	}
	if row := w.choiceRow(t, c.GetChoiceId()); row.mapSkill != nil || *row.reason == 6 || w.scopeOf(t, c.GetChoiceId()) != module {
		t.Errorf("the row of Solve = %+v, want the tutor's choice among the module's tasks", row)
	}

	comparing, adding := module+"/skill/comparing", module+"/skill/adding"
	w.exec(t, `UPDATE connected_modules SET task_skill = $2 WHERE profile_id = $1`, l.adult, comparing)
	c, err = w.solveIn(l, l.adult, module)
	if err != nil || c.GetKind() != v1.ChoiceKind_CHOICE_KIND_TASK {
		t.Fatalf("Solve on the task skill = %v, %v; want a task", c, err)
	}
	if row := w.choiceRow(t, c.GetChoiceId()); row.mapSkill == nil || *row.mapSkill != comparing || *row.reason != 6 {
		t.Errorf("the row of Solve on the task skill = %+v, want SKILL_CHOSEN on it", row)
	}
	attempt := w.started(t, l, l.adult, c)
	w.exec(t, `UPDATE connected_modules SET task_skill = $2 WHERE profile_id = $1`, l.adult, adding)
	next := w.shown(t, l, l.adult, attempt).GetChoice()
	if row := w.choiceRow(t, next.GetChoiceId()); next.GetKind() != v1.ChoiceKind_CHOICE_KIND_TASK || row.mapSkill == nil ||
		*row.mapSkill != adding || row.afterAttempt == nil || *row.afterAttempt != attempt || w.scopeOf(t, next.GetChoiceId()) != module {
		t.Errorf("what came next = %v %+v, want a task of the module on its skill now, after the attempt", next, row)
	}

	w.exec(t, `UPDATE connected_modules SET task_skill = $2 WHERE profile_id = $1`, l.adult, module+"/skill/gone")
	c, err = w.solveIn(l, l.adult, module)
	if err != nil {
		t.Fatal(err)
	}
	if row := w.choiceRow(t, c.GetChoiceId()); row.mapSkill != nil {
		t.Errorf("Solve on a skill gone = %+v, want the tutor's choice", row)
	}
	again := w.started(t, l, l.adult, c)
	if row := w.choiceRow(t, w.shown(t, l, l.adult, again).GetChoice().GetChoiceId()); row.mapSkill != nil {
		t.Errorf("what came next on a skill gone = %+v, want the tutor's choice", row)
	}
}

// Solve refuses a module and a skill both, a module solved on the sheet, one
// not connected and one with no current version, and closes nothing; once its
// module is gone, the end of a task of it gives nothing, with no outcome.
func TestSolveRefusesAModuleItCannotChooseIn(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	c, err := w.solveIn(l, l.adult, module)
	if err != nil {
		t.Fatal(err)
	}
	attempt := w.started(t, l, l.adult, c)
	_, err = w.learning.ChooseNext(l.ctx, &v1.ChooseNextRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString(),
		ModuleId: module, Skill: module + "/skill/comparing"})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	_, err = w.solveIn(l, l.adult, "example/none")
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	if got := w.closeOf(t, attempt); got != "" {
		t.Errorf("a refused Solve closed the open attempt %q", got)
	}

	w.exec(t, `DELETE FROM connected_modules WHERE profile_id = $1`, l.adult)
	if next := w.shown(t, l, l.adult, attempt).GetChoice(); next.GetKind() != v1.ChoiceKind_CHOICE_KIND_NOTHING || next.GetOutcome() != nil {
		t.Errorf("what came next once the module is gone = %v, want nothing, with no outcome", next)
	}

	if err := w.set.List([]formatv0.ModuleID{}); err != nil {
		t.Fatal(err)
	}
	_, err = w.solveIn(l, l.child, module)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION)
	w.set.Sheet(module)
	_, err = w.solveIn(l, l.child, module)
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
}
