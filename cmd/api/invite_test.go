package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/invite"
	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/store/storetest"
)

// invite prints one code, and the database keeps its hash with its tariff and
// nothing of the code itself.
func TestInvitePrintsACodeTheDatabaseKeepsOnlyTheHashOf(t *testing.T) {
	t.Parallel()

	url := storetest.URL(t, true)
	var stdout, stderr syncBuffer
	if code := run(t.Context(), []string{"invite"}, []string{"API_DATABASE_URL=" + url}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, want 0; stderr %q", code, stderr.String())
	}
	printed := strings.TrimSuffix(stdout.String(), "\n")
	hash, ok := invite.Read(printed)
	if !ok || strings.Contains(printed, "\n") || stderr.String() != "" {
		t.Fatalf("invite printed %q, stderr %q; want one code and nothing else", stdout.String(), stderr.String())
	}

	conn, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	var kept []byte
	var tariff string
	if err := conn.QueryRow(t.Context(), `SELECT code_sha256, tariff FROM invite_codes`).Scan(&kept, &tariff); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(kept, hash[:]) || tariff != quota.ClosedSlice {
		t.Errorf("row = %x %q, want the code's hash and %q", kept, tariff, quota.ClosedSlice)
	}
	// The code, as it is printed or as its symbols alone, is nowhere in the row.
	var row string
	if err := conn.QueryRow(t.Context(), `SELECT row_to_json(c)::text FROM invite_codes c`).Scan(&row); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(row, printed) || strings.Contains(row, strings.ReplaceAll(printed, "-", "")) {
		t.Errorf("the row %s holds the code", row)
	}
}

// Each run is a code of its own, with the tariff it is given.
func TestEachInviteIsANewCodeWithItsTariff(t *testing.T) {
	t.Parallel()

	environ := []string{"API_DATABASE_URL=" + storetest.URL(t, true)}
	codes := map[string]bool{}
	for range 2 {
		code, stdout, stderr := runCommand(t.Context(), environ, "invite", "-tariff", quota.ClosedSlice)
		if code != exitOK || codes[stdout] {
			t.Fatalf("invite = %d, %q, %q; want a new code", code, stdout, stderr)
		}
		codes[stdout] = true
	}
}

func TestInviteRefusesWhatItCannotRun(t *testing.T) {
	t.Parallel()

	url := "API_DATABASE_URL=" + storetest.URL(t, true)
	for _, tc := range []struct {
		name    string
		environ []string
		args    []string
		exit    int
		says    string
	}{
		{"an unknown tariff", []string{url}, []string{"-tariff", "plus"}, exitUsage, "closed_slice"},
		{"an unknown flag", []string{url}, []string{"-count", "2"}, exitUsage, "-count"},
		{"an argument", []string{url}, []string{"extra"}, exitUsage, "no arguments"},
		{"no database", nil, nil, exitFailure, "API_DATABASE_URL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			code, stdout, stderr := runCommand(t.Context(), tc.environ, append([]string{"invite"}, tc.args...)...)
			if code != tc.exit || stdout != "" || !strings.Contains(stderr, tc.says) {
				t.Errorf("invite = %d, %q, %q; want %d, nothing printed and a refusal saying %s", code, stdout, stderr, tc.exit, tc.says)
			}
		})
	}
	if code, stdout, stderr := runCommand(t.Context(), nil, "invite", "-h"); code != exitOK || stdout != "" || !strings.Contains(stderr, "-tariff") {
		t.Errorf("invite -h = %d, %q, %q; want 0 and the flags", code, stdout, stderr)
	}
}

// A database that is not there is a failure logged once, on stderr, and no code.
func TestAnInviteTheDatabaseDoesNotKeepIsNoCode(t *testing.T) {
	t.Parallel()

	missing := strings.Replace(storetest.URL(t, false), "/test_", "/missing_", 1)
	var stdout, stderr syncBuffer
	if code := run(t.Context(), []string{"invite"}, []string{"API_DATABASE_URL=" + missing}, &stdout, &stderr); code != exitFailure {
		t.Fatalf("exit code = %d, want %d", code, exitFailure)
	}
	lines := stderr.lines(t)
	if stdout.String() != "" || len(lines) != 1 || lines[0]["message"] != "failure" || lines[0]["moment"] != "invite" ||
		lines[0]["database"] != "api" {
		t.Errorf("stdout %q, log %v; want no code and one failure of the database api", stdout.String(), lines)
	}
}
