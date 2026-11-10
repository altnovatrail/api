package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altnovatrail/api/internal/config"
)

// With nothing declared there is no file of facts, and the stage is full,
// unnamed: closed by default (R25).
func TestAdmissionDefaultsToNoFactsAndTheStageFull(t *testing.T) {
	t.Parallel()

	for _, environ := range [][]string{nil, {"API_ADMISSION_FACTS_FILE=", "API_ADMISSION_RUNG="}, {"K_SERVICE=api"}} {
		got, err := config.AdmissionFrom(environ)
		if err != nil || got != (config.Admission{Rung: config.RungFull}) {
			t.Errorf("AdmissionFrom(%q) = %+v, %v; want no file and the stage full, unnamed", environ, got, err)
		}
	}
}

func TestAdmissionReadsWhatIsDeclared(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "facts.yaml")
	for environ, want := range map[string]config.Admission{
		"API_ADMISSION_RUNG=author":                             {Rung: config.RungAuthor, RungSet: true},
		"API_ADMISSION_RUNG=full K_SERVICE=api":                 {Rung: config.RungFull, RungSet: true},
		"API_ADMISSION_RUNG=full CLOUD_RUN_JOB=check":           {Rung: config.RungFull, RungSet: true},
		"API_ADMISSION_FACTS_FILE=" + path + " CLOUD_RUN_JOB=c": {FactsFile: path, Rung: config.RungFull},
	} {
		got, err := config.AdmissionFrom(strings.Fields(environ))
		if err != nil || got != want {
			t.Errorf("AdmissionFrom(%s) = %+v, %v; want %+v", environ, got, err, want)
		}
	}
}

// The stage author is the first app's alone: deployed, as a service or as a
// job, it is refused, by the variable the platform set. A stage tutor does not
// code, a relative path and a file in a git working tree are refused too, each
// by its variable.
func TestAdmissionRefusals(t *testing.T) {
	t.Parallel()

	tree := t.TempDir()
	if err := os.Mkdir(filepath.Join(tree, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		environ []string
		names   []string
	}{
		"author as a service": {[]string{"API_ADMISSION_RUNG=author", "K_SERVICE=api"}, []string{"API_ADMISSION_RUNG", "K_SERVICE"}},
		"author as a job":     {[]string{"API_ADMISSION_RUNG=author", "CLOUD_RUN_JOB=check"}, []string{"API_ADMISSION_RUNG", "CLOUD_RUN_JOB"}},
		"no stage of tutor":   {[]string{"API_ADMISSION_RUNG=Author"}, []string{"API_ADMISSION_RUNG"}},
		"a relative path":     {[]string{"API_ADMISSION_FACTS_FILE=facts.yaml"}, []string{"API_ADMISSION_FACTS_FILE"}},
		"a working tree":      {[]string{"API_ADMISSION_FACTS_FILE=" + filepath.Join(tree, "facts.yaml")}, []string{"API_ADMISSION_FACTS_FILE"}},
		"both at once": {[]string{"API_ADMISSION_FACTS_FILE=facts.yaml", "API_ADMISSION_RUNG=none"},
			[]string{"API_ADMISSION_FACTS_FILE", "API_ADMISSION_RUNG"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := config.AdmissionFrom(tc.environ)
			if !errors.Is(err, config.ErrInvalid) {
				t.Fatalf("AdmissionFrom error = %v, want %v", err, config.ErrInvalid)
			}
			for _, variable := range tc.names {
				if !strings.Contains(err.Error(), variable) {
					t.Errorf("error %q does not name %s", err, variable)
				}
			}
		})
	}
}

// serve reads what is declared for admission with the rest, and refuses it with
// the rest.
func TestServeReadsWhatIsDeclaredForAdmission(t *testing.T) {
	t.Parallel()

	serve, err := config.ServeFrom(append(serveEnviron, "API_ADMISSION_RUNG=author"), loaded(t, nil))
	if err != nil || serve.Admission != (config.Admission{Rung: config.RungAuthor, RungSet: true}) {
		t.Errorf("ServeFrom = %+v, %v; want the stage author", serve, err)
	}
	deployed := append(without(serveEnviron, "API_FIREBASE_CREDENTIALS"), "K_SERVICE=api", "API_ADMISSION_RUNG=author")
	if _, err := config.ServeFrom(deployed, loaded(t, deployed)); !errors.Is(err, config.ErrInvalid) ||
		!strings.Contains(err.Error(), "API_ADMISSION_RUNG") {
		t.Errorf("ServeFrom deployed with the stage author: error = %v, want a refusal of API_ADMISSION_RUNG", err)
	}
}

// The platform's keys are secrets, each optional, read from its variable or
// from the file its twin names, and both at once is refused.
func TestThePlatformsKeysAreSecrets(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "openai")
	if err := os.WriteFile(path, []byte("sk-openai\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := config.AdmissionFrom([]string{"API_ANTHROPIC_KEY=sk-anthropic", "API_OPENAI_KEY_FILE=" + path})
	if err != nil || got.Keys.Anthropic.Reveal() != "sk-anthropic" || got.Keys.OpenAI.Reveal() != "sk-openai" || len(got.Keys.ByProvider()) != 2 {
		t.Errorf("AdmissionFrom = %d keys, %v; want both", len(got.Keys.ByProvider()), err)
	}
	if none, err := config.AdmissionFrom(nil); err != nil || len(none.Keys.ByProvider()) != 0 {
		t.Errorf("AdmissionFrom of nothing = %d keys, %v; want none", len(none.Keys.ByProvider()), err)
	}
	_, err = config.AdmissionFrom([]string{"API_ANTHROPIC_KEY=sk", "API_ANTHROPIC_KEY_FILE=" + path})
	if !errors.Is(err, config.ErrInvalid) || !strings.Contains(err.Error(), "API_ANTHROPIC_KEY") || strings.Contains(err.Error(), "sk") {
		t.Errorf("both at once = %v, want a refusal naming the variable and not the key", err)
	}
}
