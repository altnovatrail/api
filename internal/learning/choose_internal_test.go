package learning

import (
	"slices"
	"testing"
	"time"

	"github.com/altnovatrail/engine"
	learnermodel "github.com/altnovatrail/learner-model"
	"github.com/altnovatrail/module-kit/modulepackage"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/altnovatrail/api/internal/admission"
	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/packages/packagestest"
	"github.com/altnovatrail/api/internal/store"
)

// A skill of a package before is named as the current version names it: kept,
// replaced by its retired skill's replacements, or dropped when the graph knows
// it not or the module has no current version.
func TestASkillOfAPackageBeforeIsTranslated(t *testing.T) {
	t.Parallel()

	graphs := learnermodel.Graphs{
		"example/m": {Current: true, Graph: learnermodel.Graph{
			Skills:  []learnermodel.Skill{{ID: "example/m/skill/a"}, {ID: "example/m/skill/c"}, {ID: "example/m/skill/d"}},
			Retired: []learnermodel.RetiredSkill{{ID: "example/m/skill/b", ReplacedBy: []string{"example/m/skill/c", "example/m/skill/d"}}},
		}},
		"example/gone": {},
	}
	got := translated(graphs, []string{"example/m/skill/a", "example/m/skill/b", "example/m/skill/x", "example/gone/skill/y", "example/none/skill/z"})
	if want := []string{"example/m/skill/a", "example/m/skill/c", "example/m/skill/d"}; !slices.Equal(got, want) {
		t.Errorf("translated = %v, want %v", got, want)
	}
}

// The result on a skill is of the items it is primary in — an item's own
// skills taking the place of the task's —: how many got a first verdict, how
// many right, and whether the solution was shown; no evidence, or evidence that
// does not decode, gives none.
func TestTheResultOnASkillIsOfItsItems(t *testing.T) {
	t.Parallel()

	graphs := learnermodel.Graphs{"example/m": {Current: true, Graph: learnermodel.Graph{
		Skills: []learnermodel.Skill{{ID: "example/m/skill/a"}, {ID: "example/m/skill/b"}},
	}}}
	e := learnermodel.Evidence{Kind: learnermodel.EvidenceKind, Outcome: learnermodel.Outcome{Kind: learnermodel.OutcomeSolutionShown},
		Task: learnermodel.Task{ID: "example/m/task/t", Difficulty: 1, Band: learnermodel.BandRef{Place: 1, ID: "b"},
			Primary: []string{"example/m/skill/a"}, KeyProvenance: learnermodel.ProvenanceAuthor, SkillTagsProvenance: learnermodel.ProvenanceAuthor,
			Items: []learnermodel.TaskItem{
				{Place: 1, Slots: []learnermodel.Slot{{Check: learnermodel.CheckKindNumber}}},
				{Place: 2, Slots: []learnermodel.Slot{{Check: learnermodel.CheckKindNumber}}},
				{Place: 3, OwnSkills: true, Primary: []string{"example/m/skill/b"}, Slots: []learnermodel.Slot{{Check: learnermodel.CheckKindNumber}}},
			}},
		StartBand: learnermodel.BandRef{Place: 1, ID: "b"},
		Items: []learnermodel.ItemAnswer{
			{Place: 1, Submitted: true, FirstVerdict: learnermodel.VerdictCorrect, HasFirstVerdict: true},
			{Place: 2, Submitted: true, FirstVerdict: learnermodel.VerdictNotYet, HasFirstVerdict: true},
			{Place: 3},
		}}
	data, err := learnermodel.EncodeEvidence(e)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	got := s.results(store.ClosedAttempt{Evidence: data}, graphs)
	if len(got) != 2 || got[0].Skill != "example/m/skill/a" || got[0].Answered != 2 || got[0].FirstRight != 1 || !got[0].SolutionShown ||
		got[1].Skill != "example/m/skill/b" || got[1].Answered != 0 {
		t.Errorf("results = %+v", got)
	}
	if none := s.results(store.ClosedAttempt{}, graphs); none != nil {
		t.Errorf("the results of no evidence = %+v", none)
	}
	if none := s.results(store.ClosedAttempt{Evidence: []byte{0xff}}, graphs); none != nil {
		t.Errorf("the results of evidence that does not decode = %+v", none)
	}
}

// The pool holds the tasks a beginning takes, with the traps of their items,
// and says when AI that is not admitted left one out; a choice a close gave
// again is it, or nothing to give.
func TestThePoolIsWhatABeginningTakes(t *testing.T) {
	t.Parallel()

	pkg := packagestest.Own(t, "choices")
	s := &Service{gate: noAI(t)}
	m, narrowed, err := s.candidates(pkg, learnermodel.Graph{}, formatv0.AgeCategoryUnder13, "", true)
	if err != nil {
		t.Fatal(err)
	}
	var pool []string
	for _, c := range m.Pool {
		pool = append(pool, c.Task.ID)
	}
	if want := []string{"example/choices/task/greater", "example/choices/task/pairs", "example/choices/task/pick"}; !slices.Equal(pool, want) || !narrowed {
		t.Errorf("the pool = %v (narrowed %v), want the tasks a tap checks, and the handwriting left out", pool, narrowed)
	}
	for _, c := range m.Pool {
		if c.Task.ID == "example/choices/task/pairs" && (len(c.Traps) != 1 || c.Traps[0].Place != 1 || c.Traps[0].Traps[0] != "example/choices/trap/first-seen") {
			t.Errorf("the traps of pairs = %+v", c.Traps)
		}
	}
	if len(m.Lessons) != 1 || m.Lessons[0].ID != "example/choices/lesson/basics" || len(m.Lessons[0].Tasks) != 2 {
		t.Errorf("the lessons = %+v, want basics alone: grown is the adult's, essay begins nothing", m.Lessons)
	}
	if c := madeChoice(store.MadeChoice{}, false); c != nil {
		t.Errorf("no choice given = %v, want none", c)
	}
	if c := madeChoice(store.MadeChoice{ID: "x", Kind: "nothing", NotAdmitted: true}, true); c.GetKind() != v1.ChoiceKind_CHOICE_KIND_NOTHING ||
		c.GetChoiceId() != "" || c.GetOutcome().GetCause() != v1.Cause_CAUSE_NOT_ADMITTED {
		t.Errorf("nothing given = %v, want nothing, no ID, with its outcome", c)
	}
	if c := madeChoice(store.MadeChoice{ID: "x", Kind: "lesson"}, true); c.GetKind() != v1.ChoiceKind_CHOICE_KIND_LESSON || c.GetChoiceId() != "x" {
		t.Errorf("a choice given = %v", c)
	}
}

// The pool goes by the route the request names (R65): platform AI, which a
// deployment with no facts closes, leaves the tasks a tap checks and says so,
// and so does the own key of a provider whose models are not admitted; the
// adult's own key of Anthropic, which the stage "author" admits, gives the
// handwriting too.
func TestThePoolGoesByTheRoute(t *testing.T) {
	t.Parallel()

	pkg := packagestest.Own(t, "choices")
	s := &Service{gate: gateAt(t, "author")}
	for _, tc := range []struct {
		ownKey   string
		apples   bool
		narrowed bool
	}{
		{"", false, true},
		{"openai", false, true},
		{"anthropic", true, false},
	} {
		m, narrowed, err := s.candidates(pkg, learnermodel.Graph{}, formatv0.AgeCategoryAdult, tc.ownKey, true)
		if err != nil {
			t.Fatal(err)
		}
		apples := slices.ContainsFunc(m.Pool, func(c engine.Candidate) bool { return c.Task.ID == "example/choices/task/apples" })
		if apples != tc.apples || len(m.Lessons) != 2 || narrowed != tc.narrowed {
			t.Errorf("the pool by %q holds apples %v, %d lessons, narrowed %v; want %v, both lessons, %v", tc.ownKey, apples,
				len(m.Lessons), narrowed, tc.apples, tc.narrowed)
		}
	}
}

// noAI is the gate of a deployment that declares no facts, at the stage "full":
// no AI is admitted to read handwriting.
func noAI(t *testing.T) *admission.Gate {
	t.Helper()
	return gateAt(t, "full")
}

// gateAt is the gate of a deployment that declares no facts, at the stage rung,
// with the platform's key of Anthropic: platform AI is closed, and at "author"
// an adult's own key of Anthropic is open.
func gateAt(t *testing.T, rung string) *admission.Gate {
	t.Helper()
	cfg, err := config.AdmissionFrom([]string{"API_ADMISSION_RUNG=" + rung, "API_ANTHROPIC_KEY=sk-test"})
	if err != nil {
		t.Fatal(err)
	}
	g, _, err := admission.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// Nothing to give says why only when the closed AI left a task out of the pool:
// "not admitted" — as a choice written before the causes were kept —, "quota"
// with when the next family day begins, or "platform limit"; a choice no module
// of the engine holds has no package.
func TestNothingToGiveSaysWhyOnlyForAI(t *testing.T) {
	t.Parallel()

	next := time.Date(2026, 11, 13, 5, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		made  store.MadeChoice
		cause v1.Cause
	}{
		{store.MadeChoice{Kind: "nothing", NotAdmitted: true}, v1.Cause_CAUSE_NOT_ADMITTED},
		{store.MadeChoice{Kind: "nothing", NotAdmitted: true, Closed: store.NothingNotAdmitted}, v1.Cause_CAUSE_NOT_ADMITTED},
		{store.MadeChoice{Kind: "nothing", NotAdmitted: true, Closed: store.NothingQuota, NextDay: &next}, v1.Cause_CAUSE_QUOTA},
		{store.MadeChoice{Kind: "nothing", NotAdmitted: true, Closed: store.NothingPlatformLimit}, v1.Cause_CAUSE_PLATFORM_LIMIT},
	} {
		c := nothing(tc.made)
		if c.GetKind() != v1.ChoiceKind_CHOICE_KIND_NOTHING || c.GetOutcome().GetCause() != tc.cause ||
			(tc.cause == v1.Cause_CAUSE_QUOTA) != (c.GetOutcome().GetNextFamilyDay() != nil) {
			t.Errorf("nothing of %+v = %v, want %v", tc.made, c, tc.cause)
		}
	}
	if c := nothing(store.MadeChoice{Kind: "nothing"}); c.GetOutcome() != nil {
		t.Errorf("nothing for another reason = %v, want no outcome", c)
	}
	if cause, day := closedCause(true, &v1.Outcome{Cause: v1.Cause_CAUSE_QUOTA, NextFamilyDay: timestamppb.New(next)}); cause != store.NothingQuota || !day.Equal(next) {
		t.Errorf("the cause of a quota = %q, %v", cause, day)
	}
	if cause, _ := closedCause(false, nil); cause != store.NothingNotAdmitted {
		t.Errorf("the cause of no admission = %q", cause)
	}
	pkg := packagestest.Own(t, "choices")
	if got, _, _, _ := chosenIn([]*modulepackage.Package{pkg}, engine.Choice{Kind: engine.ChoiceTask, ID: "example/choices/task/none"}); got != nil {
		t.Error("a task the package has not was found")
	}
	if got, kind, local, skills := chosenIn([]*modulepackage.Package{pkg}, engine.Choice{Kind: engine.ChoiceLesson, ID: "example/choices/lesson/basics"}); got != pkg ||
		kind != "lesson" || local != "basics" || len(skills) != 2 {
		t.Errorf("the lesson chosen = %v %q %q %v", got != nil, kind, local, skills)
	}
}

// The next task segment of a run is its first task segment that has not ended:
// an explanation is none, and a run whose every task segment has ended has none.
func TestTheNextTaskOfARunIsItsFirstNotEnded(t *testing.T) {
	t.Parallel()

	lesson := packagestest.Own(t, "choices").View.Lessons["basics"]
	for _, c := range []struct {
		ended []int
		want  formatv0.TaskID
		found bool
	}{
		{nil, "pick", true},
		{[]int{2}, "pairs", true},
		{[]int{4}, "pick", true},
		{[]int{2, 4}, "", false},
	} {
		if got, found := nextTask(lesson, c.ended); got != c.want || found != c.found {
			t.Errorf("the next task after %v = %q (%v), want %q (%v)", c.ended, got, found, c.want, c.found)
		}
	}
}

// The lesson of a run that waits leaves the lessons of every module of the
// engine, and the others stay.
func TestTheLessonOfARunThatWaitsLeavesTheEngine(t *testing.T) {
	t.Parallel()

	in := engine.Input{Modules: []engine.Module{
		{Lessons: []engine.Lesson{{ID: "example/a/lesson/one"}, {ID: "example/a/lesson/two"}}},
		{Lessons: []engine.Lesson{{ID: "example/b/lesson/one"}}},
	}}
	withoutLesson(&in, "example/a/lesson/one")
	if l := in.Modules[0].Lessons; len(l) != 1 || l[0].ID != "example/a/lesson/two" || len(in.Modules[1].Lessons) != 1 {
		t.Errorf("the lessons = %+v, want all but example/a/lesson/one", in.Modules)
	}
}
