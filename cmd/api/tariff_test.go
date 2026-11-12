package main

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/invite"
	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

// redeemed is an account that redeemed a new code of the closed slice, on the
// database of url, with the code as the operator holds it.
func redeemed(t *testing.T, url string) (code, accountID string) {
	t.Helper()
	pool := poolOn(t, url)
	code, hash := invite.New()
	if err := store.CreateInvite(t.Context(), pool, hash[:], quota.ClosedSlice); err != nil {
		t.Fatal(err)
	}
	accountID, err := store.CreateAccount(t.Context(), pool, "uid-"+code, hash[:], nil)
	if err != nil {
		t.Fatal(err)
	}
	return code, accountID
}

// tariffOf is the tariff of the account, as the database keeps it.
func tariffOf(t *testing.T, url, accountID string) *string {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	var tariff *string
	if err := conn.QueryRow(t.Context(), `SELECT tariff FROM accounts WHERE id = $1`, accountID).Scan(&tariff); err != nil {
		t.Fatal(err)
	}
	return tariff
}

// tariff gives the account of a code the tariff named, written any way the code
// is read, and says the tariff alone: neither its output nor its log names the
// account. The code keeps the tariff it was made with.
func TestTariffGivesTheAccountOfACodeItsTariff(t *testing.T) {
	t.Parallel()

	url := storetest.URL(t, true)
	code, accountID := redeemed(t, url)
	other, otherID := redeemed(t, url)
	exit, stdout, stderr := runCommand(t.Context(), []string{"API_DATABASE_URL=" + url}, "tariff", "-tariff", quota.Probe,
		strings.ToLower(strings.ReplaceAll(code, "-", " ")))
	if exit != exitOK || stdout != quota.Probe+"\n" || stderr != "" {
		t.Fatalf("tariff = %d, %q, %q; want 0 and the tariff alone", exit, stdout, stderr)
	}
	if got := tariffOf(t, url, accountID); got == nil || *got != quota.Probe {
		t.Errorf("the account's tariff = %v, want %s", got, quota.Probe)
	}
	if got := tariffOf(t, url, otherID); got == nil || *got != quota.ClosedSlice {
		t.Errorf("another account's tariff = %v, want it as it was", got)
	}
	if strings.Contains(stdout+stderr, accountID) || strings.Contains(stdout+stderr, other) {
		t.Errorf("the output %q %q names an account or a code", stdout, stderr)
	}
	pool := poolOn(t, url)
	hash, _ := invite.Read(code)
	var kept string
	if err := pool.QueryRow(t.Context(), `SELECT tariff FROM invite_codes WHERE code_sha256 = $1`, hash[:]).Scan(&kept); err != nil || kept != quota.ClosedSlice {
		t.Errorf("the code's tariff = %q (%v), want the one it was made with", kept, err)
	}
}

// A code that no account redeemed, or whose account is being deleted, changes
// no tariff; what is no code or no tariff is a usage the command refuses.
func TestTariffRefusesWhatItCannotRun(t *testing.T) {
	t.Parallel()

	url := storetest.URL(t, true)
	env := []string{"API_DATABASE_URL=" + url}
	pool := poolOn(t, url)
	unredeemed, hash := invite.New()
	if err := store.CreateInvite(t.Context(), pool, hash[:], quota.ClosedSlice); err != nil {
		t.Fatal(err)
	}
	deleting, deletingID := redeemed(t, url)
	if err := pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error { return store.MarkDeleting(t.Context(), tx, deletingID) }); err != nil {
		t.Fatal(err)
	}
	code, _ := redeemed(t, url)
	for _, tc := range []struct {
		name    string
		environ []string
		args    []string
		exit    int
		says    string
	}{
		{"a code no account redeemed", env, []string{"-tariff", quota.Probe, unredeemed}, exitFailure, "no account"},
		{"an account being deleted", env, []string{"-tariff", quota.Probe, deleting}, exitFailure, "no account"},
		{"an unknown tariff", env, []string{"-tariff", "plus", code}, exitUsage, "closed_slice, probe"},
		{"no tariff", env, []string{code}, exitUsage, "-tariff must be"},
		{"no code", env, []string{"-tariff", quota.Probe}, exitUsage, "one argument"},
		{"two codes", env, []string{"-tariff", quota.Probe, code, code}, exitUsage, "one argument"},
		{"no code but words", env, []string{"-tariff", quota.Probe, "not-a-code"}, exitUsage, "no invite code"},
		{"no database", nil, []string{"-tariff", quota.Probe, code}, exitFailure, "API_DATABASE_URL"},
		{"an unknown flag", env, []string{"-count", "2"}, exitUsage, "-count"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			exit, stdout, stderr := runCommand(t.Context(), tc.environ, append([]string{"tariff"}, tc.args...)...)
			if exit != tc.exit || stdout != "" || !strings.Contains(stderr, tc.says) || strings.Contains(stderr, "not-a-code") {
				t.Errorf("tariff = %d, %q, %q; want %d, nothing printed and a refusal saying %s", exit, stdout, stderr, tc.exit, tc.says)
			}
		})
	}
	if got := tariffOf(t, url, deletingID); got == nil || *got != quota.ClosedSlice {
		t.Errorf("the tariff of the account being deleted = %v, want it as it was", got)
	}
	if exit, stdout, stderr := runCommand(t.Context(), nil, "tariff", "-h"); exit != exitOK || stdout != "" || !strings.Contains(stderr, "-tariff") {
		t.Errorf("tariff -h = %d, %q, %q; want 0 and the flags", exit, stdout, stderr)
	}
}

// A database that is not there is a failure logged once, on stderr.
func TestATariffTheDatabaseDoesNotTakeIsAFailure(t *testing.T) {
	t.Parallel()

	missing := strings.Replace(storetest.URL(t, false), "/test_", "/missing_", 1)
	code, _ := invite.New()
	var stdout, stderr syncBuffer
	if exit := run(t.Context(), []string{"tariff", "-tariff", quota.Probe, code}, []string{"API_DATABASE_URL=" + missing}, &stdout, &stderr); exit != exitFailure {
		t.Fatalf("exit code = %d, want %d", exit, exitFailure)
	}
	lines := stderr.lines(t)
	if stdout.String() != "" || len(lines) != 1 || lines[0]["message"] != "failure" || lines[0]["moment"] != "tariff" {
		t.Errorf("stdout %q, log %v; want one failure of the tariff", stdout.String(), lines)
	}
}

// poolOn is a pool on the database of url, closed when the test ends.
func poolOn(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
