package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Mark is what the cache of a profile's skill state was computed on (R06): the
// version of learner-model, the release of modules and the window of the
// format's versions. An empty field is a cache never written.
type Mark struct {
	LearnerModel string
	Release      string
	Window       string
}

// SkillState is a profile's row of the cache of the skill state: the state as
// learner-model's codec gives it, empty while it was never written, its mark,
// and the number of the last evidence it holds.
type SkillState struct {
	State   []byte
	Mark    Mark
	LastSeq int64
}

// HoldSkillState holds the profile's row of the cache FOR UPDATE in tx, so that
// the evidence of one profile is numbered and folded one piece after another
// (R06). The profile's own row is held FOR KEY SHARE first, as the rows of the
// journal that the caller then adds refer to it: a deletion of the profile,
// which takes it FOR UPDATE before its cascade reaches the cache, waits, rather
// than locking the two rows in the other order. A profile the image before this
// one made has no row of the cache, so the row is made first, empty, when it is
// not there (the 2026-11-10 amendment to R06); a profile gone is ErrNoProfile.
func HoldSkillState(ctx context.Context, tx pgx.Tx, profileID string) (SkillState, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM profiles WHERE id = $1 FOR KEY SHARE`, profileID).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return SkillState{}, ErrNoProfile
	case err != nil:
		return SkillState{}, fmt.Errorf("store: hold the profile: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO skill_states (profile_id) VALUES ($1) ON CONFLICT DO NOTHING`, profileID); err != nil {
		return SkillState{}, fmt.Errorf("store: start the skill state: %w", err)
	}
	var s SkillState
	var learnerModel, release, window *string
	if err := tx.QueryRow(ctx, `SELECT state, learner_model_version, modules_release, format_window, last_seq
		FROM skill_states WHERE profile_id = $1 FOR UPDATE`, profileID).
		Scan(&s.State, &learnerModel, &release, &window, &s.LastSeq); err != nil {
		return SkillState{}, fmt.Errorf("store: hold the skill state: %w", err)
	}
	s.Mark = Mark{LearnerModel: deref(learnerModel), Release: deref(release), Window: deref(window)}
	return s, nil
}

// WriteSkillState writes the profile's cache: its state, the mark it was
// computed on and the number of the last evidence it holds.
func WriteSkillState(ctx context.Context, tx pgx.Tx, profileID string, s SkillState) error {
	if _, err := tx.Exec(ctx, `UPDATE skill_states SET state = $2, learner_model_version = $3, modules_release = $4,
		format_window = $5, last_seq = $6 WHERE profile_id = $1`,
		profileID, s.State, s.Mark.LearnerModel, s.Mark.Release, s.Mark.Window, s.LastSeq); err != nil {
		return fmt.Errorf("store: write the skill state: %w", err)
	}
	return nil
}

// Owed is a closed attempt whose evidence is not in the journal yet, with what
// its evidence is built of (R06, R10): what it was of, how it closed, its last
// action, the techniques given at a step and on request, whether a hint asked
// for gave the task's leading question, how many times "explain differently"
// was asked for, whether the solution of its task reached the device before it
// began, and the answers of its submissions, in order.
type Owed struct {
	ID                  string
	PackageDigest       string
	ModuleID            string
	TaskID              string
	Instance            *int
	CloseReason         string
	WrongAnswers        int
	LastActionAt        time.Time
	StepTechniques      bool
	RequestedTechniques bool
	LeadingQuestion     bool
	ExplainedAgain      int
	SolutionSeenBefore  bool
	Answers             [][]byte
}

// owedWhere is the closed attempts that owe their evidence, on the packages of
// the parameter digests: closed correct, with the solution shown or unchecked;
// or left, abandoned or not suitable with wrong answers, since one with none
// gives no evidence (the 2026-11-02 amendments to R06 and R10). "Done" gives
// none (О-17).
func owedWhere(digests string) string {
	return `a.closed_at IS NOT NULL AND a.package_digest = ANY(` + digests + `::text[])
	AND (a.close_reason IN ('correct', 'solution_shown', 'unchecked')
		OR (a.close_reason IN ('left', 'abandoned', 'not_suitable') AND a.wrong_answers > 0))
	AND NOT EXISTS (SELECT 1 FROM evidence e WHERE e.attempt_id = a.id)`
}

// OwedAttempts is the profile's attempts that owe their evidence, on the
// packages digests names — those the image holds: one whose package left the
// image gives none (Д27) —, in the order they closed.
func OwedAttempts(ctx context.Context, tx pgx.Tx, profileID string, digests []string) ([]Owed, error) {
	rows, err := tx.Query(ctx, `SELECT a.id, a.package_digest, a.module_id, a.task_id, a.instance, a.close_reason,
			a.wrong_answers, a.last_action_at, a.step_techniques_at IS NOT NULL, a.requested_techniques_at IS NOT NULL,
			a.leading_question_at IS NOT NULL,
			(SELECT count(*) FROM attempt_rungs r WHERE r.profile_id = a.profile_id AND r.attempt_id = a.id
				AND r.explained_at IS NOT NULL),
			EXISTS (SELECT 1 FROM attempts p WHERE p.profile_id = a.profile_id AND p.module_id = a.module_id
				AND p.task_id = a.task_id AND p.close_reason IN ('correct', 'solution_shown')
				AND p.closed_at <= a.started_at),
			ARRAY(SELECT s.answer FROM submissions s WHERE s.profile_id = a.profile_id AND s.attempt_id = a.id
				ORDER BY s.number)
		FROM attempts a WHERE a.profile_id = $1 AND `+owedWhere("$2")+` ORDER BY a.closed_at, a.id`, profileID, digests)
	if err != nil {
		return nil, fmt.Errorf("store: read the attempts that owe evidence: %w", err)
	}
	owed, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Owed, error) {
		var o Owed
		err := row.Scan(&o.ID, &o.PackageDigest, &o.ModuleID, &o.TaskID, &o.Instance, &o.CloseReason, &o.WrongAnswers,
			&o.LastActionAt, &o.StepTechniques, &o.RequestedTechniques, &o.LeadingQuestion, &o.ExplainedAgain, &o.SolutionSeenBefore,
			&o.Answers)
		return o, err
	})
	if err != nil {
		return nil, fmt.Errorf("store: read the attempts that owe evidence: %w", err)
	}
	return owed, nil
}

// ProfilesOwingEvidence is up to n of the profiles with an attempt that owes its
// evidence, on the packages digests names, by ID after the ID after, for the
// daily job.
func ProfilesOwingEvidence(ctx context.Context, db Lister, digests []string, after string, n int) ([]string, error) {
	// By the uuid itself, as the index of the attempts of a profile orders them.
	rows, err := db.Query(ctx, `SELECT DISTINCT a.profile_id FROM attempts a
		WHERE ($2 = '' OR a.profile_id > $2::uuid) AND `+owedWhere("$1")+` ORDER BY a.profile_id LIMIT $3`,
		digests, after, n)
	if err != nil {
		return nil, fmt.Errorf("store: read the profiles that owe evidence: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("store: read the profiles that owe evidence: %w", err)
	}
	return ids, nil
}

// Entry is a row of a profile's journal of evidence: its number, its attempt,
// the package and the module of the attempt, when the attempt closed, and the
// evidence as learner-model's codec gives it.
type Entry struct {
	Seq           int64
	AttemptID     string
	PackageDigest string
	ModuleID      string
	ClosedAt      time.Time
	Evidence      []byte
}

// Journal is the profile's journal of evidence after the number after, in order.
func Journal(ctx context.Context, db Lister, profileID string, after int64) ([]Entry, error) {
	rows, err := db.Query(ctx, `SELECT e.seq, e.attempt_id, e.package_digest, e.module_id, a.closed_at, e.evidence
		FROM evidence e JOIN attempts a ON a.profile_id = e.profile_id AND a.id = e.attempt_id
		WHERE e.profile_id = $1 AND e.seq > $2 ORDER BY e.seq`, profileID, after)
	if err != nil {
		return nil, fmt.Errorf("store: read the journal: %w", err)
	}
	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Entry, error) {
		var e Entry
		err := row.Scan(&e.Seq, &e.AttemptID, &e.PackageDigest, &e.ModuleID, &e.ClosedAt, &e.Evidence)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("store: read the journal: %w", err)
	}
	return entries, nil
}

// JournalModules is the modules of the profile's journal, by ID.
func JournalModules(ctx context.Context, tx pgx.Tx, profileID string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT module_id FROM evidence WHERE profile_id = $1 ORDER BY module_id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("store: read the modules of the journal: %w", err)
	}
	modules, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("store: read the modules of the journal: %w", err)
	}
	return modules, nil
}

// AppendEvidence adds the evidence of the profile's attempt to its journal at at,
// under the number after the journal's last, and gives the number. The caller
// holds the profile's row of the cache, which orders the numbers (R06).
func AppendEvidence(ctx context.Context, tx pgx.Tx, profileID string, o Owed, evidence []byte, at time.Time) (int64, error) {
	var seq int64
	if err := tx.QueryRow(ctx, `INSERT INTO evidence (profile_id, seq, attempt_id, package_digest, module_id, recorded_at, evidence)
		SELECT $1, coalesce(max(seq), 0) + 1, $2, $3, $4, $5, $6 FROM evidence WHERE profile_id = $1
		RETURNING seq`, profileID, o.ID, o.PackageDigest, o.ModuleID, at, evidence).Scan(&seq); err != nil {
		return 0, fmt.Errorf("store: append the evidence: %w", err)
	}
	return seq, nil
}

// ProfileGrade is the profile's school grade, nil when it names none.
func ProfileGrade(ctx context.Context, db Querier, profileID string) (*int, error) {
	var grade *int
	err := db.QueryRow(ctx, `SELECT grade FROM profiles WHERE id = $1`, profileID).Scan(&grade)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrNoProfile
	case err != nil:
		return nil, fmt.Errorf("store: read the grade: %w", err)
	}
	return grade, nil
}
