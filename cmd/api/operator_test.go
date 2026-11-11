package main

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/store/storetest"
)

// The operator lists the parents' forms sent, by their codes and the objects of
// their scans, confirms one and rejects another; a code with no form waiting is
// said so and exits 1.
func TestTheOperatorJudgesTheForms(t *testing.T) {
	t.Parallel()

	url := storetest.URL(t, true)
	conn, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	var account string
	if err := conn.QueryRow(t.Context(), `INSERT INTO accounts (auth_uid) VALUES ('uid-synthetic') RETURNING id`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"ABCDE-FGHJK", "KMNPQ-RSTVW"} {
		if _, err := conn.Exec(t.Context(), `WITH c AS (INSERT INTO consents (account_id, subject, state, age_category, country,
				giver, request_code, form_state) VALUES ($1, 'child', 'waiting', 'under_13', 'US', 'parent', $2, 'sent') RETURNING id)
			INSERT INTO consent_forms (object, consent_id, media_type, sent_at) SELECT id::text, id, 'image/png', now() FROM c`,
			account, code); err != nil {
			t.Fatal(err)
		}
	}
	environ := []string{"API_DATABASE_URL=" + url}
	code, stdout, _ := runCommand(t.Context(), environ, "consents", "list")
	if code != exitOK || !strings.Contains(stdout, "ABCDE-FGHJK\t") || !strings.Contains(stdout, "KMNPQ-RSTVW\t") {
		t.Fatalf("consents list = %d, %q; want both forms", code, stdout)
	}
	for _, step := range []struct {
		args []string
		exit int
		says string
	}{
		{[]string{"confirm", "ABCDE-FGHJK"}, exitOK, "ABCDE-FGHJK confirmed\n"},
		{[]string{"reject", "KMNPQ-RSTVW"}, exitOK, "KMNPQ-RSTVW rejected"},
		{[]string{"confirm", "KMNPQ-RSTVW"}, exitFailure, "no consent of KMNPQ-RSTVW waits"},
		{[]string{"list"}, exitOK, ""},
	} {
		code, stdout, _ := runCommand(t.Context(), environ, append([]string{"consents"}, step.args...)...)
		if code != step.exit || !strings.HasPrefix(stdout, step.says) || (step.says == "" && stdout != "") {
			t.Errorf("consents %v = %d, %q; want %d and %q", step.args, code, stdout, step.exit, step.says)
		}
	}
	var state, method string
	if err := conn.QueryRow(t.Context(), `SELECT state, method FROM consents WHERE request_code = 'ABCDE-FGHJK'`).Scan(&state, &method); err != nil ||
		state != "confirmed" || method != "signed_form" {
		t.Errorf("the consent confirmed = %s %s, %v; want confirmed by the signed form", state, method, err)
	}
}

// The operator lists the reports not reviewed, with what each says, and marks
// one reviewed; one reviewed already is said so and exits 1.
func TestTheOperatorReviewsTheReports(t *testing.T) {
	t.Parallel()

	url := storetest.URL(t, true)
	conn, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	var id string
	if err := conn.QueryRow(t.Context(), `INSERT INTO problem_reports (reply, rung, words, kind, text, created_at)
		VALUES ('rung', 2, 'The words of the rung.', 'harmful', 'What we saw.', now()) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	environ := []string{"API_DATABASE_URL=" + url}
	code, stdout, _ := runCommand(t.Context(), environ, "reports", "list")
	if code != exitOK || !strings.HasPrefix(stdout, id+"\t") || !strings.Contains(stdout, "rung 2\tharmful") ||
		!strings.Contains(stdout, `"The words of the rung."`) || !strings.Contains(stdout, `"What we saw."`) {
		t.Fatalf("reports list = %d, %q; want the report", code, stdout)
	}
	if code, stdout, _ := runCommand(t.Context(), environ, "reports", "review", id); code != exitOK || stdout != id+" reviewed\n" {
		t.Errorf("reports review = %d, %q", code, stdout)
	}
	if code, stdout, _ := runCommand(t.Context(), environ, "reports", "review", id); code != exitFailure || !strings.Contains(stdout, "no report") {
		t.Errorf("a second review = %d, %q; want 1 and no report", code, stdout)
	}
	if code, stdout, _ := runCommand(t.Context(), environ, "reports", "list"); code != exitOK || stdout != "" {
		t.Errorf("reports list after = %d, %q; want nothing", code, stdout)
	}
}

func TestTheOperatorsCommandsRefuseWhatTheyMayNotDo(t *testing.T) {
	t.Parallel()

	url := "API_DATABASE_URL=" + storetest.URL(t, true)
	missing := "API_DATABASE_URL=" + strings.Replace(storetest.URL(t, false), "/test_", "/missing_", 1)
	for _, tc := range []struct {
		environ []string
		args    []string
		exit    int
		says    string
	}{
		{[]string{url}, []string{"consents"}, exitUsage, "list, confirm"},
		{[]string{url}, []string{"consents", "confirm", "abcde-fghjk"}, exitUsage, "no code of a request"},
		{[]string{url}, []string{"consents", "-x"}, exitUsage, "-x"},
		{[]string{url}, []string{"consents", "-h"}, exitOK, "Usage: api consents"},
		{[]string{url}, []string{"reports", "review"}, exitUsage, "list or review"},
		{[]string{url}, []string{"reports", "review", "nope"}, exitUsage, "no ID of a report"},
		{nil, []string{"reports", "list"}, exitFailure, "API_DATABASE_URL"},
		{[]string{missing}, []string{"reports", "list"}, exitFailure, ""},
		{[]string{missing}, []string{"consents", "list"}, exitFailure, ""},
		{[]string{missing}, []string{"consents", "confirm", "ABCDE-FGHJK"}, exitFailure, ""},
		{[]string{missing}, []string{"reports", "review", "00000000-0000-0000-0000-000000000000"}, exitFailure, ""},
	} {
		code, _, stderr := runCommand(t.Context(), tc.environ, tc.args...)
		if code != tc.exit || !strings.Contains(stderr, tc.says) {
			t.Errorf("%v = %d, %q; want %d and %q", tc.args, code, stderr, tc.exit, tc.says)
		}
	}
}
