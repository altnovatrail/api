// Package storetest runs the tests of api's database on a real PostgreSQL with no
// Docker, as the devcontainer and CI have none: embedded-postgres starts one server
// for a test binary, when its first test asks for a database, migrates a template database once, and gives each test a
// database of its own made from it, which is a copy and takes milliseconds (R29;
// amendment of 2026-11-02 to R05).
//
// A package whose tests use it runs them under its TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }
package storetest

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/store"
)

// version is the PostgreSQL the tests run on, and the one the Neon project of the
// first app is made with (R29).
const version = embeddedpostgres.V17

// supervisorPackage is embedded-postgres's own supervisor, which stops PostgreSQL
// when the test binary dies. It is built here from the module's sources, which
// go.sum holds, rather than downloaded and checked against a sum the library
// reads from the same release while it runs (amendment of 2026-11-02 to R05).
const supervisorPackage = "github.com/fergusstrange/embedded-postgres/v2/cmd/embedded-postgres"

// The databases the migrations run in once, which each test's is a copy of: the
// database api's, and the rights' in the database openfga, with its store and model.
const (
	template        = "api_template"
	openfgaTemplate = "openfga_template"
)

var (
	// once starts the server for the first test that asks for a database, and
	// started is how that went: a package whose tests ask for none starts none.
	once    sync.Once
	started error
	// rightsOnce makes the template of the rights for the first test that asks for
	// it, and rightsStarted is how that went.
	rightsOnce    sync.Once
	rightsStarted error
	// work is the folder of the server and its supervisor, and postgres the
	// server, which Main stops.
	work     string
	postgres *embeddedpostgres.EmbeddedPostgres
	// server is the URL of the server's database postgres, from which the
	// databases of the tests are made.
	server string
	// admin is a connection to it; mu keeps one statement on it at a time.
	admin *pgx.Conn
	mu    sync.Mutex
	// databases numbers the databases of the tests.
	databases atomic.Int64
)

// Main runs the tests and then stops the server, if a test started it, and
// returns the code the test binary exits with.
func Main(m *testing.M) int {
	code := m.Run()
	stop()
	return code
}

// start starts PostgreSQL and migrates the template, once.
func start() error {
	once.Do(func() { started = boot(context.Background()) })
	return started
}

func boot(ctx context.Context) error {
	var err error
	if work, err = os.MkdirTemp("", "storetest-"); err != nil {
		return err
	}

	// The supervisor is built by the Go that built the tests, for this machine, whatever
	// the tests' own GOOS, GOARCH and GOFLAGS say: it runs here, beside PostgreSQL.
	supervisor := filepath.Join(work, "embedded-postgres")
	build := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", supervisor, supervisorPackage)
	build.Env = append(os.Environ(), "GOOS=", "GOARCH=", "GOFLAGS=")
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("build the supervisor: %w\n%s", err, out)
	}
	if err := identity(work); err != nil {
		return err
	}

	postgres = embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(version).
		Port(0).
		Supervisor(supervisor).
		Logger(io.Discard).
		Storage(embeddedpostgres.Storage{WorkDir: work}))
	if err := postgres.StartContext(ctx); err != nil {
		return fmt.Errorf("start PostgreSQL: %w\n%s", err, postgres.Logs())
	}
	server = postgres.ConnectionURL()

	if admin, err = pgx.Connect(ctx, server); err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+template); err != nil {
		return fmt.Errorf("create the template: %w", err)
	}
	if _, _, err := store.Migrate(ctx, at(template)); err != nil {
		return fmt.Errorf("migrate the template: %w", err)
	}
	return nil
}

// identity gives PostgreSQL a name to run under when the tests run as a uid the
// system does not know, as CI runs them: .github's go.yml starts the toolchain
// container as the runner's uid, and initdb stops on a uid it cannot look up.
// nss_wrapper, which the toolchain stage installs, answers the lookup from a passwd
// and a group file of its own, written here; PostgreSQL's processes take it from
// the environment (R30). A uid the system knows needs none of it.
func identity(work string) error {
	uid, gid := strconv.Itoa(os.Getuid()), strconv.Itoa(os.Getgid())
	if _, err := user.LookupId(uid); err == nil {
		return nil
	}
	libraries, _ := filepath.Glob("/usr/lib/*-linux-gnu/libnss_wrapper.so")
	if len(libraries) == 0 {
		return fmt.Errorf("uid %s has no entry in /etc/passwd, and libnss_wrapper, which would give it one, is not installed (.devcontainer/Dockerfile)", uid)
	}
	passwd, group := filepath.Join(work, "passwd"), filepath.Join(work, "group")
	if err := os.WriteFile(passwd, []byte("storetest:x:"+uid+":"+gid+":storetest:"+work+":/usr/sbin/nologin\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(group, []byte("storetest:x:"+gid+":\n"), 0o600); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"LD_PRELOAD":         libraries[0],
		"NSS_WRAPPER_PASSWD": passwd,
		"NSS_WRAPPER_GROUP":  group,
	} {
		if err := os.Setenv(name, value); err != nil {
			return err
		}
	}
	return nil
}

// startRights makes the template of the rights, once, for the first test that
// asks for a database openfga: a package whose tests ask for none pays nothing for
// OpenFGA's migrations, store and model.
func startRights() error {
	if err := start(); err != nil {
		return err
	}
	rightsOnce.Do(func() {
		ctx := context.Background()
		if rightsStarted = exec1(ctx, "CREATE DATABASE "+openfgaTemplate); rightsStarted != nil {
			rightsStarted = fmt.Errorf("create the template of the rights: %w", rightsStarted)
			return
		}
		if _, err := authz.Migrate(ctx, at(openfgaTemplate)); err != nil {
			rightsStarted = fmt.Errorf("migrate the template of the rights: %w", err)
		}
	})
	return rightsStarted
}

// stop closes what start opened.
func stop() {
	if admin != nil {
		_ = admin.Close(context.Background())
	}
	if postgres != nil {
		_ = postgres.Close()
	}
	if work != "" {
		_ = os.RemoveAll(work)
	}
}

// at is the URL of the server's database name.
func at(name string) string {
	u, err := url.Parse(server)
	if err != nil {
		panic(fmt.Sprintf("storetest: the server's URL: %v", err))
	}
	u.Path, u.RawPath = "/"+name, ""
	return u.String()
}

// URL is the connection string of a database api of the test's own: migrated, as
// a copy of the template, or empty, made from template0 with no migration. It is
// dropped when the test ends.
func URL(t testing.TB, migrated bool) string {
	t.Helper()
	return database(t, template, migrated)
}

// OpenFGAURL is the connection string of a database openfga of the test's own:
// migrated, with the store api and its model, or empty. It is dropped when the
// test ends.
func OpenFGAURL(t testing.TB, migrated bool) string {
	t.Helper()
	if err := startRights(); err != nil {
		t.Fatalf("storetest: %v", err)
	}
	return database(t, openfgaTemplate, migrated)
}

// database is a copy of the template, or an empty database, dropped when the test
// ends.
func database(t testing.TB, migratedFrom string, migrated bool) string {
	t.Helper()
	if err := start(); err != nil {
		t.Fatalf("storetest: %v", err)
	}

	name := fmt.Sprintf("test_%d", databases.Add(1))
	from := migratedFrom
	if !migrated {
		from = "template0"
	}
	ctx := context.Background()
	if err := exec1(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, from)); err != nil {
		t.Fatalf("storetest: create the database: %v", err)
	}
	t.Cleanup(func() {
		if err := exec1(ctx, fmt.Sprintf("DROP DATABASE %s WITH (FORCE)", name)); err != nil {
			t.Errorf("storetest: drop the database: %v", err)
		}
	})
	return at(name)
}

// DB is a pool on a migrated database of the test's own, closed when the test
// ends.
func DB(t testing.TB) *pgxpool.Pool {
	t.Helper()

	pool, err := store.Open(context.Background(), URL(t, true))
	if err != nil {
		t.Fatalf("storetest: open the pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// exec1 runs a statement on the admin connection, one at a time: CREATE DATABASE
// copies its template only while nobody else is in it.
func exec1(ctx context.Context, statement string) error {
	mu.Lock()
	defer mu.Unlock()
	_, err := admin.Exec(ctx, statement)
	return err
}
