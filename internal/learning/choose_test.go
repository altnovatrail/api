package learning_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/rpc"
)

// chooseNext asks "something else" for the profile under a new key, with the
// skill chosen on the map when given.
func (w *world) chooseNext(l learner, profile string, skill ...string) (*v1.EngineChoice, error) {
	req := &v1.ChooseNextRequest{ProfileId: profile, IdempotencyKey: uuid.NewString()}
	if len(skill) > 0 {
		req.Skill = skill[0]
	}
	res, err := w.learning.ChooseNext(l.ctx, req)
	return res.GetChoice(), err
}

// started begins what the choice gives: a task by BeginAttempt, or a lesson by
// OpenLesson and its first task segment; it gives the attempt.
func (w *world) started(t *testing.T, l learner, profile string, c *v1.EngineChoice) string {
	t.Helper()
	switch c.GetKind() {
	case v1.ChoiceKind_CHOICE_KIND_TASK:
		res, err := w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: profile, IdempotencyKey: uuid.NewString(),
			Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: c.GetChoiceId()}})
		if err != nil {
			t.Fatalf("BeginAttempt by the choice %v: %v", c, err)
		}
		return res.GetAttempt().GetAttemptId()
	case v1.ChoiceKind_CHOICE_KIND_LESSON:
		res, err := w.learning.OpenLesson(l.ctx, &v1.OpenLessonRequest{ProfileId: profile, ChoiceId: c.GetChoiceId()})
		if err != nil {
			t.Fatalf("OpenLesson by the choice %v: %v", c, err)
		}
		run := res.GetRun()
		if open := run.GetOpenAttempt(); open != nil {
			return open.GetAttemptId()
		}
		for place := int32(1); place <= 20; place++ {
			if a, err := w.segment(l, profile, run.GetLessonRunId(), place); err == nil {
				return a.GetAttemptId()
			}
		}
	}
	t.Fatalf("the choice %v gives nothing to begin", c)
	return ""
}

// choiceRow is what the row of a choice keeps that the tests read.
type choiceRow struct {
	kind, ageCategory, engineVersion, release, window string
	reason                                            *int
	mapSkill, packageDigest, afterAttempt, afterRun   *string
	issued, digests, engineDigests                    []string
	poolLimited                                       *bool
	lastSeq                                           *int64
	grade                                             *int
}

func (w *world) choiceRow(t *testing.T, id string) choiceRow {
	t.Helper()
	var r choiceRow
	var category, engine, release, window *string
	if err := w.pool.QueryRow(t.Context(), `SELECT kind, reason_code, map_skill, package_digest, after_attempt_id::text,
			after_lesson_run_id::text, issued_skills, digests, engine_digests, pool_limited, last_seq, age_category,
			engine_version, modules_release, format_window, grade
		FROM engine_choices WHERE id = $1`, id).Scan(&r.kind, &r.reason, &r.mapSkill, &r.packageDigest, &r.afterAttempt,
		&r.afterRun, &r.issued, &r.digests, &r.engineDigests, &r.poolLimited, &r.lastSeq, &category, &engine, &release,
		&window, &r.grade); err != nil {
		t.Fatal(err)
	}
	for to, from := range map[*string]*string{&r.ageCategory: category, &r.engineVersion: engine, &r.release: release, &r.window: window} {
		if from != nil {
			*to = *from
		}
	}
	return r
}

// "Something else" closes the open attempt "left" and gives the engine's choice,
// written with all a replay needs, which BeginAttempt or OpenLesson begins; a
// repeat under its key gets the same choice, and a new key a new one.
func TestChooseNextGivesTheEnginesChoice(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	// The records the engine reads besides: an attempt with a rubric closed
	// "Done", and a run on a package the image does not hold, which it does not.
	w.exec(t, `UPDATE profiles SET grade = 2 WHERE id = $1`, l.child)
	essay := w.attempt(t, l.child, string(w.current.Digest), "story")
	w.exec(t, `UPDATE attempts SET close_reason = 'done', closed_at = now() WHERE id = $1`, essay)
	w.exec(t, `INSERT INTO lesson_runs (profile_id, lesson_id, module_id, module_version, package_digest, choice_id, end_reason, ended_at)
		VALUES ($1, $2, $3, '0.8.0', $4, $5, 'abandoned', now())`, l.child, module+"/lesson/basics", module,
		"sha256:"+strings.Repeat("0", 64), w.lessonChoice(t, l.child, "basics"))
	open := w.begin(t, l, l.child, "pick").GetAttemptId()
	req := &v1.ChooseNextRequest{ProfileId: l.child, IdempotencyKey: uuid.NewString()}
	res, err := w.learning.ChooseNext(l.ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	c := res.GetChoice()
	if c.GetChoiceId() == "" || (c.GetKind() != v1.ChoiceKind_CHOICE_KIND_TASK && c.GetKind() != v1.ChoiceKind_CHOICE_KIND_LESSON) {
		t.Fatalf("the choice = %v, want a task or a lesson", c)
	}
	if got := w.closeOf(t, open); got != "left" {
		t.Errorf("the open attempt is closed %q, want left", got)
	}
	row := w.choiceRow(t, c.GetChoiceId())
	if row.reason == nil || *row.reason < 1 || *row.reason > 9 || row.mapSkill != nil || *row.packageDigest != string(w.current.Digest) ||
		len(row.issued) == 0 || len(row.engineDigests) != 1 || row.engineDigests[0] != string(w.current.Digest) ||
		len(row.digests) != 1 || !*row.poolLimited || row.ageCategory != "under_13" || row.engineVersion == "" ||
		row.release != "v9.0.0" || row.window != "0.4" || row.lastSeq == nil || row.afterAttempt != nil || row.afterRun != nil ||
		row.grade == nil || *row.grade != 2 {
		t.Errorf("the row of the choice = %+v", row)
	}
	again, err := w.learning.ChooseNext(l.ctx, req)
	if err != nil || again.GetChoice().GetChoiceId() != c.GetChoiceId() {
		t.Errorf("the repeat = %v, %v; want the same choice", again, err)
	}
	w.started(t, l, l.child, c)
	if other, err := w.chooseNext(l, l.child); err != nil || other.GetChoiceId() == c.GetChoiceId() {
		t.Errorf("a new key = %v, %v; want a new choice", other, err)
	}
}

// A skill chosen on the skill map gives a choice on it, and holds in what comes
// next until "something else" asks without it; a skill no module of the engine
// has is "not found", and closes nothing.
func TestASkillChosenOnTheMapHolds(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	comparing := module + "/skill/comparing"
	open := w.begin(t, l, l.adult, "pick").GetAttemptId()
	_, err := w.chooseNext(l, l.adult, module+"/skill/none")
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	if got := w.closeOf(t, open); got != "" {
		t.Errorf("a refused choice closed the open attempt %q", got)
	}
	chosen, err := w.chooseNext(l, l.adult, comparing)
	if err != nil {
		t.Fatal(err)
	}
	if row := w.choiceRow(t, chosen.GetChoiceId()); *row.reason != 6 || row.mapSkill == nil || *row.mapSkill != comparing {
		t.Errorf("the choice from the map = %+v, want SKILL_CHOSEN with the skill", row)
	}
	// What comes next after a task outside a lesson holds the skill.
	pick := w.begin(t, l, l.adult, "pick").GetAttemptId()
	solved, err := w.submit(l, l.adult, pick, tap(1, 1, "three and four"))
	if err != nil {
		t.Fatal(err)
	}
	next := solved.GetNext().GetChoice()
	if row := w.choiceRow(t, next.GetChoiceId()); row.mapSkill == nil || *row.mapSkill != comparing || row.afterAttempt == nil || *row.afterAttempt != pick {
		t.Errorf("what came next = %+v, want the skill held, after the attempt", row)
	}
	// A skill its module no longer has holds no more.
	w.exec(t, `UPDATE engine_choices SET map_skill = $2 WHERE id = $1`, next.GetChoiceId(), module+"/skill/gone")
	again := w.begin(t, l, l.adult, "pairs").GetAttemptId()
	later, err := w.submit(l, l.adult, again, tap(1, 1, "21"), tap(2, 1, "30"))
	if err != nil {
		t.Fatal(err)
	}
	if row := w.choiceRow(t, later.GetNext().GetChoice().GetChoiceId()); row.mapSkill != nil {
		t.Errorf("what came next after a skill gone = %+v, want the engine leading", row)
	}
	led, err := w.chooseNext(l, l.adult)
	if err != nil {
		t.Fatal(err)
	}
	if row := w.choiceRow(t, led.GetChoiceId()); row.mapSkill != nil {
		t.Errorf("something else without a skill = %+v, want the engine leading", row)
	}

	// Nothing to give holds too: something else without a skill that gives nothing
	// gives the lead back all the same.
	if _, err := w.chooseNext(l, l.adult, comparing); err != nil {
		t.Fatal(err)
	}
	w.exec(t, `DELETE FROM connected_modules WHERE profile_id = $1`, l.adult)
	if c, err := w.chooseNext(l, l.adult); err != nil || c.GetKind() != v1.ChoiceKind_CHOICE_KIND_NOTHING || c.GetChoiceId() != "" || c.GetOutcome() != nil {
		t.Fatalf("something else with no module = %v, %v; want nothing, with no ID and no outcome", c, err)
	}
	w.exec(t, `INSERT INTO connected_modules (profile_id, module_id) VALUES ($1, $2)`, l.adult, module)
	last := w.begin(t, l, l.adult, "pick").GetAttemptId()
	afterNothing, err := w.submit(l, l.adult, last, tap(1, 1, "three and four"))
	if err != nil {
		t.Fatal(err)
	}
	if row := w.choiceRow(t, afterNothing.GetNext().GetChoice().GetChoiceId()); row.mapSkill != nil {
		t.Errorf("what came next after nothing to give = %+v, want the engine leading", row)
	}
}

// What comes next after a task outside a lesson closed correct or with its
// solution shown is the engine's choice; a repeat of the submission gets the
// same choice; and the progress names when the engine chose each attempt's task,
// or its run's lesson, and why — none for one begun by a choice written before.
func TestWhatComesNextOutsideALessonIsTheEnginesChoice(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	first, err := w.chooseNext(l, l.adult)
	if err != nil {
		t.Fatal(err)
	}
	chosen := w.started(t, l, l.adult, first)

	pick := w.begin(t, l, l.adult, "pick").GetAttemptId()
	req := &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: pick, IdempotencyKey: uuid.NewString(),
		TappedChoices: []*v1.TappedChoice{tap(1, 1, "three and four")}}
	solved, err := w.attempts.SubmitWork(l.ctx, req)
	if err != nil || solved.GetNext().GetChoice().GetChoiceId() == "" {
		t.Fatalf("the correct answer = %v (%v), want the engine's choice next", solved, err)
	}
	again, err := w.attempts.SubmitWork(l.ctx, req)
	if err != nil || again.GetNext().GetChoice().GetChoiceId() != solved.GetNext().GetChoice().GetChoiceId() {
		t.Errorf("the repeat = %v (%v), want the same choice", again, err)
	}

	shown := w.begin(t, l, l.adult, "pairs").GetAttemptId()
	res, err := w.attempts.ShowSolution(l.ctx, &v1.ShowSolutionRequest{ProfileId: l.adult, AttemptId: shown,
		IdempotencyKey: uuid.NewString(), Action: v1.Action_ACTION_LEARNING})
	if err != nil || res.GetNext().GetChoice().GetChoiceId() == "" {
		t.Errorf("the solution shown = %v (%v), want the engine's choice next", res, err)
	}

	page, err := w.progress.GetProgress(l.ctx, &v1.GetProgressRequest{ProfileId: l.adult})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range page.GetAttempts() {
		w.checkChosen(t, a, chosen, pick)
	}
}

// checkChosen checks that the attempt record names when its choice was made and
// why, but the attempt begun by the test's own choice, written before the codes.
func (w *world) checkChosen(t *testing.T, a *v1.AttemptRecord, chosen, byTest string) {
	t.Helper()
	var id string
	if err := w.pool.QueryRow(t.Context(), `SELECT id FROM attempts WHERE task_id = $1 AND started_at = $2 LIMIT 1`,
		a.GetTask()[len(module+"/task/"):], a.GetBegunAt().AsTime()).Scan(&id); err != nil {
		t.Fatal(err)
	}
	switch id {
	case chosen:
		if a.GetChosenAt() == nil || a.GetReasonCode() == v1.ReasonCode_REASON_CODE_UNSPECIFIED {
			t.Errorf("the attempt by the engine's choice = %v, want when and why", a)
		}
	case byTest:
		if a.GetChosenAt() != nil || a.GetReasonCode() != v1.ReasonCode_REASON_CODE_UNSPECIFIED {
			t.Errorf("the attempt by a choice with no code = %v, want neither", a)
		}
	}
}

// A database that fails is unavailable, at each step of the choice.
func TestAChoiceOnADatabaseThatFailsIsUnavailable(t *testing.T) {
	t.Parallel()

	for _, failOn := range []string{
		"INSERT INTO engine_choices", "unnest(c.issued_skills)", "GROUP BY module_id ORDER BY", "GROUP BY module_id, task_id",
		"LEFT JOIN evidence e", "WHERE profile_id = $1 ORDER BY started_at, id", "SELECT map_skill", "FROM connected_modules",
		"SET close_reason", "SELECT DISTINCT segment",
	} {
		t.Run(failOn, func(t *testing.T) {
			t.Parallel()

			w := newWorld(t, func(p *pgxpool.Pool) rpc.DB { return &failing{Pool: p, failOn: failOn} })
			l := w.learner(t)
			// An open run of a lesson whose task with a rubric does not begin, whose
			// ended segments the choice reads.
			w.exec(t, `INSERT INTO lesson_runs (profile_id, lesson_id, module_id, module_version, package_digest, choice_id)
				VALUES ($1, $2, $3, '1.0.0', $4, $5)`, l.adult, module+"/lesson/essay", module, string(w.current.Digest),
				w.lessonChoice(t, l.adult, "essay"))
			pick := w.attempt(t, l.adult, string(w.current.Digest), "pick")
			_, submitErr := w.submit(l, l.adult, pick, tap(1, 1, "three and four"))
			w.attempt(t, l.adult, string(w.current.Digest), "pick")
			_, chooseErr := w.chooseNext(l, l.adult)
			if connect.CodeOf(chooseErr) != connect.CodeUnavailable && connect.CodeOf(submitErr) != connect.CodeUnavailable {
				t.Errorf("neither was unavailable: %v; %v", chooseErr, submitErr)
			}
		})
	}
}

// The provider of an own key is named by the owner's profile of an adult alone:
// a child's that names one is refused at every request whose answer may give
// an engine's choice, before anything is read or written (R65).
func TestAChildNamesNoProvider(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	open := w.begin(t, l, l.child, "pick").GetAttemptId()
	run, choice := uuid.NewString(), uuid.NewString()
	const own = "anthropic"
	for name, call := range map[string]func() error{
		"ChooseNext": func() error {
			_, err := w.learning.ChooseNext(l.ctx, &v1.ChooseNextRequest{ProfileId: l.child, IdempotencyKey: uuid.NewString(), OwnKeyProvider: own})
			return err
		},
		"OpenLesson": func() error {
			_, err := w.learning.OpenLesson(l.ctx, &v1.OpenLessonRequest{ProfileId: l.child, ChoiceId: choice, OwnKeyProvider: own})
			return err
		},
		"GetLessonRun": func() error {
			_, err := w.learning.GetLessonRun(l.ctx, &v1.GetLessonRunRequest{ProfileId: l.child, LessonRunId: run, OwnKeyProvider: own})
			return err
		},
		"CompleteLesson": func() error {
			_, err := w.learning.CompleteLesson(l.ctx, &v1.CompleteLessonRequest{ProfileId: l.child, LessonRunId: run, OwnKeyProvider: own})
			return err
		},
		"Done": func() error {
			_, err := w.attempts.Done(l.ctx, &v1.DoneRequest{ProfileId: l.child, AttemptId: open, OwnKeyProvider: own})
			return err
		},
		"ShowSolution": func() error {
			_, err := w.attempts.ShowSolution(l.ctx, &v1.ShowSolutionRequest{ProfileId: l.child, AttemptId: open,
				IdempotencyKey: uuid.NewString(), Action: v1.Action_ACTION_LEARNING, OwnKeyProvider: own})
			return err
		},
	} {
		err := call()
		t.Run(name, func(t *testing.T) { refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE) })
	}
	if got := w.closeOf(t, open); got != "" {
		t.Errorf("the open attempt is closed %q, want it open", got)
	}
}
