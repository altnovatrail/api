package sweep_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/altnovatrail/api/internal/forms"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/sweep"
)

// The daily job erases the scans whose consent is gone and whose term ended,
// from the store and then their rows, and leaves the others; and it deletes the
// reports reviewed more than a year ago (R116, R117, R123 spec).
func TestThePassErasesTheFormsAndTheReportsPastTheirTerm(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	for _, f := range []struct {
		object, until string
		gone          bool
	}{{"past", "now() - interval '1 day'", true}, {"today", "now()", true}, {"later", "now() + interval '1 year'", true},
		{"kept", "NULL", false}} {
		if err := os.WriteFile(filepath.Join(string(w.forms), f.object), []byte("scan"), 0o600); err != nil {
			t.Fatal(err)
		}
		consent := "NULL"
		if !f.gone {
			account := w.account(t, "uid-"+f.object, time.Now())
			consent = "'" + w.id(t, `INSERT INTO consents (account_id, subject, state, age_category, country)
				VALUES ($1, 'child', 'waiting', 'under_13', 'US') RETURNING id`, account) + "'"
		}
		if _, err := w.pool.Exec(t.Context(), `INSERT INTO consent_forms (object, consent_id, media_type, sent_at, keep_until)
			VALUES ($1, `+consent+`, 'image/png', now(), (`+f.until+`)::date)`, f.object); err != nil {
			t.Fatal(err)
		}
	}
	for _, reviewed := range []string{"now() - interval '366 days'", "now() - interval '1 day'", "NULL"} {
		if _, err := w.pool.Exec(t.Context(), `INSERT INTO problem_reports (reply, words, kind, text, created_at, reviewed_at)
			VALUES ('correct_answer', 'w', 'other', '', now() - interval '400 days', `+reviewed+`)`); err != nil {
			t.Fatal(err)
		}
	}
	report := w.run(t)
	if s := step(t, report, logging.StepConsentForms); !s.Finished || s.Done != 2 {
		t.Errorf("%+v, want the two past their term erased", s)
	}
	if s := step(t, report, logging.StepProblemReports); !s.Finished || s.Done != 1 {
		t.Errorf("%+v, want the report reviewed a year ago deleted", s)
	}
	for object, there := range map[string]bool{"past": false, "today": false, "later": true, "kept": true} {
		if _, err := os.Stat(filepath.Join(string(w.forms), object)); (err == nil) != there {
			t.Errorf("the scan %s is there = %v, want %v", object, err == nil, there)
		}
	}
	if n := w.count(t, "consent_forms"); n != 2 {
		t.Errorf("consent_forms = %d, want two", n)
	}
	if n := w.count(t, "problem_reports"); n != 2 {
		t.Errorf("problem_reports = %d, want two", n)
	}
	// A store that cannot erase leaves the row to the next pass.
	if _, err := w.pool.Exec(t.Context(), `UPDATE consent_forms SET keep_until = now()::date - 1 WHERE object = 'later'`); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	w.forms = forms.Dir(blocked)
	if s := step(t, w.run(t), logging.StepConsentForms); s.Failed != 1 || w.count(t, "consent_forms WHERE object = 'later'") != 1 {
		t.Errorf("%+v, want the failure and the row kept", s)
	}
}

// With no store of the forms, a scan past its term is a failure of the step,
// and none is a clean one.
func TestNoStoreOfTheFormsFailsWhenAScanIsPastItsTerm(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	none := sweep.New(w.pool, w.set, w.model, w.rights, w.signin, settle, logging.New(&w.log, slog.LevelDebug))
	if s := step(t, none.Run(t.Context(), time.Now()), logging.StepConsentForms); !s.Finished || s.Failed != 0 {
		t.Errorf("%+v, want a clean step with nothing to erase", s)
	}
	if _, err := w.pool.Exec(t.Context(), `INSERT INTO consent_forms (object, media_type, sent_at, keep_until)
		VALUES ('past', 'image/png', now(), now()::date - 1)`); err != nil {
		t.Fatal(err)
	}
	if s := step(t, none.Run(t.Context(), time.Now()), logging.StepConsentForms); s.Finished || s.Failed != 1 {
		t.Errorf("%+v, want the failure", s)
	}
}
