package learning

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/module-kit/openview"
	"github.com/altnovatrail/module-kit/template"
	"github.com/altnovatrail/module-kit/verdict"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
)

// newSeed is the seed of an attempt, from a cryptographically strong source: it
// never reaches the device, and a template's instance comes of it (R59 spec).
func newSeed() uint64 {
	var b [8]byte
	// crypto/rand.Read never fails, as its documentation says from Go 1.24; go.mod
	// requires a later Go.
	_, _ = rand.Read(b[:])
	return binary.BigEndian.Uint64(b[:])
}

// instance is the task an attempt of seed gets in pkg: the task itself, or the
// instance its seed picks when the task is a template, and that instance's
// number. The numbers are the package's, and no Starlark runs (R60 spec).
func instance(pkg *modulepackage.Package, task formatv0.Task, seed uint64) (formatv0.Task, *int, error) {
	if task.Template == nil {
		return task, nil, nil
	}
	numbers := pkg.View.Instances[task.ID]
	picked, err := template.AttemptInstance(seed, task.ID, len(numbers))
	if err != nil {
		return formatv0.Task{}, nil, err
	}
	of, err := template.Instance(task, numbers[picked])
	if err != nil {
		return formatv0.Task{}, nil, err
	}
	return of, &picked, nil
}

// instanceAgain is the task an attempt got, by the number of its instance.
func instanceAgain(pkg *modulepackage.Package, id formatv0.TaskID, picked *int) (formatv0.Task, error) {
	task, ok := pkg.View.Tasks[id]
	if !ok {
		return formatv0.Task{}, fmt.Errorf("learning: the package has no task %s", id)
	}
	if picked == nil {
		return task, nil
	}
	numbers := pkg.View.Instances[id]
	if *picked < 0 || *picked >= len(numbers) {
		return formatv0.Task{}, fmt.Errorf("learning: the template %s has no instance %d", id, *picked)
	}
	return template.Instance(task, numbers[*picked])
}

// openTask is the open view of the task, an instance's when it is one, as JSON
// of the format beside its version: no check, key, solution, trap, technique or
// leading question is in it (R05 spec).
func openTask(pkg *modulepackage.Package, task formatv0.Task) (*v1.OpenView, error) {
	view, err := openview.Task(task)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	return &v1.OpenView{Json: data, ModuleFormatVersion: string(pkg.View.Manifest.ModuleFormatVersion)}, nil
}

// suits says whether the task suits a learner of category: by its own age
// categories, or by the module's audience when it names none (R51 spec).
func suits(pkg *modulepackage.Package, task formatv0.Task, category formatv0.AgeCategory) bool {
	categories := task.AgeCategories
	if len(categories) == 0 {
		categories = pkg.View.Manifest.Audience.AgeCategories
	}
	return slices.Contains(categories, category)
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

// byTap says whether a tap checks the whole task: every answer slot of every
// item is bound choice, so that no AI reads it.
func byTap(task formatv0.Task) bool {
	for _, item := range task.Items {
		for _, slot := range verdict.AnswerSlots(item) {
			if slot.Binding != string(formatv0.AnswerSlotBindingChoice) {
				return false
			}
		}
	}
	return true
}

// techniques is the IDs of the task's techniques, local IDs of the module's
// catalog, as a step or a hint gives them.
func techniques(task formatv0.Task) []string {
	ids := make([]string, len(task.Techniques))
	for i, id := range task.Techniques {
		ids[i] = string(id)
	}
	return ids
}

// solved is what reaches the device once the task is solved: the key of each
// answer slot that has one, by its place, the solution as JSON of the format,
// beside the format's version, and on a task with a rubric the texts of its
// criteria in order, without the marks of any assessment (R14; R87 spec).
func solved(pkg *modulepackage.Package, task formatv0.Task) (*v1.SolvedTask, error) {
	out := &v1.SolvedTask{ModuleFormatVersion: string(pkg.View.Manifest.ModuleFormatVersion)}
	if rubric(task) {
		out.Criteria = criteriaOf(task)
	}
	for i, item := range task.Items {
		for j, slot := range verdict.AnswerSlots(item) {
			if slot.Check.Key != nil {
				out.Keys = append(out.Keys, &v1.AnswerSlotKey{AnswerSlot: placeOf(i, j), Key: *slot.Check.Key})
			}
		}
	}
	if len(task.Solution) > 0 {
		data, err := json.Marshal(task.Solution)
		if err != nil {
			return nil, err
		}
		out.Solution = data
	}
	return out, nil
}

// placeOf is the place of the j-th answer slot of the i-th item, each from 0,
// as the contract counts them, from 1.
func placeOf(i, j int) *observationv1.AnswerSlotPlace {
	return &observationv1.AnswerSlotPlace{Item: int32(i + 1), Slot: int32(j + 1)}
}

// itemVerdicts is the verdicts in the contract's words, each answer slot with
// what was read in it: the text of the option tapped, or the value AI read and
// the readers agreed on, in joined; the trap a value shows stays in api. wrong
// says whether a slot got "not_yet", which makes the submission a wrong answer
// (R09), and correct whether every item is correct.
func itemVerdicts(verdicts []verdict.ItemVerdict, tapped map[string]string, joined *observationv1.Observation) (items []*v1.ItemVerdict, wrong, correct bool) {
	read := maps.Clone(tapped)
	if read == nil {
		read = map[string]string{}
	}
	for _, o := range joined.GetAnswerSlots() {
		if v, ok := o.GetRead().(*observationv1.AnswerSlotObservation_Value); ok {
			read[placeKey(o.GetAnswerSlot())] = v.Value
		}
	}
	correct = true
	for i, item := range verdicts {
		out := &v1.ItemVerdict{Verdict: string(item.Verdict)}
		for j, slot := range item.Slots {
			place := placeOf(i, j)
			s := &v1.AnswerSlotVerdict{AnswerSlot: place, Verdict: string(slot.Verdict), Reason: string(slot.Reason)}
			if value, ok := read[placeKey(place)]; ok {
				s.Value = &value
			}
			out.AnswerSlots = append(out.AnswerSlots, s)
			wrong = wrong || slot.Verdict == verdict.NotYet
		}
		correct = correct && item.Verdict == verdict.Correct
		items = append(items, out)
	}
	return items, wrong, correct
}

// placeKey is a place of an answer slot as a key of a map.
func placeKey(p *observationv1.AnswerSlotPlace) string {
	return fmt.Sprintf("%d.%d", p.GetItem(), p.GetSlot())
}

// callOf is tutor's kind of call that reads the work of the task, by its canvas
// (R81): the reading of a sheet, CallSheet, for a handwriting sheet tutor reads
// (sheetRead), and of a workbook page, CallWorkbook, for a workbook (bookRead).
// A task of neither has none: one a tap checks, and one with a zone or the
// whole sheet, which tutor does not read.
func callOf(task formatv0.Task) (tutor.Call, bool) {
	switch {
	case sheetRead(task):
		return tutor.CallSheet, true
	case bookRead(task):
		return tutor.CallWorkbook, true
	}
	return "", false
}

// bookRead says whether tutor reads the task as a workbook page: a workbook
// with a box at least, every answer slot of which is a box, as the format has
// it (tutor's ReadWorkbook). The place of an answer slot is then the place of
// its box among its item's.
func bookRead(task formatv0.Task) bool {
	if task.Canvas.Kind != formatv0.CanvasKindWorkbook {
		return false
	}
	boxes := 0
	for _, item := range task.Items {
		for _, slot := range verdict.AnswerSlots(item) {
			if slot.Binding != string(formatv0.LinePartBindingBox) {
				return false
			}
			boxes++
		}
	}
	return boxes > 0
}

// sheetRead says whether tutor reads the task as a sheet: a handwriting sheet
// with one answer slot bound anywhere, which AI reads, and the others bound
// choice (tutor's ReadSheet). A zone or the whole sheet it does not read.
func sheetRead(task formatv0.Task) bool {
	if task.Canvas.Kind != formatv0.CanvasKindHandwritingSheet {
		return false
	}
	anywhere := 0
	for _, item := range task.Items {
		for _, slot := range verdict.AnswerSlots(item) {
			switch slot.Binding {
			case string(formatv0.AnswerSlotBindingAnywhere):
				anywhere++
			case string(formatv0.AnswerSlotBindingChoice):
			default:
				return false
			}
		}
	}
	return anywhere == 1
}

// essayRead says whether tutor reads the task as an essay: a handwriting sheet
// whose one answer slot is bound sheet and checked by a rubric, as the format
// has a task with a rubric (tutor's ReadEssay).
func essayRead(task formatv0.Task) bool {
	if task.Canvas.Kind != formatv0.CanvasKindHandwritingSheet {
		return false
	}
	var slots []verdict.AnswerSlot
	for _, item := range task.Items {
		slots = append(slots, verdict.AnswerSlots(item)...)
	}
	return len(slots) == 1 && slots[0].Binding == string(formatv0.AnswerSlotBindingSheet) &&
		slots[0].Check.Kind == formatv0.CheckKindRubric
}

// criteriaOf is the texts of the criteria of the task's rubric, in their order:
// the assessment numbers them from 1, and a mark is kept by that number (R15).
func criteriaOf(task formatv0.Task) []string {
	var out []string
	for _, item := range task.Items {
		for _, slot := range verdict.AnswerSlots(item) {
			for _, c := range slot.Check.Criteria {
				out = append(out, string(c.Text))
			}
		}
	}
	return out
}
