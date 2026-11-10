package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/store"
)

// The row of the cache is made when a profile has none, held and written; the
// attempts that owe evidence are those of the packages named that close with
// evidence and have none yet, in the order they closed; the journal is numbered
// one after another; the progress pages the attempts newest first.
func TestTheModelsRowsAreWrittenAndRead(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	at := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	// The profile of the image before has no row of the cache.
	if _, err := e.pool.Exec(ctx, `DELETE FROM skill_states WHERE profile_id = $1`, e.profileID); err != nil {
		t.Fatal(err)
	}
	owes, err := e.attempt(t, "choice_id, close_reason, closed_at, wrong_answers", e.choice(t, "task"), "left", at, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]any{{"left", at, 0}, {"done", at, 0}} {
		if _, err := e.attempt(t, "choice_id, close_reason, closed_at, wrong_answers", append([]any{e.choice(t, "task")}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		held, err := store.HoldSkillState(ctx, tx, e.profileID)
		if err != nil || held.LastSeq != 0 || held.State != nil || held.Mark != (store.Mark{}) {
			t.Errorf("HoldSkillState of a profile with no row = %+v, %v", held, err)
		}
		if none, err := store.OwedAttempts(ctx, tx, e.profileID, []string{"sha256:" + "b"}); err != nil || len(none) != 0 {
			t.Errorf("OwedAttempts of other packages = %+v, %v", none, err)
		}
		owed, err := store.OwedAttempts(ctx, tx, e.profileID, []string{digest})
		if err != nil || len(owed) != 1 || owed[0].ID != owes || owed[0].WrongAnswers != 1 || owed[0].CloseReason != "left" {
			t.Fatalf("OwedAttempts = %+v, %v; want the one left with a wrong answer", owed, err)
		}
		if seq, err := store.AppendEvidence(ctx, tx, e.profileID, owed[0], []byte{1}, at); err != nil || seq != 1 {
			t.Errorf("AppendEvidence = %d, %v", seq, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// One attempt, one piece of evidence.
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		_, err := store.AppendEvidence(ctx, tx, e.profileID, store.Owed{ID: owes, PackageDigest: digest, ModuleID: "example/m"}, []byte{1}, at)
		return err
	})
	if sqlstate(err) != "23505" {
		t.Errorf("a second piece of evidence of the attempt = %v, want a unique violation", err)
	}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		journal, err := store.Journal(ctx, tx, e.profileID, 0)
		if err != nil || len(journal) != 1 || journal[0].AttemptID != owes || !journal[0].ClosedAt.Equal(at) {
			t.Errorf("Journal = %+v, %v", journal, err)
		}
		if modules, err := store.JournalModules(ctx, tx, e.profileID); err != nil || len(modules) != 1 || modules[0] != "example/m" {
			t.Errorf("JournalModules = %v, %v", modules, err)
		}
		mark := store.Mark{LearnerModel: "v0.1.4", Release: "v0.1.0", Window: "0.4"}
		if err := store.WriteSkillState(ctx, tx, e.profileID, store.SkillState{State: []byte{2}, Mark: mark, LastSeq: 1}); err != nil {
			return err
		}
		held, err := store.HoldSkillState(ctx, tx, e.profileID)
		if err != nil || held.LastSeq != 1 || held.Mark != mark || len(held.State) != 1 {
			t.Errorf("the cache written = %+v, %v", held, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if ids, err := store.ProfilesOwingEvidence(ctx, e.pool, []string{digest}, "", 10); err != nil || len(ids) != 0 {
		t.Errorf("ProfilesOwingEvidence once the evidence is in = %v, %v; want none", ids, err)
	}
	if grade, err := store.ProfileGrade(ctx, e.pool, e.profileID); err != nil || grade != nil {
		t.Errorf("ProfileGrade = %v, %v; want none", grade, err)
	}
	if _, err := store.ProfileGrade(ctx, e.pool, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNoProfile) {
		t.Errorf("ProfileGrade of none = %v, want ErrNoProfile", err)
	}
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		_, err := store.HoldSkillState(ctx, tx, "00000000-0000-0000-0000-000000000000")
		return err
	})
	if !errors.Is(err, store.ErrNoProfile) {
		t.Errorf("HoldSkillState of no profile = %v, want ErrNoProfile", err)
	}

	page, err := store.ProgressPage(ctx, e.pool, e.profileID, nil, 2)
	if err != nil || len(page) != 2 || page[0].StartedAt.Before(page[1].StartedAt) {
		t.Fatalf("ProgressPage = %+v, %v", page, err)
	}
	rest, err := store.ProgressPage(ctx, e.pool, e.profileID, &store.Cursor{StartedAt: page[1].StartedAt, ID: page[1].ID}, 2)
	if err != nil || len(rest) != 1 || rest[0].ID == page[0].ID || rest[0].ID == page[1].ID {
		t.Errorf("the page after = %+v, %v", rest, err)
	}
}

// A profile owes evidence while an attempt of it does, and the profiles go by ID,
// a page at a time.
func TestTheProfilesThatOweEvidenceGoByID(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	if _, err := e.attempt(t, "choice_id, close_reason, closed_at", e.choice(t, "task"), "correct", time.Now()); err != nil {
		t.Fatal(err)
	}
	ids, err := store.ProfilesOwingEvidence(t.Context(), e.pool, []string{digest}, "", 10)
	if err != nil || len(ids) != 1 || ids[0] != e.profileID {
		t.Errorf("ProfilesOwingEvidence = %v, %v", ids, err)
	}
	if after, err := store.ProfilesOwingEvidence(t.Context(), e.pool, []string{digest}, e.profileID, 10); err != nil || len(after) != 0 {
		t.Errorf("the page after = %v, %v", after, err)
	}
}

// The statements of the model that fail are their failures.
func TestTheStatementsOfTheModelFail(t *testing.T) {
	t.Parallel()

	e := newE3(t)
	ctx := t.Context()
	for failOn, call := range map[string]func(tx pgx.Tx) error{
		"FOR KEY SHARE": func(tx pgx.Tx) error {
			_, err := store.HoldSkillState(ctx, tx, e.profileID)
			return err
		},
		"INSERT INTO skill_states": func(tx pgx.Tx) error {
			_, err := store.HoldSkillState(ctx, tx, e.profileID)
			return err
		},
		"FROM skill_states WHERE": func(tx pgx.Tx) error {
			_, err := store.HoldSkillState(ctx, tx, e.profileID)
			return err
		},
		"UPDATE skill_states": func(tx pgx.Tx) error {
			return store.WriteSkillState(ctx, tx, e.profileID, store.SkillState{})
		},
		"FROM attempts a WHERE a.profile_id = $1": func(tx pgx.Tx) error {
			_, err := store.OwedAttempts(ctx, tx, e.profileID, nil)
			return err
		},
		"FROM evidence e JOIN": func(tx pgx.Tx) error {
			_, err := store.Journal(ctx, tx, e.profileID, 0)
			return err
		},
		"DISTINCT module_id": func(tx pgx.Tx) error {
			_, err := store.JournalModules(ctx, tx, e.profileID)
			return err
		},
		"INSERT INTO evidence": func(tx pgx.Tx) error {
			_, err := store.AppendEvidence(ctx, tx, e.profileID, store.Owed{}, nil, time.Now())
			return err
		},
		"SELECT grade": func(tx pgx.Tx) error {
			_, err := store.ProfileGrade(ctx, tx, e.profileID)
			return err
		},
		"DISTINCT a.profile_id": func(tx pgx.Tx) error {
			_, err := store.ProfilesOwingEvidence(ctx, tx, nil, "", 1)
			return err
		},
		"ORDER BY a.started_at DESC": func(tx pgx.Tx) error {
			_, err := store.ProgressPage(ctx, tx, e.profileID, nil, 1)
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
