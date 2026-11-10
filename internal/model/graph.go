package model

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	learnermodel "github.com/altnovatrail/learner-model"
	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/module-kit/verdict"
	"github.com/altnovatrail/spec/types/formatv0"
)

// anchorPrefix opens the ID of an anchor skill of the shared taxonomy, which a
// module names as it is: it has no module (format/README.md, section 13).
const anchorPrefix = "core."

// SkillID and TaskID are the full IDs of a skill and a task of a module, as the
// learner model, the engine and the progress know them: the module's ID, the
// kind and the local ID (concepts 3 spec).
func SkillID(module formatv0.ModuleID, id string) string {
	return string(module) + "/skill/" + id
}

func TaskID(module formatv0.ModuleID, id formatv0.TaskID) string {
	return string(module) + "/task/" + string(id)
}

// trapID is the full ID of a trap of a module.
func trapID(module formatv0.ModuleID, id formatv0.TrapID) string {
	return string(module) + "/trap/" + string(id)
}

// IsAnchor says whether a skill a module names is an anchor skill of the shared
// taxonomy.
func IsAnchor(id string) bool { return strings.HasPrefix(id, anchorPrefix) }

// skillOrAnchor is a skill a module names by its local ID, or an anchor skill,
// by its full ID.
func skillOrAnchor(module formatv0.ModuleID, id string) string {
	if IsAnchor(id) {
		return id
	}
	return SkillID(module, id)
}

func skillIDs[S ~string](module formatv0.ModuleID, ids []S) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = skillOrAnchor(module, string(id))
	}
	return out
}

// bandsOf is the bands of the package's manifest, by place from 1.
func bandsOf(pkg *modulepackage.Package) []learnermodel.Band {
	bands := make([]learnermodel.Band, len(pkg.View.Manifest.Bands))
	for i, b := range pkg.View.Manifest.Bands {
		grades := make([]int32, len(b.Grades))
		for j, g := range b.Grades {
			grades[j] = int32(g)
		}
		bands[i] = learnermodel.Band{Place: int32(i + 1), ID: string(b.ID), Grades: grades}
	}
	return bands
}

// assessabilities is each assessability of the format in learner-model's words.
var assessabilities = map[formatv0.SkillAssessability]learnermodel.Assessability{
	formatv0.SkillAssessabilityVerdict: learnermodel.AssessabilityVerdict,
	formatv0.SkillAssessabilityRubric:  learnermodel.AssessabilityRubric,
	formatv0.SkillAssessabilityNone:    learnermodel.AssessabilityNone,
}

// graphOf is the graph of the package's version, as learner-model builds it of
// its bands, its skills with their tiers, its retired skills and the
// descriptions of its tasks (R08).
func graphOf(pkg *modulepackage.Package) (learnermodel.Graph, error) {
	module := pkg.View.Manifest.ID
	var skills []learnermodel.Skill
	var retired []learnermodel.RetiredSkill
	if g := pkg.View.SkillGraph; g != nil {
		for id, s := range g.Skills {
			skills = append(skills, learnermodel.Skill{
				ID: SkillID(module, string(id)), Assessability: assessabilities[s.Assessability],
				Teaches: skillIDs(module, s.Teaches), Prerequisites: skillIDs(module, s.Prerequisites),
				Tier: int32(pkg.View.SkillTiers[id]),
			})
		}
		for id, r := range g.RetiredSkills {
			retired = append(retired, learnermodel.RetiredSkill{ID: SkillID(module, string(id)), ReplacedBy: skillIDs(module, r.ReplacedBy)})
		}
	}
	var tasks []learnermodel.Task
	for _, id := range slices.Sorted(maps.Keys(pkg.View.Tasks)) {
		t, err := Describe(pkg, pkg.View.Tasks[id])
		if err != nil {
			return learnermodel.Graph{}, err
		}
		tasks = append(tasks, t)
	}
	g, err := learnermodel.NewGraph(string(module), bandsOf(pkg), skills, retired, tasks)
	if err != nil {
		return learnermodel.Graph{}, fmt.Errorf("model: the graph of %s %s: %w", module, pkg.View.Manifest.Version, err)
	}
	return g, nil
}

// checkKinds is each check kind of the format that learner-model knows, in its
// words. A time of day it does not know yet, and an answer slot of one is
// written as a kind not given, whose item it reads as one with no answer (Д28):
// the kind comes with the release that knows it.
var checkKinds = map[formatv0.CheckKind]learnermodel.CheckKind{
	formatv0.CheckKindNumber:    learnermodel.CheckKindNumber,
	formatv0.CheckKindFraction:  learnermodel.CheckKindFraction,
	formatv0.CheckKindChoice:    learnermodel.CheckKindChoice,
	formatv0.CheckKindPredicate: learnermodel.CheckKindPredicate,
}

// Describe is the task of the package as learner-model describes it, for the
// evidence of an attempt and for a candidate of the engine (R08): its full ID —
// a template's own —, its band and difficulty, its skills and those of its
// items, where its key and skill tags came from, and the kind of the check of
// each answer slot, with the number of options of a choice. Every key of a
// module is the author's, but a template's, which the program works out, and so
// are its skill tags (concepts 9.4 spec).
func Describe(pkg *modulepackage.Package, task formatv0.Task) (learnermodel.Task, error) {
	m := pkg.View.Manifest
	band := slices.IndexFunc(m.Bands, func(b formatv0.Band) bool { return b.ID == task.Band })
	if band < 0 {
		return learnermodel.Task{}, fmt.Errorf("model: the task %s is in no band of the manifest", task.ID)
	}
	key := learnermodel.ProvenanceAuthor
	if task.Template != nil {
		key = learnermodel.ProvenanceSolver
	}
	t := learnermodel.Task{
		ID: TaskID(m.ID, task.ID), Template: task.Template != nil,
		Band:       learnermodel.BandRef{Place: int32(band + 1), ID: string(task.Band)},
		Difficulty: int32(task.Difficulty), Primary: skillIDs(m.ID, task.Skills.Primary),
		Supporting: skillIDs(m.ID, task.Skills.Supporting), KeyProvenance: key,
		SkillTagsProvenance: learnermodel.ProvenanceAuthor,
	}
	for i, item := range task.Items {
		it := learnermodel.TaskItem{Place: int32(i + 1)}
		if item.Skills != nil {
			it.OwnSkills, it.Primary, it.Supporting = true, skillIDs(m.ID, item.Skills.Primary), skillIDs(m.ID, item.Skills.Supporting)
		}
		for _, slot := range verdict.AnswerSlots(item) {
			s := learnermodel.Slot{Check: checkKinds[slot.Check.Kind]}
			if s.Check == learnermodel.CheckKindChoice {
				s.Options, s.HasOptions = int32(len(choiceOptions(slot))), true
			}
			it.Slots = append(it.Slots, s)
		}
		t.Items = append(t.Items, it)
	}
	return t, nil
}

// choiceOptions is the options a choice chooses from: of the binding, when the
// learner taps one, and of the check, when AI reads the one written.
func choiceOptions(slot verdict.AnswerSlot) []formatv0.Text {
	if len(slot.Options) > 0 {
		return slot.Options
	}
	return slot.Check.Options
}
