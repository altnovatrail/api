package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/store"
)

// A choice is written with what it keeps and read back after the close or the
// end it came after; the skill the last choice holds is its own; the summary
// counts what the choices handed out and the solutions sent; the closed
// attempts come with the code of their choice and their evidence, the last
// first; and the runs with their ends. A choice after both a close and an end
// is refused.
func TestAChoiceIsWrittenAndRead(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	at := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	closed, err := e.attempt(t, "choice_id, close_reason, closed_at", e.choice(t, "task"), "correct", at)
	if err != nil {
		t.Fatal(err)
	}
	run, err := e.run(t, "example/m/lesson/l1")
	if err != nil {
		t.Fatal(err)
	}
	grade := 2
	c := store.NewChoice{Kind: "task", ModuleID: "example/m", TaskID: "t1", ReasonCode: 6,
		IssuedSkills: []string{"example/m/skill/a"}, PackageDigest: digest, MapSkill: "example/m/skill/a",
		AgeCategory: "under_13", Grade: &grade, PoolLimited: true,
		Mark: store.Mark{LearnerModel: "v0.1.4", Release: "v0.1.0", Window: "0.4"}, EngineVersion: "v0.1.4", LastSeq: 3,
		Digests: []string{digest}, EngineDigests: []string{digest}, AfterAttemptID: &closed}
	var first, second string
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		var err error
		if first, err = store.WriteChoice(ctx, tx, e.profileID, c, at); err != nil {
			return err
		}
		lesson := store.NewChoice{Kind: "lesson", ModuleID: "example/m", LessonID: "l1", ReasonCode: 2,
			IssuedSkills: []string{"example/m/skill/a", "example/m/skill/b"}, PackageDigest: digest, AgeCategory: "under_13",
			Mark: c.Mark, EngineVersion: "v0.1.4", AfterLessonRunID: &run}
		second, err = store.WriteChoice(ctx, tx, e.profileID, lesson, at.Add(time.Minute))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, found, err := store.ChoiceAfterAttempt(ctx, e.pool, e.profileID, closed); err != nil || !found || got.ID != first || got.Kind != "task" {
		t.Errorf("ChoiceAfterAttempt = %+v, %v, %v", got, found, err)
	}
	if got, found, err := store.ChoiceAfterRun(ctx, e.pool, e.profileID, run); err != nil || !found || got.ID != second || got.Kind != "lesson" {
		t.Errorf("ChoiceAfterRun = %+v, %v, %v", got, found, err)
	}
	if _, found, err := store.ChoiceAfterRun(ctx, e.pool, e.profileID, closed); err != nil || found {
		t.Errorf("ChoiceAfterRun of none = %v, %v", found, err)
	}
	if held, err := store.HeldMapSkill(ctx, e.pool, e.profileID); err != nil || held != "" {
		t.Errorf("HeldMapSkill = %q, %v; want the last choice's, none", held, err)
	}
	issued, err := store.IssuedSkills(ctx, e.pool, e.profileID)
	if err != nil || len(issued) != 2 || issued[0].Skill != "example/m/skill/a" || !issued[0].At.Equal(at.Add(time.Minute)) ||
		issued[1].Skill != "example/m/skill/b" {
		t.Errorf("IssuedSkills = %+v, %v", issued, err)
	}
	if counts, err := store.ChoicesByModule(ctx, e.pool, e.profileID); err != nil || len(counts) != 1 || counts[0].Choices < 3 {
		t.Errorf("ChoicesByModule = %+v, %v; want the choices of the module, the tests' own too", counts, err)
	}
	if sent, err := store.SolutionsSent(ctx, e.pool, e.profileID); err != nil || len(sent) != 1 || sent[0].TaskID != "t1" || !sent[0].At.Equal(at) {
		t.Errorf("SolutionsSent = %+v, %v", sent, err)
	}
	attempts, err := store.ClosedAttempts(ctx, e.pool, e.profileID)
	if err != nil || len(attempts) != 1 || attempts[0].ID != closed || attempts[0].ReasonCode != nil || attempts[0].Evidence != nil {
		t.Errorf("ClosedAttempts = %+v, %v; want the attempt, its test's choice with no code", attempts, err)
	}
	runs, err := store.ProfileRuns(ctx, e.pool, e.profileID)
	if err != nil || len(runs) != 1 || runs[0].LessonID != "example/m/lesson/l1" || runs[0].EndReason != nil {
		t.Errorf("ProfileRuns = %+v, %v", runs, err)
	}

	// After both a close and an end, a choice is refused.
	both := c
	both.AfterAttemptID, both.AfterLessonRunID = nil, &run
	other, err := e.attempt(t, "choice_id, close_reason, closed_at", e.choice(t, "task"), "left", at)
	if err != nil {
		t.Fatal(err)
	}
	both.AfterAttemptID = &other
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		_, err := store.WriteChoice(ctx, tx, e.profileID, both, at)
		return err
	})
	if sqlstate(err) != "23514" {
		t.Errorf("a choice after a close and an end = %v, want a check violation", err)
	}
}

// The statements of the choice that fail are their failures.
func TestTheStatementsOfTheChoiceFail(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	none := "00000000-0000-0000-0000-000000000000"
	for failOn, call := range map[string]func(tx pgx.Tx) error{
		"INSERT INTO engine_choices": func(tx pgx.Tx) error {
			_, err := store.WriteChoice(ctx, tx, e.profileID, store.NewChoice{}, time.Now())
			return err
		},
		"after_attempt_id = $2": func(tx pgx.Tx) error {
			_, _, err := store.ChoiceAfterAttempt(ctx, tx, e.profileID, none)
			return err
		},
		"SELECT map_skill": func(tx pgx.Tx) error {
			_, err := store.HeldMapSkill(ctx, tx, e.profileID)
			return err
		},
		"unnest(c.issued_skills)": func(tx pgx.Tx) error {
			_, err := store.IssuedSkills(ctx, tx, e.profileID)
			return err
		},
		"GROUP BY module_id ORDER BY": func(tx pgx.Tx) error {
			_, err := store.ChoicesByModule(ctx, tx, e.profileID)
			return err
		},
		"GROUP BY module_id, task_id": func(tx pgx.Tx) error {
			_, err := store.SolutionsSent(ctx, tx, e.profileID)
			return err
		},
		"LEFT JOIN evidence e": func(tx pgx.Tx) error {
			_, err := store.ClosedAttempts(ctx, tx, e.profileID)
			return err
		},
		"ORDER BY started_at, id": func(tx pgx.Tx) error {
			_, err := store.ProfileRuns(ctx, tx, e.profileID)
			return err
		},
	} {
		tx, err := (&flaky{Pool: e.pool, failOn: failOn}).Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := call(tx); !errors.Is(err, errDown) {
			t.Errorf("on %q: %v, want the failure", failOn, err)
		}
		_ = tx.Rollback(ctx)
	}
}

// A choice of nothing to give is written with no module and its outcome, and
// read back after the close it came after; a kind the schema does not name is
// refused (migration 0011).
func TestNothingToGiveIsWrittenToo(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	closed, err := e.attempt(t, "choice_id, close_reason, closed_at", e.choice(t, "task"), "correct", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		_, err := store.WriteChoice(ctx, tx, e.profileID, store.NewChoice{Kind: "nothing", NotAdmitted: true,
			MapSkill: "example/m/skill/a", AgeCategory: "adult", EngineVersion: "v0.1.4", AfterAttemptID: &closed,
			Mark: store.Mark{LearnerModel: "v0.1.4", Release: "v0.1.0", Window: "0.4"}}, time.Now())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, found, err := store.ChoiceAfterAttempt(ctx, e.pool, e.profileID, closed); err != nil || !found || got.Kind != "nothing" || !got.NotAdmitted {
		t.Errorf("the choice of nothing = %+v, %v, %v", got, found, err)
	}
	if held, err := store.HeldMapSkill(ctx, e.pool, e.profileID); err != nil || held != "example/m/skill/a" {
		t.Errorf("HeldMapSkill = %q, %v; want the skill of the choice of nothing", held, err)
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO engine_choices (profile_id, kind) VALUES ($1, 'other')`, e.profileID); sqlstate(err) != "23514" {
		t.Errorf("a choice of a kind not named = %v, want a check violation", err)
	}
}
