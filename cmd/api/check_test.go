package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// facts is a file of facts with contents, in a folder of the test's own.
func facts(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "facts.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// api check says what the deployment declares in one line, and serves nothing;
// what it cannot take is refused before any log, naming the variable.
func TestCheckSaysWhatIsDeclared(t *testing.T) {
	t.Parallel()

	known := facts(t, "anthropic:\n  data_processing_agreement: [adult]\n  minimal_retention: [adult]\n")
	unknown := facts(t, "anthropic:\n  data_procesing_agreement: [adult]\n")
	for _, tc := range []struct {
		name    string
		environ []string
		args    []string
		code    int
		line    map[string]any
		stderr  string
	}{
		{"nothing declared", []string{"CLOUD_RUN_JOB=check"}, nil, exitOK,
			map[string]any{"message": "admission", "admission_table": "0.2", "facts_file": false, "rung": "full", "rung_set": false}, ""},
		{"the first app's", []string{"API_ADMISSION_FACTS_FILE=" + known, "API_ADMISSION_RUNG=author"}, nil, exitOK,
			map[string]any{"message": "admission", "admission_table": "0.2", "facts_file": true, "rung": "author", "rung_set": true}, ""},
		{"a fact the table does not know", []string{"CLOUD_RUN_JOB=check", "API_ADMISSION_FACTS_FILE=" + unknown}, nil, exitFailure, nil,
			`api: config: invalid: API_ADMISSION_FACTS_FILE: admission: the file of facts names, for "anthropic", the fact "data_procesing_agreement"`},
		{"the stage author as a job", []string{"CLOUD_RUN_JOB=check", "API_ADMISSION_RUNG=author"}, nil, exitFailure, nil,
			"api: config: invalid: API_ADMISSION_RUNG must not be author when CLOUD_RUN_JOB is set"},
		{"a wrong level", []string{"API_LOG_LEVEL=loud"}, nil, exitFailure, nil, "api: config: invalid: API_LOG_LEVEL"},
		{"an argument", nil, []string{"now"}, exitUsage, nil, "api check takes no arguments"},
		{"-h", nil, []string{"-h"}, exitOK, nil, "Usage of api check"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr syncBuffer
			code := run(t.Context(), append([]string{"check"}, tc.args...), tc.environ, &stdout, &stderr)
			if code != tc.code || !strings.HasPrefix(stderr.String(), tc.stderr) || (tc.stderr == "" && stderr.String() != "") {
				t.Errorf("api check = %d, stderr %q; want %d, %q", code, stderr.String(), tc.code, tc.stderr)
			}
			lines := stdout.lines(t)
			if tc.line == nil {
				if len(lines) != 0 {
					t.Errorf("the log is %v, want nothing", lines)
				}
				return
			}
			if len(lines) != 1 {
				t.Fatalf("the log is %v, want one line", lines)
			}
			for key, want := range tc.line {
				if lines[0][key] != want {
					t.Errorf("%s = %v, want %v, in %v", key, lines[0][key], want, lines[0])
				}
			}
		})
	}
}

// serve refuses a file of facts the table does not take as it refuses the
// variables, before it writes a line or opens anything.
func TestServeRefusesAFileOfFactsBeforeTheLog(t *testing.T) {
	t.Parallel()

	code, stdout, stderr := runCommand(t.Context(),
		serveEnviron(t, "PORT=0", "API_ADMISSION_FACTS_FILE="+facts(t, "acme:\n  minimal_retention: [adult]\n")), "serve")
	if code != exitFailure || stdout != "" || !strings.HasPrefix(stderr, "api: config: invalid: API_ADMISSION_FACTS_FILE") {
		t.Errorf("serve with a provider the table does not know = %d, %q, %q; want a refusal of the file", code, stdout, stderr)
	}
}
