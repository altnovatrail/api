package learning_test

import (
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	learnermodel "github.com/altnovatrail/learner-model"
	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/module-kit/verdict"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/google/uuid"

	api "github.com/altnovatrail/api"
	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/model"
	"github.com/altnovatrail/api/internal/packages"
)

// releaseModule is the module of the first app (R76 spec): the one module of
// the pinned release whose every lesson this code must begin.
const releaseModule = "novatrail/olympiad-math"

// The pinned release of modules, laid by api packages into the folder that
// API_RELEASE_PACKAGES names, as ci-modules lays it (R53): its packages are read
// as serve reads them, every lesson of the module of the first app begins for an
// adult whose handwriting AI may read, and the owner's path on it — connect,
// "something else" and what it gives begun, a lesson opened with its first
// task segment, a task of the pool tapped with its leading question asked for,
// a wrong answer and its rung's words, then the key — gives evidence of kind 2
// that holds the question. A task with a time
// of day describes its slot as one. With no folder named, the test is skipped:
// ci-test runs with no network, and its packages are the vectors.
func TestThePinnedReleaseIsLearnedOn(t *testing.T) {
	dir := os.Getenv("API_RELEASE_PACKAGES")
	if dir == "" {
		t.Skip("API_RELEASE_PACKAGES names no folder of the pinned release's packages")
	}
	cfg, err := api.Modules()
	if err != nil {
		t.Fatal(err)
	}
	set, err := packages.Read(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	pkg, ok := set.Current(releaseModule)
	if !ok {
		t.Fatalf("the release %s gives %s no current version", cfg.Release, releaseModule)
	}

	w := worldOn(t, set, gate(t, adultFacts, config.RungAuthor), nil)
	pool, lessons, err := w.service.Candidates(pkg, formatv0.AgeCategoryAdult, "")
	if err != nil || len(pool) == 0 || lessons != len(pkg.View.Lessons) {
		t.Errorf("the engine gets %d tasks and %d lessons of %d of %s, %v; want every lesson", len(pool), lessons,
			len(pkg.View.Lessons), releaseModule, err)
	}
	timed := 0
	for _, task := range pkg.View.Tasks {
		if slot, item, ok := timeSlot(task); ok {
			described, err := model.Describe(pkg, task)
			if err != nil || described.Items[item].Slots[slot].Check != learnermodel.CheckKindTime {
				t.Errorf("the slot of a time of day of %s is described as %+v, %v", task.ID, described.Items, err)
			}
			timed++
		}
	}
	if timed == 0 {
		t.Errorf("no task of %s has a time of day, which format 0.4 brought it", releaseModule)
	}

	l := w.learner(t)
	if _, err := w.modules.ConnectModule(l.ctx, &v1.ConnectModuleRequest{ProfileId: l.adult, ModuleId: releaseModule}); err != nil {
		t.Fatal(err)
	}
	c, err := w.chooseNext(l, l.adult)
	if err != nil || c.GetKind() != v1.ChoiceKind_CHOICE_KIND_TASK && c.GetKind() != v1.ChoiceKind_CHOICE_KIND_LESSON {
		t.Fatalf("ChooseNext = %v, %v; want a task or a lesson", c, err)
	}
	// What the engine gives begins: a task, or a lesson's run and its first task segment.
	w.started(t, l, l.adult, c)
	lesson := slices.Sorted(maps.Keys(pkg.View.Lessons))[0]
	opened, err := w.learning.OpenLesson(l.ctx, &v1.OpenLessonRequest{ProfileId: l.adult,
		ChoiceId: w.id(t, `INSERT INTO engine_choices (profile_id, kind, module_id, lesson_id) VALUES ($1, 'lesson', $2, $3)
			RETURNING id`, l.adult, releaseModule, string(lesson))})
	if err != nil {
		t.Fatalf("OpenLesson %s: %v", lesson, err)
	}
	if _, err := w.segment(l, l.adult, opened.GetRun().GetLessonRunId(), firstTaskSegment(pkg.View.Lessons[lesson])); err != nil {
		t.Errorf("the first task segment of %s did not begin: %v", lesson, err)
	}

	// A task of the pool, tapped, with its leading question: the question before a
	// wrong answer, the rung's words after it, then the key.
	task, wrong, key := tappedWithQuestion(t, pkg, pool)
	begun, err := w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString(),
		Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: w.choiceOf(t, l.adult, releaseModule, string(task.ID))}})
	if err != nil {
		t.Fatalf("BeginAttempt %s: %v", task.ID, err)
	}
	attempt := begun.GetAttempt().GetAttemptId()
	hint, err := w.attempts.GetHint(l.ctx, &v1.GetHintRequest{ProfileId: l.adult, AttemptId: attempt})
	if err != nil || hint.GetLeadingQuestion() != string(*task.LeadingQuestion) {
		t.Errorf("GetHint of %s = %v, %v; want its leading question", task.ID, hint, err)
	}
	if res, err := w.submit(l, l.adult, attempt, wrong...); err != nil || res.GetWrongAnswers() != 1 {
		t.Fatalf("SubmitWork of a wrong option of %s = %v, %v; want one wrong answer", task.ID, res, err)
	}
	rung, err := w.attempts.GetHint(l.ctx, &v1.GetHintRequest{ProfileId: l.adult, AttemptId: attempt})
	if err != nil || rung.GetRung().GetRung() != 1 || rung.GetRung().GetWords() == "" {
		t.Errorf("GetHint of %s after a wrong answer = %v, %v; want the words of rung 1", task.ID, rung, err)
	}
	if res, err := w.submit(l, l.adult, attempt, key...); err != nil || w.closeOf(t, attempt) != "correct" {
		t.Fatalf("SubmitWork of the key of %s = %v, %v; want it closed correct", task.ID, res, err)
	}
	if _, err := w.progress.GetProgress(l.ctx, &v1.GetProgressRequest{ProfileId: l.adult}); err != nil {
		t.Fatal(err)
	}
	var written []byte
	if err := w.pool.QueryRow(t.Context(), `SELECT evidence FROM evidence WHERE attempt_id = $1`, attempt).Scan(&written); err != nil {
		t.Fatal(err)
	}
	e, err := learnermodel.DecodeEvidence(written)
	if err != nil || e.Kind != 2 || !e.LeadingQuestionOnRequest || e.Outcome.Kind != learnermodel.OutcomeAfterHint {
		t.Errorf("the evidence of %s = %+v, %v; want kind 2, the question on request, correct after a hint", task.ID, e, err)
	}
}

// firstTaskSegment is the place of the lesson's first task segment, from 1.
func firstTaskSegment(lesson formatv0.Lesson) int32 {
	for i, s := range lesson.Segments {
		if s.Task != nil {
			return int32(i + 1)
		}
	}
	return 0
}

// timeSlot is the place of a slot of the task whose check is a time of day, of
// its item and in it, each from 0.
func timeSlot(task formatv0.Task) (slot, item int, ok bool) {
	for i, it := range task.Items {
		for j, s := range verdict.AnswerSlots(it) {
			if s.Check.Kind == formatv0.CheckKindTime {
				return j, i, true
			}
		}
	}
	return 0, 0, false
}

// tappedWithQuestion is a task of the engine's pool, the first by ID, that a tap
// checks whole and that has a leading question, with an answer whose first slot
// is wrong and with its key: the learner's path the release must give.
func tappedWithQuestion(t *testing.T, pkg *modulepackage.Package, pool []string) (task formatv0.Task, wrong, key []*v1.TappedChoice) {
	t.Helper()
	for _, full := range slices.Sorted(slices.Values(pool)) {
		task = pkg.View.Tasks[formatv0.TaskID(full[strings.LastIndex(full, "/")+1:])]
		if task.LeadingQuestion == nil || task.Template != nil {
			continue
		}
		wrong, key = nil, nil
		whole := true
		for i, it := range task.Items {
			for j, s := range verdict.AnswerSlots(it) {
				if s.Binding != string(formatv0.AnswerSlotBindingChoice) || s.Check.Key == nil {
					whole = false
					continue
				}
				right := *s.Check.Key
				given := right
				if len(wrong) == 0 && len(key) == 0 {
					for _, o := range s.Options {
						if string(o) != right {
							given = string(o)
							break
						}
					}
				}
				key = append(key, tap(int32(i+1), int32(j+1), right))
				wrong = append(wrong, tap(int32(i+1), int32(j+1), given))
			}
		}
		if whole && len(key) > 0 && wrong[0].GetValue() != key[0].GetValue() {
			return task, wrong, key
		}
	}
	t.Fatalf("no task of the pool of %s is tapped whole with a leading question", pkg.View.Manifest.ID)
	return formatv0.Task{}, nil, nil
}
