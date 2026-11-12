package model

import (
	"context"
	"fmt"
	"maps"
	"slices"

	learnermodel "github.com/altnovatrail/learner-model"
	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/module-kit/template"
	"github.com/altnovatrail/module-kit/verdict"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/store"
)

// lastRung is the hint ladder's last rung: wrong answers past it give no rung
// of their own (R09).
const lastRung = 3

// outcomeOf is the outcome of an attempt closed with reason after wrong wrong
// answers (concepts 11 spec; architecture 3.5): correct at the first
// submission, or after the hint of the rung its wrong answers reached; the
// solution shown; left unfinished, when the learner left it, it was abandoned or
// its lesson run ended "not suitable"; or unchecked. A hint asked for and
// "explain differently" do not change it, and no hint of api is a sketch.
func outcomeOf(reason string, wrong int) learnermodel.Outcome {
	switch reason {
	case "correct":
		if wrong == 0 {
			return learnermodel.Outcome{Kind: learnermodel.OutcomeFirstSubmission}
		}
		return learnermodel.Outcome{Kind: learnermodel.OutcomeAfterHint, Rung: int32(min(wrong, lastRung)), HasRung: true}
	case "solution_shown":
		return learnermodel.Outcome{Kind: learnermodel.OutcomeSolutionShown}
	case "unchecked":
		return learnermodel.Outcome{Kind: learnermodel.OutcomeUnchecked}
	}
	return learnermodel.Outcome{Kind: learnermodel.OutcomeLeftUnfinished}
}

// evidence is the evidence of the attempt o, of a learner of grade, on its
// package pkg, and false for an attempt of a task with a rubric, which gives
// none (О-17): none begins in this code (R14), and the path of a task with a
// rubric takes one closed "unchecked" out of what is owed, which each settling
// would find again. What it holds is the facts of the attempt, as learner-model
// describes them (R06; architecture 3.5): the task, the start band by the
// grade, the outcome, the time of the last action, the techniques given at a
// step and on request, whether a hint asked for gave the leading question — a
// hint gives it only before the first wrong answer, and only when asked for
// (R47) —, how many times "explain differently" was asked for, whether the
// solution of the task — not of a template — reached the device before, what
// came of each item, and the traps the program found.
func evidence(ctx context.Context, pkg *modulepackage.Package, o store.Owed, grade learnermodel.Grade) (learnermodel.Evidence, bool, error) {
	task, ok := pkg.View.Tasks[formatv0.TaskID(o.TaskID)]
	if !ok {
		return learnermodel.Evidence{}, false, fmt.Errorf("model: the package has no task %s", o.TaskID)
	}
	if rubric(task) {
		return learnermodel.Evidence{}, false, nil
	}
	got, err := instanceOf(pkg, task, o.Instance)
	if err != nil {
		return learnermodel.Evidence{}, false, err
	}
	described, err := Describe(pkg, task)
	if err != nil {
		return learnermodel.Evidence{}, false, err
	}
	start, err := learnermodel.BandByGrade(grade, bandsOf(pkg))
	if err != nil {
		return learnermodel.Evidence{}, false, fmt.Errorf("model: the start band: %w", err)
	}
	answers := make([]*v1.SubmitWorkResponse, len(o.Answers))
	for i, written := range o.Answers {
		answers[i] = &v1.SubmitWorkResponse{}
		if err := proto.Unmarshal(written, answers[i]); err != nil {
			return learnermodel.Evidence{}, false, fmt.Errorf("model: read a submission: %w", err)
		}
	}
	traps, err := trapsOf(ctx, pkg, got, answers)
	if err != nil {
		return learnermodel.Evidence{}, false, err
	}
	return learnermodel.Evidence{
		Kind: learnermodel.EvidenceKind, Task: described, StartBand: start,
		Outcome: outcomeOf(o.CloseReason, o.WrongAnswers), PracticeTime: o.LastActionAt.Unix(),
		TechniqueOnStep: o.StepTechniques, TechniqueOnRequest: o.RequestedTechniques,
		LeadingQuestionOnRequest: o.LeadingQuestion,
		ExplainAgainCount:        int32(o.ExplainedAgain),
		SolutionSeenBefore:       o.SolutionSeenBefore && task.Template == nil, HasSolutionSeenFact: task.Template == nil,
		Items: itemsOf(len(got.Items), answers), Traps: traps,
	}, true, nil
}

// rubric says whether the task has an item with a rubric, which has no verdict.
func rubric(task formatv0.Task) bool {
	for _, item := range task.Items {
		for _, slot := range verdict.AnswerSlots(item) {
			if slot.Check.Kind == formatv0.CheckKindRubric {
				return true
			}
		}
	}
	return false
}

// instanceOf is the task the attempt got: the task itself, or a template's
// instance by its number.
func instanceOf(pkg *modulepackage.Package, task formatv0.Task, picked *int) (formatv0.Task, error) {
	if picked == nil {
		return task, nil
	}
	numbers := pkg.View.Instances[task.ID]
	if *picked < 0 || *picked >= len(numbers) {
		return formatv0.Task{}, fmt.Errorf("model: the template %s has no instance %d", task.ID, *picked)
	}
	return template.Instance(task, numbers[*picked])
}

// itemsOf is what came of each of the n items of the attempt in its submissions,
// in order: each submitted with each submission; the reason of each submission
// before the first verdict that gave none; the first verdict, and the wrong
// answers of the attempt before the submission that gave it; the verdict at the
// end; and the wrong answers before the first "correct". A submission's wrong
// answers count it too, so those before it are the submission's before.
func itemsOf(n int, answers []*v1.SubmitWorkResponse) []learnermodel.ItemAnswer {
	items := make([]learnermodel.ItemAnswer, n)
	for i := range items {
		items[i] = learnermodel.ItemAnswer{Place: int32(i + 1), Submitted: len(answers) > 0}
	}
	before := int32(0)
	for _, answer := range answers {
		for i, item := range answer.GetItems() {
			if i >= n {
				break
			}
			it := &items[i]
			switch v := verdict.Verdict(item.GetVerdict()); v {
			case verdict.Correct, verdict.NotYet:
				given := learnermodel.VerdictNotYet
				if v == verdict.Correct {
					given = learnermodel.VerdictCorrect
				}
				if !it.HasFirstVerdict {
					it.FirstVerdict, it.HasFirstVerdict, it.WrongBeforeFirst = given, true, before
				}
				it.FinalVerdict, it.HasFinalVerdict = given, true
				if v == verdict.Correct && !it.HasFirstCorrect {
					it.WrongBeforeFirstCorrect, it.HasFirstCorrect = before, true
				}
			default:
				if !it.HasFirstVerdict {
					it.Unverdicted = append(it.Unverdicted, reasonOf(item))
				}
			}
		}
		before = answer.GetWrongAnswers()
	}
	return items
}

// reasonOf is why an item gave no verdict: blank, when every answer slot that
// could not be read is blank; unreadable otherwise; or unchecked, when its
// check could not run (concepts 9.1 spec).
func reasonOf(item *v1.ItemVerdict) learnermodel.UnverdictedReason {
	if verdict.Verdict(item.GetVerdict()) != verdict.Unreadable {
		return learnermodel.UnverdictedUnchecked
	}
	for _, s := range item.GetAnswerSlots() {
		if verdict.Verdict(s.GetVerdict()) == verdict.Unreadable && verdict.Reason(s.GetReason()) != verdict.Blank {
			return learnermodel.UnverdictedUnreadableOther
		}
	}
	return learnermodel.UnverdictedUnreadableBlank
}

// trapsOf is the traps the program found in the attempt by its wrong values —
// the trap value each value "not yet" matches, found again by module-kit's
// verdict of the instance's answer slot — with their skills in the trap catalog
// of the attempt's version and the places of the items they were found in, by
// ID. A trap that AI suggested is not among them (R82 spec).
func trapsOf(ctx context.Context, pkg *modulepackage.Package, task formatv0.Task, answers []*v1.SubmitWorkResponse) ([]learnermodel.FoundTrap, error) {
	found := map[formatv0.TrapID]map[int32]bool{}
	for _, answer := range answers {
		for _, item := range answer.GetItems() {
			for _, s := range item.GetAnswerSlots() {
				if verdict.Verdict(s.GetVerdict()) != verdict.NotYet {
					continue
				}
				i, j := int(s.GetAnswerSlot().GetItem())-1, int(s.GetAnswerSlot().GetSlot())-1
				if i < 0 || i >= len(task.Items) || j < 0 || j >= len(verdict.AnswerSlots(task.Items[i])) {
					continue
				}
				v, err := verdict.OfSlot(ctx, verdict.AnswerSlots(task.Items[i])[j], s.Value)
				if err != nil {
					return nil, fmt.Errorf("model: the verdict of a slot again: %w", err)
				}
				if v.Trap == nil {
					continue
				}
				if found[v.Trap.Trap] == nil {
					found[v.Trap.Trap] = map[int32]bool{}
				}
				found[v.Trap.Trap][int32(i+1)] = true
			}
		}
	}
	module := pkg.View.Manifest.ID
	var traps []learnermodel.FoundTrap
	for _, id := range slices.Sorted(maps.Keys(found)) {
		t := learnermodel.FoundTrap{ID: trapID(module, id), ItemPlaces: slices.Sorted(maps.Keys(found[id]))}
		if catalog := pkg.View.Traps; catalog != nil {
			t.Skills = skillIDs(module, catalog.Traps[id].Skills)
		}
		traps = append(traps, t)
	}
	return traps, nil
}
