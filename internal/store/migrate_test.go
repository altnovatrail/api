package store_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

func TestMigrationsMakeTheSchemaOnAnEmptyDatabase(t *testing.T) {
	t.Parallel()

	url := storetest.URL(t, false)
	applied, version, err := store.Migrate(t.Context(), url)
	if err != nil {
		t.Fatalf("Migrate error = %v, want nil", err)
	}
	files := migrationFiles(t)
	if applied != len(files) || version != int64(len(files)) {
		t.Errorf("Migrate applied %d, at version %d; want %d and %d", applied, version, len(files), len(files))
	}

	again, version2, err := store.Migrate(t.Context(), url)
	if err != nil || again != 0 || version2 != version {
		t.Errorf("Migrate again = %d at %d, %v; want 0 at %d", again, version2, err, version)
	}
}

func TestMigrateRefusesAStringItCannotRead(t *testing.T) {
	t.Parallel()

	_, _, err := store.Migrate(t.Context(), "postgres://u:not-a-password@host:notaport/api")
	if err == nil || strings.Contains(err.Error(), "not-a-password") {
		t.Errorf("Migrate error = %v, want a refusal that holds no password", err)
	}
}

func TestMigrateFailsOnADatabaseThatIsNotThere(t *testing.T) {
	t.Parallel()

	url := strings.Replace(storetest.URL(t, false), "/test_", "/missing_", 1)
	if _, _, err := store.Migrate(t.Context(), url); err == nil {
		t.Error("Migrate on a database that is not there: error = nil")
	}
}

// migrationFiles is the names of the migrations, in their folder's order.
func migrationFiles(t *testing.T) []string {
	t.Helper()

	files, err := filepath.Glob("migrations/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("no migrations: %v", err)
	}
	return files
}

// What drops, renames or narrows what the release still serving reads or writes:
// a statement that only a migration marked "-- contract:" may hold. A table this
// migration creates no release has used yet, so what narrows it here is free.
var (
	removes = regexp.MustCompile(`(?is)^(DROP|TRUNCATE)\b`)
	altered = regexp.MustCompile(`(?is)^ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?(\w+)`)
	narrows = regexp.MustCompile(`(?is)\b(DROP|RENAME)\b|\bALTER\s+(?:COLUMN\s+)?\w+\s+(?:SET\s+DATA\s+)?TYPE\b|\bSET\s+NOT\s+NULL\b|\bADD\s+CONSTRAINT\b`)
	// A column added NOT NULL with no DEFAULT fails the old release's inserts.
	notNull   = regexp.MustCompile(`(?is)\bNOT\s+NULL\b`)
	defaulted = regexp.MustCompile(`(?is)\bDEFAULT\b`)
	unique    = regexp.MustCompile(`(?is)^CREATE\s+UNIQUE\s+INDEX\b.*?\bON\s+(?:ONLY\s+)?(\w+)`)
	created   = regexp.MustCompile(`(?is)^CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)`)
)

// contract is the mark of a migration that removes, with its reason.
var contract = regexp.MustCompile(`(?m)^-- contract: \S`)

// broken says what a migration breaks of the rules of R29: it goes only forward,
// it has its Up, and it removes only when marked.
func broken(content string) []string {
	var problems []string
	if !strings.Contains(content, "-- +goose Up") {
		problems = append(problems, "it has no -- +goose Up")
	}
	if strings.Contains(content, "-- +goose Down") {
		problems = append(problems, "it has a -- +goose Down: migrations go only forward")
	}
	if contract.MatchString(content) {
		return problems
	}
	var code strings.Builder
	for line := range strings.Lines(content) {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			code.WriteString(line)
		}
	}
	statements := strings.Split(code.String(), ";")
	fresh := map[string]bool{}
	for _, statement := range statements {
		if table := created.FindStringSubmatch(strings.TrimSpace(statement)); table != nil {
			fresh[strings.ToLower(table[1])] = true
		}
	}
	for _, statement := range statements {
		statement = strings.TrimSpace(statement)
		var table string
		switch {
		case statement == "":
			continue
		case removes.MatchString(statement):
			problems = append(problems, "it removes, with no line -- contract: <reason>: "+firstLine(statement))
			continue
		case altered.MatchString(statement):
			table = altered.FindStringSubmatch(statement)[1]
			if !narrows.MatchString(statement) && !(notNull.MatchString(statement) && !defaulted.MatchString(statement)) {
				continue
			}
		case unique.MatchString(statement):
			table = unique.FindStringSubmatch(statement)[1]
		default:
			continue
		}
		if !fresh[strings.ToLower(table)] {
			problems = append(problems, "it narrows "+table+", which a release has used, with no line -- contract: <reason>: "+firstLine(statement))
		}
	}
	return problems
}

// firstLine is a statement's first line, to name it by.
func firstLine(statement string) string {
	line, _, _ := strings.Cut(statement, "\n")
	return line
}

func TestTheRulesOfAMigration(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		content string
		wants   int
	}{
		{"an addition", "-- +goose Up\nCREATE TABLE t (id int);\n", 0},
		{"a drop", "-- +goose Up\nALTER TABLE t DROP COLUMN c;\n", 1},
		{"a rename", "-- +goose Up\nALTER TABLE t RENAME TO u;\n", 1},
		{"a narrowing", "-- +goose Up\nALTER TABLE t ALTER COLUMN c SET NOT NULL;\n", 1},
		{"a new type", "-- +goose Up\nalter table t alter column c type bigint;\n", 1},
		{"a new type without COLUMN", "-- +goose Up\nALTER TABLE t ALTER c TYPE bigint;\n", 1},
		{"a column added NOT NULL with no default", "-- +goose Up\nALTER TABLE t ADD COLUMN c text NOT NULL;\n", 1},
		{"a column added NOT NULL with a default", "-- +goose Up\nALTER TABLE t ADD COLUMN c text NOT NULL DEFAULT '';\n", 0},
		{"a column added that may be null", "-- +goose Up\nALTER TABLE t ADD COLUMN c text;\n", 0},
		{"a constraint added", "-- +goose Up\nALTER TABLE t ADD CONSTRAINT c CHECK (n > 0);\n", 1},
		{"a unique index on a table in use", "-- +goose Up\nCREATE UNIQUE INDEX i ON t (c);\n", 1},
		{"what narrows a table made here", "-- +goose Up\nCREATE TABLE t (c text);\nCREATE UNIQUE INDEX i ON t (c);\nALTER TABLE t ADD CONSTRAINT k CHECK (c <> '');\n", 0},
		{"a truncate", "-- +goose Up\nTRUNCATE t;\n", 1},
		{"a marked drop", "-- contract: v0.1.4 no longer reads c\n-- +goose Up\nALTER TABLE t DROP COLUMN c;\n", 0},
		{"a drop in a comment", "-- +goose Up\n-- we never DROP here\nCREATE TABLE t (id int);\n", 0},
		{"a Down", "-- +goose Up\nCREATE TABLE t (id int);\n-- +goose Down\nDROP TABLE t;\n", 2},
		{"no Up", "CREATE TABLE t (id int);\n", 1},
	} {
		if got := broken(tc.content); len(got) != tc.wants {
			t.Errorf("%s: %v, want %d problems", tc.name, got, tc.wants)
		}
	}
}

// Every migration keeps the rules, and they are numbered from 0001 with no gap.
func TestTheMigrationsKeepTheRules(t *testing.T) {
	t.Parallel()

	name := regexp.MustCompile(`^(\d{4})_[a-z0-9_]+\.sql$`)
	for i, file := range migrationFiles(t) {
		parts := name.FindStringSubmatch(filepath.Base(file))
		if parts == nil {
			t.Errorf("%s is not named NNNN_name.sql", file)
			continue
		}
		if number, _ := strconv.Atoi(parts[1]); number != i+1 {
			t.Errorf("%s is number %d, want %d: the numbers go from 0001 with no gap", file, number, i+1)
		}
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, problem := range broken(string(content)) {
			t.Errorf("%s: %s", file, problem)
		}
	}
}
