package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNoForm is a code of a request with no consent that waits for its form to
// be checked.
var ErrNoForm = errors.New("store: no form of the code waits")

// SentForm is a parent's consent whose form the operator checks: its code and
// where its scan lies (R116 spec).
type SentForm struct {
	RequestCode string
	Object      string
	MediaType   string
	SentAt      time.Time
}

// SentForms is every parent's consent that waits with its form sent, by when it
// was sent.
func SentForms(ctx context.Context, db Lister) ([]SentForm, error) {
	rows, err := db.Query(ctx, `SELECT c.request_code, f.object, f.media_type, f.sent_at FROM consents c
		JOIN consent_forms f ON f.consent_id = c.id
		WHERE c.giver = 'parent' AND c.state = 'waiting' AND c.form_state = 'sent' ORDER BY f.sent_at, c.request_code`)
	if err != nil {
		return nil, fmt.Errorf("store: read the forms sent: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SentForm, error) {
		var f SentForm
		err := row.Scan(&f.RequestCode, &f.Object, &f.MediaType, &f.SentAt)
		return f, err
	})
	if err != nil {
		return nil, fmt.Errorf("store: read the forms sent: %w", err)
	}
	return list, nil
}

// JudgeForm confirms, when accept, the parent's consent of the code, which
// waits with its form sent, as checked by its signed form; otherwise it marks
// the form not accepted, and the adult sends a new one. A code of no such
// consent is ErrNoForm.
func JudgeForm(ctx context.Context, db Querier, code string, accept bool) error {
	sql := `UPDATE consents SET form_state = 'not_accepted'
		WHERE request_code = $1 AND giver = 'parent' AND state = 'waiting' AND form_state = 'sent'`
	args := []any{code}
	if accept {
		sql = `UPDATE consents SET state = 'confirmed', confirmed_at = now(), method = $2
			WHERE request_code = $1 AND giver = 'parent' AND state = 'waiting' AND form_state = 'sent'`
		args = append(args, ConsentBySignedForm)
	}
	tag, err := db.Exec(ctx, sql, args...)
	switch {
	case err != nil:
		return fmt.Errorf("store: judge the form: %w", err)
	case tag.RowsAffected() == 0:
		return ErrNoForm
	}
	return nil
}

// ReportToReview is a problem report the operator reads, with the day it is to
// be reviewed by.
type ReportToReview struct {
	Report
	ReviewBy time.Time
}

// ReportsToReview is every report not reviewed yet, by when it came.
func ReportsToReview(ctx context.Context, db Lister, within time.Duration) ([]ReportToReview, error) {
	rows, err := db.Query(ctx, `SELECT id, reply, rung, words, kind, text, created_at FROM problem_reports
		WHERE reviewed_at IS NULL ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("store: read the reports: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ReportToReview, error) {
		var r ReportToReview
		err := row.Scan(&r.ID, &r.Reply, &r.Rung, &r.Words, &r.Kind, &r.Text, &r.CreatedAt)
		r.ReviewBy = r.CreatedAt.Add(within)
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("store: read the reports: %w", err)
	}
	return list, nil
}

// ReviewReport marks the report reviewed now, which starts its year; a report
// not there, or reviewed already, is ErrNotFound.
func ReviewReport(ctx context.Context, db Querier, reportID string) error {
	tag, err := db.Exec(ctx, `UPDATE problem_reports SET reviewed_at = now() WHERE id = $1 AND reviewed_at IS NULL`, reportID)
	switch {
	case err != nil:
		return fmt.Errorf("store: review the report: %w", err)
	case tag.RowsAffected() == 0:
		return ErrNotFound
	}
	return nil
}

// DropReports deletes the reports reviewed before before, a batch at a time
// (R123 spec).
func DropReports(ctx context.Context, db Querier, before time.Time) (int64, error) {
	return inBatches(ctx, db, `DELETE FROM problem_reports WHERE id IN (
		SELECT id FROM problem_reports WHERE reviewed_at < $1 LIMIT $2)`, before, "drop the reports reviewed")
}

// FormsPastTerm is up to limit objects of scans whose consent is gone and whose
// term ended by today, the day by UTC, in the order of their names after
// after.
func FormsPastTerm(ctx context.Context, db Lister, today time.Time, after string, limit int) ([]string, error) {
	rows, err := db.Query(ctx, `SELECT object FROM consent_forms
		WHERE consent_id IS NULL AND keep_until <= $1 AND object > $2 ORDER BY object LIMIT $3`, dateOf(today), after, limit)
	if err != nil {
		return nil, fmt.Errorf("store: choose the forms: %w", err)
	}
	list, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("store: choose the forms: %w", err)
	}
	return list, nil
}

// DropForm deletes the row of a scan the store erased, unless its consent took
// it back meanwhile.
func DropForm(ctx context.Context, db Querier, object string) error {
	if _, err := db.Exec(ctx, `DELETE FROM consent_forms WHERE object = $1 AND consent_id IS NULL`, object); err != nil {
		return fmt.Errorf("store: drop the form: %w", err)
	}
	return nil
}
