package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/altnovatrail/api/internal/config"
)

// Where the texts come from and the scans go: a folder outside git, or, on a
// deployment, a bucket with api's prefix; none, and no store of the forms.
func TestTheAgreementsAreRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, tc := range []struct {
		environ []string
		forms   string
	}{
		{nil, config.FormsNone},
		{[]string{"API_LEGAL_DIR=" + dir, "API_CONSENT_FORMS_DIR=" + dir}, config.FormsFolder},
		{[]string{"K_SERVICE=api", "API_CONSENT_FORMS_BUCKET=b", "API_CONSENT_FORMS_PREFIX=api/forms/"}, config.FormsBucket},
		{[]string{"CLOUD_RUN_JOB=api-sweep", "API_CONSENT_FORMS_BUCKET=b", "API_CONSENT_FORMS_PREFIX=api/forms/"}, config.FormsBucket},
	} {
		a, err := config.AgreementsFrom(tc.environ)
		if err != nil || a.Forms() != tc.forms {
			t.Errorf("AgreementsFrom(%v) = %+v, %v; want the store %s", tc.environ, a, err, tc.forms)
		}
	}
}

// Every refusal names its variable.
func TestTheAgreementsRefusals(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		environ []string
		names   string
	}{
		{[]string{"API_LEGAL_DIR=texts"}, "API_LEGAL_DIR"},
		{[]string{"API_CONSENT_FORMS_DIR=/workspaces/api/forms"}, "API_CONSENT_FORMS_DIR"},
		{[]string{"API_CONSENT_FORMS_BUCKET=b", "API_CONSENT_FORMS_PREFIX=p/"}, "API_CONSENT_FORMS_BUCKET"},
		{[]string{"K_SERVICE=api", "API_CONSENT_FORMS_BUCKET=b"}, "API_CONSENT_FORMS_PREFIX"},
		{[]string{"K_SERVICE=api", "API_CONSENT_FORMS_BUCKET=b", "API_CONSENT_FORMS_PREFIX=p"}, "API_CONSENT_FORMS_PREFIX"},
		{[]string{"API_CONSENT_FORMS_PREFIX=p/"}, "API_CONSENT_FORMS_PREFIX"},
		{[]string{"K_SERVICE=api", "API_CONSENT_FORMS_BUCKET=b", "API_CONSENT_FORMS_PREFIX=p/", "API_CONSENT_FORMS_DIR=" + t.TempDir()}, "API_CONSENT_FORMS_DIR"},
	} {
		if _, err := config.AgreementsFrom(tc.environ); !errors.Is(err, config.ErrInvalid) || !strings.Contains(err.Error(), tc.names) {
			t.Errorf("AgreementsFrom(%v) error = %v, want it to name %s", tc.environ, err, tc.names)
		}
	}
	if _, err := config.ServeFrom(append(serveEnviron, "API_LEGAL_DIR=texts"), &config.Config{}); !errors.Is(err, config.ErrInvalid) {
		t.Errorf("ServeFrom with a folder of no absolute path: %v, want a refusal", err)
	}
	if _, err := config.SweepFrom(append(serveEnviron, "API_CONSENT_FORMS_DIR=forms")); !errors.Is(err, config.ErrInvalid) {
		t.Errorf("SweepFrom with a folder of no absolute path: %v, want a refusal", err)
	}
}
