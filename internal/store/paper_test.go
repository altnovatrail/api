package store_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/store"
)

// An attempt of a task from paper holds no module, package, task or seed, and
// begins by no choice, run or earlier attempt; one of a module holds them all,
// as before. The open attempts of a page go beside the one open attempt of a
// module.
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
	begin := func(page string, n int) []string {
		t.Helper()
		var ids []string
		err := pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
			var err error
			ids, err = store.BeginPaperAttempts(ctx, tx, e.profileID, page, n, time.Now())
			return err
		})
		if err != nil || len(ids) != n {
			t.Fatalf("a page of %d = %v, %v", n, ids, err)
		}
		return ids
	}
	begin("page-1", 2)
	// "Add" on the same page: a Start again closes none of its tasks.
	begin("page-1", 1)
	second := begin("page-2", 2)
	var left, open int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE close_reason = 'left'), count(*) FILTER (WHERE closed_at IS NULL)
		FROM attempts WHERE kind = 'paper' AND profile_id = $1`, e.profileID).Scan(&left, &open); err != nil || left != 3 || open != 2 {
		t.Errorf("the pages: %d left, %d open (%v), want 3 and 2", left, open, err)
	}
	// The close clears the page, as it clears the rest of what the attempt held.
	var pages int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM attempts WHERE kind = 'paper' AND profile_id = $1 AND paper_page IS NOT NULL`,
		e.profileID).Scan(&pages); err != nil || pages != 2 {
		t.Errorf("attempts with a page = %d (%v), want the two open", pages, err)
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

// The Check of a task from paper, its history and the reading of its statement
// are kept on its open attempt alone, and erased as the attempt closes: an
// attempt of a module holds none. Each Check replaces the one before, which
// goes to the end of the history when a turn is given, and lets the rungs of
// the ladder before go; the rungs shown are each rung's words, then its words
// put another way.
func TestTheCheckOfAPaperAttemptGoesWithItsClose(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	module, err := e.attempt(t, "")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	var shown []string
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		if ids, err = store.BeginPaperAttempts(ctx, tx, e.profileID, "page-1", 1, time.Now()); err != nil {
			return err
		}
		if err := store.KeepPaperCheck(ctx, tx, e.profileID, module, []byte{1}, nil); err == nil {
			t.Error("a Check on the module's attempt was kept")
		}
		if err := store.SetPaperReading(ctx, tx, e.profileID, module, []byte{1}); err == nil {
			t.Error("a reading on the module's attempt was set")
		}
		if err := store.SetPaperReading(ctx, tx, e.profileID, ids[0], []byte{3}); err != nil {
			return err
		}
		if err := store.KeepPaperCheck(ctx, tx, e.profileID, ids[0], []byte{1}, nil); err != nil {
			return err
		}
		if err := store.WriteRung(ctx, tx, e.profileID, ids[0], 2, store.Words{Source: store.SourceModel, Text: "Which step?"}); err != nil {
			return err
		}
		if err := store.ExplainRung(ctx, tx, e.profileID, ids[0], 1, store.Words{Source: store.SourceModel, Text: "What is asked?"},
			store.Words{Source: store.SourceModel, Text: "What do you find?"}, time.Now()); err != nil {
			return err
		}
		if shown, err = store.ShownRungs(ctx, tx, e.profileID, ids[0]); err != nil {
			return err
		}
		return store.KeepPaperCheck(ctx, tx, e.profileID, ids[0], []byte{1, 2}, []byte{4})
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"What is asked?", "What do you find?", "Which step?"}; !slices.Equal(shown, want) {
		t.Errorf("the rungs shown = %q, want %q", shown, want)
	}
	hold := func() store.Attempt {
		t.Helper()
		var a store.Attempt
		if err := pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
			a, err = store.HoldAttempt(ctx, tx, e.profileID, ids[0])
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return a
	}
	if a := hold(); string(a.PaperCheck) != "\x01\x02" || len(a.PaperHistory) != 1 || string(a.PaperHistory[0]) != "\x04" || string(a.PaperReading) != "\x03" {
		t.Errorf("the Check kept = %x, the history %x, the reading %x; want 0102, [04] and 03", a.PaperCheck, a.PaperHistory, a.PaperReading)
	}
	var rungs int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM attempt_rungs WHERE attempt_id = $1`, ids[0]).Scan(&rungs); err != nil || rungs != 0 {
		t.Errorf("the attempt keeps %d rungs (%v) past its new Check, want none", rungs, err)
	}
	for column, value := range map[string]string{"paper_check": `'\x01'`, "paper_history": `ARRAY['\x01'::bytea]`, "paper_reading": `'\x01'`} {
		if _, err := e.pool.Exec(ctx, `UPDATE attempts SET `+column+` = `+value+` WHERE id = $1`, module); sqlstate(err) != "23514" {
			t.Errorf("%s on a module's attempt = %v, want a check violation", column, err)
		}
	}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		return store.CloseAttempt(ctx, tx, e.profileID, ids[0], "left", time.Now())
	})
	if err != nil {
		t.Fatal(err)
	}
	if a := hold(); a.PaperCheck != nil || a.PaperHistory != nil || a.PaperReading != nil {
		t.Errorf("after the close the Check = %x, the history %x, the reading %x; want none", a.PaperCheck, a.PaperHistory, a.PaperReading)
	}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error { return store.KeepPaperCheck(ctx, tx, e.profileID, ids[0], []byte{1}, nil) })
	if err == nil {
		t.Error("a Check on a closed attempt was kept")
	}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error { return store.SetPaperReading(ctx, tx, e.profileID, ids[0], []byte{1}) })
	if err == nil {
		t.Error("a reading on a closed attempt was set")
	}
	for column, value := range map[string]string{"paper_check": `'\x01'`, "paper_history": `ARRAY['\x01'::bytea]`, "paper_reading": `'\x01'`} {
		if _, err := e.pool.Exec(ctx, `UPDATE attempts SET `+column+` = `+value+` WHERE id = $1`, ids[0]); sqlstate(err) != "23514" {
			t.Errorf("%s on a closed attempt by hand = %v, want a check violation", column, err)
		}
	}
}

// The close of an attempt of a task from paper leaves nothing it kept while
// open but its row: its submissions go, with the idempotency keys that recorded
// them, its rungs, the opening question among them, and its count of wrong
// answers, and a problem report on its words stays. An attempt of a module
// keeps them as before.
func TestTheCloseOfAPaperAttemptLeavesItsRowAlone(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	module, err := e.attempt(t, "")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) (err error) {
		ids, err = store.BeginPaperAttempts(ctx, tx, e.profileID, "page-1", 1, time.Now())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	paper := ids[0]
	for _, a := range []string{module, paper} {
		err := pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
			id, err := store.Submit(ctx, tx, e.profileID, a, []byte{1}, []byte{2}, true, time.Now())
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO idempotency_keys (account_id, key, method, fingerprint, state, profile_id, attempt_id, submission_id)
				VALUES ($1, $2, 'm', 'f', 'done', $3, $4, $5)`, e.accountID, "key-"+a, e.profileID, a, id); err != nil {
				return err
			}
			return store.WriteRung(ctx, tx, e.profileID, a, 1, store.Words{Source: store.SourceModel, Text: "Look again."})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO attempt_rungs (profile_id, attempt_id, rung, source, words) VALUES ($1, $2, 0, 'model', 'What is asked?')`,
		e.profileID, paper); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO problem_reports (account_id, profile_id, attempt_id, reply, rung, words, kind, text, created_at)
		VALUES ($1, $2, $3, 'rung', 1, 'Look again.', 'unclear', '', now())`, e.accountID, e.profileID, paper); err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{module, paper} {
		if err := pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
			return store.CloseAttempt(ctx, tx, e.profileID, a, "left", time.Now())
		}); err != nil {
			t.Fatal(err)
		}
	}
	kept := func(a string) (rows [4]int, wrong int) {
		t.Helper()
		err := e.pool.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM submissions WHERE attempt_id = $1),
				(SELECT count(*) FROM idempotency_keys WHERE attempt_id = $1),
				(SELECT count(*) FROM attempt_rungs WHERE attempt_id = $1),
				(SELECT count(*) FROM problem_reports WHERE attempt_id = $1),
				wrong_answers
			FROM attempts WHERE id = $1`, a).Scan(&rows[0], &rows[1], &rows[2], &rows[3], &wrong)
		if err != nil {
			t.Fatal(err)
		}
		return rows, wrong
	}
	if rows, wrong := kept(paper); rows != [4]int{0, 0, 0, 1} || wrong != 0 {
		t.Errorf("the closed attempt of paper keeps submissions, keys, rungs, reports %v and %d wrong answers; want [0 0 0 1] and 0", rows, wrong)
	}
	if rows, wrong := kept(module); rows != [4]int{1, 1, 1, 0} || wrong != 1 {
		t.Errorf("the closed attempt of a module keeps submissions, keys, rungs, reports %v and %d wrong answers; want [1 1 1 0] and 1", rows, wrong)
	}
}
