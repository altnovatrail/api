package main

import (
	"strings"
	"testing"

	"github.com/altnovatrail/api/internal/store/storetest"
)

// The operator allows a country, lists it and removes it.
func TestTheOperatorKeepsTheListOfCountries(t *testing.T) {
	t.Parallel()

	environ := []string{"API_DATABASE_URL=" + storetest.URL(t, true)}
	for _, step := range []struct {
		args []string
		says string
	}{
		{[]string{"list"}, ""},
		{[]string{"allow", "US"}, "US allowed\n"},
		{[]string{"allow", "US"}, "US was allowed already\n"},
		{[]string{"allow", "CA"}, "CA allowed\n"},
		{[]string{"list"}, "CA\nUS\n"},
		{[]string{"remove", "US"}, "US removed\n"},
		{[]string{"remove", "US"}, "US was not allowed\n"},
		{[]string{"list"}, "CA\n"},
	} {
		code, stdout, stderr := runCommand(t.Context(), environ, append([]string{"countries"}, step.args...)...)
		if code != exitOK || stdout != step.says || stderr != "" {
			t.Fatalf("countries %v = %d, %q, %q; want %q", step.args, code, stdout, stderr, step.says)
		}
	}
}

func TestTheCountriesCommandRefusesWhatItMayNotDo(t *testing.T) {
	t.Parallel()

	url := "API_DATABASE_URL=" + storetest.URL(t, true)
	for _, tc := range []struct {
		name    string
		environ []string
		args    []string
		exit    int
		says    string
	}{
		{"a country of the EU", []string{url}, []string{"allow", "DE"}, exitUsage, "DPIA"},
		{"the United Kingdom", []string{url}, []string{"allow", "GB"}, exitUsage, "DPIA"},
		{"a country of no form", []string{url}, []string{"allow", "usa"}, exitUsage, "ISO 3166-1"},
		{"no action", []string{url}, nil, exitUsage, "list, allow"},
		{"an action unknown", []string{url}, []string{"open", "US"}, exitUsage, "list, allow"},
		{"a list with a country", []string{url}, []string{"list", "US"}, exitUsage, "list, allow"},
		{"a flag unknown", []string{url}, []string{"-all"}, exitUsage, "-all"},
		{"no database", nil, []string{"list"}, exitFailure, "API_DATABASE_URL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			code, stdout, stderr := runCommand(t.Context(), tc.environ, append([]string{"countries"}, tc.args...)...)
			if code != tc.exit || stdout != "" || !strings.Contains(stderr, tc.says) {
				t.Errorf("countries %v = %d, %q, %q; want %d and a refusal saying %s", tc.args, code, stdout, stderr, tc.exit, tc.says)
			}
		})
	}
	// A closed country can still be removed, if a list ever held one.
	if code, _, stderr := runCommand(t.Context(), []string{url}, "countries", "remove", "DE"); code != exitOK {
		t.Errorf("countries remove DE = %d, %q; want it done", code, stderr)
	}
	if code, _, stderr := runCommand(t.Context(), nil, "countries", "-h"); code != exitOK || !strings.Contains(stderr, "DPIA") {
		t.Errorf("countries -h = %d, %q; want its usage", code, stderr)
	}
}

// A database that is not there is a failure logged once, on stderr.
func TestACountryTheDatabaseDoesNotKeepIsAFailure(t *testing.T) {
	t.Parallel()

	missing := strings.Replace(storetest.URL(t, false), "/test_", "/missing_", 1)
	for _, args := range [][]string{{"list"}, {"allow", "US"}, {"remove", "US"}} {
		var stdout, stderr syncBuffer
		if code := run(t.Context(), append([]string{"countries"}, args...), []string{"API_DATABASE_URL=" + missing}, &stdout, &stderr); code != exitFailure {
			t.Fatalf("countries %v: exit code = %d, want %d", args, code, exitFailure)
		}
		lines := stderr.lines(t)
		if stdout.String() != "" || len(lines) != 1 || lines[0]["moment"] != "countries" || lines[0]["database"] != "api" {
			t.Errorf("countries %v: stdout %q, log %v; want one failure of the database api", args, stdout.String(), lines)
		}
	}
}
