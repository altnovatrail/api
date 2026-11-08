package main

import (
	"os"
	"strings"
	"testing"

	"github.com/altnovatrail/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

// migrate applies every migration to an empty database, says so in its log,
// and on a second run applies none.
func TestMigrateAppliesTheMigrations(t *testing.T) {
	t.Parallel()

	environ := []string{"API_MIGRATE_DATABASE_URL=" + storetest.URL(t, false)}
	var stdout, stderr syncBuffer
	if code := run(t.Context(), []string{"migrate"}, environ, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, want 0; stderr %q, log %s", code, stderr.String(), stdout.String())
	}
	lines := stdout.lines(t)
	if len(lines) != 1 || lines[0]["message"] != "migrated" || lines[0]["applied"] == float64(0) ||
		lines[0]["schema_version"] == float64(0) {
		t.Errorf("the log is %v, want one migrated line that applied the migrations", lines)
	}
	if _, lost := lines[0]["dropped_fields"]; lost {
		t.Errorf("the migrated line lost fields to the guard: %v", lines[0])
	}

	var again syncBuffer
	if code := run(t.Context(), []string{"migrate"}, environ, &again, &stderr); code != exitOK {
		t.Fatalf("second run: exit code = %d, want 0", code)
	}
	if lines := again.lines(t); len(lines) != 1 || lines[0]["applied"] != float64(0) {
		t.Errorf("the second run's log is %v, want a migrated line that applied none", lines)
	}
}

// Without its connection string migrate starts nothing, and says which variable.
func TestMigrateWithoutItsStringIsRefused(t *testing.T) {
	t.Parallel()

	code, stdout, stderr := runCommand(t.Context(), nil, "migrate")
	if code != exitFailure || stdout != "" || !strings.Contains(stderr, "API_MIGRATE_DATABASE_URL") {
		t.Errorf("migrate without its string = %d, %q, %q; want a refusal naming the variable", code, stdout, stderr)
	}
}

// A database migrate cannot reach is a failure, logged once, by its kind.
func TestMigrateOnADatabaseThatIsNotThereFails(t *testing.T) {
	t.Parallel()

	url := strings.Replace(storetest.URL(t, false), "/test_", "/missing_", 1)
	var stdout, stderr syncBuffer
	code := run(t.Context(), []string{"migrate"}, []string{"API_MIGRATE_DATABASE_URL=" + url}, &stdout, &stderr)
	if code != exitFailure || stderr.String() != "" {
		t.Fatalf("exit code = %d, stderr %q; want 1 and the failure in the log alone", code, stderr.String())
	}
	lines := stdout.lines(t)
	if len(lines) != 1 || lines[0]["message"] != "failure" || lines[0]["moment"] != "migrate" {
		t.Errorf("the log is %v, want one failure at the moment migrate", lines)
	}
	if strings.Contains(stdout.String(), "postgres:") {
		t.Errorf("the log holds the connection string: %s", stdout.String())
	}
}

func TestMigrateTakesNoArguments(t *testing.T) {
	t.Parallel()

	if code, _, _ := runCommand(t.Context(), nil, "migrate", "now"); code != exitUsage {
		t.Errorf("migrate now: exit code = %d, want %d", code, exitUsage)
	}
}
