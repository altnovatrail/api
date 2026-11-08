package config_test

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altnovatrail/api/internal/config"
)

// The connection strings below are synthetic: no test holds a real one.
const (
	remote = "postgresql://api:not-a-password@ep-example-123456.us-east-1.aws.neon.tech/api?sslmode=verify-full"
	local  = "postgresql://postgres:postgres@127.0.0.1:5432/api?sslmode=disable"
)

func TestMigrateReadsTheDirectConnectionString(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		environ []string
		want    string
	}{
		{"remote with verify-full", []string{"API_MIGRATE_DATABASE_URL=" + remote}, remote},
		{"the loopback", []string{"API_MIGRATE_DATABASE_URL=" + local}, local},
		{"localhost", []string{"API_MIGRATE_DATABASE_URL=postgres://u:p@localhost/api"}, "postgres://u:p@localhost/api"},
		{"IPv6 loopback", []string{"API_MIGRATE_DATABASE_URL=postgres://u:p@[::1]:5432/api"}, "postgres://u:p@[::1]:5432/api"},
		{"a Unix socket", []string{"API_MIGRATE_DATABASE_URL=postgres://u:p@/api?host=/var/run/postgresql"}, "postgres://u:p@/api?host=/var/run/postgresql"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := config.MigrateFrom(append(tc.environ, "API_LOG_LEVEL=warn"))
			if err != nil {
				t.Fatalf("MigrateFrom error = %v, want nil", err)
			}
			if cfg.DatabaseURL.Reveal() != tc.want || cfg.LogLevel != slog.LevelWarn {
				t.Errorf("MigrateFrom = %v %v, want the string and level warn", cfg.DatabaseURL, cfg.LogLevel)
			}
		})
	}
}

func TestMigrateReadsTheStringFromAFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "database-url")
	if err := os.WriteFile(path, []byte(remote+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.MigrateFrom([]string{"API_MIGRATE_DATABASE_URL_FILE=" + path})
	if err != nil {
		t.Fatalf("MigrateFrom error = %v, want nil", err)
	}
	if cfg.DatabaseURL.Reveal() != remote {
		t.Errorf("the string read from the file is not the file's")
	}
}

// A refusal names the variable, and holds nothing of the value.
func TestMigrateRefusals(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		environ []string
	}{
		{"unset", nil},
		{"not a URL", []string{"API_MIGRATE_DATABASE_URL=host=ep-example dbname=api"}},
		{"another scheme", []string{"API_MIGRATE_DATABASE_URL=mysql://u:not-a-password@ep-example/api"}},
		// The four below a URL parser reads as safe, and pgx as a server off this
		// machine with no check of its certificate.
		{"a host in the query", []string{"API_MIGRATE_DATABASE_URL=postgres://u:not-a-password@localhost/api?host=ep-example.neon.tech"}},
		{"a second sslmode", []string{"API_MIGRATE_DATABASE_URL=" + remote + "&sslmode=disable"}},
		{"ssl=true", []string{"API_MIGRATE_DATABASE_URL=" + remote + "&ssl=true"}},
		{"a user that hides the host", []string{"API_MIGRATE_DATABASE_URL=postgres://localhost?a=not-a-password@ep-example.neon.tech/api"}},
		{"sslmode prefer, which falls back to plain text", []string{"API_MIGRATE_DATABASE_URL=" + strings.Replace(remote, "verify-full", "prefer", 1)}},
		{"sslmode require", []string{"API_MIGRATE_DATABASE_URL=" + strings.Replace(remote, "verify-full", "require", 1)}},
		{"no sslmode", []string{"API_MIGRATE_DATABASE_URL=" + strings.Replace(remote, "?sslmode=verify-full", "", 1)}},
		{"unparsable", []string{"API_MIGRATE_DATABASE_URL=postgres://u:not-a-password@ep example/%zz"}},
		{"bad level", []string{"API_MIGRATE_DATABASE_URL=" + remote, "API_LOG_LEVEL=loud"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.MigrateFrom(tc.environ)
			if !errors.Is(err, config.ErrInvalid) {
				t.Fatalf("MigrateFrom error = %v, want %v", err, config.ErrInvalid)
			}
			if !strings.Contains(err.Error(), "API_") {
				t.Errorf("error %q names no variable", err)
			}
			if strings.Contains(err.Error(), "not-a-password") {
				t.Errorf("error %q holds the connection string", err)
			}
		})
	}
}
