package consentage_test

import (
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/altnovatrail/spec"
	"github.com/altnovatrail/spec/types/consentagev1"

	"github.com/altnovatrail/api/internal/consentage"
)

func day(t *testing.T, d consentagev1.Day) time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", string(d))
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

// The category, applied and asking again are those of the vectors of the part,
// consent-age/vectors/category/ (consent-age/README.md, section 8).
func TestTheVectorsOfTheCategory(t *testing.T) {
	t.Parallel()

	data, err := fs.ReadFile(spec.ConsentAge, "vectors/category/ages.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var vector consentagev1.CategoryVector
	if err := consentage.FromYAML(data, &vector); err != nil {
		t.Fatal(err)
	}
	table, err := consentage.New(vector.Table)
	if err != nil {
		t.Fatal(err)
	}
	if len(vector.Cases) == 0 {
		t.Fatal("the vector has no case")
	}
	for i, c := range vector.Cases {
		country := string(c.Country)
		if c.AgeYears != nil {
			category, applied := table.Category(int(*c.AgeYears), country, day(t, *c.Day))
			if category != string(c.Category) || applied != int(c.Applied) {
				t.Errorf("case %d: Category(%d, %s, %s) = %s, %d; want %s, %d", i, *c.AgeYears, country, *c.Day,
					category, applied, c.Category, c.Applied)
			}
			continue
		}
		if got := table.AskAgain(string(c.Category), int(c.Applied), country, day(t, *c.Today)); got != *c.AskAgain {
			t.Errorf("case %d: AskAgain(%s, %d, %s, %s) = %v, want %v", i, c.Category, c.Applied, country, *c.Today,
				got, *c.AskAgain)
		}
	}
}

// The table of the tag reads, and its countries are the 249 codes of the
// schema, the United Kingdom's among them by GB, and not by UK.
func TestTheTableOfTheTagReads(t *testing.T) {
	t.Parallel()

	table, err := consentage.Load()
	if err != nil {
		t.Fatal(err)
	}
	for code, want := range map[string]bool{"US": true, "GB": true, "DE": true, "UK": false, "EL": false, "us": false} {
		if table.Country(code) != want {
			t.Errorf("Country(%s) = %v, want %v", code, !want, want)
		}
	}
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	if category, applied := table.Category(14, "AT", now); category != consentage.FromConsentTo18 || applied != 14 {
		t.Errorf("Category(14, AT) = %s, %d; want from_consent_to_18 by 14", category, applied)
	}
	if !consentage.ParentConsents(consentage.Under13) || !consentage.ParentConsents(consentage.From13ToConsent) ||
		consentage.ParentConsents(consentage.FromConsentTo18) || consentage.ParentConsents(consentage.Adult) {
		t.Error("ParentConsents does not tell a parent's consent from the learner's")
	}
}

// What cannot be read is refused: no table, a table that is no YAML, no
// schema, a schema with no country and a day that is none.
func TestWhatCannotBeReadIsRefused(t *testing.T) {
	t.Parallel()

	table, err := fs.ReadFile(spec.ConsentAge, "consent-age.yaml")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := fs.ReadFile(spec.ConsentAge, "schemas/consent-age.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	for name, fsys := range map[string]fstest.MapFS{
		"no table":     {"schemas/consent-age.schema.json": {Data: schema}},
		"no YAML":      {"consent-age.yaml": {Data: []byte("countries: [")}, "schemas/consent-age.schema.json": {Data: schema}},
		"no schema":    {"consent-age.yaml": {Data: table}},
		"no JSON":      {"consent-age.yaml": {Data: table}, "schemas/consent-age.schema.json": {Data: []byte("{")}},
		"no countries": {"consent-age.yaml": {Data: table}, "schemas/consent-age.schema.json": {Data: []byte("{}")}},
		"no day": {"consent-age.yaml": {Data: []byte(`{format: 1, outside_table: 18, countries: {AT: [{age: 14, from: "2018-13-25"}]}}`)},
			"schemas/consent-age.schema.json": {Data: schema}},
	} {
		if _, err := consentage.Read(fsys); err == nil {
			t.Errorf("%s: Read = nil error, want a refusal", name)
		}
	}
}
