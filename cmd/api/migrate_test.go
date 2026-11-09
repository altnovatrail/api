package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/altnovatrail/api/internal/authn"
	"github.com/altnovatrail/api/internal/authn/authntest"
	"github.com/altnovatrail/api/internal/store/storetest"
)

func TestMain(m *testing.M) {
	// serve's and sweep's tests have no Firebase project: the sign-in service is a
	// fake, the project "broken" one that does not open, and "unlisted" one whose
	// users cannot be listed.
	newSignin = func(_ context.Context, o authn.Options) (authn.Port, error) {
		switch o.ProjectID {
		case "broken":
			return nil, errors.New("the test's sign-in service does not open")
		case "unlisted":
			failing := &authntest.Fake{}
			failing.FailUsers()
			return failing, nil
		}
		return &authntest.Fake{}, nil
	}
	os.Exit(storetest.Main(m))
}

// migrateEnviron is migrate's environment on two empty databases of the test's
// own, api and openfga.
func migrateEnviron(t *testing.T) []string {
	t.Helper()

	return []string{
		"API_MIGRATE_DATABASE_URL=" + storetest.URL(t, false),
		"API_MIGRATE_OPENFGA_DATABASE_URL=" + storetest.OpenFGAURL(t, false),
	}
}

// migrate brings both databases up, the rights with their store and model, says
// so in two lines, and on a second run does nothing.
func TestMigrateAppliesTheMigrations(t *testing.T) {
	t.Parallel()

	environ := migrateEnviron(t)
	var stdout, stderr syncBuffer
	if code := run(t.Context(), []string{"migrate"}, environ, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, want 0; stderr %q, log %s", code, stderr.String(), stdout.String())
	}
	lines := stdout.lines(t)
	if len(lines) != 2 {
		t.Fatalf("the log is %v, want a migrated line for each database", lines)
	}
	api, rights := lines[0], lines[1]
	if api["message"] != "migrated" || api["database"] != "api" || api["applied"] == float64(0) {
		t.Errorf("the line of api is %v, want the migrations applied", api)
	}
	if rights["message"] != "migrated" || rights["database"] != "openfga" || rights["applied"] == float64(0) ||
		rights["store_created"] != true || rights["model_written"] != true {
		t.Errorf("the line of openfga is %v, want the schema, the store and the model", rights)
	}
	for _, line := range lines {
		if _, lost := line["dropped_fields"]; lost {
			t.Errorf("a migrated line lost fields to the guard: %v", line)
		}
	}

	var again syncBuffer
	if code := run(t.Context(), []string{"migrate"}, environ, &again, &stderr); code != exitOK {
		t.Fatalf("second run: exit code = %d, want 0", code)
	}
	lines = again.lines(t)
	if len(lines) != 2 || lines[0]["applied"] != float64(0) || lines[1]["applied"] != float64(0) ||
		lines[1]["store_created"] != false || lines[1]["model_written"] != false {
		t.Errorf("the second run's log is %v, want nothing applied, made or written", lines)
	}
}

// Without its connection strings migrate starts nothing, and names them.
func TestMigrateWithoutItsStringsIsRefused(t *testing.T) {
	t.Parallel()

	code, stdout, stderr := runCommand(t.Context(), nil, "migrate")
	if code != exitFailure || stdout != "" ||
		!strings.Contains(stderr, "API_MIGRATE_DATABASE_URL") || !strings.Contains(stderr, "API_MIGRATE_OPENFGA_DATABASE_URL") {
		t.Errorf("migrate without its strings = %d, %q, %q; want a refusal naming both", code, stdout, stderr)
	}
}

// A database migrate cannot reach is a failure, logged once, by its kind: api's,
// and, after api's has migrated, the rights'.
func TestMigrateOnADatabaseThatIsNotThereFails(t *testing.T) {
	t.Parallel()

	for which, database := range map[string]string{"API_MIGRATE_DATABASE_URL": "api", "API_MIGRATE_OPENFGA_DATABASE_URL": "openfga"} {
		t.Run(which, func(t *testing.T) {
			t.Parallel()

			environ := migrateEnviron(t)
			for i, entry := range environ {
				if strings.HasPrefix(entry, which+"=") {
					environ[i] = strings.Replace(entry, "/test_", "/missing_", 1)
				}
			}
			var stdout, stderr syncBuffer
			code := run(t.Context(), []string{"migrate"}, environ, &stdout, &stderr)
			if code != exitFailure || stderr.String() != "" {
				t.Fatalf("exit code = %d, stderr %q; want 1 and the failure in the log alone", code, stderr.String())
			}
			lines := stdout.lines(t)
			last := lines[len(lines)-1]
			if last["message"] != "failure" || last["moment"] != "migrate" || last["database"] != database {
				t.Errorf("the log is %v, want it to end in a failure at the moment migrate of %s", lines, database)
			}
			if strings.Contains(stdout.String(), "postgres") {
				t.Errorf("the log holds a connection string: %s", stdout.String())
			}
		})
	}
}

func TestMigrateTakesNoArguments(t *testing.T) {
	t.Parallel()

	if code, _, _ := runCommand(t.Context(), nil, "migrate", "now"); code != exitUsage {
		t.Errorf("migrate now: exit code = %d, want %d", code, exitUsage)
	}
}
