package api_test

import (
	"testing"

	"github.com/altnovatrail/api"
)

// The embedded modules.json is a release and the digest of its index.
func TestModulesJSONPinsAReleaseAndItsIndex(t *testing.T) {
	t.Parallel()

	cfg, err := api.Modules()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Release == "" || cfg.Index == "" {
		t.Errorf("Modules = %+v", cfg)
	}
}
