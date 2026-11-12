package main

import (
	"strings"
	"testing"

	"github.com/altnovatrail/api/internal/store/storetest"
)

// reopen opens platform AI that the limit of its provider closed, and says
// whether it was closed, on stdout and in its line of the log; a second run
// finds it open.
func TestReopenOpensPlatformAIAgain(t *testing.T) {
	t.Parallel()

	url := storetest.URL(t, true)
	pool := poolOn(t, url)
	if _, err := pool.Exec(t.Context(), `UPDATE platform_ai SET closed_at = now()`); err != nil {
		t.Fatal(err)
	}
	env := []string{"API_DATABASE_URL=" + url}
	var stdout, stderr syncBuffer
	if exit := run(t.Context(), []string{"reopen"}, env, &stdout, &stderr); exit != exitOK ||
		!strings.Contains(stdout.String(), "open again") {
		t.Fatalf("reopen = %d, %q, %q", exit, stdout.String(), stderr.String())
	}
	if lines := stderr.lines(t); len(lines) != 1 || lines[0]["message"] != "platform_ai_opened" || lines[0]["was_closed"] != true {
		t.Errorf("the log = %v, want one line that it was closed", lines)
	}
	var closed bool
	if err := pool.QueryRow(t.Context(), `SELECT closed_at IS NOT NULL FROM platform_ai`).Scan(&closed); err != nil || closed {
		t.Errorf("platform AI closed after reopen = %v (%v)", closed, err)
	}
	if exit, stdout, _ := runCommand(t.Context(), env, "reopen"); exit != exitOK || !strings.Contains(stdout, "open already") {
		t.Errorf("a second reopen = %d, %q; want it open already", exit, stdout)
	}
	for _, tc := range []struct {
		environ []string
		args    []string
		exit    int
		says    string
	}{
		{env, []string{"extra"}, exitUsage, "no arguments"},
		{nil, nil, exitFailure, "API_DATABASE_URL"},
	} {
		if exit, stdout, stderr := runCommand(t.Context(), tc.environ, append([]string{"reopen"}, tc.args...)...); exit != tc.exit ||
			stdout != "" || !strings.Contains(stderr, tc.says) {
			t.Errorf("reopen %v = %d, %q, %q; want %d and %s", tc.args, exit, stdout, stderr, tc.exit, tc.says)
		}
	}
	missing := strings.Replace(storetest.URL(t, false), "/test_", "/missing_", 1)
	var out, log syncBuffer
	if exit := run(t.Context(), []string{"reopen"}, []string{"API_DATABASE_URL=" + missing}, &out, &log); exit != exitFailure {
		t.Errorf("reopen on no database = %d", exit)
	}
	if lines := log.lines(t); len(lines) != 1 || lines[0]["moment"] != "reopen" {
		t.Errorf("the log of reopen on no database = %v", lines)
	}
}
