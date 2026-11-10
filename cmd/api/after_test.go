package main

import (
	"strings"
	"testing"

	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/store/storetest"
)

// afterEnviron is what the job after a rollout reads: the database api, by url,
// and the packages laid, as a deployment gives them.
func afterEnviron(t *testing.T, url string, more ...string) []string {
	t.Helper()
	return append([]string{"API_DATABASE_URL=" + url, "API_PACKAGES_DIR=" + laidPackages(t)}, more...)
}

// A pass on the pinned release, whose versions are all out of the window, asks
// for what is open on them, writes a line for each step and exits 0 when it is
// clean; it opens neither the rights nor the sign-in service.
func TestAfterSaysEachStep(t *testing.T) {
	t.Parallel()

	var stdout, stderr syncBuffer
	if code := run(t.Context(), []string{"after", "-for", "1m"}, afterEnviron(t, storetest.URL(t, true)), &stdout, &stderr); code != exitOK || stderr.String() != "" {
		t.Fatalf("after = %d, stderr %q, stdout %q; want 0 and nothing", code, stderr.String(), stdout.String())
	}
	lines := stdout.lines(t)
	if len(lines) != len(logging.AfterSteps) {
		t.Fatalf("the log is %v, want a line for each step", lines)
	}
	for i, line := range lines {
		if line["message"] != "swept" || line["step"] != logging.AfterSteps[i] || line["finished"] != true || line["done"] != float64(0) {
			t.Errorf("line %d = %v, want step %s finished with nothing done", i, line, logging.AfterSteps[i])
		}
	}
}

// A pass whose steps fail exits 1, its log having said which; what it cannot
// open or read is one failure of the job, before any step.
func TestAfterThatIsNotCleanExits1(t *testing.T) {
	t.Parallel()

	// A database with no schema: each step's query fails.
	code, out, errOut := runCommand(t.Context(), afterEnviron(t, storetest.URL(t, false)), "after")
	if code != exitFailure || errOut != "" || !strings.Contains(out, `"moment":"after","step":"gone_attempts"`) ||
		!strings.Contains(out, `"step":"gone_runs","done":0,"failed":1,"finished":false`) {
		t.Errorf("after on no schema = %d, %q, %q; want each step's failure", code, out, errOut)
	}

	missing := strings.Replace(storetest.URL(t, false), "/test_", "/missing_", 1)
	var stdout, stderr syncBuffer
	code = run(t.Context(), []string{"after"}, afterEnviron(t, missing), &stdout, &stderr)
	if lines := stdout.lines(t); code != exitFailure || len(lines) != 1 || lines[0]["moment"] != "after" || lines[0]["database"] != "api" {
		t.Errorf("after with no database = %d, log %v; want one failure of the database api", code, lines)
	}
	code, out, _ = runCommand(t.Context(), afterEnviron(t, storetest.URL(t, true), "API_PACKAGES_DIR="+t.TempDir()), "after")
	if code != exitFailure || !strings.Contains(out, `"moment":"after"`) || strings.Contains(out, `"step"`) {
		t.Errorf("after with no packages = %d, %q; want one failure before any step", code, out)
	}
}

func TestAfterRefusesWhatItCannotRun(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		exit int
		says string
	}{
		{"no time", []string{"-for", "-1m"}, exitUsage, "api after: -for must be a positive duration"},
		{"an argument", []string{"now"}, exitUsage, "api after takes no arguments"},
		{"an unknown flag", []string{"-fast"}, exitUsage, "flag provided but not defined"},
		{"help", []string{"-h"}, exitOK, "Usage of api after"},
		{"no configuration", nil, exitFailure, "API_DATABASE_URL"},
	} {
		code, stdout, stderr := runCommand(t.Context(), nil, append([]string{"after"}, tc.args...)...)
		if code != tc.exit || stdout != "" || !strings.Contains(stderr, tc.says) {
			t.Errorf("%s: after = %d, %q, %q; want %d and %q", tc.name, code, stdout, stderr, tc.exit, tc.says)
		}
	}
}
