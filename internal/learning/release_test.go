package learning_test

import (
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	learnermodel "github.com/altnovatrail/learner-model"
	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/module-kit/verdict"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/google/uuid"

	api "github.com/altnovatrail/api"
	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/model"
	"github.com/altnovatrail/api/internal/packages"
	"github.com/altnovatrail/api/internal/tutorstub"
)

// releaseModule is the module of the first app: the one module of the pinned
// release whose every lesson this code must begin.
const releaseModule = "novatrail/olympiad-math"

// boardModule is the module of tutor's board, whose tasks the author solves in
// app: every task of it is on a handwriting sheet.
const boardModule = "novatrail/check-board"

// The pinned release of modules, laid by api packages into the folder that
// API_RELEASE_PACKAGES names, as ci-modules lays it: its packages are read as
// serve reads them, every lesson of the module of the first app begins for an
// adult whose handwriting AI may read, and the owner's path on it — connect,
// "something else" and what it gives begun, a lesson opened with its first task
// segment, a task of the pool tapped with its leading question asked for, a
// wrong answer and its rung's words, then the key — gives evidence of kind 2
// that holds the question. A task with a time of day describes its slot as one.
// The module of the board begins on the sheet, on the stub of tutor, as
// boardIsLearnedOn tells, so does every task with a rubric of the module of
// essays for an adult, as essaysAreLearnedOn tells, and the catalog of
// novatrail is its list, as novatrailCatalog tells. With no folder named, the
// test is skipped: ci-test runs with no network, and its packages are the
// vectors.
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
	t.Run("the board module", func(t *testing.T) { boardIsLearnedOn(t, set) })
	t.Run("the essay module", func(t *testing.T) { essaysAreLearnedOn(t, set) })
	t.Run("the catalog of novatrail", func(t *testing.T) { novatrailCatalog(t, dir, cfg) })
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
		ChoiceId: w.lessonChoiceOf(t, l.adult, releaseModule, string(lesson))})
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
	if err != nil || e.Kind != learnermodel.EvidenceKind || !e.LeadingQuestionOnRequest || e.Outcome.Kind != learnermodel.OutcomeAfterHint {
		t.Errorf("the evidence of %s = %+v, %v; want the kind of this version, the question on request, correct after a hint", task.ID, e, err)
	}
}

// boardIsLearnedOn walks the owner's path on the module of tutor's board in the
// dev mode, the gate and the stub of tutor as just dev builds them: the owner's
// catalog gives its current version, it connects as a module solved on the
// sheet, its page begins with every task of it open, the engine chooses none,
// and a task is checked as a task from paper is — not yet, then correct — with
// no evidence.
func boardIsLearnedOn(t *testing.T, set *packages.Set) {
	pkg, ok := set.Current(boardModule)
	if !ok {
		t.Fatalf("the pinned release gives %s no current version", boardModule)
	}
	g := stubGate(t)
	w := worldOn(t, set, g, []learning.Option{learning.WithTutor(tutorstub.New(g))})
	l := w.learner(t)
	catalog, err := w.modules.ListCatalog(l.ctx, &v1.ListCatalogRequest{ProfileId: l.adult})
	if err != nil || !slices.ContainsFunc(catalog.GetModules(), func(m *v1.CatalogModule) bool {
		return m.GetModuleId() == boardModule && m.GetPackageDigest() == string(pkg.Digest)
	}) {
		t.Fatalf("ListCatalog = %v, %v; want %s on its current version", catalog, err, boardModule)
	}
	connected, err := w.modules.ConnectModule(l.ctx, &v1.ConnectModuleRequest{ProfileId: l.adult, ModuleId: boardModule})
	if err != nil || len(connected.GetModules()) != 1 || !connected.GetModules()[0].GetOnSheet() {
		t.Fatalf("ConnectModule = %v, %v; want %s on the sheet", connected, err, boardModule)
	}
	page, err := w.paper.BeginPaperAttempts(l.ctx, &v1.BeginPaperAttemptsRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString(),
		PageId: "module.novatrail.check-board", ModuleId: boardModule})
	if err != nil || len(page.GetTasks()) != len(pkg.View.Tasks) || len(page.GetAttemptIds()) != len(pkg.View.Tasks) {
		t.Fatalf("the page of %s = %d tasks, %v; want all %d", boardModule, len(page.GetTasks()), err, len(pkg.View.Tasks))
	}
	if c, err := w.chooseNext(l, l.adult); err != nil || c.GetKind() != v1.ChoiceKind_CHOICE_KIND_NOTHING {
		t.Errorf("something else with the board alone = %v, %v; want nothing", c, err)
	}
	attempt := page.GetAttemptIds()[0]
	if v := slotOf(t, w.submitPaper(t, l, attempt, wrongWork)); v.GetVerdict() != "not_yet" {
		t.Fatalf("a wrong work = %v; want not yet", v)
	}
	if v := slotOf(t, w.submitPaper(t, l, attempt, rightWork)); v.GetVerdict() != "correct" || w.closeOf(t, attempt) != "correct" {
		t.Fatalf("a right work = %v, the attempt closed %q; want correct and closed correct", v, w.closeOf(t, attempt))
	}
	if n := w.evidenceOf(t, attempt); n != 0 {
		t.Errorf("the journal holds %d pieces of evidence of the board's attempt, want none", n)
	}
}

// novatrailList is the list of modules of the deployment novatrail: olympiad
// mathematics and the check board, and no essays.
var novatrailList = []formatv0.ModuleID{releaseModule, boardModule}

// novatrailCatalog reads the pinned release again, as serve reads it on
// novatrail at the stage author with its list: the pinned index holds both
// modules of the list, the owner's catalog is the two on their current
// versions, and a module the list leaves out is refused a connection as one
// with no current version.
func novatrailCatalog(t *testing.T, dir string, cfg packages.Config) {
	set, err := packages.Read(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.List(novatrailList); err != nil {
		t.Fatalf("the list of novatrail: %v", err)
	}
	w := worldOn(t, set, gate(t, adultFacts, config.RungAuthor), nil)
	l := w.learner(t)
	catalog, err := w.modules.ListCatalog(l.ctx, &v1.ListCatalogRequest{ProfileId: l.adult})
	if err != nil {
		t.Fatal(err)
	}
	var got []formatv0.ModuleID
	for _, m := range catalog.GetModules() {
		pkg, ok := set.CurrentFor(formatv0.ModuleID(m.GetModuleId()), formatv0.AgeCategoryAdult)
		if !ok || m.GetPackageDigest() != string(pkg.Digest) {
			t.Errorf("the catalog gives %s on %s, not on its current version", m.GetModuleId(), m.GetPackageDigest())
		}
		got = append(got, formatv0.ModuleID(m.GetModuleId()))
	}
	if !slices.Equal(got, slices.Sorted(slices.Values(novatrailList))) {
		t.Errorf("the catalog of novatrail = %v, want %v", got, novatrailList)
	}
	for _, id := range set.Modules() {
		if slices.Contains(novatrailList, id) {
			continue
		}
		_, err := w.modules.ConnectModule(l.ctx, &v1.ConnectModuleRequest{ProfileId: l.adult, ModuleId: string(id)})
		refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION)
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

// essayModule is the module of the pinned release whose tasks with a rubric are
// essays on a handwriting sheet, beside tasks a tap checks.
const essayModule = "novatrail/english-essays"

// essaysAreLearnedOn walks an adult's path on the module of essays in the dev
// mode, on the stub of tutor, which admits an essay and its assessment where
// the gate admits a sheet: every task and lesson of it reaches the engine for
// an adult, it connects, and every task of it with a rubric begins, a wide crop
// of its sheet ends in feedback — the stub's text read, its marks, and its
// words, which pass the leak check and the rule of feedback on each task —, and
// "Done" closes it with the criteria of its rubric.
func essaysAreLearnedOn(t *testing.T, set *packages.Set) {
	pkg, ok := set.Current(essayModule)
	if !ok {
		t.Fatalf("the pinned release gives %s no current version", essayModule)
	}
	g := stubGate(t)
	w := worldOn(t, set, g, []learning.Option{learning.WithTutor(tutorstub.New(g))})
	pool, lessons, err := w.service.Candidates(pkg, formatv0.AgeCategoryAdult, "")
	if err != nil || len(pool) != len(pkg.View.Tasks) || lessons != len(pkg.View.Lessons) {
		t.Errorf("the engine gets %d tasks of %d and %d lessons of %d of %s, %v; want all", len(pool), len(pkg.View.Tasks),
			lessons, len(pkg.View.Lessons), essayModule, err)
	}
	l := w.learner(t)
	if _, err := w.modules.ConnectModule(l.ctx, &v1.ConnectModuleRequest{ProfileId: l.adult, ModuleId: essayModule}); err != nil {
		t.Fatal(err)
	}
	essays := 0
	for _, id := range slices.Sorted(maps.Keys(pkg.View.Tasks)) {
		task := pkg.View.Tasks[id]
		criteria := 0
		for _, item := range task.Items {
			for _, slot := range item.AnswerSlots {
				criteria += len(slot.Check.Criteria)
			}
		}
		if criteria == 0 {
			continue
		}
		essays++
		begun, err := w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString(),
			Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: w.choiceOf(t, l.adult, essayModule, string(id))}})
		if err != nil {
			t.Errorf("BeginAttempt %s: %v", id, err)
			continue
		}
		attempt := begun.GetAttempt().GetAttemptId()
		res, err := w.attempts.SubmitEssay(l.ctx, &v1.SubmitEssayRequest{ProfileId: l.adult, AttemptId: attempt,
			IdempotencyKey: uuid.NewString(), Crop: pngOf(t, 40, 10)})
		if err != nil || res.GetEnd() != v1.SubmissionEnd_SUBMISSION_END_FEEDBACK || res.GetText() == "" || res.GetFeedback() == "" {
			t.Errorf("a wide essay of %s = %v, %v; want feedback with the text and the stub's words", id, res, err)
		}
		done, err := w.attempts.Done(l.ctx, &v1.DoneRequest{ProfileId: l.adult, AttemptId: attempt})
		if err != nil || len(done.GetSolved().GetCriteria()) != criteria || w.closeOf(t, attempt) != "done" {
			t.Errorf("Done of %s = %v, %v; want its %d criteria and the attempt closed done", id, done, err, criteria)
		}
	}
	if essays == 0 {
		t.Errorf("%s holds no task with a rubric", essayModule)
	}
}
