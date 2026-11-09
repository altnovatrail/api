package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/altnovatrail/api/internal/config"
)

// serveEnviron is what serve needs off a deployment, all synthetic.
var serveEnviron = []string{
	"API_DATABASE_URL=" + remote,
	"API_OPENFGA_DATABASE_URL=" + local,
	"API_FIREBASE_PROJECT_ID=novatrail-test",
	`API_FIREBASE_CREDENTIALS={"type":"service_account"}`,
}

// without is environ with the variable called name left out.
func without(environ []string, name string) []string {
	var kept []string
	for _, entry := range environ {
		if !strings.HasPrefix(entry, name+"=") {
			kept = append(kept, entry)
		}
	}
	return kept
}

func loaded(t *testing.T, environ []string) *config.Config {
	t.Helper()
	cfg, err := config.LoadFrom(environ)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestServeReadsWhatItOpens(t *testing.T) {
	t.Parallel()

	environ := append(serveEnviron, "API_APP_ORIGIN=https://app.example")
	serve, err := config.ServeFrom(environ, loaded(t, nil))
	if err != nil {
		t.Fatalf("ServeFrom error = %v, want nil", err)
	}
	if serve.DatabaseURL.Reveal() != remote || serve.OpenFGADatabaseURL.Reveal() != local ||
		serve.FirebaseProjectID != "novatrail-test" || serve.FirebaseCredentials.Reveal() != `{"type":"service_account"}` ||
		serve.AppOrigin != "https://app.example" {
		t.Errorf("ServeFrom = %+v, want the environment's values", serve)
	}
}

// Deployed, the service takes its own account's token, with no key; off a
// deployment, the key is required, and no origin is needed.
func TestTheKeyIsForThisMachineOnly(t *testing.T) {
	t.Parallel()

	deployed := loaded(t, []string{"K_SERVICE=api"})
	serve, err := config.ServeFrom(without(serveEnviron, "API_FIREBASE_CREDENTIALS"), deployed)
	if err != nil || serve.FirebaseCredentials.Reveal() != "" || serve.AppOrigin != "" {
		t.Errorf("ServeFrom deployed with no key = %+v, %v; want no key and no error", serve, err)
	}
	for name, environ := range map[string][]string{
		"the key":        serveEnviron,
		"the key's file": append(without(serveEnviron, "API_FIREBASE_CREDENTIALS"), "API_FIREBASE_CREDENTIALS_FILE=/run/key.json"),
	} {
		if _, err := config.ServeFrom(environ, deployed); !errors.Is(err, config.ErrInvalid) ||
			!strings.Contains(err.Error(), "API_FIREBASE_CREDENTIALS") {
			t.Errorf("ServeFrom deployed with %s: error = %v, want a refusal naming the key", name, err)
		}
	}
}

// A refusal names its variable, and every refusal is named at once.
func TestServeRefusals(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		environ []string
		names   []string
	}{
		{"nothing", nil, []string{"API_DATABASE_URL", "API_OPENFGA_DATABASE_URL", "API_FIREBASE_PROJECT_ID", "API_FIREBASE_CREDENTIALS"}},
		{"a database with no check of the server",
			append(without(serveEnviron, "API_DATABASE_URL"), "API_DATABASE_URL=postgres://u:p@db.example/api?sslmode=require"),
			[]string{"API_DATABASE_URL"}},
		{"a store of rights pgx cannot read",
			append(without(serveEnviron, "API_OPENFGA_DATABASE_URL"), "API_OPENFGA_DATABASE_URL=postgres://u:p@h:port/x"),
			[]string{"API_OPENFGA_DATABASE_URL"}},
		{"no key off a deployment", without(serveEnviron, "API_FIREBASE_CREDENTIALS"), []string{"API_FIREBASE_CREDENTIALS"}},
		{"a wildcard origin", append(serveEnviron, "API_APP_ORIGIN=*"), []string{"API_APP_ORIGIN"}},
		{"an origin with a path", append(serveEnviron, "API_APP_ORIGIN=https://app.example/"), []string{"API_APP_ORIGIN"}},
		{"an origin of no web scheme", append(serveEnviron, "API_APP_ORIGIN=ftp://app.example"), []string{"API_APP_ORIGIN"}},
		{"an origin with a user", append(serveEnviron, "API_APP_ORIGIN=https://u@app.example"), []string{"API_APP_ORIGIN"}},
		{"no origin at all", append(serveEnviron, "API_APP_ORIGIN=app.example"), []string{"API_APP_ORIGIN"}},
		{"an origin that does not parse", append(serveEnviron, "API_APP_ORIGIN=https://%zz"), []string{"API_APP_ORIGIN"}},
		{"an origin with its default port", append(serveEnviron, "API_APP_ORIGIN=https://app.example:443"), []string{"API_APP_ORIGIN"}},
		{"an origin in uppercase", append(serveEnviron, "API_APP_ORIGIN=https://App.Example"), []string{"API_APP_ORIGIN"}},
		{"an origin with an empty query", append(serveEnviron, "API_APP_ORIGIN=https://app.example?"), []string{"API_APP_ORIGIN"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			serve, err := config.ServeFrom(tc.environ, loaded(t, nil))
			if !errors.Is(err, config.ErrInvalid) {
				t.Fatalf("ServeFrom = %+v, %v; want %v", serve, err, config.ErrInvalid)
			}
			for _, name := range tc.names {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("error %q does not name %s", err, name)
				}
			}
			if strings.Contains(err.Error(), "not-a-password") {
				t.Errorf("error %q holds a password", err)
			}
		})
	}
}

// Off a deployment app may be served over http, on a port of its own; deployed,
// only over https.
func TestAnOriginOverHTTPIsForThisMachineOnly(t *testing.T) {
	t.Parallel()

	local := append(serveEnviron, "API_APP_ORIGIN=http://localhost:5173")
	if serve, err := config.ServeFrom(local, loaded(t, nil)); err != nil || serve.AppOrigin != "http://localhost:5173" {
		t.Errorf("ServeFrom off a deployment = %+v, %v; want the origin", serve, err)
	}
	deployed := append(without(serveEnviron, "API_FIREBASE_CREDENTIALS"), "API_APP_ORIGIN=http://app.example")
	if _, err := config.ServeFrom(deployed, loaded(t, []string{"K_SERVICE=api"})); !errors.Is(err, config.ErrInvalid) ||
		!strings.Contains(err.Error(), "API_APP_ORIGIN") {
		t.Errorf("ServeFrom deployed over http = %v, want a refusal naming API_APP_ORIGIN", err)
	}
}
