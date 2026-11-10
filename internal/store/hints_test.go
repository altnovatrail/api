package store_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/store"
)

// The words of an attempt's rungs are written, explained and read back, the
// questions of the model among them in order, until the attempt closes, which
// deletes them however it closes and keeps each rung's number and source.
func TestTheWordsOfTheRungsGoWithTheClose(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	id, err := e.attempt(t, "")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	model := store.Words{Source: store.SourceModel, Text: "Which number is greater?"}
	fallback := store.Words{Source: store.SourceFallback, Text: "Look at both numbers."}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		if _, found, err := store.RungOf(ctx, tx, e.profileID, id, 1); err != nil || found {
			t.Errorf("RungOf before = %v, %v; want none", found, err)
		}
		if err := store.WriteRung(ctx, tx, e.profileID, id, 1, model); err != nil {
			return err
		}
		if err := store.ExplainRung(ctx, tx, e.profileID, id, 1, model, store.Words{Source: store.SourceModel, Text: "How many tens?"}, at); err != nil {
			return err
		}
		// A rung whose words never came is written with those read.
		return store.ExplainRung(ctx, tx, e.profileID, id, 2, fallback, fallback, at)
	})
	if err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		r, found, err := store.RungOf(ctx, tx, e.profileID, id, 1)
		if err != nil || !found || r.Words != model || !r.Explained || r.Current().Text != "How many tens?" {
			t.Errorf("RungOf 1 = %+v, %v, %v", r, found, err)
		}
		r, found, err = store.RungOf(ctx, tx, e.profileID, id, 2)
		if err != nil || !found || r.Words != fallback || r.Current() != fallback {
			t.Errorf("RungOf 2 = %+v, %v, %v", r, found, err)
		}
		asked, err := store.AskedQuestions(ctx, tx, e.profileID, id)
		if err != nil || !slices.Equal(asked, []string{"Which number is greater?", "How many tens?"}) {
			t.Errorf("AskedQuestions = %q, %v; want the model's, in order", asked, err)
		}
		return store.CloseAttempt(ctx, tx, e.profileID, id, "solution_shown", at)
	})
	if err != nil {
		t.Fatal(err)
	}
	var words, sources int
	if err := e.pool.QueryRow(ctx, `SELECT count(words) + count(explained_words), count(source) + count(explained_source)
		FROM attempt_rungs WHERE attempt_id = $1`, id).Scan(&words, &sources); err != nil || words != 0 || sources != 4 {
		t.Errorf("after the close: %d words and %d sources (%v); want the sources alone", words, sources, err)
	}

	// The daily job's close deletes them too.
	idle, err := e.attempt(t, "")
	if err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error { return store.WriteRung(ctx, tx, e.profileID, idle, 1, fallback) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE attempts SET last_action_at = now() - interval '8 days' WHERE id = $1`, idle); err != nil {
		t.Fatal(err)
	}
	if n, err := store.CloseAbandonedAttempts(ctx, e.pool, time.Now().Add(-7*24*time.Hour), time.Now()); err != nil || n != 1 {
		t.Fatalf("CloseAbandonedAttempts = %d, %v", n, err)
	}
	if err := e.pool.QueryRow(ctx, `SELECT count(words) FROM attempt_rungs WHERE attempt_id = $1`, idle).Scan(&words); err != nil || words != 0 {
		t.Errorf("after the job: %d words (%v), want none", words, err)
	}
}

// The schema keeps a rung's number, source and words in their bounds.
func TestTheSchemaOfTheRungsHoldsItsRules(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	id, err := e.attempt(t, "")
	if err != nil {
		t.Fatal(err)
	}
	for name, values := range map[string]string{
		"a fourth rung":                 `4, 'model', 'w', NULL, NULL, NULL`,
		"no source":                     `1, 'other', 'w', NULL, NULL, NULL`,
		"empty words":                   `1, 'model', '', NULL, NULL, NULL`,
		"explained with no time":        `1, 'model', 'w', NULL, 'model', NULL`,
		"other words with no explained": `1, 'model', 'w', NULL, NULL, 'o'`,
	} {
		_, err := e.pool.Exec(t.Context(), `INSERT INTO attempt_rungs
			(profile_id, attempt_id, rung, source, words, explained_at, explained_source, explained_words)
			VALUES ($1, $2, `+values+`)`, e.profileID, id)
		if sqlstate(err) != "23514" {
			t.Errorf("%s = %v, want a check violation", name, err)
		}
	}
}

// A hint asked for records the techniques and the leading question the first
// time each is given, and only when the task has it; the setting of the
// showing and the profile's last attempt are read.
func TestWhatAHintGivesIsRecordedOnce(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	id, err := e.attempt(t, "")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	later := at.Add(time.Minute)
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		if err := store.GiveRequested(ctx, tx, e.profileID, id, true, false, at); err != nil {
			return err
		}
		a, err := store.HoldAttempt(ctx, tx, e.profileID, id)
		if err != nil || !a.RequestedTechniques || a.LeadingQuestion {
			t.Errorf("after the techniques alone = %+v, %v", a, err)
		}
		if err := store.GiveRequested(ctx, tx, e.profileID, id, true, true, later); err != nil {
			return err
		}
		return store.GiveRequested(ctx, tx, e.profileID, id, false, true, later.Add(time.Minute))
	})
	if err != nil {
		t.Fatal(err)
	}
	var techniques, leading, last time.Time
	if err := e.pool.QueryRow(ctx, `SELECT requested_techniques_at, leading_question_at, last_action_at FROM attempts WHERE id = $1`, id).
		Scan(&techniques, &leading, &last); err != nil || !techniques.Equal(at) || !leading.Equal(later) || !last.Equal(later.Add(time.Minute)) {
		t.Errorf("given at %v and %v, last action %v (%v); want each the first time, and the last action", techniques, leading, last, err)
	}
	if asks, err := store.AsksBeforeShowing(ctx, e.pool, e.profileID); err != nil || asks {
		t.Errorf("AsksBeforeShowing of an adult = %v, %v", asks, err)
	}
	if _, err := store.AsksBeforeShowing(ctx, e.pool, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNoProfile) {
		t.Errorf("AsksBeforeShowing of none = %v, want ErrNoProfile", err)
	}
	if last, err := store.LastAttempt(ctx, e.pool, e.profileID, id); err != nil || !last {
		t.Errorf("LastAttempt = %v, %v; want the last", last, err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE attempts SET close_reason = 'correct', closed_at = now() WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.attempt(t, ""); err != nil {
		t.Fatal(err)
	}
	if last, err := store.LastAttempt(ctx, e.pool, e.profileID, id); err != nil || last {
		t.Errorf("LastAttempt after another = %v, %v; want not", last, err)
	}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		if _, err := store.Submit(ctx, tx, e.profileID, id, []byte{1}, nil, true, at); err != nil {
			return err
		}
		answers, err := store.Answers(ctx, tx, e.profileID, id)
		if err != nil || len(answers) != 1 || answers[0][0] != 1 {
			t.Errorf("Answers = %v, %v", answers, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The statements of the ladder that fail are its failures.
func TestTheLaddersStatementsFail(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	id, err := e.attempt(t, "")
	if err != nil {
		t.Fatal(err)
	}
	words := store.Words{Source: store.SourceFallback, Text: "w"}
	for failOn, call := range map[string]func(tx pgx.Tx) error{
		"FROM attempt_rungs WHERE": func(tx pgx.Tx) error {
			_, _, err := store.RungOf(ctx, tx, e.profileID, id, 1)
			return err
		},
		"INSERT INTO attempt_rungs (": func(tx pgx.Tx) error { return store.WriteRung(ctx, tx, e.profileID, id, 1, words) },
		"ON CONFLICT":                 func(tx pgx.Tx) error { return store.ExplainRung(ctx, tx, e.profileID, id, 1, words, words, time.Now()) },
		"UNION ALL": func(tx pgx.Tx) error {
			_, err := store.AskedQuestions(ctx, tx, e.profileID, id)
			return err
		},
		"requested_techniques_at =": func(tx pgx.Tx) error { return store.GiveRequested(ctx, tx, e.profileID, id, true, true, time.Now()) },
		"SELECT answer FROM submissions": func(tx pgx.Tx) error {
			_, err := store.Answers(ctx, tx, e.profileID, id)
			return err
		},
		"SELECT ask_before_showing": func(tx pgx.Tx) error {
			_, err := store.AsksBeforeShowing(ctx, tx, e.profileID)
			return err
		},
		"NOT EXISTS": func(tx pgx.Tx) error {
			_, err := store.LastAttempt(ctx, tx, e.profileID, id)
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
