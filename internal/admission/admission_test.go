package admission_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/spec/types/formatv0"

	"github.com/altnovatrail/api/internal/admission"
	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/logging"
)

// adultFacts open the reading of handwriting by Anthropic's models on platform
// AI for an adult, and for nobody else.
const adultFacts = `# The first app's facts.
anthropic:
  data_processing_agreement: [adult]
  minimal_retention: [adult]
`

// file is a file of facts in a folder of the test's own, outside any working
// tree.
func file(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "facts.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func load(t *testing.T, cfg config.Admission) *admission.Gate {
	t.Helper()
	g, _, err := admission.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// What the gate admits is what the table, the facts and the stage admit: with no
// facts no platform AI, at the stage full none of the models admitted at the
// author's, a child never, and an own key by its provider alone.
func TestTheGateAdmitsByTheFactsAndTheStage(t *testing.T) {
	t.Parallel()

	none := config.Admission{Rung: config.RungAuthor}
	facts := config.Admission{FactsFile: file(t, adultFacts), Rung: config.RungAuthor}
	full := config.Admission{FactsFile: file(t, adultFacts), Rung: config.RungFull}
	empty := config.Admission{FactsFile: file(t, "# none yet\n"), Rung: config.RungAuthor}
	for name, tc := range map[string]struct {
		cfg      config.Admission
		category formatv0.AgeCategory
		provider string
		want     bool
	}{
		"no facts":                     {none, formatv0.AgeCategoryAdult, "", false},
		"a file with no facts":         {empty, formatv0.AgeCategoryAdult, "", false},
		"facts for an adult":           {facts, formatv0.AgeCategoryAdult, "", true},
		"facts for an adult, a child":  {facts, formatv0.AgeCategoryUnder13, "", false},
		"facts for an adult, a teen":   {facts, formatv0.AgeCategoryFrom13ToConsent, "", false},
		"facts at the stage full":      {full, formatv0.AgeCategoryAdult, "", false},
		"an own key of Anthropic":      {none, formatv0.AgeCategoryAdult, "anthropic", true},
		"an own key of OpenAI":         {none, formatv0.AgeCategoryAdult, "openai", false},
		"an own key no table names":    {none, formatv0.AgeCategoryAdult, "acme", false},
		"an own key of a child":        {none, formatv0.AgeCategoryUnder13, "anthropic", false},
		"an own key at the stage full": {config.Admission{Rung: config.RungFull}, formatv0.AgeCategoryAdult, "anthropic", false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := load(t, tc.cfg).Handwriting(tc.category, tc.provider); got != tc.want {
				t.Errorf("Handwriting(%s, %q) = %v, want %v", tc.category, tc.provider, got, tc.want)
			}
		})
	}
}

// A file that cannot be read, or that names what the table does not, is a
// refusal of the configuration that names the variable.
func TestAFileOfFactsTheTableRefusesIsInvalid(t *testing.T) {
	t.Parallel()

	for name, path := range map[string]string{
		"no such file":    filepath.Join(t.TempDir(), "gone.yaml"),
		"an unknown fact": file(t, "anthropic:\n  data_procesing_agreement: [adult]\n"),
		"an unknown one":  file(t, "acme:\n  minimal_retention: [adult]\n"),
		"no YAML":         file(t, "anthropic: [\n"),
		"no category":     file(t, "anthropic:\n  minimal_retention: [grown_up]\n"),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := admission.Load(config.Admission{FactsFile: path, Rung: config.RungFull})
			if !errors.Is(err, config.ErrInvalid) || !strings.Contains(err.Error(), config.VarAdmissionFactsFile) {
				t.Errorf("Load = %v, want a refusal of %s", err, config.VarAdmissionFactsFile)
			}
		})
	}
}

// The report says what was declared, and what the log may carry of it.
func TestTheReportSaysWhatWasDeclared(t *testing.T) {
	t.Parallel()

	_, r, err := admission.Load(config.Admission{FactsFile: file(t, adultFacts), Rung: config.RungAuthor, RungSet: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := (admission.Report{TableVersion: "0.2", FactsFile: true, Rung: "author", RungSet: true}); r != want {
		t.Errorf("Report = %+v, want %+v", r, want)
	}
	_, r, err = admission.Load(config.Admission{Rung: config.RungFull})
	if err != nil {
		t.Fatal(err)
	}
	if want := (admission.Report{TableVersion: "0.2", Rung: "full"}); r != want {
		t.Errorf("Report of nothing declared = %+v, want %+v", r, want)
	}
}

// The age categories of the format and of the table are one set of strings, so
// that the gate passes the learner's as it is; and the stages of config are
// the log's.
func TestTheCategoriesAndTheStagesAreOneSet(t *testing.T) {
	t.Parallel()

	pairs := map[formatv0.AgeCategory]admissionv0.AgeCategory{
		formatv0.AgeCategoryUnder13:         admissionv0.AgeCategoryUnder13,
		formatv0.AgeCategoryFrom13ToConsent: admissionv0.AgeCategoryFrom13ToConsent,
		formatv0.AgeCategoryFromConsentTo18: admissionv0.AgeCategoryFromConsentTo18,
		formatv0.AgeCategoryAdult:           admissionv0.AgeCategoryAdult,
	}
	for f, a := range pairs {
		if string(f) != string(a) {
			t.Errorf("the format's %q is the table's %q", f, a)
		}
	}
	if got := strings.Join(logging.Rungs, " "); got != config.RungAuthor+" "+config.RungFull {
		t.Errorf("the log's stages = %q, want config's", got)
	}
}
