package admission_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/altnovatrail/spec"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	table "github.com/altnovatrail/tutor/admission"
	"github.com/altnovatrail/tutor/provider"
	"github.com/altnovatrail/tutor/provider/claude"

	"github.com/altnovatrail/api/internal/admission"
	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/tutorstub"
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
			g := load(t, tc.cfg)
			if got := g.Reads(tutor.CallSheet, tc.category, tc.provider); got != tc.want {
				t.Errorf("Reads(sheet, %s, %q) = %v, want %v", tc.category, tc.provider, got, tc.want)
			}
			// The essay and the rubric are admitted by the author's word at the
			// rung author alone, where the sheet is.
			for _, call := range []tutor.Call{tutor.CallEssay, tutor.CallRubric} {
				if got := g.Reads(call, tc.category, tc.provider); got != tc.want {
					t.Errorf("Reads(%s, %s, %q) = %v, want %v", call, tc.category, tc.provider, got, tc.want)
				}
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
	if want := (admission.Report{TableVersion: "0.7", FactsFile: true, Rung: "author", RungSet: true, Reading: "pair"}); r != want {
		t.Errorf("Report = %+v, want %+v", r, want)
	}
	_, r, err = admission.Load(config.Admission{Rung: config.RungFull})
	if err != nil {
		t.Fatal(err)
	}
	if want := (admission.Report{TableVersion: "0.7", Rung: "full", Reading: "pair"}); r != want {
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
	if got := strings.Join(logging.ReadingModes, " "); got != config.ReadingPair+" "+config.ReadingSingle {
		t.Errorf("the log's modes of reading = %q, want config's", got)
	}
	if config.ReadingPair != string(tutor.ReadingPair) || config.ReadingSingle != string(tutor.ReadingSingle) {
		t.Errorf("config's modes of reading are not tutor's")
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

// The gate reads a sheet and an essay, and assesses an essay,
// by tutor, which calls no model for a reading with no task or an assessment
// with no text.
func TestTheGateReadsByTutor(t *testing.T) {
	t.Parallel()

	read := load(t, keyed(t, "", config.RungAuthor)).ReadSheet(t.Context(), tutor.SheetReading{
		Route: tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategoryAdult},
	})
	if read.Outcome.Kind != tutor.Failed || len(read.Observations) != 0 || len(read.Outcome.Usage) != 0 {
		t.Errorf("ReadSheet with no task = %v, want a failure with no call", read.Outcome.Kind)
	}
	essay := load(t, keyed(t, "", config.RungAuthor)).ReadEssay(t.Context(), tutor.EssayReading{
		Route: tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategoryAdult},
	})
	if essay.Outcome.Kind != tutor.Failed || essay.Observation != nil || len(essay.Outcome.Usage) != 0 {
		t.Errorf("ReadEssay with no task = %v, want a failure with no call", essay.Outcome.Kind)
	}
	assessed := load(t, keyed(t, "", config.RungAuthor)).AssessEssay(t.Context(), tutor.RubricAssessment{
		Route: tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategoryAdult},
	})
	if assessed.Outcome.Kind != tutor.Failed || assessed.Feedback != "" || len(assessed.Outcome.Usage) != 0 {
		t.Errorf("AssessEssay with no text = %v, want a failure with no call", assessed.Outcome.Kind)
	}
}

// The tutor's words are admitted where the table admits them: at the author's
// stage, for an adult, by the platform's key of Anthropic or an own key of
// Anthropic, the hint and the analysis with the work alike; at the stage full,
// for a child, with no key of the platform, or by a provider api gives no
// adapter of, they are not. Either is admitted exactly when the choice of its
// kind of call by the gate's tutor gives an entry: the hint without the work
// by CallHint, the words with it by CallWords. The call of words with no task
// asks no model.
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
			route := tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategory(tc.category)}
			if tc.provider != "" {
				route.AI, route.Own = admissionv0.AiRouteOwnKey, tutor.NewOwnKey(admissionv0.Provider(tc.provider), "own key")
			}
			for withWork, call := range map[bool]tutor.Call{false: tutor.CallHint, true: tutor.CallWords} {
				if got := admission.WordsCall(withWork); got != call {
					t.Errorf("WordsCall(%v) = %s, want %s", withWork, got, call)
				}
				got := g.Words(withWork, tc.category, tc.provider)
				if got != tc.want {
					t.Errorf("Words(%v, %s, %q) = %v, want %v", withWork, tc.category, tc.provider, got, tc.want)
				}
				entry, _, err := admission.TutorOf(g).ChooseCall(call, route)
				if chosen := err == nil && len(entry.Models) > 0; got != chosen {
					t.Errorf("Words(%v) = %v, but ChooseCall(%s) chose an entry: %v", withWork, got, call, chosen)
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

// "Explain differently" on a task from paper is admitted exactly when tutor
// chooses an entry for its call, CallPaperHint.
func TestTheGateAdmitsPaperWordsWhereTutorChooses(t *testing.T) {
	t.Parallel()

	facts := keyed(t, file(t, adultFacts), config.RungAuthor)
	unkeyed := config.Admission{FactsFile: file(t, adultFacts), Rung: config.RungAuthor}
	for name, tc := range map[string]struct {
		cfg      config.Admission
		category formatv0.AgeCategory
		provider string
	}{
		"facts for an adult":            {facts, formatv0.AgeCategoryAdult, ""},
		"facts for an adult, a child":   {facts, formatv0.AgeCategoryUnder13, ""},
		"facts, no key of the platform": {unkeyed, formatv0.AgeCategoryAdult, ""},
		"an own key of Anthropic":       {unkeyed, formatv0.AgeCategoryAdult, "anthropic"},
		"an own key of OpenAI":          {unkeyed, formatv0.AgeCategoryAdult, "openai"},
	} {
		t.Run(name, func(t *testing.T) {
			g := load(t, tc.cfg)
			route := tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategory(tc.category)}
			if tc.provider != "" {
				route.AI, route.Own = admissionv0.AiRouteOwnKey, tutor.NewOwnKey(admissionv0.Provider(tc.provider), "own key")
			}
			got := g.PaperWords(tc.category, tc.provider)
			entry, _, err := admission.TutorOf(g).ChooseCall(tutor.CallPaperHint, route)
			if chosen := err == nil && len(entry.Models) > 0; got != chosen {
				t.Errorf("PaperWords = %v, but ChooseCall chose an entry: %v", got, chosen)
			}
		})
	}
	if !load(t, facts).PaperWords(formatv0.AgeCategoryAdult, "") {
		t.Error("explain differently is not admitted for an adult by the platform's key at the author's stage")
	}
}

// The gate finds the tasks on a page, reads the statement of a task from
// paper, checks the work on it, words its tutor's words and draws its act of
// grown-up mode by tutor, which calls no model where no facts admit the call,
// for a Check of no task, for words with no style, or for a drawing with no
// work.
func TestTheGateChecksATaskFromPaperByTutor(t *testing.T) {
	t.Parallel()

	g := load(t, keyed(t, "", config.RungAuthor))
	adult := tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategoryAdult}
	page := tutor.PaperTask{Route: adult, Statement: []byte("a page")}
	for name, o := range map[string]tutor.Outcome{
		"FindTasks":      g.FindTasks(t.Context(), tutor.TaskFinding{Route: adult, Photo: []byte("a page")}).Outcome,
		"ReadStatement":  g.ReadStatement(t.Context(), page).Outcome,
		"CheckPaper":     g.CheckPaper(t.Context(), tutor.PaperCheck{Route: adult, Statement: []byte("a page")}).Outcome,
		"TellPaperWords": g.TellPaperWords(t.Context(), tutor.PaperWordsRequest{Route: adult, Ask: tutor.PaperAgain}).Outcome,
		"GrownUp":        g.GrownUp(t.Context(), tutor.GrownUpRequest{Route: adult, Question: "Draw", Statement: []byte("a page")}).Outcome,
	} {
		if o.Kind == tutor.Answered || len(o.Usage) != 0 {
			t.Errorf("%s = %v, want no answer and no call", name, o.Kind)
		}
	}
}

// The words after a correct answer are admitted exactly where tutor chooses an
// entry for their kind of call, CallCorrect: at the stage author for an adult,
// by the platform's key of Anthropic or an own key of Anthropic; not at the
// stage full, for a child or a teen, with no key of the platform, or by an own
// key of OpenAI. The call with no task asks no model.
func TestTheGateAdmitsCorrectWordsWhereTutorChooses(t *testing.T) {
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
		"facts for an adult":                {facts, formatv0.AgeCategoryAdult, "", true},
		"facts for an adult, a child":       {facts, formatv0.AgeCategoryUnder13, "", false},
		"facts for an adult, a teen":        {facts, formatv0.AgeCategoryFrom13ToConsent, "", false},
		"facts at the stage full":           {full, formatv0.AgeCategoryAdult, "", false},
		"facts, no key of the platform":     {unkeyed, formatv0.AgeCategoryAdult, "", false},
		"an own key of Anthropic":           {unkeyed, formatv0.AgeCategoryAdult, "anthropic", true},
		"an own key of Anthropic, at full":  {config.Admission{Rung: config.RungFull}, formatv0.AgeCategoryAdult, "anthropic", false},
		"an own key of Anthropic, a child":  {unkeyed, formatv0.AgeCategoryUnder13, "anthropic", false},
		"an own key of OpenAI":              {unkeyed, formatv0.AgeCategoryAdult, "openai", false},
		"an own key of a provider no table": {unkeyed, formatv0.AgeCategoryAdult, "acme", false},
	} {
		t.Run(name, func(t *testing.T) {
			g := load(t, tc.cfg)
			got := g.CorrectWords(tc.category, tc.provider)
			if got != tc.want {
				t.Errorf("CorrectWords(%s, %q) = %v, want %v", tc.category, tc.provider, got, tc.want)
			}
			route := tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategory(tc.category)}
			if tc.provider != "" {
				route.AI, route.Own = admissionv0.AiRouteOwnKey, tutor.NewOwnKey(admissionv0.Provider(tc.provider), "own key")
			}
			entry, _, err := admission.TutorOf(g).ChooseCall(tutor.CallCorrect, route)
			if chosen := err == nil && len(entry.Models) > 0; got != chosen {
				t.Errorf("CorrectWords = %v, but ChooseCall(%s) chose an entry: %v", got, tutor.CallCorrect, chosen)
			}
		})
	}
	told := load(t, facts).TellCorrectWords(t.Context(), tutor.CorrectRequest{Route: tutor.Route{
		AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategoryAdult}, Style: "an adult"})
	if told.Outcome.Kind != tutor.Failed || told.Words != "" || len(told.Outcome.Usage) != 0 {
		t.Errorf("TellCorrectWords with no task = %v, %q, want a failure with no call", told.Outcome.Kind, told.Words)
	}
}

// The gate gives tutor Anthropic's adapter alone, and OpenAI's is not given.
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

// The price of a call is tutor's, on a route it admits a model for: the
// expected price of a sheet read by the pair, of the words and of the words
// after a correct answer, which the quota reserves; on a route it admits none
// for, no price, and the quota reserves its own value.
func TestTheGatePricesWhatItAdmits(t *testing.T) {
	t.Parallel()

	g := load(t, keyed(t, file(t, adultFacts), config.RungAuthor))
	adult := tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategory(formatv0.AgeCategoryAdult)}
	for _, call := range []tutor.Call{tutor.CallSheet, tutor.CallWords, tutor.CallCorrect} {
		p := g.Price(call, adult)
		if !p.Expected || p.ExpectedUSD <= 0 || p.ExpectedUSD > 0.05 {
			t.Errorf("the price of %s for an adult = %+v, want an expected price of cents", call, p)
		}
		if got := quota.Reserve(call, p); got != quota.USD(p.ExpectedUSD) {
			t.Errorf("%s reserves %d, want its expected price, %d", call, got, quota.USD(p.ExpectedUSD))
		}
	}
	// The hint without the work has no expected tokens at tutor, so the quota
	// reserves its own value of the kind.
	if p := g.Price(tutor.CallHint, adult); p.Expected || !p.Worst || p.WorstUSD <= 0 {
		t.Errorf("the price of a hint without the work = %+v, want its worst alone", p)
	}
	child := tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategory(formatv0.AgeCategoryUnder13)}
	if p := g.Price(tutor.CallSheet, child); p.Expected || p.ExpectedUSD != 0 {
		t.Errorf("the price of a route not admitted = %+v, want none", p)
	}
	// The essay and the rubric have the expected price of the entries the
	// author's word admits at the rung author, and none at the rung full, which
	// admits neither.
	full := load(t, keyed(t, file(t, adultFacts), config.RungFull))
	for _, call := range []tutor.Call{tutor.CallEssay, tutor.CallRubric} {
		if p := g.Price(call, adult); !p.Expected || p.ExpectedUSD <= 0 || p.ExpectedUSD > 0.05 {
			t.Errorf("the price of %s for an adult = %+v, want an expected price of cents", call, p)
		}
		if p := full.Price(call, adult); p.Expected {
			t.Errorf("the price of %s at the rung full = %+v, want none", call, p)
		}
	}
}

// The dev mode's stub: with no key of the platform, tutor admits the reading
// and the words on the route of platform AI as with Anthropic's key, and
// prices them by the catalog of its adapter, while its models answer no call
// and check no key. The report says so, and counts no key of the platform.
func TestTheStubAdmitsAndPricesAndCallsNothing(t *testing.T) {
	t.Parallel()

	cfg, err := config.AdmissionFrom([]string{"API_TUTOR_STUB=1", "API_ADMISSION_RUNG=author",
		"API_ADMISSION_FACTS_FILE=" + file(t, adultFacts)})
	if err != nil {
		t.Fatal(err)
	}
	g, r, err := admission.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Stub || r.PlatformKeys != 0 {
		t.Errorf("Report = %+v, want the stub and no key of the platform", r)
	}
	if !g.Reads(tutor.CallSheet, formatv0.AgeCategoryAdult, "") || !g.Words(true, formatv0.AgeCategoryAdult, "") ||
		!g.CorrectWords(formatv0.AgeCategoryAdult, "") {
		t.Error("the stub's gate does not admit the reading, the words and the words after a correct answer for an adult")
	}
	adult := tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategoryAdult}
	for _, call := range []tutor.Call{tutor.CallSheet, tutor.CallCorrect} {
		if p := g.Price(call, adult); !p.Expected || p.ExpectedUSD <= 0 {
			t.Errorf("the price of %s on the stub = %+v, want tutor's expected price", call, p)
		}
	}
	dial := admission.StubAdapters()["anthropic"]
	m, err := dial("claude-opus-5-5", "sk-never")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Ask(t.Context(), provider.Request{}); err == nil {
		t.Error("the stub's model answered a call")
	}
	if err := m.Check(t.Context()); err != nil {
		t.Errorf("the stub's check of a key with no ending of its own = %v, want it taken", err)
	}
	if m.Name() == "" || m.Prices().Read == "" {
		t.Errorf("the stub's model has no name or prices of the catalog: %q, %+v", m.Name(), m.Prices())
	}
	if _, err := dial("no-such-model", ""); err == nil {
		t.Error("the stub's adapter dialled a model the catalog does not hold")
	}
}

// apiCalls are the kinds of call api makes, and before are the first three of
// them: the reading of a sheet, the words and the hint.
var (
	apiCalls = []tutor.Call{tutor.CallSheet, tutor.CallWords, tutor.CallHint, tutor.CallCorrect,
		tutor.CallEssay, tutor.CallRubric}
	before = []tutor.Call{tutor.CallSheet, tutor.CallWords, tutor.CallHint}
)

// chosen says whether tu chooses an entry for one of the kinds of call api
// makes, on the route of the own key of p at category: what the gate's
// OwnKeyOpen must answer.
func chosen(tu *tutor.Tutor, category formatv0.AgeCategory, p admissionv0.Provider) bool {
	return chosenOf(tu, apiCalls, category, p)
}

// chosenOf says whether tu chooses an entry for one of calls, on the route of
// the own key of p at category.
func chosenOf(tu *tutor.Tutor, calls []tutor.Call, category formatv0.AgeCategory, p admissionv0.Provider) bool {
	route := tutor.Route{AI: admissionv0.AiRouteOwnKey, Category: admissionv0.AgeCategory(category),
		Own: tutor.NewOwnKey(p, "own key")}
	for _, call := range calls {
		if entry, _, err := tu.ChooseCall(call, route); err == nil && len(entry.Models) > 0 {
			return true
		}
	}
	return false
}

// The words after a correct answer open no own key that the reading, the words
// and the hint leave closed: at either stage, for every category and every
// provider of the table, OwnKeyOpen answers as the choice of those three kinds
// of call alone, so ListOwnKeyProviders lists the providers those three open.
func TestTheWordsAfterACorrectAnswerOpenNoOtherOwnKey(t *testing.T) {
	t.Parallel()

	providers, err := admission.TableProviders()
	if err != nil {
		t.Fatal(err)
	}
	categories := []formatv0.AgeCategory{formatv0.AgeCategoryUnder13, formatv0.AgeCategoryFrom13ToConsent,
		formatv0.AgeCategoryFromConsentTo18, formatv0.AgeCategoryAdult}
	for _, rung := range []string{config.RungAuthor, config.RungFull} {
		g := load(t, config.Admission{Rung: rung})
		for _, category := range categories {
			for _, p := range providers {
				if got, want := g.OwnKeyOpen(category, string(p.Code)), chosenOf(admission.TutorOf(g), before, category, p.Code); got != want {
					t.Errorf("at %s, OwnKeyOpen(%s, %s) = %v, while the kinds of call before give %v", rung, category, p.Code, got, want)
				}
			}
		}
	}
}

// The check of a key asks Opus 5.5, Haiku 5.5 and Sonnet 5.5, each once: the
// words after a correct answer are on Opus 5.5, as the words, so they add no
// model to check, and Sonnet is an attempt of the cascade of a task from
// paper.
func TestTheCheckOfAKeyAsksTheSameModels(t *testing.T) {
	t.Parallel()

	tbl, err := table.Platform()
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu     sync.Mutex
		dialed []string
	)
	dial := func(id, _ string) (provider.Model, error) {
		mu.Lock()
		dialed = append(dialed, id)
		mu.Unlock()
		m, err := claude.Dial(id, "")
		if err != nil {
			return nil, err
		}
		return checkingModel{Model: m}, nil
	}
	tt, err := tutor.New(tutor.Config{Table: tbl, Least: table.Stage(config.RungAuthor),
		Adapters: map[admissionv0.Provider]provider.Dial{"anthropic": dial}})
	if err != nil {
		t.Fatal(err)
	}
	g, err := admission.GateOn(tt)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := g.CheckKey(t.Context(), formatv0.AgeCategoryAdult, "anthropic", "sk-ant-synthetic"); got != tutor.KeyAccepted {
		t.Fatalf("CheckKey = %v, %v, want accepted", got, err)
	}
	slices.Sort(dialed)
	if want := []string{"claude-haiku-5-5", "claude-opus-5-5", "claude-sonnet-5-5"}; !slices.Equal(dialed, want) {
		t.Errorf("the check dialled %v, want %v", dialed, want)
	}
}

// The own key is open where tutor admits it for a kind of call api makes: an
// adult's of Anthropic at the stage author, with no facts or key of the
// platform, which the own key needs none of; not OpenAI's, whose adapter api
// does not give and whose model tutor admits none of, nor a provider the table
// does not know, nor none; at the stage full nobody's, and below 18 none.
func TestTheOwnKeyIsOpenWhereTutorChoosesACall(t *testing.T) {
	t.Parallel()

	author := load(t, config.Admission{Rung: config.RungAuthor})
	full := load(t, config.Admission{Rung: config.RungFull})
	for name, tc := range map[string]struct {
		gate     *admission.Gate
		category formatv0.AgeCategory
		provider string
		want     bool
	}{
		"Anthropic for an adult":             {author, formatv0.AgeCategoryAdult, "anthropic", true},
		"OpenAI for an adult":                {author, formatv0.AgeCategoryAdult, "openai", false},
		"Google for an adult":                {author, formatv0.AgeCategoryAdult, "google", false},
		"a provider no table names":          {author, formatv0.AgeCategoryAdult, "acme", false},
		"no provider":                        {author, formatv0.AgeCategoryAdult, "", false},
		"Anthropic for a child":              {author, formatv0.AgeCategoryUnder13, "anthropic", false},
		"Anthropic for a teen":               {author, formatv0.AgeCategoryFrom13ToConsent, "anthropic", false},
		"Anthropic for one past consent":     {author, formatv0.AgeCategoryFromConsentTo18, "anthropic", false},
		"Anthropic for an adult at full":     {full, formatv0.AgeCategoryAdult, "anthropic", false},
		"a provider no table names, at full": {full, formatv0.AgeCategoryAdult, "acme", false},
	} {
		t.Run(name, func(t *testing.T) {
			got := tc.gate.OwnKeyOpen(tc.category, tc.provider)
			if got != tc.want {
				t.Errorf("OwnKeyOpen(%s, %q) = %v, want %v", tc.category, tc.provider, got, tc.want)
			}
			if tc.provider != "" {
				if want := chosen(admission.TutorOf(tc.gate), tc.category, admissionv0.Provider(tc.provider)); got != want {
					t.Errorf("OwnKeyOpen(%s, %q) = %v, but tutor's choice says %v", tc.category, tc.provider, got, want)
				}
			}
		})
	}
	want := []admission.Provider{{Code: "anthropic", Name: "Anthropic"}}
	if got := author.OwnKeyProviders(formatv0.AgeCategoryAdult); !slices.Equal(got, want) {
		t.Errorf("OwnKeyProviders of an adult at author = %v, want %v", got, want)
	}
	if got := full.OwnKeyProviders(formatv0.AgeCategoryAdult); len(got) != 0 {
		t.Errorf("OwnKeyProviders of an adult at full = %v, want none", got)
	}
	for _, category := range []formatv0.AgeCategory{formatv0.AgeCategoryUnder13, formatv0.AgeCategoryFrom13ToConsent,
		formatv0.AgeCategoryFromConsentTo18} {
		if got := author.OwnKeyProviders(category); len(got) != 0 {
			t.Errorf("OwnKeyProviders of %s = %v, want none", category, got)
		}
	}
}

// With the adapters of Anthropic and OpenAI both, as api would give them with
// a model of OpenAI's admitted, the list is still what the choice of the same
// tutor gives, provider by provider, in the order of the table, at either
// stage.
func TestTheListOfProvidersIsTutorsChoice(t *testing.T) {
	t.Parallel()

	tbl, err := table.Platform()
	if err != nil {
		t.Fatal(err)
	}
	never := func(string, string) (provider.Model, error) { return nil, errors.New("the test dials nothing") }
	for _, rung := range []string{config.RungAuthor, config.RungFull} {
		tt, err := tutor.New(tutor.Config{Table: tbl, Least: table.Stage(rung),
			Adapters: map[admissionv0.Provider]provider.Dial{"anthropic": never, "openai": never}})
		if err != nil {
			t.Fatal(err)
		}
		g, err := admission.GateOn(tt)
		if err != nil {
			t.Fatal(err)
		}
		var want []admission.Provider
		for _, p := range []admission.Provider{{"anthropic", "Anthropic"}, {"google", "Google"}, {"openai", "OpenAI"}} {
			if chosen(tt, formatv0.AgeCategoryAdult, p.Code) {
				want = append(want, p)
			}
		}
		if got := g.OwnKeyProviders(formatv0.AgeCategoryAdult); !slices.Equal(got, want) {
			t.Errorf("OwnKeyProviders at %s = %v, want tutor's choice %v", rung, got, want)
		}
		if rung == config.RungAuthor && len(want) == 0 {
			t.Error("tutor chooses no own key at author: the test compares nothing")
		}
	}
}

// The providers come in the order the table's JSON writes them, whatever their
// codes; a table that is no object of providers is a fault.
func TestTheProvidersKeepTheTablesOrder(t *testing.T) {
	t.Parallel()

	got, err := admission.ReadProviders([]byte(`{"providers": {"openai": {"name": "OpenAI"}, "anthropic": {"name": "Anthropic"}},
		"cells": []}`))
	want := []admission.Provider{{Code: "openai", Name: "OpenAI"}, {Code: "anthropic", Name: "Anthropic"}}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("ReadProviders = %v, %v, want %v", got, err, want)
	}
	for name, data := range map[string]string{
		"no JSON":            `{`,
		"no providers":       `{}`,
		"a name not text":    `{"providers": {"openai": {"name": 1}}}`,
		"providers a list":   `{"providers": ["a", {"name": "A"}]}`,
		"providers null":     `{"providers": null}`,
		"providers a string": `{"providers": "anthropic"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := admission.ReadProviders([]byte(data)); err == nil {
				t.Errorf("ReadProviders = %v, want a fault", got)
			}
		})
	}
}

// checkingModel is a model of Claude's catalog whose check of a key answers
// err, and records the deadline it was given.
type checkingModel struct {
	provider.Model
	err      error
	deadline chan<- time.Time
}

func (m checkingModel) Check(ctx context.Context) error {
	if m.deadline != nil {
		d, _ := ctx.Deadline()
		m.deadline <- d
	}
	return m.err
}

// checkingGate is a gate at the stage author whose adapters of Anthropic and
// OpenAI both answer the check of a key err, and count how often they dial.
func checkingGate(t *testing.T, err error, deadline chan<- time.Time) (*admission.Gate, *atomic.Int32) {
	t.Helper()
	tbl, e := table.Platform()
	if e != nil {
		t.Fatal(e)
	}
	dials := &atomic.Int32{}
	dial := func(id, _ string) (provider.Model, error) {
		dials.Add(1)
		m, err2 := claude.Dial(id, "")
		if err2 != nil {
			return nil, err2
		}
		return checkingModel{Model: m, err: err, deadline: deadline}, nil
	}
	tt, e := tutor.New(tutor.Config{Table: tbl, Least: table.Stage(config.RungAuthor),
		Adapters: map[admissionv0.Provider]provider.Dial{"anthropic": dial, "openai": dial}})
	if e != nil {
		t.Fatal(e)
	}
	g, e := admission.GateOn(tt)
	if e != nil {
		t.Fatal(e)
	}
	return g, dials
}

// The check of a key is tutor's: what the provider answers the check of each
// model is how it ends — taken, a limit too, the key refused, no rights, or a
// failure of neither kind, which is no answer —, and each model tutor would
// call is dialled with it.
func TestTheGateChecksAKeyByTutor(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		err  error
		want tutor.KeyCheck
	}{
		"taken":       {nil, tutor.KeyAccepted},
		"a limit":     {&provider.LimitError{Limit: provider.LimitTier, Err: errors.New("429")}, tutor.KeyAccepted},
		"refused":     {provider.ErrKeyRefused, tutor.KeyWrong},
		"no rights":   {provider.ErrNoRights, tutor.KeyNoRights},
		"no answer":   {errors.New("connection reset"), tutor.KeyUnanswered},
		"cut short":   {context.DeadlineExceeded, tutor.KeyUnanswered},
		"a 5xx error": {fmt.Errorf("overloaded: %w", errors.New("529")), tutor.KeyUnanswered},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g, dials := checkingGate(t, tc.err, nil)
			got, err := g.CheckKey(t.Context(), formatv0.AgeCategoryAdult, "anthropic", "sk-ant-synthetic")
			if got != tc.want {
				t.Errorf("CheckKey = %v, %v, want %v", got, err, tc.want)
			}
			if (got == tutor.KeyAccepted) != (err == nil) {
				t.Errorf("CheckKey = %v with the error %v", got, err)
			}
			if err != nil && strings.Contains(err.Error(), "sk-ant-synthetic") {
				t.Errorf("the error of the check holds the key: %v", err)
			}
			if dials.Load() == 0 {
				t.Error("the check dialled no model")
			}
		})
	}
}

// A provider closed to the key — OpenAI, whose model tutor admits none of, one
// the table does not know, any below 18 — is "provider closed" with no model
// dialled, as the list leaves it out.
func TestTheGateChecksNoKeyOfAClosedProvider(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		category formatv0.AgeCategory
		provider string
	}{
		"OpenAI":                {formatv0.AgeCategoryAdult, "openai"},
		"Google":                {formatv0.AgeCategoryAdult, "google"},
		"a provider no table":   {formatv0.AgeCategoryAdult, "acme"},
		"Anthropic for a child": {formatv0.AgeCategoryUnder13, "anthropic"},
		"Anthropic for a teen":  {formatv0.AgeCategoryFromConsentTo18, "anthropic"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g, dials := checkingGate(t, nil, nil)
			if got, err := g.CheckKey(t.Context(), tc.category, tc.provider, "sk-synthetic"); got != tutor.KeyProviderClosed || err != nil {
				t.Errorf("CheckKey = %v, %v, want provider closed", got, err)
			}
			if n := dials.Load(); n != 0 {
				t.Errorf("the check of a closed provider dialled %d models", n)
			}
		})
	}
	full := load(t, config.Admission{Rung: config.RungFull})
	if got, err := full.CheckKey(t.Context(), formatv0.AgeCategoryAdult, "anthropic", "sk-synthetic"); got != tutor.KeyProviderClosed || err != nil {
		t.Errorf("CheckKey at full = %v, %v, want provider closed", got, err)
	}
}

// The check ends checkReserve before the request's deadline, so that the
// handler answers how it ended in time.
func TestTheCheckOfAKeyEndsBeforeTheDeadline(t *testing.T) {
	t.Parallel()

	deadlines := make(chan time.Time, 8)
	g, _ := checkingGate(t, nil, deadlines)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	outer, _ := ctx.Deadline()
	if got, err := g.CheckKey(ctx, formatv0.AgeCategoryAdult, "anthropic", "sk-synthetic"); got != tutor.KeyAccepted {
		t.Fatalf("CheckKey = %v, %v, want accepted", got, err)
	}
	close(deadlines)
	for d := range deadlines {
		if !d.Equal(outer.Add(-time.Second)) {
			t.Errorf("a model checked the key until %v, want a second before %v", d, outer)
		}
	}
}

// The stub's check of a key answers by its ending, through tutor's own check:
// each of the five ends with no call of a provider, OpenAI closed as on the
// models. The endings by which the stub refuses a call of AI are accepted, so
// that app connects such a key before it meets the refusal.
func TestTheStubChecksAKeyByItsEnding(t *testing.T) {
	t.Parallel()

	cfg, err := config.AdmissionFrom([]string{"API_TUTOR_STUB=1", "API_ADMISSION_RUNG=author"})
	if err != nil {
		t.Fatal(err)
	}
	g := load(t, cfg)
	for key, want := range map[string]tutor.KeyCheck{
		"sk-ant-synthetic": tutor.KeyAccepted,
		"sk-ant-synthetic" + admission.StubKeyInvalid:  tutor.KeyWrong,
		"sk-ant-synthetic" + admission.StubKeyNoPerm:   tutor.KeyNoRights,
		"sk-ant-synthetic" + admission.StubKeySilent:   tutor.KeyUnanswered,
		"sk-ant-invalid-but-not-at-the-end":            tutor.KeyAccepted,
		"sk-ant-synthetic" + tutorstub.KeyRefused:      tutor.KeyAccepted,
		"sk-ant-synthetic" + tutorstub.KeyLimit:        tutor.KeyAccepted,
		"sk-ant-synthetic" + tutorstub.KeyLimitOwner:   tutor.KeyAccepted,
		"sk-ant-synthetic" + tutorstub.KeyLimitTier:    tutor.KeyAccepted,
		"sk-ant-synthetic" + tutorstub.KeyLimitBalance: tutor.KeyAccepted,
		"sk-ant-synthetic" + tutorstub.KeyLimitPayment: tutor.KeyAccepted,
	} {
		if got, err := g.CheckKey(t.Context(), formatv0.AgeCategoryAdult, "anthropic", key); got != want {
			t.Errorf("the stub's CheckKey(%q) = %v, %v, want %v", key, got, err, want)
		}
	}
	if got, _ := g.CheckKey(t.Context(), formatv0.AgeCategoryAdult, "openai", "sk-synthetic"); got != tutor.KeyProviderClosed {
		t.Errorf("the stub's CheckKey of OpenAI = %v, want provider closed", got)
	}
}

// With less than two seconds left the check does not begin: the client's
// deadline, not the provider, would end it.
func TestACheckWithTooLittleTimeDoesNotBegin(t *testing.T) {
	t.Parallel()

	g, dials := checkingGate(t, nil, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 1500*time.Millisecond)
	defer cancel()
	if got, err := g.CheckKey(ctx, formatv0.AgeCategoryAdult, "anthropic", "sk-synthetic"); got != tutor.KeyUnanswered ||
		!errors.Is(err, admission.ErrTooLittleTime) {
		t.Errorf("CheckKey with 1.5 s left = %v, %v, want too little time", got, err)
	}
	if n := dials.Load(); n != 0 {
		t.Errorf("a check with too little time dialled %d models", n)
	}
}

// In the mode single, tutor reads a sheet by Haiku 5.5 at medium alone, which
// the report and the gate say: it admits the reading where the pair is
// admitted, at the author's stage, for less than the pair, and at the stage
// full, which admits no model of the pair, it is no refusal of the
// configuration but no admission of the reading.
func TestTheModeSingleReadsByOneReader(t *testing.T) {
	t.Parallel()

	single := keyed(t, file(t, adultFacts), config.RungAuthor)
	single.Reading = config.ReadingSingle
	g, r, err := admission.Load(single)
	if err != nil || r.Reading != config.ReadingSingle || g.Reading() != tutor.ReadingSingle {
		t.Fatalf("Load in the mode single = %+v, %v; want the mode single", r, err)
	}
	pair := load(t, keyed(t, file(t, adultFacts), config.RungAuthor))
	if pair.Reading() != tutor.ReadingPair {
		t.Errorf("the mode by default = %q, want the pair", pair.Reading())
	}
	adult := tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategory(formatv0.AgeCategoryAdult)}
	one, two := g.Price(tutor.CallSheet, adult), pair.Price(tutor.CallSheet, adult)
	if !g.Reads(tutor.CallSheet, formatv0.AgeCategoryAdult, "") || !one.Expected || one.ExpectedUSD <= 0 || one.ExpectedUSD >= two.ExpectedUSD {
		t.Errorf("a sheet by one reader costs %+v, by the pair %+v; want it admitted, for less", one, two)
	}
	full := keyed(t, file(t, adultFacts), config.RungFull)
	full.Reading = config.ReadingSingle
	if g := load(t, full); g.Reads(tutor.CallSheet, formatv0.AgeCategoryAdult, "") {
		t.Error("the stage full admits the reading by one reader, want none: it admits no model of the pair")
	}
	if _, _, err := admission.Load(config.Admission{Rung: config.RungFull, Reading: "both"}); !errors.Is(err, config.ErrInvalid) {
		t.Errorf("Load in an unknown mode = %v, want a refusal of the configuration", err)
	}
}

// The debug mode reaches tutor through the gate on every run with the key of
// Anthropic and none on the stub or with no key, and the gate reports it, and
// apart whether the flag gives every account its right; the gate gives the
// models the list admits for each call of a task from paper for an adult on
// platform AI, the attempts of its cascade in their order, none at the stage
// full.
func TestTheGateOfTheDebugMode(t *testing.T) {
	t.Parallel()

	cfg := keyed(t, file(t, adultFacts), "author")
	if _, r, err := admission.Load(cfg); err != nil || !r.Debug || r.DebugEvery {
		t.Fatalf("Load with the key = %+v, %v; want the debug mode served, its right by the account", r, err)
	}
	for name, without := range map[string]config.Admission{
		"the stub": {Rung: config.RungFull, Reading: config.ReadingPair, Stub: true},
		"no key":   {Rung: config.RungFull, Reading: config.ReadingPair},
	} {
		if _, r, err := admission.Load(without); err != nil || r.Debug {
			t.Errorf("Load on %s = %+v, %v; want no debug mode", name, r, err)
		}
	}
	cfg.Debug = true
	g, r, err := admission.Load(cfg)
	if err != nil || !r.Debug || !r.DebugEvery {
		t.Fatalf("Load = %+v, %v; want the debug mode reported, its right every account's", r, err)
	}
	for _, call := range []tutor.Call{tutor.CallFinding, tutor.CallStatement, tutor.CallChecking} {
		var got []string
		for _, m := range g.Admitted(call) {
			got = append(got, m.Name())
		}
		if strings.Join(got, ", ") != "claude-haiku-5-5 high, claude-sonnet-5-5 medium, claude-opus-5-5 medium" {
			t.Errorf("Admitted(%s) = %v; want the cascade of Haiku, Sonnet and Opus", call, got)
		}
	}
	if m := load(t, keyed(t, file(t, adultFacts), "full")).Admitted(tutor.CallFinding); len(m) != 0 {
		t.Error("the stage full admitted the finding")
	}
}
