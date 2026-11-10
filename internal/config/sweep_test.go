package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/altnovatrail/api/internal/config"
)

func TestSweepReadsWhatItOpens(t *testing.T) {
	t.Parallel()

	sweep, err := config.SweepFrom(serveEnviron)
	if err != nil {
		t.Fatalf("SweepFrom error = %v, want nil", err)
	}
	if sweep.DatabaseURL.Reveal() != remote || sweep.OpenFGADatabaseURL.Reveal() != local ||
		sweep.FirebaseProjectID != "novatrail-test" || sweep.FirebaseCredentials.Reveal() != `{"type":"service_account"}` ||
		sweep.Deployed() || sweep.PackagesDir != config.DefaultPackagesDir {
		t.Errorf("SweepFrom = %+v, want the environment's values", sweep)
	}
	if sweep, err := config.SweepFrom(append(serveEnviron, "API_PACKAGES_DIR=/data/packages")); err != nil || sweep.PackagesDir != "/data/packages" {
		t.Errorf("SweepFrom = %+v, %v; want the folder of the packages named", sweep, err)
	}
}

// As a Cloud Run job, sweep takes its own account's token, with no key; off a
// deployment, the key is required (R32).
func TestTheJobsKeyIsForThisMachineOnly(t *testing.T) {
	t.Parallel()

	deployed := append(without(serveEnviron, "API_FIREBASE_CREDENTIALS"), "CLOUD_RUN_JOB=api-sweep")
	sweep, err := config.SweepFrom(deployed)
	if err != nil || sweep.FirebaseCredentials.Reveal() != "" || !sweep.Deployed() {
		t.Errorf("SweepFrom deployed with no key = %+v, %v; want no key and no error", sweep, err)
	}
	if _, err := config.SweepFrom(append(serveEnviron, "CLOUD_RUN_JOB=api-sweep")); !errors.Is(err, config.ErrInvalid) ||
		!strings.Contains(err.Error(), "CLOUD_RUN_JOB") {
		t.Errorf("SweepFrom deployed with the key: error = %v, want a refusal naming the job's variable", err)
	}
	if _, err := config.SweepFrom(without(serveEnviron, "API_FIREBASE_CREDENTIALS")); !errors.Is(err, config.ErrInvalid) {
		t.Errorf("SweepFrom off a deployment with no key: error = %v, want a refusal", err)
	}
}

// Every refusal is named at once, by its variable.
func TestSweepRefusals(t *testing.T) {
	t.Parallel()

	environ := append(without(without(without(serveEnviron, "API_DATABASE_URL"), "API_FIREBASE_PROJECT_ID"), "API_OPENFGA_DATABASE_URL"),
		"API_OPENFGA_DATABASE_URL=not a string", "API_PACKAGES_DIR=packages")
	_, err := config.SweepFrom(environ)
	for _, name := range []string{"API_DATABASE_URL", "API_OPENFGA_DATABASE_URL", "API_FIREBASE_PROJECT_ID", "API_PACKAGES_DIR"} {
		if !errors.Is(err, config.ErrInvalid) || !strings.Contains(err.Error(), name) {
			t.Errorf("SweepFrom error = %v, want it to name %s", err, name)
		}
	}
}
