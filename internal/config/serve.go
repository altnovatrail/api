package config

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// The variables of the command serve beyond the server's own (R29, R31, R32,
// R33).
const (
	// VarDatabaseURL is the pooler's connection string of the database api: a
	// secret, read as VarMigrateDatabaseURL is.
	VarDatabaseURL = "API_DATABASE_URL"
	// VarOpenFGADatabaseURL is the pooler's connection string of the database
	// openfga, the store of the rights.
	VarOpenFGADatabaseURL = "API_OPENFGA_DATABASE_URL"
	// VarFirebaseProjectID is the Firebase project whose tokens api accepts.
	VarFirebaseProjectID = "API_FIREBASE_PROJECT_ID"
	// VarFirebaseCredentials is the key of a service account of that project: a
	// secret, off a deployment only. Deployed, api takes the token of its
	// service's own account from the metadata server, with no key.
	VarFirebaseCredentials = "API_FIREBASE_CREDENTIALS"
	// VarAppOrigin is the exact origin of app, which CORS answers when api has an
	// origin of its own; unset, api stands on app's origin and answers no CORS.
	VarAppOrigin = "API_APP_ORIGIN"
	// VarPackagesDir is the folder of the packages of modules that api packages
	// laid: in the image, DefaultPackagesDir; for the first app, a folder outside
	// git (R41).
	VarPackagesDir = "API_PACKAGES_DIR"
)

// DefaultPackagesDir is where the stage of the Dockerfile lays the packages in
// the image.
const DefaultPackagesDir = "/packages"

// packagesDir is the folder of the packages env names, DefaultPackagesDir when
// it names none, and its refusal unless it is an absolute path: serve, sweep
// and after read it alike.
func packagesDir(env environment) (string, error) {
	dir := env.or(VarPackagesDir, DefaultPackagesDir)
	if !filepath.IsAbs(dir) {
		return dir, fmt.Errorf("%w: %s must be an absolute path, got %q", ErrInvalid, VarPackagesDir, dir)
	}
	return dir, nil
}

func init() {
	for _, name := range []string{VarFirebaseProjectID, VarAppOrigin, VarPackagesDir} {
		declared[name] = struct{}{}
	}
	for _, name := range []string{VarDatabaseURL, VarOpenFGADatabaseURL, VarFirebaseCredentials} {
		declared[name] = struct{}{}
		declared[name+fileSuffix] = struct{}{}
	}
}

// Serve is what serve opens beyond the server: the databases api and openfga
// through the pooler, the sign-in service and the origin of app.
type Serve struct {
	// DatabaseURL is the pooler's connection string of the database api (R29).
	DatabaseURL Secret
	// OpenFGADatabaseURL is the pooler's connection string of the database
	// openfga (R31).
	OpenFGADatabaseURL Secret
	// FirebaseProjectID is the Firebase project of the tokens (R32).
	FirebaseProjectID string
	// FirebaseCredentials is a service account's key off a deployment; deployed,
	// none (R32).
	FirebaseCredentials Secret
	// AppOrigin is the exact origin of app, or empty for none (R33).
	AppOrigin string
	// PackagesDir is the absolute path of the folder of the packages (R41).
	PackagesDir string
	// Admission is what the deployment declares for admission to AI (R25, R45).
	Admission Admission
}

// ServeFrom reads what serve opens from a set of KEY=VALUE strings, as
// MigrateFrom reads migrate's, and validates it. Deployed — cfg.Deployed —
// a key of the service account is refused; off a deployment it is required:
// the machine's default credentials, which might be gcloud's, are never taken.
func ServeFrom(environ []string, cfg *Config) (*Serve, error) {
	env := read(environ)

	var problems []error
	serve := &Serve{FirebaseProjectID: env[VarFirebaseProjectID], AppOrigin: env[VarAppOrigin]}
	// In this order, so that the refusals read the same on every run.
	problems = append(problems, databaseURLs(env, []namedSecret{
		{VarDatabaseURL, &serve.DatabaseURL},
		{VarOpenFGADatabaseURL, &serve.OpenFGADatabaseURL},
	})...)
	if serve.FirebaseProjectID == "" {
		problems = append(problems, fmt.Errorf("%w: %s must be set", ErrInvalid, VarFirebaseProjectID))
	}
	if err := firebaseCredentials(env, cfg.Deployed(), VarService, &serve.FirebaseCredentials); err != nil {
		problems = append(problems, err)
	}
	if err := checkOrigin(serve.AppOrigin, cfg.Deployed()); err != nil {
		problems = append(problems, err)
	}
	var err error
	if serve.PackagesDir, err = packagesDir(env); err != nil {
		problems = append(problems, err)
	}
	admission, err := AdmissionFrom(environ)
	if err != nil {
		problems = append(problems, err)
	}
	serve.Admission = admission
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return serve, nil
}

// checkOrigin refuses what is not one exact origin as a browser writes it in
// Origin — a scheme of the web, a host in lowercase and a port only when it is not
// the scheme's own, and nothing after — since CORS compares the two as they are,
// and an origin written otherwise would never match. A wildcard would answer every
// site; and deployed, app is served over https alone.
func checkOrigin(origin string, deployed bool) error {
	if origin == "" {
		return nil
	}
	refused := fmt.Errorf("%w: %s must be one origin as a browser writes it, such as https://app.example, got %q",
		ErrInvalid, VarAppOrigin, origin)
	parsed, err := url.Parse(origin)
	if err != nil {
		return refused
	}
	schemes := map[string]string{"https": "443", "http": "80"}
	if deployed {
		delete(schemes, "http")
	}
	defaultPort, known := schemes[parsed.Scheme]
	if !known || parsed.Hostname() == "" || parsed.Host != strings.ToLower(parsed.Host) ||
		parsed.Port() == defaultPort || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" ||
		parsed.ForceQuery || parsed.Fragment != "" || parsed.String() != origin {
		return refused
	}
	return nil
}

// firebaseCredentials reads the key of the service account into into by R32's
// rule: deployed — deployedBy, the variable the platform sets, is — a key is
// refused, since a deployment takes its own account's token with no key; off a
// deployment it is required, since the machine's default credentials might be
// gcloud's, which are never taken.
func firebaseCredentials(env environment, deployed bool, deployedBy string, into *Secret) error {
	_, inline := env[VarFirebaseCredentials]
	_, fromFile := env[VarFirebaseCredentials+fileSuffix]
	switch {
	case deployed && (inline || fromFile):
		return fmt.Errorf("%w: %s and %s%s must not be set when %s is: a deployment takes its service's own account, with no key",
			ErrInvalid, VarFirebaseCredentials, VarFirebaseCredentials, fileSuffix, deployedBy)
	case !deployed:
		var err error
		*into, err = env.secret(VarFirebaseCredentials)
		return err
	}
	return nil
}
