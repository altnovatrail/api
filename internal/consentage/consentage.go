// Package consentage is the age of digital consent by country (R52; R134
// spec): the table of spec's part consent-age/, at the tag api pins, and the
// category of a learner it gives — consent-age/README.md, section 6. api keeps
// the category, the day it was given and applied, the age of the country on
// that day, and never the age in years (privacy 3.3 spec).
package consentage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"sync"
	"time"

	"github.com/altnovatrail/spec"
	"github.com/altnovatrail/spec/types/consentagev1"
	"go.yaml.in/yaml/v3"
)

// The categories of a learner by age (privacy 3.1 spec).
const (
	Under13         = "under_13"
	From13ToConsent = "from_13_to_consent"
	FromConsentTo18 = "from_consent_to_18"
	Adult           = "adult"
)

// The ages section 6 is bound by: below the first a learner is under 13
// everywhere, and from the second an adult.
const (
	childAge = 13
	adultAge = 18
)

// dayLayout is a day of the calendar in UTC, as the table writes it.
const dayLayout = "2006-01-02"

// Table is the table of the age of digital consent, read and checked.
type Table struct {
	outside int
	// entries is each country's entries, by the day they apply from.
	entries map[string][]entry
	// countries is every code of ISO 3166-1 alpha-2 the schema lists, which a
	// learner's country may be.
	countries map[string]bool
}

type entry struct {
	age  int
	from time.Time
}

// Load is the table of spec's Go module at the tag api pins, spec.ConsentAge:
// spec's own check accepted it before its release, so it is read, not checked
// again (consent-age/README.md, section 5).
func Load() (*Table, error) {
	return Read(spec.ConsentAge)
}

// Read is the table of the part in fsys: consent-age.yaml, and the codes of
// countries of its schema.
func Read(fsys fs.FS) (*Table, error) {
	data, err := fs.ReadFile(fsys, "consent-age.yaml")
	if err != nil {
		return nil, fmt.Errorf("consentage: read the table: %w", err)
	}
	var table consentagev1.ConsentAge
	if err := FromYAML(data, &table); err != nil {
		return nil, fmt.Errorf("consentage: read the table: %w", err)
	}
	schema, err := fs.ReadFile(fsys, "schemas/consent-age.schema.json")
	if err != nil {
		return nil, fmt.Errorf("consentage: read the schema: %w", err)
	}
	var codes struct {
		Defs struct {
			Country struct {
				Enum []string `json:"enum"`
			} `json:"country"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schema, &codes); err != nil {
		return nil, fmt.Errorf("consentage: read the schema: %w", err)
	}
	if len(codes.Defs.Country.Enum) == 0 {
		return nil, errors.New("consentage: the schema lists no country")
	}
	t, err := New(table)
	if err != nil {
		return nil, err
	}
	t.countries = map[string]bool{}
	for _, code := range codes.Defs.Country.Enum {
		t.countries[code] = true
	}
	return t, nil
}

// New is the table of a value read already, with no list of countries.
func New(table consentagev1.ConsentAge) (*Table, error) {
	t := &Table{outside: int(table.OutsideTable), entries: map[string][]entry{}}
	for country, entries := range table.Countries {
		for _, e := range entries {
			from, err := time.Parse(dayLayout, string(e.From))
			if err != nil {
				return nil, fmt.Errorf("consentage: %s: a day %q: %w", country, e.From, err)
			}
			t.entries[string(country)] = append(t.entries[string(country)], entry{age: int(e.Age), from: from})
		}
		slices.SortFunc(t.entries[string(country)], func(a, b entry) int { return a.from.Compare(b.from) })
	}
	return t, nil
}

// FromYAML reads a value of the part's YAML into v by its JSON names, as the
// generated types of spec take them.
func FromYAML(data []byte, v any) error {
	var value any
	if err := yaml.Unmarshal(data, &value); err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, v)
}

// Country says whether code is one of the codes the schema lists: a learner's
// country is one of them, and anything else is refused.
func (t *Table) Country(code string) bool { return t.countries[code] }

// AgeOn is the age of digital consent of the country on the day: the age of its
// latest entry that applies by then, or the age outside the table.
func (t *Table) AgeOn(country string, day time.Time) int {
	day = dateOf(day)
	age := t.outside
	for _, e := range t.entries[country] {
		if e.from.After(day) {
			break
		}
		age = e.age
	}
	return age
}

// Category is the category of a learner of ageYears in the country on the
// day, and applied, the age of the country on it.
func (t *Table) Category(ageYears int, country string, day time.Time) (category string, applied int) {
	applied = t.AgeOn(country, day)
	switch {
	case ageYears < childAge:
		return Under13, applied
	case ageYears < applied:
		return From13ToConsent, applied
	case ageYears < adultAge:
		return FromConsentTo18, applied
	}
	return Adult, applied
}

// AskAgain says whether a profile of the category, given by applied, needs its
// age given again on today: a learner who consents themselves, in a country
// whose age rose above applied. A lower age waits for the yearly check.
func (t *Table) AskAgain(category string, applied int, country string, today time.Time) bool {
	return category == FromConsentTo18 && t.AgeOn(country, today) > applied
}

// ParentConsents says whether a learner of the category consents by a parent:
// below the age of digital consent of their country.
func ParentConsents(category string) bool {
	return category == Under13 || category == From13ToConsent
}

// dateOf is the day of t by UTC.
func dateOf(t time.Time) time.Time {
	year, month, day := t.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// specTable is the table of the tag, read once.
var specTable = sync.OnceValues(Load)

// Spec is the table of the tag api pins. It is embedded in the binary and read
// by the tests of this package, so that it cannot fail to read: a release that
// broke it is refused before it is made.
func Spec() *Table {
	t, err := specTable()
	if err != nil {
		panic(err)
	}
	return t
}
