package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// The variables of the texts and the forms of consent (R52).
const (
	// VarLegalDir is the absolute path of a self-hosted copy's folder of its
	// operator's legal texts, outside any git working tree; unset, api serves
	// the texts of its tag of spec (legal/README.md of spec, section 8).
	VarLegalDir = "API_LEGAL_DIR"
	// VarConsentFormsBucket is the operator's closed bucket of Cloud Storage the
	// scans of the forms of a parent's consent go to, under the prefix of
	// VarConsentFormsPrefix (R116 spec). A deployment alone names it: api
	// writes with its own account, and off a deployment takes no default
	// credentials.
	VarConsentFormsBucket = "API_CONSENT_FORMS_BUCKET"
	// VarConsentFormsPrefix is api's prefix in the bucket, ending in a slash.
	VarConsentFormsPrefix = "API_CONSENT_FORMS_PREFIX"
	// VarConsentFormsDir is the absolute path of a folder outside any git
	// working tree the scans go to instead, off a deployment: the dev projects
	// and the first app's.
	VarConsentFormsDir = "API_CONSENT_FORMS_DIR"
)

func init() {
	for _, name := range []string{VarLegalDir, VarConsentFormsBucket, VarConsentFormsPrefix, VarConsentFormsDir} {
		declared[name] = struct{}{}
	}
}

// Agreements is where the legal texts come from and where the scans of the
// forms go.
type Agreements struct {
	// LegalDir is the copy's folder of texts, or empty for spec's.
	LegalDir string
	// FormsBucket and FormsPrefix are the bucket of the scans and api's prefix in
	// it, FormsDir the folder instead; all empty, no store is configured, and
	// SendConsentForm is refused unavailable.
	FormsBucket string
	FormsPrefix string
	FormsDir    string
}

// AgreementsFrom reads where the texts come from and the scans go from a set of
// KEY=VALUE strings, and validates it: a folder is an absolute path outside any
// git working tree, a bucket comes with its prefix and only on a deployment —
// K_SERVICE or CLOUD_RUN_JOB set —, and a bucket and a folder are not both
// named.
func AgreementsFrom(environ []string) (Agreements, error) {
	env := read(environ)
	_, service := env[VarService]
	_, job := env[VarJob]
	deployed := service || job
	cfg := Agreements{LegalDir: env[VarLegalDir], FormsBucket: env[VarConsentFormsBucket],
		FormsPrefix: env[VarConsentFormsPrefix], FormsDir: env[VarConsentFormsDir]}
	var problems []error
	for _, dir := range []struct{ name, path string }{{VarLegalDir, cfg.LegalDir}, {VarConsentFormsDir, cfg.FormsDir}} {
		switch {
		case dir.path == "":
		case !filepath.IsAbs(dir.path):
			problems = append(problems, fmt.Errorf("%w: %s must be an absolute path, got %q", ErrInvalid, dir.name, dir.path))
		case inWorkingTree(dir.path):
			problems = append(problems, fmt.Errorf("%w: %s names a folder in a git working tree, %q: keep it outside any repository",
				ErrInvalid, dir.name, dir.path))
		}
	}
	switch {
	case cfg.FormsBucket != "" && cfg.FormsDir != "":
		problems = append(problems, fmt.Errorf("%w: %s and %s are both set: name one store of the forms",
			ErrInvalid, VarConsentFormsBucket, VarConsentFormsDir))
	case cfg.FormsBucket != "" && !deployed:
		problems = append(problems, fmt.Errorf("%w: %s is set off a deployment: name a folder by %s",
			ErrInvalid, VarConsentFormsBucket, VarConsentFormsDir))
	case cfg.FormsBucket != "" && (cfg.FormsPrefix == "" || !strings.HasSuffix(cfg.FormsPrefix, "/")):
		problems = append(problems, fmt.Errorf("%w: %s must name api's prefix in the bucket, ending in a slash",
			ErrInvalid, VarConsentFormsPrefix))
	case cfg.FormsBucket == "" && cfg.FormsPrefix != "":
		problems = append(problems, fmt.Errorf("%w: %s is set with no %s", ErrInvalid, VarConsentFormsPrefix,
			VarConsentFormsBucket))
	}
	if len(problems) > 0 {
		return Agreements{}, errors.Join(problems...)
	}
	return cfg, nil
}

// The stores of the forms, as the lines of startup and admission name them.
const (
	FormsNone   = "none"
	FormsBucket = "bucket"
	FormsFolder = "folder"
)

// Forms is the store of the scans that is configured, by its word.
func (a Agreements) Forms() string {
	switch {
	case a.FormsBucket != "":
		return FormsBucket
	case a.FormsDir != "":
		return FormsFolder
	}
	return FormsNone
}
