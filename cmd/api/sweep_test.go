package main

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/store/storetest"
)

// A pass finishes a mark a request left, writes a line for each step, and exits
// 0 when it is clean.
func TestSweepFinishesAMarkAndSaysEachStep(t *testing.T) {
	t.Parallel()

	url := storetest.URL(t, true)
	conn, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(t.Context(), `INSERT INTO accounts (auth_uid, deleting_at)
		VALUES ('uid-synthetic', now() - interval '1 minute')`); err != nil {
		t.Fatal(err)
	}

	environ := serveEnviron(t)
	environ[0] = "API_DATABASE_URL=" + url
	var stdout, stderr syncBuffer
	if code := run(t.Context(), []string{"sweep", "-for", "1m"}, environ, &stdout, &stderr); code != exitOK || stderr.String() != "" {
		t.Fatalf("sweep = %d, stderr %q; want 0 and nothing", code, stderr.String())
	}
	lines := stdout.lines(t)
	if len(lines) != len(logging.Steps) {
		t.Fatalf("the log is %v, want a line for each step", lines)
	}
	for i, line := range lines {
		if line["message"] != "swept" || line["step"] != logging.Steps[i] || line["finished"] != true {
			t.Errorf("line %d = %v, want step %s finished", i, line, logging.Steps[i])
		}
	}
	if lines[0]["done"] != float64(1) {
		t.Errorf("marks = %v, want the one finished", lines[0])
	}
	var left int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM accounts`).Scan(&left); err != nil || left != 0 {
		t.Errorf("accounts = %d, %v; want the mark's gone", left, err)
	}
}

// A pass with a failure exits 1, its log having said which step.
func TestSweepThatIsNotCleanExits1(t *testing.T) {
	t.Parallel()

	var stdout, stderr syncBuffer
	code := run(t.Context(), []string{"sweep"}, serveEnviron(t, "API_FIREBASE_PROJECT_ID=unlisted"), &stdout, &stderr)
	if code != exitFailure || stderr.String() != "" {
		t.Fatalf("sweep = %d, stderr %q; want %d and nothing", code, stderr.String(), exitFailure)
	}
	if log := stdout.String(); !strings.Contains(log, `"moment":"sweep","step":"users"`) ||
		!strings.Contains(log, `"step":"users","done":0,"failed":1,"finished":false`) {
		t.Errorf("the log does not name the users' failure: %s", log)
	}
}

// What sweep cannot open is one failure, with the database it is of.
func TestSweepThatCannotOpenIsOneFailure(t *testing.T) {
	t.Parallel()

	missing := "API_DATABASE_URL=" + strings.Replace(storetest.URL(t, false), "/test_", "/missing_", 1)
	var stdout, stderr syncBuffer
	code := run(t.Context(), []string{"sweep"}, serveEnviron(t, missing), &stdout, &stderr)
	lines := stdout.lines(t)
	if code != exitFailure || len(lines) != 1 || lines[0]["moment"] != "sweep" || lines[0]["database"] != "api" {
		t.Errorf("sweep = %d, log %v; want one failure of the database api", code, lines)
	}
	code, out, _ := runCommand(t.Context(), serveEnviron(t, "API_FIREBASE_PROJECT_ID=broken"), "sweep")
	if code != exitFailure || !strings.Contains(out, `"moment":"sweep"`) || strings.Contains(out, `"database"`) {
		t.Errorf("sweep with no sign-in service = %d, %q; want one failure with no database", code, out)
	}
	// The evidence the pass writes is built of the packages, read as serve reads them.
	code, out, _ = runCommand(t.Context(), serveEnviron(t, "API_PACKAGES_DIR="+t.TempDir()), "sweep")
	if code != exitFailure || !strings.Contains(out, `"moment":"sweep"`) || strings.Contains(out, `"step"`) {
		t.Errorf("sweep with no packages = %d, %q; want one failure before any step", code, out)
	}
}

func TestSweepRefusesWhatItCannotRun(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		exit int
		says string
	}{
		{"no time", []string{"-for", "0s"}, exitUsage, "-for must be a positive duration"},
		{"an argument", []string{"now"}, exitUsage, "takes no arguments"},
		{"an unknown flag", []string{"-fast"}, exitUsage, "flag provided but not defined"},
		{"help", []string{"-h"}, exitOK, "-for"},
		{"no configuration", nil, exitFailure, "API_DATABASE_URL"},
	} {
		code, stdout, stderr := runCommand(t.Context(), nil, append([]string{"sweep"}, tc.args...)...)
		if code != tc.exit || stdout != "" || !strings.Contains(stderr, tc.says) {
			t.Errorf("%s: sweep = %d, %q, %q; want %d and %q", tc.name, code, stdout, stderr, tc.exit, tc.says)
		}
	}
}

// The process's configuration is refused as serve's is, naming its variable.
func TestSweepRefusesALevelItCannotRead(t *testing.T) {
	t.Parallel()

	code, stdout, stderr := runCommand(t.Context(), []string{"API_LOG_LEVEL=loud"}, "sweep")
	if code != exitFailure || stdout != "" || !strings.Contains(stderr, "API_LOG_LEVEL") {
		t.Errorf("sweep = %d, %q, %q; want a refusal naming API_LOG_LEVEL", code, stdout, stderr)
	}
}
