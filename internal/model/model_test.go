package model_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	learnermodel "github.com/altnovatrail/learner-model"
	"github.com/altnovatrail/module-kit/modulepackage"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/model"
	"github.com/altnovatrail/api/internal/packages"
	"github.com/altnovatrail/api/internal/packages/packagestest"
	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

// version is the version of learner-model the tests' models name.
const version = "v0.1.6"

// The packages of the module choices, built once: 1.0.0, 0.9.0 and 2.0.0, which
// renames the skill adding summing and retires it.
var (
	builtOnce               sync.Once
	current, earlier, major *modulepackage.Package
)

func built(t *testing.T) {
	t.Helper()
	builtOnce.Do(func() {
		current, earlier = packagestest.Own(t, "choices"), packagestest.Own(t, "choices", "0.9.0")
		major = packagestest.OwnEdited(t, "choices", "2.0.0", func(path string, data []byte) []byte {
			data = bytes.ReplaceAll(data, []byte("adding"), []byte("summing"))
			if path == "skill-graph.yaml" {
				data = append(data, "retired_skills:\n  adding:\n    replaced_by: [summing]\n"...)
			}
			return data
		})
	})
}

// modelOf is the model of the entries' packages.
func modelOf(t *testing.T, entries ...packagestest.Entry) (*model.Model, *packages.Set) {
	t.Helper()
	set := packagestest.Set(t, entries...)
	m, err := model.New(set, version)
	if err != nil {
		t.Fatal(err)
	}
	return m, set
}

// base is the model of 1.0.0, current, and 0.9.0, withdrawn for a wrong key.
func base(t *testing.T) *model.Model {
	t.Helper()
	built(t)
	m, _ := modelOf(t, packagestest.Entry{Pkg: current, Release: "v0.2.0"},
		packagestest.Entry{Pkg: earlier, Release: "v0.1.0", Withdrawn: "wrong_key"})
	return m
}

// learner is a profile of a synthetic account, made with no row of the cache,
// as the image before this one made it.
type learner struct {
	pool    *pgxpool.Pool
	profile string
}

func newLearner(t *testing.T, pool *pgxpool.Pool, grade any) learner {
	t.Helper()
	var account, profile string
	ctx := t.Context()
	if err := pool.QueryRow(ctx, `INSERT INTO accounts (auth_uid, last_request_on) VALUES ($1, now()) RETURNING id`,
		"uid-"+uuid.NewString()).Scan(&account); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO profiles (account_id, owner, pseudonym, age_category, country,
		voicing_enabled, default_mode, ask_before_showing, active_on, grade)
		VALUES ($1, true, 'Owl', 'adult', 'US', false, 'self', false, now(), $2) RETURNING id`, account, grade).Scan(&profile); err != nil {
		t.Fatal(err)
	}
	return learner{pool: pool, profile: profile}
}

// slot is an answer slot's verdict in a submission's answer.
type slot struct{ verdict, reason, value string }

// answer is a submission's answer with wrong answers so far, of items each of
// whose slots is given: an item's verdict is its slots', as module-kit makes it.
func answer(wrong int32, items ...[]slot) []byte {
	a := &v1.SubmitWorkResponse{WrongAnswers: wrong}
	for i, slots := range items {
		item := &v1.ItemVerdict{Verdict: "correct"}
		for j, s := range slots {
			v := &v1.AnswerSlotVerdict{AnswerSlot: &observationv1.AnswerSlotPlace{Item: int32(i + 1), Slot: int32(j + 1)},
				Verdict: s.verdict, Reason: s.reason}
			if s.value != "" {
				v.Value = &s.value
			}
			item.AnswerSlots = append(item.AnswerSlots, v)
			switch {
			case s.verdict == "not_yet":
				item.Verdict = "not_yet"
			case s.verdict != "correct" && item.Verdict == "correct":
				item.Verdict = s.verdict
			}
		}
		a.Items = append(a.Items, item)
	}
	data, _ := proto.Marshal(a)
	return data
}

// closed is an attempt closed as given.
type closed struct {
	pkg      *modulepackage.Package
	digest   string
	task     string
	instance any
	reason   string
	wrong    int
	at       time.Time
	answers  [][]byte
	// what is set when not zero.
	stepTechniques, requestedTechniques, leadingQuestion bool
	explained                                            int
}

// attempt makes the closed attempt c of the learner, begun a minute before it
// closed, and gives its ID.
func (l learner) attempt(t *testing.T, c closed) string {
	t.Helper()
	ctx := t.Context()
	digest, version := c.digest, "1.0.0"
	if c.pkg != nil {
		digest, version = string(c.pkg.Digest), string(c.pkg.View.Manifest.Version)
	}
	var choice, id string
	if err := l.pool.QueryRow(ctx, `INSERT INTO engine_choices (profile_id, kind, module_id, task_id)
		VALUES ($1, 'task', 'example/choices', $2) RETURNING id`, l.profile, c.task).Scan(&choice); err != nil {
		t.Fatal(err)
	}
	var step, requested *time.Time
	if c.stepTechniques {
		step = &c.at
	}
	if c.requestedTechniques {
		requested = &c.at
	}
	var question *time.Time
	if c.leadingQuestion {
		question = &c.at
	}
	if err := l.pool.QueryRow(ctx, `INSERT INTO attempts (profile_id, package_digest, module_id, module_version,
			module_format_version, module_kit_version, task_id, seed, instance, choice_id, started_at, last_action_at,
			close_reason, closed_at, wrong_answers, step_techniques_at, requested_techniques_at, leading_question_at)
		VALUES ($1, $2, 'example/choices', $3, '0.4', 'v0.3.0', $4, 1, $5, $6, $7::timestamptz - interval '1 minute', $7,
			$8, $7, $9, $10, $11, $12) RETURNING id`,
		l.profile, digest, version, c.task, c.instance, choice, c.at, c.reason, c.wrong, step, requested, question).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for i, a := range c.answers {
		if _, err := l.pool.Exec(ctx, `INSERT INTO submissions (profile_id, attempt_id, number, answer) VALUES ($1, $2, $3, $4)`,
			l.profile, id, i+1, a); err != nil {
			t.Fatal(err)
		}
	}
	for rung := 1; rung <= c.explained; rung++ {
		if _, err := l.pool.Exec(ctx, `INSERT INTO attempt_rungs (profile_id, attempt_id, rung, source, explained_at, explained_source)
			VALUES ($1, $2, $3, 'fallback', now(), 'fallback')`, l.profile, id, rung); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

// settle settles the learner's evidence on m, in a transaction of its own.
func (l learner) settle(t *testing.T, m *model.Model) model.Settled {
	t.Helper()
	var s model.Settled
	if err := pgx.BeginFunc(t.Context(), l.pool, func(tx pgx.Tx) error {
		var err error
		s, err = m.Settle(t.Context(), tx, l.profile, time.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

// journal is the learner's journal, by attempt, and its rows in order.
func (l learner) journal(t *testing.T) (map[string]learnermodel.Evidence, []store.Entry) {
	t.Helper()
	entries, err := store.Journal(t.Context(), l.pool, l.profile, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]learnermodel.Evidence{}
	for _, e := range entries {
		ev, err := learnermodel.DecodeEvidence(e.Evidence)
		if err != nil {
			t.Fatal(err)
		}
		out[e.AttemptID] = ev
	}
	return out, entries
}

// cache is the learner's row of the cache.
func (l learner) cache(t *testing.T) store.SkillState {
	t.Helper()
	var s store.SkillState
	err := pgx.BeginFunc(t.Context(), l.pool, func(tx pgx.Tx) error {
		var err error
		s, err = store.HoldSkillState(t.Context(), tx, l.profile)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Each close gives the outcome its reason and wrong answers name, and a close
// with no evidence gives none: left, abandoned or not suitable with no wrong
// answer, and "done". The journal goes in the order the attempts closed, and
// the cache holds them all.
func TestEachCloseGivesItsOutcome(t *testing.T) {
	t.Parallel()

	m := base(t)
	l := newLearner(t, storetest.DB(t), nil)
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	want := map[string]learnermodel.Outcome{}
	none := map[string]bool{}
	for i, c := range []struct {
		reason string
		wrong  int
		want   *learnermodel.Outcome
	}{
		{"correct", 0, &learnermodel.Outcome{Kind: learnermodel.OutcomeFirstSubmission}},
		{"correct", 5, &learnermodel.Outcome{Kind: learnermodel.OutcomeAfterHint, Rung: 3, HasRung: true}},
		{"correct", 2, &learnermodel.Outcome{Kind: learnermodel.OutcomeAfterHint, Rung: 2, HasRung: true}},
		{"solution_shown", 4, &learnermodel.Outcome{Kind: learnermodel.OutcomeSolutionShown}},
		{"left", 1, &learnermodel.Outcome{Kind: learnermodel.OutcomeLeftUnfinished}},
		{"abandoned", 1, &learnermodel.Outcome{Kind: learnermodel.OutcomeLeftUnfinished}},
		{"not_suitable", 2, &learnermodel.Outcome{Kind: learnermodel.OutcomeLeftUnfinished}},
		{"unchecked", 0, &learnermodel.Outcome{Kind: learnermodel.OutcomeUnchecked}},
		{"left", 0, nil},
		{"abandoned", 0, nil},
		{"not_suitable", 0, nil},
		{"done", 0, nil},
	} {
		id := l.attempt(t, closed{pkg: current, task: "pick", reason: c.reason, wrong: c.wrong, at: at.Add(time.Duration(-i) * time.Minute)})
		if c.want == nil {
			none[id] = true
		} else {
			want[id] = *c.want
		}
	}
	// An attempt on a package the image does not hold owes none it could build.
	gone := l.attempt(t, closed{digest: "sha256:" + strings.Repeat("a", 64), task: "pick", reason: "correct", at: at})
	settled := l.settle(t, m)

	journal, entries := l.journal(t)
	if len(journal) != len(want) || journal[gone].Kind != 0 {
		t.Fatalf("the journal holds %d pieces of evidence, want %d", len(journal), len(want))
	}
	for id, outcome := range want {
		if got := journal[id].Outcome; got != outcome {
			t.Errorf("the outcome of %s = %+v, want %+v", id, got, outcome)
		}
	}
	for id := range none {
		if _, ok := journal[id]; ok {
			t.Errorf("the attempt %s has evidence, want none", id)
		}
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].Seq != entries[i-1].Seq+1 || entries[i].ClosedAt.Before(entries[i-1].ClosedAt) {
			t.Errorf("the journal is not numbered in the order the attempts closed: %+v", entries)
		}
	}
	if c := l.cache(t); c.LastSeq != int64(len(want)) || c.Mark.LearnerModel != version || c.Mark.Window != "0.4" || c.Mark.Release != "v9.0.0" {
		t.Errorf("the cache = %d %+v, want every piece taken under the image's mark", c.LastSeq, c.Mark)
	}
	if len(settled.State.Modules) != 1 || settled.State.Modules[0].Module != "example/choices" {
		t.Errorf("the state = %+v, want the module choices", settled.State)
	}
	// Owed once: a second settling writes nothing.
	l.settle(t, m)
	if again, _ := l.journal(t); len(again) != len(want) {
		t.Errorf("a second settling wrote %d pieces, want none", len(again)-len(want))
	}
}

// The evidence holds the facts of the attempt: the task with its skills and
// slots, the start band by the grade, what came of each item, the traps the
// program found with their skills, the techniques, "explain differently", and
// whether the solution was seen before; of a template, not that.
func TestTheEvidenceHoldsTheAttemptsFacts(t *testing.T) {
	t.Parallel()

	m := base(t)
	l := newLearner(t, storetest.DB(t), 2)
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	// pairs: the first answer is blank, the second wrong in a) by the trap
	// first-seen and right in b), the third right in both.
	first := l.attempt(t, closed{pkg: current, task: "pairs", reason: "correct", wrong: 1, at: at, answers: [][]byte{
		answer(0, []slot{{"unreadable", "blank", ""}}, []slot{{"unreadable", "blank", ""}}),
		answer(1, []slot{{"not_yet", "", "12"}}, []slot{{"correct", "", "30"}}),
		answer(1, []slot{{"correct", "", "21"}}, []slot{{"correct", "", "30"}}),
	}, stepTechniques: true, requestedTechniques: true, leadingQuestion: true, explained: 2})
	again := l.attempt(t, closed{pkg: current, task: "pairs", reason: "solution_shown", wrong: 3, at: at.Add(time.Minute)})
	template := l.attempt(t, closed{pkg: current, task: "greater", instance: 0, reason: "correct", at: at.Add(2 * time.Minute)})
	l.settle(t, m)
	journal, _ := l.journal(t)

	e := journal[first]
	if e.Task.ID != "example/choices/task/pairs" || e.Task.Template || e.Task.Band != (learnermodel.BandRef{Place: 1, ID: "grades-1-2"}) ||
		e.Task.Difficulty != 1 || len(e.Task.Primary) != 1 || e.Task.Primary[0] != "example/choices/skill/comparing" ||
		e.Task.KeyProvenance != learnermodel.ProvenanceAuthor || e.Task.SkillTagsProvenance != learnermodel.ProvenanceAuthor {
		t.Errorf("the task = %+v", e.Task)
	}
	if len(e.Task.Items) != 2 || e.Task.Items[0].Slots[0] != (learnermodel.Slot{Check: learnermodel.CheckKindChoice, Options: 2, HasOptions: true}) {
		t.Errorf("the items of the task = %+v", e.Task.Items)
	}
	if e.StartBand != (learnermodel.BandRef{Place: 1, ID: "grades-1-2"}) || e.PracticeTime != at.Unix() ||
		e.Kind != 2 || !e.LeadingQuestionOnRequest ||
		!e.TechniqueOnStep || !e.TechniqueOnRequest || e.ExplainAgainCount != 2 || !e.HasSolutionSeenFact || e.SolutionSeenBefore {
		t.Errorf("the evidence = %+v", e)
	}
	a, b := e.Items[0], e.Items[1]
	if !a.Submitted || len(a.Unverdicted) != 1 || a.Unverdicted[0] != learnermodel.UnverdictedUnreadableBlank ||
		a.FirstVerdict != learnermodel.VerdictNotYet || a.WrongBeforeFirst != 0 || a.FinalVerdict != learnermodel.VerdictCorrect ||
		!a.HasFirstCorrect || a.WrongBeforeFirstCorrect != 1 {
		t.Errorf("item a) = %+v", a)
	}
	if b.FirstVerdict != learnermodel.VerdictCorrect || b.WrongBeforeFirstCorrect != 0 || !b.HasFirstCorrect {
		t.Errorf("item b) = %+v", b)
	}
	if len(e.Traps) != 1 || e.Traps[0].ID != "example/choices/trap/first-seen" || len(e.Traps[0].Skills) != 1 ||
		e.Traps[0].Skills[0] != "example/choices/skill/comparing" || len(e.Traps[0].ItemPlaces) != 1 || e.Traps[0].ItemPlaces[0] != 1 {
		t.Errorf("the traps = %+v", e.Traps)
	}
	if !journal[again].SolutionSeenBefore || len(journal[again].Items) != 2 || journal[again].Items[0].Submitted {
		t.Errorf("the attempt after the task was solved = %+v, want its solution seen before and nothing submitted", journal[again])
	}
	if g := journal[template]; !g.Task.Template || g.Task.ID != "example/choices/task/greater" ||
		g.Task.KeyProvenance != learnermodel.ProvenanceSolver || g.HasSolutionSeenFact {
		t.Errorf("the template's evidence = %+v", g)
	}
}

// A slot whose check is a time of day, from format 0.4, is written as its own
// kind, and one with a rubric, which learner-model does not know, as a kind not
// given.
func TestATimeOfDayIsItsOwnKind(t *testing.T) {
	t.Parallel()

	built(t)
	task := current.View.Tasks["apples"]
	slots := []formatv0.AnswerSlot{task.Items[0].AnswerSlots[0]}
	slots[0].Check.Kind = formatv0.CheckKindTime
	task.Items = []formatv0.Item{{AnswerSlots: slots}}
	got, err := model.Describe(current, task)
	if err != nil || got.Items[0].Slots[0] != (learnermodel.Slot{Check: learnermodel.CheckKindTime}) {
		t.Errorf("Describe = %+v, %v; want a time of day", got.Items, err)
	}
	slots[0].Check.Kind = formatv0.CheckKindRubric
	if got, err := model.Describe(current, task); err != nil || got.Items[0].Slots[0] != (learnermodel.Slot{Check: learnermodel.CheckKindUnspecified}) {
		t.Errorf("Describe = %+v, %v; want a kind not given", got.Items, err)
	}
	task.Band = "nowhere"
	if _, err := model.Describe(current, task); err == nil {
		t.Error("a task in no band of the manifest was described")
	}
}

// The cache appended piece by piece is the state recomputed of the journal;
// an older mark recomputes it, a newer one is not written over, and a tail the
// cache has not taken is appended before the new evidence.
func TestTheCacheIsTheJournalFolded(t *testing.T) {
	t.Parallel()

	m := base(t)
	pool := storetest.DB(t)
	l := newLearner(t, pool, 1)
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	for i, task := range []string{"pick", "pairs", "apples", "pick"} {
		l.attempt(t, closed{pkg: current, task: task, reason: "correct", wrong: i % 2, at: at.Add(time.Duration(i) * time.Minute), answers: right(task)})
		l.settle(t, m)
	}
	appended := l.cache(t)

	if _, err := pool.Exec(t.Context(), `UPDATE skill_states SET learner_model_version = 'v0.1.3' WHERE profile_id = $1`, l.profile); err != nil {
		t.Fatal(err)
	}
	l.settle(t, m)
	if recomputed := l.cache(t); !bytes.Equal(recomputed.State, appended.State) || recomputed.LastSeq != 4 || recomputed.Mark.LearnerModel != version {
		t.Errorf("the state recomputed under the image's mark differs from the state appended")
	}

	empty, err := learnermodel.EncodeState(learnermodel.State{Kind: learnermodel.StateKind})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE skill_states SET state = $2, last_seq = 0 WHERE profile_id = $1`, l.profile, empty); err != nil {
		t.Fatal(err)
	}
	l.settle(t, m)
	if tail := l.cache(t); !bytes.Equal(tail.State, appended.State) || tail.LastSeq != 4 {
		t.Errorf("the tail appended gives another state, or the cache took %d", tail.LastSeq)
	}

	// The image after this one wrote the cache: the evidence goes to the journal,
	// and the cache stays as that image wrote it.
	if _, err := pool.Exec(t.Context(), `UPDATE skill_states SET modules_release = 'v10.0.0' WHERE profile_id = $1`, l.profile); err != nil {
		t.Fatal(err)
	}
	l.attempt(t, closed{pkg: current, task: "apples", reason: "correct", at: at.Add(time.Hour), answers: right("apples")})
	settled := l.settle(t, m)
	if newer := l.cache(t); newer.LastSeq != 4 || newer.Mark.Release != "v10.0.0" || !bytes.Equal(newer.State, appended.State) {
		t.Errorf("a newer mark was written over: %d %+v", newer.LastSeq, newer.Mark)
	}
	if journal, _ := l.journal(t); len(journal) != 5 || len(settled.State.Modules) != 1 || settled.State.Modules[0].Skills == nil {
		t.Errorf("the journal holds %d, and the state for the answer %+v", len(journal), settled.State)
	}
}

// The evidence of a version withdrawn for a wrong key is in the journal and not
// in the state, appended or recomputed; a module with no current version keeps
// its evidence waiting.
func TestAWrongKeyIsLeftOutAndAModuleGoneWaits(t *testing.T) {
	t.Parallel()

	m := base(t)
	pool := storetest.DB(t)
	l := newLearner(t, pool, nil)
	at := time.Now().UTC().Add(-time.Hour)
	l.attempt(t, closed{pkg: earlier, task: "pick", reason: "correct", at: at, answers: right("pick")})
	settled := l.settle(t, m)
	if journal, _ := l.journal(t); len(journal) != 1 || len(settled.State.Modules) != 0 {
		t.Errorf("the journal holds %d and the state %+v, want the evidence kept and left out", len(journal), settled.State)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE skill_states SET learner_model_version = 'v0.1.3' WHERE profile_id = $1`, l.profile); err != nil {
		t.Fatal(err)
	}
	if recomputed := l.settle(t, m); len(recomputed.State.Modules) != 0 {
		t.Errorf("the recomputed state = %+v, want the wrong key left out", recomputed.State)
	}

	built(t)
	leaked, _ := modelOf(t, packagestest.Entry{Pkg: current, Release: "v0.2.0", Withdrawn: "answer_leak"})
	w := newLearner(t, pool, nil)
	w.attempt(t, closed{pkg: current, task: "pick", reason: "unchecked", at: at})
	waits := w.settle(t, leaked)
	if journal, _ := w.journal(t); len(journal) != 1 || waits.Graphs["example/choices"].Current || len(waits.State.Modules) != 0 {
		t.Errorf("the module with no current version: journal %d, state %+v", len(journal), waits.State)
	}
}

// Under a major version that renamed adding summing, the evidence of adding on
// the earlier version goes to summing, appended or recomputed (R08).
func TestTheProgressMovesByTheRetiredSkills(t *testing.T) {
	t.Parallel()

	built(t)
	m, _ := modelOf(t, packagestest.Entry{Pkg: current, Release: "v0.2.0"}, packagestest.Entry{Pkg: major, Release: "v0.3.0"})
	l := newLearner(t, storetest.DB(t), nil)
	l.attempt(t, closed{pkg: current, task: "apples", reason: "correct", at: time.Now().Add(-time.Hour), answers: right("apples")})
	settled := l.settle(t, m)
	read, err := m.Read(settled, time.Now(), []string{"example/choices"})
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Modules) != 1 || len(read.Modules[0].Skills) != 1 || read.Modules[0].Skills[0].Skill != "example/choices/skill/summing" {
		t.Errorf("the readout = %+v, want the evidence of adding at summing", read.Modules)
	}
	journal, _ := l.journal(t)
	for _, e := range journal {
		if e.Task.Primary[0] != "example/choices/skill/adding" {
			t.Errorf("the journal names %v, want the skill the attempt was of", e.Task.Primary)
		}
	}
}

// A binary that recorded no version of learner-model has no model.
func TestAModelNamesItsVersion(t *testing.T) {
	t.Parallel()

	built(t)
	if _, err := model.New(packagestest.Set(t, packagestest.Entry{Pkg: current, Release: "v0.2.0"}), ""); err == nil {
		t.Error("a model with no version of learner-model was made")
	}
}

// right is the answers of an attempt of the task given right at the first
// submission.
func right(task string) [][]byte {
	switch task {
	case "pairs":
		return [][]byte{answer(0, []slot{{"correct", "", "21"}}, []slot{{"correct", "", "30"}})}
	case "apples":
		return [][]byte{answer(0, []slot{{"correct", "", "7"}})}
	}
	return [][]byte{answer(0, []slot{{"correct", "", "three and four"}})}
}

// A trap repeats when two closed attempts found it, those of a version withdrawn
// for a wrong key not counted, with the texts of the version of the last; one
// whose last version left the image is left out (Д27).
func TestATrapRepeatsInTwoAttempts(t *testing.T) {
	t.Parallel()

	m := base(t)
	l := newLearner(t, storetest.DB(t), nil)
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	wrong := [][]byte{answer(1, []slot{{"not_yet", "", "five"}}), answer(1, []slot{{"correct", "", "three and four"}})}
	for i, pkg := range []*modulepackage.Package{current, earlier, current} {
		l.attempt(t, closed{pkg: pkg, task: "pick", reason: "correct", wrong: 1, at: at.Add(time.Duration(i) * time.Minute), answers: wrong})
	}
	l.settle(t, m)
	_, journal := l.journal(t)
	traps, err := m.RepeatingTraps(journal)
	if err != nil {
		t.Fatal(err)
	}
	if len(traps) != 1 || traps[0].ID != "example/choices/trap/first-seen" || traps[0].Attempts != 2 ||
		!traps[0].LastClosedAt.Equal(at.Add(2*time.Minute)) || traps[0].Advice != "Look at both numbers before you choose." ||
		!strings.HasPrefix(traps[0].Description, "Chose the first number") {
		t.Errorf("the traps that repeat = %+v", traps)
	}

	moved, _ := modelOf(t, packagestest.Entry{Pkg: major, Release: "v0.3.0"})
	if gone, err := moved.RepeatingTraps(journal); err != nil || len(gone) != 0 {
		t.Errorf("the traps of versions gone from the image = %+v, %v; want none", gone, err)
	}
	if _, err := m.RepeatingTraps([]store.Entry{{Seq: 1, Evidence: []byte{0xff}}}); err == nil {
		t.Error("evidence that does not decode gave traps")
	}
}

// failingTx is a transaction whose statements that hold failOn fail.
type failingTx struct {
	pgx.Tx
	failOn string
}

var errDown = errors.New("the test's database is down")

type failedRow struct{}

func (failedRow) Scan(...any) error { return errDown }

func (f failingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, f.failOn) {
		return pgconn.CommandTag{}, errDown
	}
	return f.Tx.Exec(ctx, sql, args...)
}

func (f failingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, f.failOn) {
		return failedRow{}
	}
	return f.Tx.QueryRow(ctx, sql, args...)
}

func (f failingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, f.failOn) {
		return nil, errDown
	}
	return f.Tx.Query(ctx, sql, args...)
}

// A statement of the settling that fails is its failure, at each step.
func TestASettlingFailsWithItsStatements(t *testing.T) {
	t.Parallel()

	m := base(t)
	pool := storetest.DB(t)
	l := newLearner(t, pool, nil)
	l.attempt(t, closed{pkg: current, task: "pick", reason: "correct", at: time.Now().Add(-time.Hour), answers: right("pick")})
	for _, failOn := range []string{"FROM profiles WHERE id = $1 FOR KEY SHARE", "INSERT INTO skill_states", "SELECT grade", "FROM attempts a WHERE", "DISTINCT module_id",
		"FROM evidence e JOIN", "INSERT INTO evidence", "UPDATE skill_states"} {
		tx, err := pool.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.Settle(t.Context(), failingTx{Tx: tx, failOn: failOn}, l.profile, time.Now()); !errors.Is(err, errDown) {
			t.Errorf("on %q: %v, want the failure", failOn, err)
		}
		_ = tx.Rollback(t.Context())
	}
}

// What cannot be read or built stops nothing: a cache that does not decode is
// recomputed; a piece of the journal that does not decode, or that the fold
// refuses, is left out of the state, recomputed or appended; an attempt of a
// task the package does not have, with a submission that does not decode or of
// an instance the template does not have stays owed, unwritten. An attempt of a
// task with a rubric owes none; a module of the engine that is none cannot be
// read.
func TestWhatCannotBeReadStopsNothing(t *testing.T) {
	t.Parallel()

	m := base(t)
	pool := storetest.DB(t)
	at := time.Now().UTC().Add(-time.Hour)

	cached := newLearner(t, pool, nil)
	cached.attempt(t, closed{pkg: current, task: "pick", reason: "correct", at: at, answers: right("pick")})
	cached.settle(t, m)
	appended := cached.cache(t)
	if _, err := pool.Exec(t.Context(), `UPDATE skill_states SET state = '\xff' WHERE profile_id = $1`, cached.profile); err != nil {
		t.Fatal(err)
	}
	cached.settle(t, m)
	if again := cached.cache(t); !bytes.Equal(again.State, appended.State) || again.LastSeq != 1 {
		t.Error("a cache that does not decode was not recomputed")
	}

	refused, err := learnermodel.EncodeEvidence(learnermodel.Evidence{Kind: learnermodel.EvidenceKind,
		Task:      learnermodel.Task{ID: "nowhere", Difficulty: 1, Band: learnermodel.BandRef{Place: 1, ID: "b"}},
		StartBand: learnermodel.BandRef{Place: 1, ID: "b"}, Outcome: learnermodel.Outcome{Kind: learnermodel.OutcomeUnchecked}})
	if err != nil {
		t.Fatal(err)
	}
	empty, _ := learnermodel.EncodeState(learnermodel.State{Kind: learnermodel.StateKind})
	for _, bytes := range [][]byte{{0xff}, refused} {
		l := newLearner(t, pool, nil)
		a := l.attempt(t, closed{pkg: current, task: "pick", reason: "done", at: at})
		if _, err := pool.Exec(t.Context(), `INSERT INTO evidence (profile_id, seq, attempt_id, package_digest, module_id, evidence)
			VALUES ($1, 1, $2, $3, 'example/choices', $4)`, l.profile, a, current.Digest, bytes); err != nil {
			t.Fatal(err)
		}
		if s := l.settle(t, m); len(s.State.Modules) != 0 || l.cache(t).LastSeq != 1 {
			t.Errorf("the journal of %x recomputed = %+v, want it left out", bytes, s.State)
		}
		// Under the image's mark, the tail is appended, and left out the same.
		if _, err := pool.Exec(t.Context(), `UPDATE skill_states SET state = $2, last_seq = 0 WHERE profile_id = $1`, l.profile, empty); err != nil {
			t.Fatal(err)
		}
		if s := l.settle(t, m); len(s.State.Modules) != 0 || l.cache(t).LastSeq != 1 {
			t.Errorf("the tail of %x appended = %+v, want it left out", bytes, s.State)
		}
	}

	for _, c := range []closed{
		{pkg: current, task: "nosuch", reason: "correct", at: at},
		{pkg: current, task: "pick", reason: "correct", at: at, answers: [][]byte{{0xff}}},
		{pkg: current, task: "greater", instance: 99, reason: "correct", at: at},
	} {
		l := newLearner(t, pool, nil)
		l.attempt(t, c)
		if s := l.settle(t, m); s.Written != 0 {
			t.Errorf("the evidence of %+v was written", c)
		}
		if owing, err := m.Owing(t.Context(), pool, "", 1000); err != nil || !slices.Contains(owing, l.profile) {
			t.Errorf("the attempt of %+v is no longer owed: %v, %v", c, owing, err)
		}
	}

	essay := newLearner(t, pool, nil)
	essay.attempt(t, closed{pkg: current, task: "story", reason: "unchecked", at: at})
	settled := essay.settle(t, m)
	if journal, _ := essay.journal(t); len(journal) != 0 || settled.Written != 0 {
		t.Errorf("the attempt of a task with a rubric has evidence: %+v", journal)
	}
	if _, err := m.Read(settled, time.Now(), []string{"none"}); err == nil {
		t.Error("a module of the engine that is no module was read")
	}
}

// What an item gave with no verdict is in its evidence, in order: unchecked,
// then unreadable for another reason than blank. A submission's item the task
// has not, a slot at no place of it and a wrong value that matches no trap give
// nothing more.
func TestAnItemWithNoVerdictKeepsItsReasons(t *testing.T) {
	t.Parallel()

	m := base(t)
	l := newLearner(t, storetest.DB(t), nil)
	place := func(item, slot int32) *observationv1.AnswerSlotPlace {
		return &observationv1.AnswerSlotPlace{Item: item, Slot: slot}
	}
	zero := "zero"
	answers := []*v1.SubmitWorkResponse{
		{Items: []*v1.ItemVerdict{{Verdict: "unchecked", AnswerSlots: []*v1.AnswerSlotVerdict{{AnswerSlot: place(1, 1), Verdict: "unchecked"}}}}},
		{Items: []*v1.ItemVerdict{
			{Verdict: "unreadable", AnswerSlots: []*v1.AnswerSlotVerdict{{AnswerSlot: place(1, 1), Verdict: "unreadable", Reason: "illegible"}}},
			{Verdict: "correct"},
		}},
		{WrongAnswers: 1, Items: []*v1.ItemVerdict{{Verdict: "not_yet", AnswerSlots: []*v1.AnswerSlotVerdict{
			{AnswerSlot: place(1, 1), Verdict: "not_yet", Value: &zero}, {AnswerSlot: place(5, 1), Verdict: "not_yet", Value: &zero},
		}}}},
	}
	var written [][]byte
	for _, a := range answers {
		data, _ := proto.Marshal(a)
		written = append(written, data)
	}
	id := l.attempt(t, closed{pkg: current, task: "pick", reason: "left", wrong: 1, at: time.Now().Add(-time.Hour), answers: written})
	l.settle(t, m)
	journal, _ := l.journal(t)
	item := journal[id].Items[0]
	if len(journal[id].Items) != 1 || len(item.Unverdicted) != 2 || item.Unverdicted[0] != learnermodel.UnverdictedUnchecked ||
		item.Unverdicted[1] != learnermodel.UnverdictedUnreadableOther || item.FirstVerdict != learnermodel.VerdictNotYet ||
		item.WrongBeforeFirst != 0 || len(journal[id].Traps) != 0 {
		t.Errorf("the evidence = %+v", journal[id])
	}
}
