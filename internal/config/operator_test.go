package config_test

import (
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/altnovatrail/api/internal/config"
)

func TestAnOperatorsCommandReadsThePoolersString(t *testing.T) {
	t.Parallel()

	cfg, err := config.OperatorFrom([]string{"API_DATABASE_URL=" + remote, "API_LOG_LEVEL=warn",
		// What serve and migrate read besides is not this command's to read.
		"API_MIGRATE_DATABASE_URL=" + local, "API_FIREBASE_PROJECT_ID=novatrail-example"})
	if err != nil {
		t.Fatalf("OperatorFrom error = %v, want nil", err)
	}
	if cfg.DatabaseURL.Reveal() != remote || cfg.LogLevel != slog.LevelWarn || cfg.DeployedBy != "" {
		t.Errorf("OperatorFrom = %v %v %q, want the pooler's string, level warn and no deployment", cfg.DatabaseURL, cfg.LogLevel, cfg.DeployedBy)
	}
}

// A command run as a service or as a job knows it is deployed, by the variable
// Cloud Run sets, which it names.
func TestAnOperatorsCommandKnowsItIsDeployed(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"K_SERVICE", "CLOUD_RUN_JOB"} {
		cfg, err := config.OperatorFrom([]string{"API_DATABASE_URL=" + remote, name + "=api"})
		if err != nil {
			t.Fatalf("OperatorFrom with %s error = %v, want nil", name, err)
		}
		if cfg.DeployedBy != name {
			t.Errorf("OperatorFrom with %s: DeployedBy = %q", name, cfg.DeployedBy)
		}
	}
}

// A refusal names every variable at fault, and holds nothing of a value.
func TestAnOperatorsCommandRefusesWhatItCannotRun(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		environ []string
		names   []string
	}{
		{"unset", nil, []string{"API_DATABASE_URL"}},
		{"no TLS that checks", []string{"API_DATABASE_URL=postgresql://api:not-a-password@ep-example.neon.tech/api?sslmode=require"},
			[]string{"API_DATABASE_URL", "verify-full"}},
		{"a level and a string", []string{"API_LOG_LEVEL=loud", "API_DATABASE_URL=mysql://u:not-a-password@ep-example/api"},
			[]string{"API_LOG_LEVEL", "API_DATABASE_URL"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.OperatorFrom(tc.environ)
			if !errors.Is(err, config.ErrInvalid) {
				t.Fatalf("OperatorFrom error = %v, want ErrInvalid", err)
			}
			for _, name := range tc.names {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("the refusal %q does not name %s", err, name)
				}
			}
			if strings.Contains(err.Error(), "not-a-password") {
				t.Errorf("the refusal %q holds the password", err)
			}
		})
	}
}
