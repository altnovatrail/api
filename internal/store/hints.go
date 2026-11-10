package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The sources of a rung's words, as the schema writes them (R05).
const (
	// SourceModel is the tutor's question, which passed the leak check and the
	// form check.
	SourceModel = "model"
	// SourceFallback is the rung's fallback text.
	SourceFallback = "fallback"
)

// Words are the words of a rung and where they came from.
type Words struct {
	Source string
	Text   string
}

// Rung is what an attempt keeps of one of its rungs (R47): its words, and, once
// "explain differently" was asked for on it, the words that came of it, which
// the learner reads from then on. The words are empty once the attempt has
// closed.
type Rung struct {
	Rung      int
	Words     Words
	Explained bool
	Other     Words
}

// Current is the words the learner reads on the rung.
func (r Rung) Current() Words {
	if r.Explained {
		return r.Other
	}
	return r.Words
}

// RungOf is the attempt's rung, and whether it has its words.
func RungOf(ctx context.Context, db Querier, profileID, attemptID string, rung int) (Rung, bool, error) {
	r := Rung{Rung: rung}
	var words, other, otherSource *string
	var explained *time.Time
	err := db.QueryRow(ctx, `SELECT source, words, explained_at, explained_source, explained_words
		FROM attempt_rungs WHERE profile_id = $1 AND attempt_id = $2 AND rung = $3`, profileID, attemptID, rung).
		Scan(&r.Words.Source, &words, &explained, &otherSource, &other)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Rung{}, false, nil
	case err != nil:
		return Rung{}, false, fmt.Errorf("store: read the rung: %w", err)
	}
	r.Words.Text = deref(words)
	r.Explained = explained != nil
	r.Other = Words{Source: deref(otherSource), Text: deref(other)}
	return r, true, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// WriteRung records the words of the attempt's rung, the first that came for
// it.
func WriteRung(ctx context.Context, tx pgx.Tx, profileID, attemptID string, rung int, w Words) error {
	if _, err := tx.Exec(ctx, `INSERT INTO attempt_rungs (profile_id, attempt_id, rung, source, words)
		VALUES ($1, $2, $3, $4, nullif($5, ''))`, profileID, attemptID, rung, w.Source, w.Text); err != nil {
		return fmt.Errorf("store: record the rung's words: %w", err)
	}
	return nil
}

// ExplainRung records, at at, that "explain differently" was asked for on the
// attempt's rung, and the words that came of it. A rung whose words never came
// is recorded with read, the words the learner read before: its fallback text.
func ExplainRung(ctx context.Context, tx pgx.Tx, profileID, attemptID string, rung int, read, other Words, at time.Time) error {
	if _, err := tx.Exec(ctx, `INSERT INTO attempt_rungs
			(profile_id, attempt_id, rung, source, words, explained_at, explained_source, explained_words)
		VALUES ($1, $2, $3, $4, nullif($5, ''), $6, $7, nullif($8, ''))
		ON CONFLICT (attempt_id, rung) DO UPDATE
			SET explained_at = $6, explained_source = $7, explained_words = nullif($8, '')`,
		profileID, attemptID, rung, read.Source, read.Text, at, other.Source, other.Text); err != nil {
		return fmt.Errorf("store: record the rung explained: %w", err)
	}
	return nil
}

// AskedQuestions is the tutor's questions on the attempt so far, in order: the
// words of its rungs that came of a model, which every call of the words gets,
// so as not to ask them again (R10).
func AskedQuestions(ctx context.Context, db pgx.Tx, profileID, attemptID string) ([]string, error) {
	rows, err := db.Query(ctx, `SELECT q FROM (
			SELECT rung, 1 AS n, words AS q FROM attempt_rungs
				WHERE profile_id = $1 AND attempt_id = $2 AND source = 'model' AND words IS NOT NULL
			UNION ALL
			SELECT rung, 2, explained_words FROM attempt_rungs
				WHERE profile_id = $1 AND attempt_id = $2 AND explained_source = 'model' AND explained_words IS NOT NULL
		) asked ORDER BY rung, n`, profileID, attemptID)
	if err != nil {
		return nil, fmt.Errorf("store: read the questions asked: %w", err)
	}
	asked, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("store: read the questions asked: %w", err)
	}
	return asked, nil
}

// GiveRequested records, at at, that a hint asked for gave the techniques of
// the attempt's task, when techniques — it has some —, and its leading question,
// when leading, each the first time it did, and that the attempt's last action
// is at (R47).
func GiveRequested(ctx context.Context, tx pgx.Tx, profileID, attemptID string, techniques, leading bool, at time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE attempts
		SET requested_techniques_at = CASE WHEN $4 THEN coalesce(requested_techniques_at, $3) ELSE requested_techniques_at END,
			leading_question_at = CASE WHEN $5 THEN coalesce(leading_question_at, $3) ELSE leading_question_at END,
			last_action_at = $3
		WHERE profile_id = $1 AND id = $2`, profileID, attemptID, at, techniques, leading); err != nil {
		return fmt.Errorf("store: record the hint given: %w", err)
	}
	return nil
}

// Answers is the answers of the attempt's submissions with a verdict, in order:
// the ladder finds in them the wrong answer that gave a rung, and its answer
// slot.
func Answers(ctx context.Context, db pgx.Tx, profileID, attemptID string) ([][]byte, error) {
	rows, err := db.Query(ctx, `SELECT answer FROM submissions WHERE profile_id = $1 AND attempt_id = $2
		ORDER BY number`, profileID, attemptID)
	if err != nil {
		return nil, fmt.Errorf("store: read the submissions: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	if err != nil {
		return nil, fmt.Errorf("store: read the submissions: %w", err)
	}
	return out, nil
}

// AsksBeforeShowing says whether the profile's setting "ask before showing" is
// on: a child's, and on by default (R09).
func AsksBeforeShowing(ctx context.Context, db Querier, profileID string) (bool, error) {
	var asks bool
	err := db.QueryRow(ctx, `SELECT ask_before_showing FROM profiles WHERE id = $1`, profileID).Scan(&asks)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, ErrNoProfile
	case err != nil:
		return false, fmt.Errorf("store: read the setting of showing: %w", err)
	}
	return asks, nil
}

// LastAttempt says whether the attempt is the last the profile began: the
// words after a correct answer are given while the profile has begun no other
// (R09).
func LastAttempt(ctx context.Context, db Querier, profileID, attemptID string) (bool, error) {
	var last bool
	err := db.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM attempts b, attempts a
		WHERE a.profile_id = $1 AND a.id = $2 AND b.profile_id = $1 AND b.started_at > a.started_at)`,
		profileID, attemptID).Scan(&last)
	if err != nil {
		return false, fmt.Errorf("store: read the profile's last attempt: %w", err)
	}
	return last, nil
}
