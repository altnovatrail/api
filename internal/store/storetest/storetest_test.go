package storetest_test

import (
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

// tables is how many tables of api's schema a database holds.
func tables(t *testing.T, url string) int {
	t.Helper()

	conn, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(t.Context()) }()
	var n int
	if err := conn.QueryRow(t.Context(),
		`SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'accounts'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestADatabaseIsMigratedOrEmpty(t *testing.T) {
	t.Parallel()

	if got := tables(t, storetest.URL(t, true)); got != 1 {
		t.Errorf("a migrated database has %d accounts tables, want 1", got)
	}
	if got := tables(t, storetest.URL(t, false)); got != 0 {
		t.Errorf("an empty database has %d accounts tables, want 0", got)
	}
}

// Each test's database is its own: what one writes, another does not see.
func TestEachTestHasADatabaseOfItsOwn(t *testing.T) {
	t.Parallel()

	first, second := storetest.DB(t), storetest.DB(t)
	if _, err := first.Exec(t.Context(), `INSERT INTO accounts (auth_uid) VALUES ('uid-synthetic')`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := second.QueryRow(t.Context(), `SELECT count(*) FROM accounts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("another test's database holds %d accounts, want 0", n)
	}
}
