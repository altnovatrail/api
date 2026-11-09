package countries_test

import (
	"testing"

	"github.com/altnovatrail/api/internal/countries"
)

func TestTheEUTheEEAAndTheUKAreClosed(t *testing.T) {
	t.Parallel()

	if len(countries.Closed) != 27+3+1 {
		t.Errorf("Closed holds %d countries, want 31: the EU's 27, the EEA's 3 and GB", len(countries.Closed))
	}
	seen := map[string]bool{}
	for _, c := range countries.Closed {
		if !countries.Valid(c) || seen[c] {
			t.Errorf("Closed holds %q twice or out of form", c)
		}
		seen[c] = true
	}
	for _, c := range []string{"DE", "FR", "NO", "GB", "IS"} {
		if !countries.IsClosed(c) {
			t.Errorf("IsClosed(%q) = false", c)
		}
	}
	for _, c := range []string{"US", "CA", "CH", "UA"} {
		if countries.IsClosed(c) {
			t.Errorf("IsClosed(%q) = true", c)
		}
	}
}

func TestACountryIsTwoCapitalLetters(t *testing.T) {
	t.Parallel()

	for _, c := range []string{"US", "ZZ"} {
		if !countries.Valid(c) {
			t.Errorf("Valid(%q) = false", c)
		}
	}
	for _, c := range []string{"", "U", "USA", "us", "U1", "ÜS"} {
		if countries.Valid(c) {
			t.Errorf("Valid(%q) = true", c)
		}
	}
}
