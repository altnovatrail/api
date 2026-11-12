package learning

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/altnovatrail/engine"
	learnermodel "github.com/altnovatrail/learner-model"
	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/module-kit/verdict"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/altnovatrail/api/internal/model"
	"github.com/altnovatrail/api/internal/packages"
	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// after is the close of an attempt or the end of a lesson run that a choice
// comes after, as what comes next; neither for "something else".
type after struct {
	attemptID *string
	runID     *string
}

// engineModules is the modules of the engine for a learner of category: those
// connected to the profile that have a current version for the learner, with
// that version (R07).
func (s *Service) engineModules(ctx context.Context, tx pgx.Tx, profileID string, category formatv0.AgeCategory) ([]*modulepackage.Package, error) {
	connected, err := store.ConnectedModules(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	var pkgs []*modulepackage.Package
	for _, c := range connected {
		if pkg, ok := s.set.CurrentFor(formatv0.ModuleID(c.ModuleID), category); ok {
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs, nil
}

// knows says whether a module of the engine has the skill, by its full ID: a
// skill of its graph, as the skill map names one.
func knows(pkgs []*modulepackage.Package, skill string) bool {
	for _, pkg := range pkgs {
		local, ok := strings.CutPrefix(skill, model.SkillID(pkg.View.Manifest.ID, ""))
		if ok && pkg.View.SkillGraph != nil {
			if _, ok := pkg.View.SkillGraph.Skills[formatv0.SkillID(local)]; ok {
				return true
			}
		}
	}
	return false
}

// errNoChoice is a choice the engine could not make of what api gave it — the
// learner model or the engine refusing the input, a task the model cannot
// describe —, which is no failure of the database: an answer that closes gives
// no choice for it, and app asks "something else".
type errNoChoice struct{ err error }

func (e *errNoChoice) Error() string { return "learning: no choice: " + e.err.Error() }
func (e *errNoChoice) Unwrap() error { return e.err }

// choose asks the engine what comes next for the profile, a learner of
// category, on the modules of the engine pkgs, at now, with the skill chosen on
// the skill map when there is one, and writes the choice (R50): after the
// evidence the profile owes is settled, the engine reads the learner model, the
// pool and the lessons the learner may begin, the runs, the attempts closed and
// the summary of what was handed out, and its choice is written with all a
// replay needs — nothing to give too, so that what it holds holds after it.
// Nothing to give says why when AI that is not admitted or closed left a task
// out of the pool or kept an open run waiting, and nothing for another reason
// (contract, EngineChoice).
func (s *Service) choose(ctx context.Context, tx pgx.Tx, profileID string, category formatv0.AgeCategory, pkgs []*modulepackage.Package, mapSkill string, now time.Time, by after) (*v1.EngineChoice, error) {
	settled, err := s.model.Settle(ctx, tx, profileID, now)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(pkgs))
	current := map[string]*modulepackage.Package{}
	for i, pkg := range pkgs {
		ids[i] = string(pkg.View.Manifest.ID)
		current[ids[i]] = pkg
	}
	read, err := s.model.Read(settled, now, ids)
	if err != nil {
		return nil, &errNoChoice{err}
	}
	// The AI that checks the tasks is closed to a new task by the admission, or on
	// platform AI by its provider's limit or a family day with no room left for a
	// typical task (R11, R59): the pool is then of the tasks a tap checks.
	admitted := s.gate.Handwriting(category, "")
	var closed *v1.Outcome
	if admitted {
		if closed, err = closedTo(ctx, tx, profileID, quota.TypicalHandwriting, now); err != nil {
			return nil, err
		}
	}
	limited := !admitted || closed != nil
	in := engine.Input{Time: now.Unix(), Readout: read, MapSkill: mapSkill}
	graphs := s.model.Graphs(ids)
	narrowed := false
	for _, pkg := range pkgs {
		m, left, err := s.candidates(pkg, graphs[string(pkg.View.Manifest.ID)].Graph, category, closed == nil)
		if err != nil {
			return nil, &errNoChoice{err}
		}
		in.Modules, narrowed = append(in.Modules, m), narrowed || left
	}
	held, err := s.history(ctx, tx, profileID, category, closed == nil, current, graphs, &in)
	if err != nil {
		return nil, err
	}
	narrowed = narrowed || held
	c, err := engine.Choose(in)
	if err != nil {
		return nil, &errNoChoice{err}
	}
	row, err := s.choiceRow(c, pkgs, settled, category, mapSkill, limited, by)
	if err != nil {
		return nil, &errNoChoice{err}
	}
	row.NotAdmitted = row.Kind == "nothing" && narrowed
	if row.NotAdmitted {
		row.Closed, row.NextDay = closedCause(admitted, closed)
	}
	id, err := store.WriteChoice(ctx, tx, profileID, row, now)
	if err != nil {
		return nil, err
	}
	return madeChoice(store.MadeChoice{ID: id, Kind: row.Kind, NotAdmitted: row.NotAdmitted, Closed: row.Closed, NextDay: row.NextDay}, true), nil
}

// choiceKinds is each kind of a choice of the schema in the contract's words.
var choiceKinds = map[string]v1.ChoiceKind{
	"task":   v1.ChoiceKind_CHOICE_KIND_TASK,
	"lesson": v1.ChoiceKind_CHOICE_KIND_LESSON,
}

// candidates is the module of pkg as the engine gets it: the graph of its
// current version, its pool — the tasks that suit the learner and that this code
// begins, each with the traps of its items' checks — and its lessons that suit
// the learner whole and whose every task segment this code begins, so that the
// engine gives nothing a beginning refuses (Д26). It says too whether a task
// was left out of the pool only for AI that is not admitted or closed to read
// it; an open run that waits says so in history.
func (s *Service) candidates(pkg *modulepackage.Package, graph learnermodel.Graph, category formatv0.AgeCategory, aiOpen bool) (engine.Module, bool, error) {
	m := engine.Module{Graph: graph}
	narrowed := false
	for _, id := range slices.Sorted(maps.Keys(pkg.View.Tasks)) {
		task := pkg.View.Tasks[id]
		if !s.begins(pkg, task, category, aiOpen) {
			narrowed = narrowed || forAI(pkg, task, category)
			continue
		}
		described, err := model.Describe(pkg, task)
		if err != nil {
			return engine.Module{}, false, err
		}
		m.Pool = append(m.Pool, engine.Candidate{Task: described, Traps: itemTraps(pkg, task)})
	}
	module := pkg.View.Manifest.ID
	for _, id := range slices.Sorted(maps.Keys(pkg.View.Lessons)) {
		lesson := pkg.View.Lessons[id]
		if !lessonSuits(pkg, lesson, category) {
			continue
		}
		l := engine.Lesson{ID: lessonID(module, id), Skills: skillsOf(module, lesson.Skills)}
		whole := true
		for _, seg := range lesson.Segments {
			if seg.Task == nil {
				continue
			}
			task := pkg.View.Tasks[*seg.Task]
			if !s.begins(pkg, task, category, aiOpen) {
				whole = false
				break
			}
			described, err := model.Describe(pkg, task)
			if err != nil {
				return engine.Module{}, false, err
			}
			l.Tasks = append(l.Tasks, described)
		}
		if whole {
			m.Lessons = append(m.Lessons, l)
		}
	}
	return m, narrowed, nil
}

// begins says whether this code begins the task of pkg for a learner of
// category now, as a beginning would: one that suits the learner and that AI
// may check, and, while the AI is closed to a new task, only one a tap checks
// (R11, R59). The pool, the lessons and the open runs the engine gets all go by
// it (R50).
func (s *Service) begins(pkg *modulepackage.Package, task formatv0.Task, category formatv0.AgeCategory, aiOpen bool) bool {
	return suits(pkg, task, category) && s.admitted(task, category, "") && (aiOpen || byTap(task))
}

// forAI says whether a task that does not begin is left out only for AI that is
// not admitted or closed to read it: one that suits the learner, with no rubric,
// on a sheet this release of tutor reads.
func forAI(pkg *modulepackage.Package, task formatv0.Task, category formatv0.AgeCategory) bool {
	return suits(pkg, task, category) && !rubric(task) && sheetRead(task)
}

// nextTask is the task of the first task segment of the lesson that has not
// ended, by the places of those that have; none when every one has, and the run
// waits only for "lesson done".
func nextTask(lesson formatv0.Lesson, ended []int) (formatv0.TaskID, bool) {
	for i, seg := range lesson.Segments {
		if seg.Task != nil && !slices.Contains(ended, i+1) {
			return *seg.Task, true
		}
	}
	return "", false
}

// waits says whether the open run runID of the lesson of pkg waits, for a
// learner of category with the AI open or not: whether this code does not begin
// the task of its next task segment now; and whether that is only for AI that
// is not admitted or closed. A lesson whose every task segment begins asks the
// database nothing.
func (s *Service) waits(ctx context.Context, tx pgx.Tx, profileID, runID string, pkg *modulepackage.Package, lesson formatv0.Lesson, category formatv0.AgeCategory, aiOpen bool) (wait, byAI bool, err error) {
	if !slices.ContainsFunc(lesson.Segments, func(seg formatv0.Segment) bool {
		return seg.Task != nil && !s.begins(pkg, pkg.View.Tasks[*seg.Task], category, aiOpen)
	}) {
		return false, false, nil
	}
	ended, err := store.EndedSegments(ctx, tx, profileID, runID)
	if err != nil {
		return false, false, err
	}
	id, ok := nextTask(lesson, ended)
	if !ok {
		return false, false, nil
	}
	task := pkg.View.Tasks[id]
	wait = !s.begins(pkg, task, category, aiOpen)
	return wait, wait && forAI(pkg, task, category), nil
}

// withoutLesson leaves the lesson of a run that waits out of the lessons the
// engine gets, so that it gives the lesson by none of its branches: one on the
// current version whose every segment begins would give the run that waits, on
// a package before, again (R13). Out of the lessons, it holds back none of the
// author's order that come after it, as the engine reads them: the run on a
// package before is a rare case, and its lesson waits with it.
func withoutLesson(in *engine.Input, lesson string) {
	for i := range in.Modules {
		in.Modules[i].Lessons = slices.DeleteFunc(in.Modules[i].Lessons, func(l engine.Lesson) bool { return l.ID == lesson })
	}
}

// itemTraps is the traps of the trap values of each item's checks, by full ID.
func itemTraps(pkg *modulepackage.Package, task formatv0.Task) []engine.ItemTraps {
	var out []engine.ItemTraps
	for i, item := range task.Items {
		var traps []string
		for _, slot := range verdict.AnswerSlots(item) {
			for _, v := range slot.Check.TrapValues {
				traps = append(traps, string(pkg.View.Manifest.ID)+"/trap/"+string(v.Trap))
			}
		}
		if len(traps) > 0 {
			slices.Sort(traps)
			out = append(out, engine.ItemTraps{Place: int32(i + 1), Traps: slices.Compact(traps)})
		}
	}
	return out
}

// skillsOf is the full IDs of a module's skills.
func skillsOf(module formatv0.ModuleID, ids []formatv0.SkillID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = model.SkillID(module, string(id))
	}
	return out
}

// translated is the skills of a module of the engine as the graph of its
// current version names them: a skill it retired goes to those that replaced
// it, and one it does not know to none (R08) — the skills of a run or an
// attempt on a package before.
func translated(graphs learnermodel.Graphs, skills []string) []string {
	var out []string
	for _, skill := range skills {
		mg, ok := graphs[model.ModuleOfSkill(skill)]
		if !ok || !mg.Current {
			continue
		}
		if _, ok := slices.BinarySearchFunc(mg.Graph.Skills, skill, func(s learnermodel.Skill, id string) int { return strings.Compare(s.ID, id) }); ok {
			out = append(out, skill)
			continue
		}
		if i, ok := slices.BinarySearchFunc(mg.Graph.Retired, skill, func(r learnermodel.RetiredSkill, id string) int { return strings.Compare(r.ID, id) }); ok {
			out = append(out, mg.Graph.Retired[i].ReplacedBy...)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// runEndsOf is each end reason of a lesson run of the schema in the engine's
// words; an open run's is the zero.
var runEndsOf = map[string]engine.RunEnd{
	store.RunPassed:      engine.RunPassed,
	store.RunUnchecked:   engine.RunUnchecked,
	store.RunNotSuitable: engine.RunNotSuitable,
	store.RunAbandoned:   engine.RunAbandoned,
}

// history fills in what the engine reads of the profile's records, in the
// modules of the engine ids: its lesson runs, the attempts of tasks with a
// rubric closed "Done", its last closed attempt, and the summary of what was
// handed out (R104, R107 spec). Skills of a package before are named as the
// current versions name them, and a run whose package left the image takes its
// lesson's skills from the current version, which holds the lesson by its ID.
//
// An open run whose next task segment this code does not begin now, for a
// learner of category with the AI open or not (begins), waits: the engine gets
// neither it nor its lesson, which it would give by its first branch however
// the lessons it got leave it out (В59 RUN.md). Once the segment begins again,
// the engine gets the run again. A run on a package the image no longer gives
// is no such run: opened, it ends "unchecked". history says too whether a run waits only for
// AI that is not admitted or closed, so that nothing to give says why.
func (s *Service) history(ctx context.Context, tx pgx.Tx, profileID string, category formatv0.AgeCategory, aiOpen bool, current map[string]*modulepackage.Package, graphs learnermodel.Graphs, in *engine.Input) (bool, error) {
	runs, err := store.ProfileRuns(ctx, tx, profileID)
	if err != nil {
		return false, err
	}
	held := false
	for _, r := range runs {
		if current[r.ModuleID] == nil {
			continue
		}
		pkg, ok := s.set.Packages[formatv0.Digest(r.PackageDigest)]
		if !ok {
			pkg = current[r.ModuleID]
		}
		lesson, ok := lessonOf(pkg, r.LessonID)
		if !ok {
			continue
		}
		run := engine.Run{Lesson: r.LessonID, Skills: translated(graphs, skillsOf(pkg.View.Manifest.ID, lesson.Skills))}
		if r.EndReason != nil {
			run.End, run.EndedAt = runEndsOf[*r.EndReason], r.EndedAt.Unix()
		} else if s.set.Status(formatv0.Digest(r.PackageDigest)) == packages.Given {
			// A run on a package the image no longer gives goes to the engine as it is:
			// opened, it ends "unchecked" (R13).
			wait, byAI, err := s.waits(ctx, tx, profileID, r.ID, pkg, lesson, category, aiOpen)
			if err != nil {
				return false, err
			}
			if wait {
				held = held || byAI
				withoutLesson(in, r.LessonID)
				continue
			}
		}
		in.Runs = append(in.Runs, run)
	}

	closed, err := store.ClosedAttempts(ctx, tx, profileID)
	if err != nil {
		return false, err
	}
	last := map[string]engine.SkillIssue{}
	for i, a := range closed {
		task := model.TaskID(formatv0.ModuleID(a.ModuleID), formatv0.TaskID(a.TaskID))
		if a.CloseReason == "done" {
			in.RubricDone = append(in.RubricDone, engine.RubricDone{Task: task, At: a.ClosedAt.Unix()})
		}
		results := s.results(a, graphs)
		if i == 0 {
			in.Last, in.HasLast = engine.Last{Task: task, Left: a.CloseReason == closeLeft, Results: results}, true
			if a.LessonID != nil {
				in.Last.Lesson, in.Last.InLesson = *a.LessonID, true
			}
		}
		code := engine.ReasonUnspecified
		if a.ReasonCode != nil {
			code = engine.ReasonCode(*a.ReasonCode)
		}
		for _, r := range results {
			if _, seen := last[r.Skill]; !seen {
				last[r.Skill] = engine.SkillIssue{Skill: r.Skill, LastCode: code, LastResult: r, HasLast: true}
			}
		}
	}
	issued, err := store.IssuedSkills(ctx, tx, profileID)
	if err != nil {
		return false, err
	}
	for _, is := range issued {
		for _, skill := range translated(graphs, []string{is.Skill}) {
			issue := last[skill]
			issue.Skill = skill
			if !issue.Issued || is.At.Unix() > issue.LastIssued {
				issue.LastIssued, issue.Issued = is.At.Unix(), true
			}
			last[skill] = issue
		}
	}
	for _, skill := range slices.Sorted(maps.Keys(last)) {
		in.Summary.Skills = append(in.Summary.Skills, last[skill])
	}
	counts, err := store.ChoicesByModule(ctx, tx, profileID)
	if err != nil {
		return false, err
	}
	for _, c := range counts {
		in.Summary.Modules = append(in.Summary.Modules, engine.ModuleChoices{Module: c.ModuleID, Choices: c.Choices})
	}
	solutions, err := store.SolutionsSent(ctx, tx, profileID)
	if err != nil {
		return false, err
	}
	for _, sol := range solutions {
		in.Summary.Tasks = append(in.Summary.Tasks, engine.TaskSolution{
			Task: model.TaskID(formatv0.ModuleID(sol.ModuleID), formatv0.TaskID(sol.TaskID)), SolutionSentAt: sol.At.Unix()})
	}
	return held, nil
}

// results is the closed attempt's result on each of its primary skills, by its
// evidence, as the current versions name them (engine.SkillResult): of the items
// a skill is primary in, how many got a first verdict and how many of those are
// right, and whether the solution was shown. An attempt with no evidence, or
// one that does not decode, has none.
func (s *Service) results(a store.ClosedAttempt, graphs learnermodel.Graphs) []engine.SkillResult {
	if a.Evidence == nil {
		return nil
	}
	e, err := learnermodel.DecodeEvidence(a.Evidence)
	if err != nil {
		return nil
	}
	shown := e.Outcome.Kind == learnermodel.OutcomeSolutionShown
	answered := map[string]engine.SkillResult{}
	for _, it := range e.Task.Items {
		primary := e.Task.Primary
		if it.OwnSkills {
			primary = it.Primary
		}
		var answer learnermodel.ItemAnswer
		for _, ans := range e.Items {
			if ans.Place == it.Place {
				answer = ans
			}
		}
		for _, skill := range translated(graphs, primary) {
			r := answered[skill]
			r.Skill, r.SolutionShown = skill, shown
			if answer.HasFirstVerdict {
				r.Answered++
				if answer.FirstVerdict == learnermodel.VerdictCorrect {
					r.FirstRight++
				}
			}
			answered[skill] = r
		}
	}
	var out []engine.SkillResult
	for _, skill := range slices.Sorted(maps.Keys(answered)) {
		out = append(out, answered[skill])
	}
	return out
}

// choiceRow is the row of the engine's choice c, made on the modules of the
// engine pkgs and the settled state, with what a replay needs (R06, R50): of a
// task or a lesson, its module, package and what it hands out; of nothing to
// give, none of them.
func (s *Service) choiceRow(c engine.Choice, pkgs []*modulepackage.Package, settled model.Settled, category formatv0.AgeCategory, mapSkill string, limited bool, by after) (store.NewChoice, error) {
	row := store.NewChoice{
		Kind: "nothing", ReasonCode: int(c.Reason), MapSkill: mapSkill, AgeCategory: string(category), PoolLimited: limited,
		Mark: s.model.Mark(), EngineVersion: s.engine, LastSeq: settled.LastSeq,
		AfterAttemptID: by.attemptID, AfterLessonRunID: by.runID,
	}
	if settled.Grade.Known {
		grade := int(settled.Grade.Value)
		row.Grade = &grade
	}
	if c.Kind == engine.ChoiceTask || c.Kind == engine.ChoiceLesson {
		pkg, kind, local, skills := chosenIn(pkgs, c)
		if pkg == nil {
			return store.NewChoice{}, fmt.Errorf("learning: the engine chose %s, which no module of the engine has", c.ID)
		}
		row.Kind, row.ModuleID, row.PackageDigest, row.IssuedSkills = kind, string(pkg.View.Manifest.ID), string(pkg.Digest), skills
		if kind == "task" {
			row.TaskID = local
		} else {
			row.LessonID = local
		}
	}
	// The graphs the learner model and the engine got: of the modules of the
	// journal and of the engine, by the digests of their current versions.
	seen := map[string]bool{}
	for id, mg := range settled.Graphs {
		if mg.Current {
			seen[id] = true
		}
	}
	for _, p := range pkgs {
		seen[string(p.View.Manifest.ID)] = true
		row.EngineDigests = append(row.EngineDigests, string(p.Digest))
	}
	for _, id := range slices.Sorted(maps.Keys(seen)) {
		if current, ok := s.set.Current(formatv0.ModuleID(id)); ok {
			row.Digests = append(row.Digests, string(current.Digest))
		}
	}
	slices.Sort(row.EngineDigests)
	return row, nil
}

// primarySkills is the primary skills of the task and of its items, by full ID:
// what a choice of it hands out.
func primarySkills(module formatv0.ModuleID, task formatv0.Task) []string {
	skills := skillsOf(module, task.Skills.Primary)
	for _, item := range task.Items {
		if item.Skills != nil {
			skills = append(skills, skillsOf(module, item.Skills.Primary)...)
		}
	}
	slices.Sort(skills)
	return slices.Compact(skills)
}

// nextChoice is the engine's choice that comes next after a close or the end of
// a run, with the skill the profile's last choice holds (R107 spec): a choice
// the engine could not make of its input gives none, and app asks "something
// else", so that the answer that closes is not refused for it.
func (s *Service) nextChoice(ctx context.Context, tx pgx.Tx, profileID string, category formatv0.AgeCategory, now time.Time, by after) (*v1.EngineChoice, error) {
	held, err := store.HeldMapSkill(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	pkgs, err := s.engineModules(ctx, tx, profileID, category)
	if err != nil {
		return nil, err
	}
	if held != "" && !knows(pkgs, held) {
		// The module of the skill was disconnected, or lost its version, since.
		held = ""
	}
	c, err := s.choose(ctx, tx, profileID, category, pkgs, held, now, by)
	if none := (*errNoChoice)(nil); errors.As(err, &none) {
		return nil, nil
	}
	return c, err
}

// madeChoice is a choice written, as its answer gives it: nothing to give with
// no ID; none when there is no choice, as when the engine could not make one.
func madeChoice(c store.MadeChoice, found bool) *v1.EngineChoice {
	switch {
	case !found:
		return nil
	case c.Kind == "nothing":
		return nothing(c)
	}
	return &v1.EngineChoice{ChoiceId: c.ID, Kind: choiceKinds[c.Kind]}
}

// ChooseNext is "something else", under its idempotency key (R50): the open
// attempt is closed "left" — or as the check of a request would close it — and
// the engine chooses what comes next, with the skill the learner chose on the
// skill map, or, with none, as it leads. A skill that no module of the engine
// has is "not found", and nothing is closed. The answer, the choice, is what the
// key's record holds: a repeat gets the same choice.
func (l *LearningService) ChooseNext(ctx context.Context, req *v1.ChooseNextRequest) (*v1.ChooseNextResponse, error) {
	s := l.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	profileID := req.GetProfileId()
	answer, err := rpc.Once(ctx, s.db, rpc.Key{Method: apiv1connect.LearningServiceChooseNextProcedure}, req,
		func(tx pgx.Tx, _ string) (*v1.ChooseNextResponse, store.KeyTarget, error) {
			now := s.clock()
			pkgs, err := s.engineModules(ctx, tx, profileID, category)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			if req.GetSkill() != "" && !knows(pkgs, req.GetSkill()) {
				return nil, store.KeyTarget{}, store.ErrNotFound
			}
			if err := s.leave(ctx, tx, profileID, now); err != nil {
				return nil, store.KeyTarget{}, err
			}
			choice, err := s.choose(ctx, tx, profileID, category, pkgs, req.GetSkill(), now, after{})
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			return &v1.ChooseNextResponse{Choice: choice}, store.KeyTarget{ProfileID: profileID}, nil
		})
	if err != nil {
		return nil, refused(ctx, err)
	}
	return answer, nil
}

// chosenIn is the package of the modules of the engine pkgs that holds what the
// engine chose, with the choice's kind in the schema's words, its local ID, and
// the skills it hands out; no package when none holds it, which the engine's
// rule never gives: it chooses from the pool and the lessons alone.
func chosenIn(pkgs []*modulepackage.Package, c engine.Choice) (pkg *modulepackage.Package, kind, local string, skills []string) {
	for _, p := range pkgs {
		module := p.View.Manifest.ID
		if id, ok := strings.CutPrefix(c.ID, model.TaskID(module, "")); ok && c.Kind == engine.ChoiceTask {
			if task, ok := p.View.Tasks[formatv0.TaskID(id)]; ok {
				return p, "task", id, primarySkills(module, task)
			}
		}
		if lesson, ok := lessonOf(p, c.ID); ok && c.Kind == engine.ChoiceLesson {
			return p, "lesson", string(lesson.ID), skillsOf(module, lesson.Skills)
		}
	}
	return nil, "", "", nil
}

// nothing is the choice c of nothing to give: when the closed AI left a task the
// code would begin with it out of the pool, or kept an open run waiting, with
// the outcome that says why —
// "not admitted", "quota" with when the next family day begins, or "platform
// limit"; with none for another reason (contract, EngineChoice). A choice
// written before the causes were kept is "not admitted".
func nothing(c store.MadeChoice) *v1.EngineChoice {
	out := &v1.EngineChoice{Kind: v1.ChoiceKind_CHOICE_KIND_NOTHING}
	if !c.NotAdmitted {
		return out
	}
	switch c.Closed {
	case store.NothingQuota:
		out.Outcome = &v1.Outcome{Cause: v1.Cause_CAUSE_QUOTA}
		if c.NextDay != nil {
			out.Outcome.NextFamilyDay = timestamppb.New(*c.NextDay)
		}
	case store.NothingPlatformLimit:
		out.Outcome = &v1.Outcome{Cause: v1.Cause_CAUSE_PLATFORM_LIMIT}
	default:
		out.Outcome = &v1.Outcome{Cause: v1.Cause_CAUSE_NOT_ADMITTED}
	}
	return out
}

// closedCause is how a choice of nothing to give keeps why the AI was closed: by
// the admission, or by what closed platform AI, with when the next family day
// begins beside the quota.
func closedCause(admitted bool, closed *v1.Outcome) (string, *time.Time) {
	switch {
	case !admitted:
		return store.NothingNotAdmitted, nil
	case closed.GetCause() == v1.Cause_CAUSE_QUOTA:
		next := closed.GetNextFamilyDay().AsTime()
		return store.NothingQuota, &next
	case closed.GetCause() == v1.Cause_CAUSE_PLATFORM_LIMIT:
		return store.NothingPlatformLimit, nil
	}
	return store.NothingNotAdmitted, nil
}
