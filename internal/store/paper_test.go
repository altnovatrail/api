package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/store"
)

// An attempt of a task from paper holds no module, package, task or seed, and
// begins by no choice, run or earlier attempt; one of a module holds them all,
// as before. The open attempts of a page go beside the one open attempt of a
// module (R10).
func TestTheSchemaHoldsTheAttemptsOfPaper(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	if _, err := e.attempt(t, ""); err != nil {
		t.Fatal(err)
	}
	paper := func(extra string, args ...any) error {
		_, err := e.pool.Exec(ctx, `INSERT INTO attempts (profile_id, kind`+extra+`) VALUES ($1, 'paper'`+
			map[bool]string{true: ", $2", false: ""}[len(args) > 0]+`)`, append([]any{e.profileID}, args...)...)
		return err
	}
	for i := range 2 {
		if err := paper(""); err != nil {
			t.Fatalf("an open attempt of paper %d beside the module's: %v", i+1, err)
		}
	}
	for name, err := range map[string]error{
		"with a package":   paper(", package_digest", digest),
		"with a task":      paper(", task_id", "t1"),
		"with a seed":      paper(", seed", 7),
		"by a choice":      paper(", choice_id", e.choice(t, "task")),
		"with an instance": paper(", instance", 1),
		"of another kind": func() error {
			_, err := e.pool.Exec(ctx, `INSERT INTO attempts (profile_id, kind) VALUES ($1, 'photo')`, e.profileID)
			return err
		}(),
		"a module's with no package": func() error {
			_, err := e.pool.Exec(ctx, `INSERT INTO attempts (profile_id, choice_id) VALUES ($1, $2)`, e.profileID, e.choice(t, "task"))
			return err
		}(),
	} {
		if sqlstate(err) != "23514" {
			t.Errorf("an attempt %s = %v, want a check violation", name, err)
		}
	}
}

// BeginPaperAttempts closes the open attempts of the page before "left", and
// begins its own, their IDs in order; HoldOpenAttempt and the attempt's task read
// the module's alone.
func TestThePageBeginsItsAttempts(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	module, err := e.attempt(t, "")
	if err != nil {
		t.Fatal(err)
	}
	begin := func(n int) []string {
		t.Helper()
		var ids []string
		err := pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
			var err error
			ids, err = store.BeginPaperAttempts(ctx, tx, e.profileID, n, time.Now())
			return err
		})
		if err != nil || len(ids) != n {
			t.Fatalf("a page of %d = %v, %v", n, ids, err)
		}
		return ids
	}
	begin(3)
	second := begin(2)
	var left, open int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE close_reason = 'left'), count(*) FILTER (WHERE closed_at IS NULL)
		FROM attempts WHERE kind = 'paper' AND profile_id = $1`, e.profileID).Scan(&left, &open); err != nil || left != 3 || open != 2 {
		t.Errorf("the pages: %d left, %d open (%v), want 3 and 2", left, open, err)
	}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		a, found, err := store.HoldOpenAttempt(ctx, tx, e.profileID)
		if err == nil && (!found || a.ID != module || a.Paper) {
			t.Errorf("the open attempt = %+v, %t; want the module's", a, found)
		}
		if p, err := store.HoldAttempt(ctx, tx, e.profileID, second[0]); err != nil || !p.Paper || p.PackageDigest != "" || p.Seed != 0 {
			t.Errorf("the attempt of paper = %+v, %v", p, err)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttemptTask(ctx, e.pool, e.profileID, second[0]); !errors.Is(err, store.ErrPaper) {
		t.Errorf("the task of an attempt of paper: %v, want ErrPaper", err)
	}
}
