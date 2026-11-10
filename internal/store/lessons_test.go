package store_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/store"
)

// A lesson run begins once for a choice and once at a time for a lesson, is
// read by its ID, its choice and its lesson, its last action the later of its
// own and its attempts', and ends once; its ended segments and its open attempt
// are read from its attempts, and the list of profiles names it while it is
// open.
func TestALessonRunIsWrittenAndRead(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	at := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	choice := e.choice(t, "lesson")
	if c, err := store.ChoiceOf(ctx, e.pool, e.profileID, choice); err != nil || c.Kind != "lesson" || c.LessonID != "l1" || c.TaskID != "" {
		t.Errorf("ChoiceOf a lesson = %+v, %v", c, err)
	}
	r := store.NewRun{LessonID: "example/m/lesson/l1", ModuleID: "example/m", ModuleVersion: "0.1.0", PackageDigest: digest, ChoiceID: choice}
	var id string
	err := pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		var err error
		if id, err = store.BeginLessonRun(ctx, tx, e.profileID, r, at); err != nil {
			return err
		}
		// The same choice, and another of the same lesson, begin no second run, and tx goes on.
		if _, err := store.BeginLessonRun(ctx, tx, e.profileID, r, at); !errors.Is(err, store.ErrRunBegun) {
			t.Errorf("a second run of the choice = %v, want ErrRunBegun", err)
		}
		other := r
		other.ChoiceID = e.choice(t, "lesson")
		if _, err := store.BeginLessonRun(ctx, tx, e.profileID, other, at); !errors.Is(err, store.ErrRunBegun) {
			t.Errorf("a second open run of the lesson = %v, want ErrRunBegun", err)
		}
		held, err := store.HoldLessonRun(ctx, tx, e.profileID, id)
		if err != nil || held.LessonID != r.LessonID || held.ChoiceID != choice || !held.LastActionAt.Equal(at) || held.EndReason != nil {
			t.Errorf("HoldLessonRun = %+v, %v", held, err)
		}
		if got, found, err := store.RunOfChoice(ctx, tx, e.profileID, choice); err != nil || !found || got.ID != id {
			t.Errorf("RunOfChoice = %+v, %v, %v", got, found, err)
		}
		if got, found, err := store.OpenRunOf(ctx, tx, e.profileID, r.LessonID); err != nil || !found || got.ID != id {
			t.Errorf("OpenRunOf = %+v, %v, %v", got, found, err)
		}
		if _, found, err := store.RunOfChoice(ctx, tx, e.profileID, other.ChoiceID); err != nil || found {
			t.Errorf("RunOfChoice of a choice that opened none = %v, %v", found, err)
		}
		return store.TouchLessonRun(ctx, tx, e.profileID, id, at.Add(time.Minute))
	})
	if err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		_, err := store.HoldLessonRun(ctx, tx, e.profileID, "00000000-0000-0000-0000-000000000000")
		if !errors.Is(err, store.ErrNotFound) {
			t.Errorf("HoldLessonRun of none = %v, want ErrNotFound", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Its attempts: segment 2 ended, segment 4 open, and the later action theirs.
	ended, err := e.attempt(t, "lesson_run_id, segment, close_reason, closed_at, last_action_at", id, 2, "correct", at, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	open, err := e.attempt(t, "lesson_run_id, segment", id, 4)
	if err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		held, err := store.HoldLessonRun(ctx, tx, e.profileID, id)
		if err != nil || !held.LastActionAt.After(at.Add(time.Minute)) {
			t.Errorf("the run's last action = %v, %v; want its attempts' later one", held.LastActionAt, err)
		}
		if got, err := store.EndedSegments(ctx, tx, e.profileID, id); err != nil || !slices.Equal(got, []int{2}) {
			t.Errorf("EndedSegments = %v, %v", got, err)
		}
		a, found, err := store.RunAttempt(ctx, tx, e.profileID, id)
		if err != nil || !found || a.ID != open || *a.Segment != 4 {
			t.Errorf("RunAttempt = %+v, %v, %v", a, found, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := store.AccountView(ctx, e.pool, e.accountID)
	if err != nil {
		t.Fatal(err)
	}
	if runs := view.Profiles[0].OpenLessonRuns; len(runs) != 1 || runs[0].ID != id || runs[0].LessonID != r.LessonID || runs[0].PackageDigest != digest {
		t.Errorf("the open runs of the profile = %+v", runs)
	}
	if lesson, err := store.RunLesson(ctx, e.pool, e.profileID, id); err != nil || lesson != r.LessonID {
		t.Errorf("RunLesson = %q, %v", lesson, err)
	}

	// "Not suitable" closes its open attempt with it, and an end stays.
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		if err := store.EndLessonRun(ctx, tx, e.profileID, id, store.RunNotSuitable, at.Add(2*time.Hour)); err != nil {
			return err
		}
		return store.EndLessonRun(ctx, tx, e.profileID, id, store.RunPassed, at.Add(3*time.Hour))
	})
	if err != nil {
		t.Fatal(err)
	}
	var reason, closed string
	if err := e.pool.QueryRow(ctx, `SELECT r.end_reason, a.close_reason FROM lesson_runs r, attempts a WHERE r.id = $1 AND a.id = $2`,
		id, open).Scan(&reason, &closed); err != nil || reason != "not_suitable" || closed != "not_suitable" {
		t.Errorf("the run ended %q and its attempt closed %q (%v), want both not_suitable", reason, closed, err)
	}
	var other string
	if err := e.pool.QueryRow(ctx, `SELECT close_reason FROM attempts WHERE id = $1`, ended).Scan(&other); err != nil || other != "correct" {
		t.Errorf("the ended segment's attempt = %q (%v), want its close kept", other, err)
	}
	if view, err := store.AccountView(ctx, e.pool, e.accountID); err != nil || len(view.Profiles[0].OpenLessonRuns) != 0 {
		t.Errorf("the open runs after the end = %+v (%v), want none", view.Profiles[0].OpenLessonRuns, err)
	}
}

// An attempt of a segment closed "unchecked" ends its run "unchecked", in the
// same transaction; closed otherwise, the run stays open. A choice of a lesson
// names its lesson.
func TestAnUncheckedAttemptEndsItsRun(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	for reason, want := range map[string]string{"left": "", "unchecked": "unchecked"} {
		run, err := e.run(t, "example/m/lesson/"+reason)
		if err != nil {
			t.Fatal(err)
		}
		a, err := e.attempt(t, "lesson_run_id, segment", run, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
			return store.CloseAttempt(ctx, tx, e.profileID, a, reason, time.Now())
		}); err != nil {
			t.Fatal(err)
		}
		var got *string
		if err := e.pool.QueryRow(ctx, `SELECT end_reason FROM lesson_runs WHERE id = $1`, run).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if (got == nil) != (want == "") || (got != nil && *got != want) {
			t.Errorf("an attempt closed %s ended its run %v, want %q", reason, got, want)
		}
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO engine_choices (profile_id, kind, module_id) VALUES ($1, 'lesson', 'example/m')`,
		e.profileID); sqlstate(err) != "23514" {
		t.Errorf("a choice of a lesson that names none = %v, want a check violation", err)
	}
}

// The daily job ends a run with no action for 30 days, and keeps one whose
// attempt acted since.
func TestTheJobKeepsARunWhoseAttemptActed(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	idle, err := e.run(t, "example/m/lesson/idle")
	if err != nil {
		t.Fatal(err)
	}
	busy, err := e.run(t, "example/m/lesson/busy")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.attempt(t, "lesson_run_id, segment", busy, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE lesson_runs SET last_action_at = now() - interval '31 days'`); err != nil {
		t.Fatal(err)
	}
	if n, err := store.EndAbandonedRuns(ctx, e.pool, time.Now().Add(-store.RunAbandonedAfter), time.Now()); err != nil || n != 1 {
		t.Errorf("EndAbandonedRuns = %d, %v; want the idle one", n, err)
	}
	for run, want := range map[string]bool{idle: true, busy: false} {
		var ended bool
		if err := e.pool.QueryRow(ctx, `SELECT ended_at IS NOT NULL FROM lesson_runs WHERE id = $1`, run).Scan(&ended); err != nil || ended != want {
			t.Errorf("the run %s ended %v (%v), want %v", run, ended, err, want)
		}
	}
}

// The statements of the lesson runs that fail are their failures.
func TestTheStatementsOfTheRunsFail(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	none := "00000000-0000-0000-0000-000000000000"
	for failOn, call := range map[string]func(tx pgx.Tx) error{
		"FROM lesson_runs WHERE profile_id = $1 AND choice_id": func(tx pgx.Tx) error {
			_, _, err := store.RunOfChoice(ctx, tx, e.profileID, none)
			return err
		},
		"INSERT INTO lesson_runs": func(tx pgx.Tx) error {
			_, err := store.BeginLessonRun(ctx, tx, e.profileID, store.NewRun{}, time.Now())
			return err
		},
		"UPDATE lesson_runs SET end_reason": func(tx pgx.Tx) error {
			return store.EndLessonRun(ctx, tx, e.profileID, none, store.RunPassed, time.Now())
		},
		"SET close_reason = 'not_suitable'": func(tx pgx.Tx) error {
			return store.EndLessonRun(ctx, tx, e.profileID, none, store.RunNotSuitable, time.Now())
		},
		"UPDATE lesson_runs SET last_action_at": func(tx pgx.Tx) error {
			return store.TouchLessonRun(ctx, tx, e.profileID, none, time.Now())
		},
		"SELECT DISTINCT segment": func(tx pgx.Tx) error {
			_, err := store.EndedSegments(ctx, tx, e.profileID, none)
			return err
		},
		"SELECT id, lesson_run_id, segment": func(tx pgx.Tx) error {
			_, _, err := store.RunAttempt(ctx, tx, e.profileID, none)
			return err
		},
		"SELECT lesson_id FROM lesson_runs": func(tx pgx.Tx) error {
			_, err := store.RunLesson(ctx, tx, e.profileID, none)
			return err
		},
		"FOR UPDATE OF r": func(tx pgx.Tx) error { return store.HoldAttemptRun(ctx, tx, e.profileID, none) },
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
