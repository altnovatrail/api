package admission_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/altnovatrail/spec"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"

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

// keyed is what a deployment declares with the platform's key of Anthropic:
// the file of facts at path, when not empty, and the stage rung.
func keyed(t *testing.T, path, rung string) config.Admission {
	t.Helper()
	environ := []string{"API_ADMISSION_RUNG=" + rung, "API_ANTHROPIC_KEY=sk-test"}
	if path != "" {
		environ = append(environ, "API_ADMISSION_FACTS_FILE="+path)
	}
	cfg, err := config.AdmissionFrom(environ)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
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
	facts := keyed(t, file(t, adultFacts), config.RungAuthor)
	full := keyed(t, file(t, adultFacts), config.RungFull)
	unkeyed := config.Admission{FactsFile: file(t, adultFacts), Rung: config.RungAuthor}
	empty := config.Admission{FactsFile: file(t, "# none yet\n"), Rung: config.RungAuthor}
	for name, tc := range map[string]struct {
		cfg      config.Admission
		category formatv0.AgeCategory
		provider string
		want     bool
	}{
		"no facts":                      {none, formatv0.AgeCategoryAdult, "", false},
		"a file with no facts":          {empty, formatv0.AgeCategoryAdult, "", false},
		"facts for an adult":            {facts, formatv0.AgeCategoryAdult, "", true},
		"facts, no key of the platform": {unkeyed, formatv0.AgeCategoryAdult, "", false},
		"facts for an adult, a child":   {facts, formatv0.AgeCategoryUnder13, "", false},
		"facts for an adult, a teen":    {facts, formatv0.AgeCategoryFrom13ToConsent, "", false},
		"facts at the stage full":       {full, formatv0.AgeCategoryAdult, "", false},
		"an own key of Anthropic":       {none, formatv0.AgeCategoryAdult, "anthropic", true},
		"an own key of OpenAI":          {none, formatv0.AgeCategoryAdult, "openai", false},
		"an own key no table names":     {none, formatv0.AgeCategoryAdult, "acme", false},
		"an own key of a child":         {none, formatv0.AgeCategoryUnder13, "anthropic", false},
		"an own key at the stage full":  {config.Admission{Rung: config.RungFull}, formatv0.AgeCategoryAdult, "anthropic", false},
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

// The report counts the platform's keys, and names none of them; the log's set
// of providers is the table's.
func TestTheReportCountsThePlatformsKeys(t *testing.T) {
	t.Parallel()

	_, r, err := admission.Load(keyed(t, "", config.RungAuthor))
	if err != nil {
		t.Fatal(err)
	}
	if r.PlatformKeys != 1 {
		t.Errorf("Report.PlatformKeys = %d, want 1", r.PlatformKeys)
	}
	data, err := fs.ReadFile(spec.Admission, "admission-table.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Providers map[string]json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(logging.Providers, " "), strings.Join(slices.Sorted(maps.Keys(table.Providers)), " "); got != want {
		t.Errorf("the log's providers = %q, want the table's %q", got, want)
	}
}

// The gate reads by tutor, which calls no model for a reading with no task.
func TestTheGateReadsByTutor(t *testing.T) {
	t.Parallel()

	read := load(t, keyed(t, "", config.RungAuthor)).ReadSheet(t.Context(), tutor.SheetReading{
		Route: tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategoryAdult},
	})
	if read.Outcome.Kind != tutor.Failed || len(read.Observations) != 0 || len(read.Outcome.Usage) != 0 {
		t.Errorf("ReadSheet with no task = %v, want a failure with no call", read.Outcome.Kind)
	}
}

// The tutor's words are admitted where the table admits them (R100 of tutor):
// at the author's stage, for an adult, by the platform's key of Anthropic or an
// own key of Anthropic, the hint and the analysis with the work alike; at the
// stage full, for a child, with no key of the platform, or by a provider api
// gives no adapter of, they are not. The call of words with no task asks no
// model.
func TestTheGateAdmitsWordsWhereTheTableDoes(t *testing.T) {
	t.Parallel()

	facts := keyed(t, file(t, adultFacts), config.RungAuthor)
	full := keyed(t, file(t, adultFacts), config.RungFull)
	unkeyed := config.Admission{FactsFile: file(t, adultFacts), Rung: config.RungAuthor}
	for name, tc := range map[string]struct {
		cfg      config.Admission
		category formatv0.AgeCategory
		provider string
		want     bool
	}{
		"facts for an adult":            {facts, formatv0.AgeCategoryAdult, "", true},
		"facts for an adult, a child":   {facts, formatv0.AgeCategoryUnder13, "", false},
		"facts at the stage full":       {full, formatv0.AgeCategoryAdult, "", false},
		"facts, no key of the platform": {unkeyed, formatv0.AgeCategoryAdult, "", false},
		"an own key of Anthropic":       {unkeyed, formatv0.AgeCategoryAdult, "anthropic", true},
		"an own key of OpenAI":          {unkeyed, formatv0.AgeCategoryAdult, "openai", false},
	} {
		t.Run(name, func(t *testing.T) {
			g := load(t, tc.cfg)
			for _, withWork := range []bool{false, true} {
				if got := g.Words(withWork, tc.category, tc.provider); got != tc.want {
					t.Errorf("Words(%v, %s, %q) = %v, want %v", withWork, tc.category, tc.provider, got, tc.want)
				}
			}
		})
	}
	told := load(t, facts).TellWords(t.Context(), tutor.WordsRequest{Route: tutor.Route{AI: admissionv0.AiRoutePlatformAi,
		Category: admissionv0.AgeCategoryAdult}})
	if told.Outcome.Kind != tutor.Failed || len(told.Outcome.Usage) != 0 {
		t.Errorf("TellWords with no task = %v, want a failure with no call", told.Outcome.Kind)
	}
}

// The gate gives tutor Anthropic's adapter alone, and OpenAI's is not given
// (R54).
func TestTheGateGivesAnthropicsAdapterAlone(t *testing.T) {
	t.Parallel()

	if got := slices.Sorted(maps.Keys(admission.Adapters())); !slices.Equal(got, []admissionv0.Provider{"anthropic"}) {
		t.Errorf("Adapters = %v, want Anthropic's alone", got)
	}
}

// A configuration tutor refuses, such as a stage it does not know, is a
// refusal of the configuration.
func TestAConfigurationTutorRefusesIsInvalid(t *testing.T) {
	t.Parallel()

	if _, _, err := admission.Load(config.Admission{Rung: "everyone"}); !errors.Is(err, config.ErrInvalid) {
		t.Errorf("Load at an unknown stage = %v, want a refusal of the configuration", err)
	}
}
