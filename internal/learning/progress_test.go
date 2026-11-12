package learning_test

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/packages/packagestest"
	"github.com/altnovatrail/api/internal/rpc"
)

// evidenceOf is how many pieces of evidence the journal holds of the attempt.
func (w *world) evidenceOf(t *testing.T, attempt string) int {
	t.Helper()
	var n int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM evidence WHERE attempt_id = $1`, attempt).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// solve begins the task pick on the profile and taps it right, after the wrong
// answers given.
func (w *world) solve(t *testing.T, l learner, profile string, wrong ...string) string {
	t.Helper()
	a := w.begin(t, l, profile, "pick").GetAttemptId()
	for _, value := range append(wrong, "three and four") {
		if _, err := w.submit(l, profile, a, tap(1, 1, value)); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

// compare begins the template greater on the profile and taps its greater
// number at once: a template's evidence names no solution seen before, so that
// its skill may be mastered.
func (w *world) compare(t *testing.T, l learner, profile string) {
	t.Helper()
	start := w.begin(t, l, profile, "greater")
	var open struct {
		Items []struct {
			AnswerSlots []struct {
				Options []string `json:"options"`
			} `json:"answer_slots"`
		} `json:"items"`
	}
	if err := json.Unmarshal(start.GetTask().GetJson(), &open); err != nil {
		t.Fatal(err)
	}
	greater := slices.MaxFunc(open.Items[0].AnswerSlots[0].Options, func(a, b string) int {
		x, _ := strconv.Atoi(a)
		y, _ := strconv.Atoi(b)
		return x - y
	})
	if _, err := w.submit(l, profile, start.GetAttemptId(), tap(1, 1, greater)); err != nil {
		t.Fatal(err)
	}
}

// Every way a request closes an attempt writes its evidence in its transaction,
// once: a correct answer, the solution shown, another attempt begun, a check
// that closes it, and a lesson run that ends "not suitable"; an attempt left
// with no wrong answer gives none.
func TestEachCloseOwesItsEvidence(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	solved := w.solve(t, l, l.adult, "five")

	shown := w.begin(t, l, l.adult, "pairs").GetAttemptId()
	if _, err := w.attempts.ShowSolution(l.ctx, &v1.ShowSolutionRequest{ProfileId: l.adult, AttemptId: shown,
		IdempotencyKey: uuid.NewString(), Action: v1.Action_ACTION_LEARNING}); err != nil {
		t.Fatal(err)
	}

	untouched := w.begin(t, l, l.adult, "pairs").GetAttemptId()
	left := w.begin(t, l, l.adult, "pick").GetAttemptId()
	if _, err := w.submit(l, l.adult, left, tap(1, 1, "five")); err != nil {
		t.Fatal(err)
	}
	w.begin(t, l, l.adult, "pairs")

	withdrawn := w.attempt(t, l.adult, string(w.earlier.Digest), "pick")
	_, err := w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: l.adult, AttemptId: withdrawn})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED)

	run := w.open(t, l, l.adult, "basics").GetLessonRunId()
	segment, err := w.segment(l, l.adult, run, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.submit(l, l.adult, segment.GetAttemptId(), tap(1, 1, "five")); err != nil {
		t.Fatal(err)
	}
	w.exec(t, `DELETE FROM connected_modules WHERE profile_id = $1`, l.adult)
	_, err = w.run(l, l.adult, run)
	endedAs(t, err, v1.RunEndReason_RUN_END_REASON_NOT_SUITABLE)

	for attempt, want := range map[string]int{
		solved: 1, shown: 1, untouched: 0, left: 1, withdrawn: 1, segment.GetAttemptId(): 1,
	} {
		if got := w.evidenceOf(t, attempt); got != want {
			t.Errorf("the attempt closed %s has %d pieces of evidence, want %d", w.closeOf(t, attempt), got, want)
		}
	}
	var seqs, last int64
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*), max(seq) FROM evidence WHERE profile_id = $1`, l.adult).Scan(&seqs, &last); err != nil ||
		seqs != 5 || last != 5 {
		t.Errorf("the journal = %d pieces up to %d (%v), want 5 numbered one after another", seqs, last, err)
	}
}

// Two attempts of a profile closed at once on two devices get two numbers one
// after the other: the row of the cache orders them.
func TestTwoClosesAtOnceGetTwoNumbers(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	child := w.begin(t, l, l.child, "pick").GetAttemptId()
	adult := w.begin(t, l, l.adult, "pick").GetAttemptId()
	done := make(chan error, 2)
	for profile, attempt := range map[string]string{l.child: child, l.adult: adult} {
		go func() {
			_, err := w.attempts.SubmitWork(l.alone(t), &v1.SubmitWorkRequest{ProfileId: profile, AttemptId: attempt,
				IdempotencyKey: uuid.NewString(), TappedChoices: []*v1.TappedChoice{tap(1, 1, "three and four")}})
			done <- err
		}()
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range []string{child, adult} {
		if w.evidenceOf(t, a) != 1 {
			t.Errorf("the attempt %s has no evidence", a)
		}
	}
}

// The skill map gives each skill of the current version with a verdict, by the
// journal read at the time of the request, and each anchor skill its graph
// names; to the learner by a learning action and to an adult with can_view.
func TestTheSkillMapReadsTheModel(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	ask := func(profile string, action v1.Action, module string) (*v1.GetSkillMapResponse, error) {
		return w.progress.GetSkillMap(l.ctx, &v1.GetSkillMapRequest{ProfileId: profile, ModuleId: module, Action: action})
	}
	empty, err := ask(l.child, v1.Action_ACTION_LEARNING, module)
	if err != nil {
		t.Fatal(err)
	}
	if empty.GetPackageDigest() != string(w.current.Digest) || len(empty.GetSkills()) != 2 ||
		empty.GetSkills()[0].GetSkill() != "adding" || empty.GetSkills()[1].GetSkill() != "comparing" ||
		empty.GetSkills()[0].GetEvidenceCount() != 0 || empty.GetSkills()[0].Estimate != nil {
		t.Errorf("the map with no evidence = %v", empty)
	}
	if a := empty.GetAnchorSkills(); len(a) != 1 || a[0].GetAnchorSkill() != "core.multiplication-table" ||
		a[0].GetHolding() != v1.AnchorSkillHolding_ANCHOR_SKILL_HOLDING_NOT_HELD ||
		a[0].GetReason() != v1.AnchorSkillReason_ANCHOR_SKILL_REASON_NO_EVIDENCE ||
		len(a[0].GetTeachingSkills()) != 1 || a[0].GetTeachingSkills()[0] != module+"/skill/comparing" ||
		len(a[0].GetTeachingModules()) != 0 {
		t.Errorf("the anchor skills with no evidence, no other module teaching them = %v", a)
	}

	// Solved, the task's skill has its evidence, its estimate and its practice.
	for range 2 {
		w.compare(t, l, l.child)
	}
	got, err := ask(l.child, v1.Action_ACTION_LEARNING, module)
	if err != nil {
		t.Fatal(err)
	}
	comparing := got.GetSkills()[1]
	if comparing.GetEvidenceCount() != 2 || comparing.Estimate == nil || comparing.Uncertainty == nil ||
		comparing.Retention == nil || comparing.GetLastPracticedAt() == nil || comparing.GetMasteryBand() != "" {
		t.Errorf("comparing practised = %v", comparing)
	}

	adult, err := ask(l.child, v1.Action_ACTION_ADULT, module)
	if err != nil || adult.GetSkills()[1].GetEvidenceCount() != 2 ||
		adult.GetAnchorSkills()[0].GetReason() != v1.AnchorSkillReason_ANCHOR_SKILL_REASON_NOT_MASTERED {
		t.Errorf("the adult's view = %v, %v; want the same model, the anchor not mastered", adult, err)
	}
	w.denyView.Store(true)
	_, err = ask(l.child, v1.Action_ACTION_ADULT, module)
	refusedAs(t, err, connect.CodePermissionDenied, v1.Cause_CAUSE_UNSPECIFIED)
	w.denyView.Store(false)

	_, err = ask(l.child, v1.Action_ACTION_UNSPECIFIED, module)
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	_, err = ask(l.child, v1.Action_ACTION_LEARNING, "example/none")
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	w.exec(t, `INSERT INTO connected_modules (profile_id, module_id) VALUES ($1, 'example/none')`, l.child)
	_, err = ask(l.child, v1.Action_ACTION_LEARNING, "example/none")
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION)
	unconsented := w.child(t, l, "under_13", false)
	_, err = ask(unconsented, v1.Action_ACTION_LEARNING, module)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)
	_, err = ask(uuid.NewString(), v1.Action_ACTION_ADULT, module)
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("the map of a profile not the account's = %v, want not found", err)
	}
}

// Beside each anchor skill, the skill map offers the modules of the index that
// teach it: those whose current version suits the profile by its audience and
// that are not connected to it, by their IDs.
func TestTheSkillMapOffersTheModulesThatTeachAnAnchorSkill(t *testing.T) {
	t.Parallel()

	built(t)
	set := packagestest.Set(t,
		packagestest.Entry{Pkg: current, Release: "v0.2.0"},
		packagestest.Entry{Pkg: earlier, Release: "v0.1.0", Withdrawn: "wrong_key"},
		packagestest.Entry{Pkg: packagestest.OwnEdited(t, "choices", "1.0.0", packagestest.As("example/young", "[under_13, adult]")), Release: "v0.2.0"},
		packagestest.Entry{Pkg: packagestest.OwnEdited(t, "choices", "1.0.0", packagestest.As("example/grown", "[adult]")), Release: "v0.2.0"},
	)
	w := worldOn(t, set, gate(t, "", config.RungFull), nil)
	w.current, w.earlier, w.connected = current, earlier, []string{module}
	l := w.learner(t)
	offered := func(profile string) []string {
		t.Helper()
		got, err := w.progress.GetSkillMap(l.ctx, &v1.GetSkillMapRequest{ProfileId: profile, ModuleId: module, Action: v1.Action_ACTION_LEARNING})
		if err != nil {
			t.Fatal(err)
		}
		if a := got.GetAnchorSkills(); len(a) != 1 || a[0].GetAnchorSkill() != "core.multiplication-table" {
			t.Fatalf("the anchor skills = %v, want core.multiplication-table", a)
		}
		return got.GetAnchorSkills()[0].GetTeachingModules()
	}
	if got := offered(l.child); !slices.Equal(got, []string{"example/young"}) {
		t.Errorf("the modules offered to a child = %v, want example/young: example/grown is for adults", got)
	}
	if got := offered(l.adult); !slices.Equal(got, []string{"example/grown", "example/young"}) {
		t.Errorf("the modules offered to an adult = %v, want example/grown and example/young, by their IDs", got)
	}
	if _, err := w.modules.ConnectModule(l.ctx, &v1.ConnectModuleRequest{ProfileId: l.adult, ModuleId: "example/young"}); err != nil {
		t.Fatal(err)
	}
	if got := offered(l.adult); !slices.Equal(got, []string{"example/grown"}) {
		t.Errorf("the modules offered once example/young is connected = %v, want example/grown", got)
	}
}

// The progress gives the attempts newest first, a page at a time, each with
// its task, version, lesson, close and times, and on the first page the traps
// found in two closed attempts or more, with their texts.
func TestTheProgressGivesTheAttemptsAndTheTrapsThatRepeat(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	first := w.solve(t, l, l.child, "five")
	w.solve(t, l, l.child, "five")
	for range 50 {
		w.attempt(t, l.child, string(w.current.Digest), "pairs")
	}
	// The newest attempt, begun after the others, by the service's clock.
	w.clock.move(time.Minute)
	run := w.open(t, l, l.child, "basics").GetLessonRunId()
	if _, err := w.segment(l, l.child, run, 2); err != nil {
		t.Fatal(err)
	}

	page, err := w.progress.GetProgress(l.ctx, &v1.GetProgressRequest{ProfileId: l.child})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.GetAttempts()) != 50 || page.GetNextPageToken() == "" {
		t.Fatalf("the first page = %d attempts, token %q; want 50 and a next", len(page.GetAttempts()), page.GetNextPageToken())
	}
	top := page.GetAttempts()[0]
	if top.GetTask() != module+"/task/pick" || top.GetLesson() != module+"/lesson/basics" || top.GetModuleVersion() != "1.0.0" ||
		top.GetCloseReason() != v1.CloseReason_CLOSE_REASON_UNSPECIFIED || top.GetClosedAt() != nil || top.GetChosenAt() != nil {
		t.Errorf("the newest attempt, open, of the lesson = %v", top)
	}
	if left := page.GetAttempts()[1]; left.GetTask() != module+"/task/pairs" || left.GetLesson() != "" ||
		left.GetCloseReason() != v1.CloseReason_CLOSE_REASON_LEFT || left.GetClosedAt() == nil {
		t.Errorf("the attempt left = %v", left)
	}
	if traps := page.GetRepeatingTraps(); len(traps) != 1 || traps[0].GetTrap() != module+"/trap/first-seen" ||
		traps[0].GetAttemptCount() != 2 || !strings.HasPrefix(traps[0].GetDescription(), "Chose the first number") ||
		traps[0].GetTrapAdvice() == "" || traps[0].GetLastClosedAt() == nil {
		t.Errorf("the traps that repeat = %v", traps)
	}

	next, err := w.progress.GetProgress(l.ctx, &v1.GetProgressRequest{ProfileId: l.child, PageToken: page.GetNextPageToken()})
	if err != nil {
		t.Fatal(err)
	}
	if got := next.GetAttempts(); len(got) != 3 || next.GetNextPageToken() != "" || len(next.GetRepeatingTraps()) != 0 ||
		got[2].GetTask() != module+"/task/pick" || got[2].GetCloseReason() != v1.CloseReason_CLOSE_REASON_CORRECT ||
		got[2].GetWrongAnswers() != 1 || !got[2].GetBegunAt().AsTime().Before(got[0].GetBegunAt().AsTime().Add(time.Second)) {
		t.Errorf("the last page = %v, token %q", got, next.GetNextPageToken())
	}
	if w.evidenceOf(t, first) != 1 {
		t.Error("the solved attempt has no evidence")
	}

	for _, token := range []string{"%%", "bm90LWEtdG9rZW4", "MTIzL25vdC1hbi1pZA"} {
		_, err := w.progress.GetProgress(l.ctx, &v1.GetProgressRequest{ProfileId: l.child, PageToken: token})
		refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	}
}

// A database that fails is unavailable, at each step of the progress and the
// skill map.
func TestTheProgressOnADatabaseThatFailsIsUnavailable(t *testing.T) {
	t.Parallel()

	for _, failOn := range []string{
		"INSERT INTO skill_states", "FROM skill_states", "UPDATE skill_states", "FROM attempts a WHERE a.profile_id",
		"DISTINCT module_id", "FROM evidence e JOIN", "SELECT grade", "INSERT INTO evidence", "ORDER BY a.started_at DESC",
		"FROM connected_modules", "FROM profiles p",
	} {
		t.Run(failOn, func(t *testing.T) {
			t.Parallel()

			w := newWorld(t, func(p *pgxpool.Pool) rpc.DB { return &failing{Pool: p, failOn: failOn} })
			l := w.learner(t)
			w.attempt(t, l.adult, string(w.current.Digest), "pick")
			w.exec(t, `UPDATE attempts SET close_reason = 'correct', closed_at = now() WHERE profile_id = $1`, l.adult)
			_, mapErr := w.progress.GetSkillMap(l.ctx, &v1.GetSkillMapRequest{ProfileId: l.adult, ModuleId: module,
				Action: v1.Action_ACTION_LEARNING})
			_, progressErr := w.progress.GetProgress(l.ctx, &v1.GetProgressRequest{ProfileId: l.adult})
			if connect.CodeOf(mapErr) != connect.CodeUnavailable && connect.CodeOf(progressErr) != connect.CodeUnavailable {
				t.Errorf("neither was unavailable: %v; %v", mapErr, progressErr)
			}
		})
	}
}

// A lesson run that its check ends "not suitable" closes its attempt, whose
// evidence is written in the same transaction: a database that fails there
// leaves the run open and the request unavailable.
func TestARunEndedOnADatabaseThatFailsIsUnavailable(t *testing.T) {
	t.Parallel()

	w := newWorld(t, func(p *pgxpool.Pool) rpc.DB { return &failing{Pool: p, failOn: "INSERT INTO skill_states"} })
	l := w.learner(t)
	run := w.open(t, l, l.adult, "basics").GetLessonRunId()
	if _, err := w.segment(l, l.adult, run, 2); err != nil {
		t.Fatal(err)
	}
	w.exec(t, `DELETE FROM connected_modules WHERE profile_id = $1`, l.adult)
	_, err := w.run(l, l.adult, run)
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
	if got := w.endOf(t, run); got != "" {
		t.Errorf("the run ended %q, want it open: the end did not commit", got)
	}
}
