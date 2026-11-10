package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/altnovatrail/api/internal/config"
)

// The job after a rollout reads the database api, by the pooler, and the folder
// of the packages, and nothing of the rights or the sign-in service.
func TestAfterReadsWhatItOpens(t *testing.T) {
	t.Parallel()

	after, err := config.AfterFrom([]string{"API_DATABASE_URL=" + remote})
	if err != nil {
		t.Fatalf("AfterFrom error = %v, want nil", err)
	}
	if after.DatabaseURL.Reveal() != remote || after.PackagesDir != config.DefaultPackagesDir {
		t.Errorf("AfterFrom = %+v, want the environment's values", after)
	}
	if after, err := config.AfterFrom([]string{"API_DATABASE_URL=" + remote, "API_PACKAGES_DIR=/data/packages"}); err != nil ||
		after.PackagesDir != "/data/packages" {
		t.Errorf("AfterFrom = %+v, %v; want the folder of the packages named", after, err)
	}
}

// Every refusal is named at once, by its variable.
func TestAfterRefusals(t *testing.T) {
	t.Parallel()

	_, err := config.AfterFrom([]string{"API_DATABASE_URL=not a string", "API_PACKAGES_DIR=packages"})
	for _, name := range []string{"API_DATABASE_URL", "API_PACKAGES_DIR"} {
		if !errors.Is(err, config.ErrInvalid) || !strings.Contains(err.Error(), name) {
			t.Errorf("AfterFrom error = %v, want it to name %s", err, name)
		}
	}
}
