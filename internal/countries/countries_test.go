package countries_test

import (
	"testing"

	"github.com/altnovatrail/api/internal/countries"
)

func TestTheEUTheEEAAndTheUKAreClosed(t *testing.T) {
	t.Parallel()

	if len(countries.Closed) != 27+6+3+1+5 {
		t.Errorf("Closed holds %d codes, want 42: the EU's 27 and its 6 regions of their own codes, the EEA's 3, GB and 5 by caution", len(countries.Closed))
	}
	seen := map[string]bool{}
	for _, c := range countries.Closed {
		if !countries.Valid(c) || seen[c] {
			t.Errorf("Closed holds %q twice or out of form", c)
		}
		seen[c] = true
	}
	for _, c := range []string{"DE", "FR", "NO", "GB", "IS", "RE", "MF", "AX", "UK", "EL"} {
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
