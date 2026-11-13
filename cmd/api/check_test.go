package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
			map[string]any{"message": "admission", "admission_table": "0.3", "facts_file": false, "rung": "full", "rung_set": false, "platform_keys": float64(0), "tutor_stub": false}, ""},
		{"the first app's", []string{"API_ADMISSION_FACTS_FILE=" + known, "API_ADMISSION_RUNG=author"}, nil, exitOK,
			map[string]any{"message": "admission", "admission_table": "0.3", "facts_file": true, "rung": "author", "rung_set": true}, ""},
		{"a fact the table does not know", []string{"CLOUD_RUN_JOB=check", "API_ADMISSION_FACTS_FILE=" + unknown}, nil, exitFailure, nil,
			`api: config: invalid: API_ADMISSION_FACTS_FILE: admission: the file of facts names, for "anthropic", the fact "data_procesing_agreement"`},
		{"the stage author as a job", []string{"CLOUD_RUN_JOB=check", "API_ADMISSION_RUNG=author"}, nil, exitFailure, nil,
			"api: config: invalid: API_ADMISSION_RUNG must not be author when CLOUD_RUN_JOB is set"},
		{"the dev mode's stub", []string{"API_TUTOR_STUB=1", "API_ADMISSION_FACTS_FILE=" + known, "API_ADMISSION_RUNG=author"}, nil, exitOK,
			map[string]any{"message": "admission", "rung": "author", "platform_keys": float64(0), "tutor_stub": true}, ""},
		{"the stub as a job", []string{"CLOUD_RUN_JOB=check", "API_TUTOR_STUB=1"}, nil, exitFailure, nil,
			"api: config: invalid: API_TUTOR_STUB must not be set when CLOUD_RUN_JOB is set"},
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

// serve refuses the stub of tutor on a deployment, as it refuses the emulator
// of the sign-in service, before it writes a line or opens anything (R66).
func TestServeRefusesTheStubOnADeployment(t *testing.T) {
	t.Parallel()

	code, stdout, stderr := runCommand(t.Context(), serveEnviron(t, "PORT=8080", "K_SERVICE=api", "API_TUTOR_STUB=1"), "serve")
	if code != exitFailure || stdout != "" || !strings.Contains(stderr, "API_TUTOR_STUB must not be set when K_SERVICE is set") {
		t.Errorf("serve deployed with the stub = %d, %q, %q; want a refusal of the stub", code, stdout, stderr)
	}
}

// Off a deployment serve starts on the stub of tutor, and its startup line says
// so, with no key of the platform (R66).
func TestServeStartsOnTheStub(t *testing.T) {
	t.Parallel()

	environ := serveEnviron(t, "PORT=0", "API_TUTOR_STUB=1")
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	var stdout, stderr syncBuffer
	exited := make(chan int, 1)
	go func() { exited <- run(ctx, []string{"serve"}, environ, &stdout, &stderr) }()
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if lines := stdout.lines(t); len(lines) > 0 && lines[0]["message"] == "startup" {
			if lines[0]["tutor_stub"] != true || lines[0]["platform_keys"] != float64(0) {
				t.Errorf("the startup line %v does not say the stub, with no key of the platform", lines[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the service wrote no startup line: %s %s", stdout.String(), stderr.String())
		}
	}
	stop()
	if code := <-exited; code != exitOK {
		t.Errorf("serve on the stub exited %d, want %d: %s", code, exitOK, stderr.String())
	}
}
