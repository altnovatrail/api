package main

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

// debugRight is the right of the debug mode of the account of uid, as the
// database keeps it.
func debugRight(t *testing.T, url, uid string) bool {
	t.Helper()
	account, err := store.AccountByUID(t.Context(), poolOn(t, url), uid)
	if err != nil {
		t.Fatal(err)
	}
	return account.DebugModels
}

// debug-access gives the account of a UID the right of the debug mode and takes
// it, says which, and neither its output nor its log names the UID; another
// account keeps its own.
func TestDebugAccessGivesAndTakesTheRight(t *testing.T) {
	t.Parallel()

	url := storetest.URL(t, true)
	env := []string{"API_DATABASE_URL=" + url}
	code, _ := redeemed(t, url)
	other, _ := redeemed(t, url)
	uid, otherUID := "uid-"+code, "uid-"+other
	for _, tc := range []struct {
		action, says string
		right        bool
	}{
		{"grant", "granted\n", true},
		{"grant", "granted\n", true},
		{"revoke", "revoked\n", false},
		{"grant", "granted\n", true},
	} {
		exit, stdout, stderr := runCommand(t.Context(), env, "debug-access", tc.action, uid)
		if exit != exitOK || stdout != tc.says || stderr != "" {
			t.Fatalf("debug-access %s = %d, %q, %q; want 0 and %q alone", tc.action, exit, stdout, stderr, tc.says)
		}
		if got := debugRight(t, url, uid); got != tc.right {
			t.Errorf("the right after %s = %v, want %v", tc.action, got, tc.right)
		}
	}
	if debugRight(t, url, otherUID) {
		t.Error("another account got the right")
	}
}

// A UID with no account, or one of an account being deleted, gets no right and
// exits 1; what is no action or no UID is a usage the command refuses; none of
// it echoes the UID.
func TestDebugAccessRefusesWhatItCannotRun(t *testing.T) {
	t.Parallel()

	url := storetest.URL(t, true)
	env := []string{"API_DATABASE_URL=" + url}
	pool := poolOn(t, url)
	deleting, deletingID := redeemed(t, url)
	if err := pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error { return store.MarkDeleting(t.Context(), tx, deletingID) }); err != nil {
		t.Fatal(err)
	}
	code, _ := redeemed(t, url)
	uid := "uid-" + code
	for _, tc := range []struct {
		name    string
		environ []string
		args    []string
		exit    int
		says    string
	}{
		{"a UID with no account", env, []string{"grant", "uid-nobody"}, exitFailure, "no account has this UID"},
		{"an account being deleted", env, []string{"grant", "uid-" + deleting}, exitFailure, "no account has this UID"},
		{"no action", env, []string{uid}, exitUsage, "give grant <uid> or revoke <uid>"},
		{"an unknown action", env, []string{"give", uid}, exitUsage, "give grant <uid> or revoke <uid>"},
		{"no UID", env, []string{"grant"}, exitUsage, "give grant <uid> or revoke <uid>"},
		{"two UIDs", env, []string{"grant", uid, uid}, exitUsage, "give grant <uid> or revoke <uid>"},
		{"an empty UID", env, []string{"grant", ""}, exitUsage, "no UID of the sign-in service"},
		{"a UID too long", env, []string{"grant", strings.Repeat("u", maxUID+1)}, exitUsage, "no UID of the sign-in service"},
		{"no database", nil, []string{"grant", uid}, exitFailure, "API_DATABASE_URL"},
		{"an unknown flag", env, []string{"-all", "grant", uid}, exitUsage, "-all"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			exit, stdout, stderr := runCommand(t.Context(), tc.environ, append([]string{"debug-access"}, tc.args...)...)
			if exit != tc.exit || stdout != "" || !strings.Contains(stderr, tc.says) || strings.Contains(stderr, "uid-") {
				t.Errorf("debug-access = %d, %q, %q; want %d, nothing printed and a refusal saying %s", exit, stdout, stderr, tc.exit, tc.says)
			}
		})
	}
	if debugRight(t, url, uid) {
		t.Error("the account the refusals named got the right")
	}
	if exit, stdout, stderr := runCommand(t.Context(), nil, "debug-access", "-h"); exit != exitOK || stdout != "" || !strings.Contains(stderr, "grant <uid>") {
		t.Errorf("debug-access -h = %d, %q, %q; want 0 and the usage", exit, stdout, stderr)
	}
}

// A database that is not there is a failure logged once, on stderr.
func TestADebugAccessTheDatabaseDoesNotTakeIsAFailure(t *testing.T) {
	t.Parallel()

	missing := strings.Replace(storetest.URL(t, false), "/test_", "/missing_", 1)
	var stdout, stderr syncBuffer
	if exit := run(t.Context(), []string{"debug-access", "grant", "uid-x"}, []string{"API_DATABASE_URL=" + missing}, &stdout, &stderr); exit != exitFailure {
		t.Fatalf("exit code = %d, want %d", exit, exitFailure)
	}
	lines := stderr.lines(t)
	if stdout.String() != "" || len(lines) != 1 || lines[0]["message"] != "failure" || lines[0]["moment"] != "debug_access" {
		t.Errorf("stdout %q, log %v; want one failure of debug-access", stdout.String(), lines)
	}
}
