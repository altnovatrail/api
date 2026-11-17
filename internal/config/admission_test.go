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
		if err != nil || got != (config.Admission{Rung: config.RungFull, Reading: config.ReadingPair}) {
			t.Errorf("AdmissionFrom(%q) = %+v, %v; want no file and the stage full, unnamed", environ, got, err)
		}
	}
}

func TestAdmissionReadsWhatIsDeclared(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "facts.yaml")
	for environ, want := range map[string]config.Admission{
		"API_ADMISSION_RUNG=author":                             {Rung: config.RungAuthor, RungSet: true, Reading: config.ReadingPair},
		"API_ADMISSION_RUNG=full K_SERVICE=api":                 {Rung: config.RungFull, RungSet: true, Reading: config.ReadingPair},
		"API_ADMISSION_RUNG=full CLOUD_RUN_JOB=check":           {Rung: config.RungFull, RungSet: true, Reading: config.ReadingPair},
		"API_ADMISSION_RUNG=author K_SERVICE=api":               {Rung: config.RungAuthor, RungSet: true, Reading: config.ReadingPair},
		"API_ADMISSION_RUNG=author CLOUD_RUN_JOB=check":         {Rung: config.RungAuthor, RungSet: true, Reading: config.ReadingPair},
		"API_ADMISSION_FACTS_FILE=" + path + " CLOUD_RUN_JOB=c": {FactsFile: path, Rung: config.RungFull, Reading: config.ReadingPair},
	} {
		got, err := config.AdmissionFrom(strings.Fields(environ))
		if err != nil || got != want {
			t.Errorf("AdmissionFrom(%s) = %+v, %v; want %+v", environ, got, err, want)
		}
	}
}

// The stage is the operator's, author on a deployment too (R149 spec): a stage
// tutor does not code, a relative path and a file in a git working tree are
// refused, each by its variable.
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
		"no stage of tutor": {[]string{"API_ADMISSION_RUNG=Author"}, []string{"API_ADMISSION_RUNG"}},
		"a relative path":   {[]string{"API_ADMISSION_FACTS_FILE=facts.yaml"}, []string{"API_ADMISSION_FACTS_FILE"}},
		"a working tree":    {[]string{"API_ADMISSION_FACTS_FILE=" + filepath.Join(tree, "facts.yaml")}, []string{"API_ADMISSION_FACTS_FILE"}},
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
	if err != nil || serve.Admission != (config.Admission{Rung: config.RungAuthor, RungSet: true, Reading: config.ReadingPair}) {
		t.Errorf("ServeFrom = %+v, %v; want the stage author", serve, err)
	}
	deployed := append(without(serveEnviron, "API_FIREBASE_CREDENTIALS"), "K_SERVICE=api", "API_ADMISSION_RUNG=author")
	if serve, err := config.ServeFrom(deployed, loaded(t, deployed)); err != nil || serve.Admission.Rung != config.RungAuthor {
		t.Errorf("ServeFrom deployed with the stage author = %+v, %v; want the stage author, as on novatrail", serve, err)
	}
	broken := append(serveEnviron, "API_ADMISSION_RUNG=none")
	if _, err := config.ServeFrom(broken, loaded(t, nil)); !errors.Is(err, config.ErrInvalid) ||
		!strings.Contains(err.Error(), "API_ADMISSION_RUNG") {
		t.Errorf("ServeFrom with no stage of tutor: error = %v, want a refusal of API_ADMISSION_RUNG", err)
	}
}

// The platform's key is a secret, optional, read from its variable or from the
// file its twin names, and both at once is refused.
func TestThePlatformsKeysAreSecrets(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "anthropic")
	if err := os.WriteFile(path, []byte("sk-anthropic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, environ := range [][]string{{"API_ANTHROPIC_KEY=sk-anthropic"}, {"API_ANTHROPIC_KEY_FILE=" + path}} {
		got, err := config.AdmissionFrom(environ)
		if err != nil || got.Keys.Anthropic.Reveal() != "sk-anthropic" || len(got.Keys.ByProvider()) != 1 {
			t.Errorf("AdmissionFrom(%s) = %d keys, %v; want Anthropic's", strings.SplitN(environ[0], "=", 2)[0],
				len(got.Keys.ByProvider()), err)
		}
	}
	if none, err := config.AdmissionFrom(nil); err != nil || len(none.Keys.ByProvider()) != 0 {
		t.Errorf("AdmissionFrom of nothing = %d keys, %v; want none", len(none.Keys.ByProvider()), err)
	}
	_, err := config.AdmissionFrom([]string{"API_ANTHROPIC_KEY=sk", "API_ANTHROPIC_KEY_FILE=" + path})
	if !errors.Is(err, config.ErrInvalid) || !strings.Contains(err.Error(), "API_ANTHROPIC_KEY") || strings.Contains(err.Error(), "sk") {
		t.Errorf("both at once = %v, want a refusal naming the variable and not the key", err)
	}
}

// A key of OpenAI, inline or by its file, is refused: api gives tutor no
// adapter of OpenAI (R54), and the refusal names the variable, not the key.
func TestAKeyOfOpenAIIsRefused(t *testing.T) {
	t.Parallel()

	for _, entry := range []string{"API_OPENAI_KEY=sk-openai", "API_OPENAI_KEY_FILE=/run/secrets/openai"} {
		name := strings.SplitN(entry, "=", 2)[0]
		_, err := config.AdmissionFrom([]string{"API_ANTHROPIC_KEY=sk-anthropic", entry})
		if !errors.Is(err, config.ErrInvalid) || !strings.Contains(err.Error(), name+" is set") ||
			strings.Contains(err.Error(), "sk-") || strings.Contains(err.Error(), "/run/secrets") {
			t.Errorf("AdmissionFrom with %s = %v, want a refusal naming the variable alone", name, err)
		}
	}
}

// The stub of tutor is the dev mode's alone (R66): off a deployment, with no
// platform key, API_TUTOR_STUB=1 is taken, and empty is unset. Deployed, as a
// service or as a job, it is refused by the variable the platform set; so is
// it beside a key of Anthropic, inline or by its file, and with any value but 1.
func TestTheStubOfTutorIsTheDevModesAlone(t *testing.T) {
	t.Parallel()

	for environ, want := range map[string]config.Admission{
		"API_TUTOR_STUB=1":                           {Rung: config.RungFull, Stub: true, Reading: config.ReadingPair},
		"API_TUTOR_STUB=1 API_ADMISSION_RUNG=author": {Rung: config.RungAuthor, RungSet: true, Stub: true, Reading: config.ReadingPair},
		"API_TUTOR_STUB=":                            {Rung: config.RungFull, Reading: config.ReadingPair},
	} {
		got, err := config.AdmissionFrom(strings.Fields(environ))
		if err != nil || got != want {
			t.Errorf("AdmissionFrom(%s) = %+v, %v; want %+v", environ, got, err, want)
		}
	}
	for name, tc := range map[string]struct {
		environ []string
		names   []string
	}{
		"as a service":        {[]string{"API_TUTOR_STUB=1", "K_SERVICE=api"}, []string{"API_TUTOR_STUB", "K_SERVICE"}},
		"as a job":            {[]string{"API_TUTOR_STUB=1", "CLOUD_RUN_JOB=check"}, []string{"API_TUTOR_STUB", "CLOUD_RUN_JOB"}},
		"beside a key":        {[]string{"API_TUTOR_STUB=1", "API_ANTHROPIC_KEY=sk-anthropic"}, []string{"API_TUTOR_STUB", "API_ANTHROPIC_KEY"}},
		"beside a key's file": {[]string{"API_TUTOR_STUB=1", "API_ANTHROPIC_KEY_FILE=/run/secrets/anthropic"}, []string{"API_TUTOR_STUB", "API_ANTHROPIC_KEY"}},
		"another value":       {[]string{"API_TUTOR_STUB=true"}, []string{"API_TUTOR_STUB"}},
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
			if strings.Contains(err.Error(), "sk-") {
				t.Errorf("error %q holds the key", err)
			}
		})
	}
}

// The mode of reading is pair unset or empty, and single where it is named, at
// either stage, deployed too (R88): a value tutor does not code is refused by
// its variable.
func TestTheModeOfReading(t *testing.T) {
	t.Parallel()

	for environ, want := range map[string]config.Admission{
		"API_READING_MODE=":                                             {Rung: config.RungFull, Reading: config.ReadingPair},
		"API_READING_MODE=pair":                                         {Rung: config.RungFull, Reading: config.ReadingPair},
		"API_READING_MODE=single":                                       {Rung: config.RungFull, Reading: config.ReadingSingle},
		"API_READING_MODE=single API_ADMISSION_RUNG=full":               {Rung: config.RungFull, RungSet: true, Reading: config.ReadingSingle},
		"API_READING_MODE=single API_ADMISSION_RUNG=author":             {Rung: config.RungAuthor, RungSet: true, Reading: config.ReadingSingle},
		"API_READING_MODE=single K_SERVICE=api":                         {Rung: config.RungFull, Reading: config.ReadingSingle},
		"API_READING_MODE=single API_TUTOR_STUB=1":                      {Rung: config.RungFull, Stub: true, Reading: config.ReadingSingle},
		"API_READING_MODE=pair API_ADMISSION_RUNG=author K_SERVICE=api": {Rung: config.RungAuthor, RungSet: true, Reading: config.ReadingPair},
	} {
		got, err := config.AdmissionFrom(strings.Fields(environ))
		if err != nil || got != want {
			t.Errorf("AdmissionFrom(%s) = %+v, %v; want %+v", environ, got, err, want)
		}
	}
	for _, value := range []string{"Single", "one", "pair,single"} {
		_, err := config.AdmissionFrom([]string{"API_READING_MODE=" + value})
		if !errors.Is(err, config.ErrInvalid) || !strings.Contains(err.Error(), "API_READING_MODE") {
			t.Errorf("AdmissionFrom(API_READING_MODE=%s) error = %v, want %v naming the variable", value, err, config.ErrInvalid)
		}
	}
}
