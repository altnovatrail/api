package config

import "errors"

// After is what the job after a rollout opens (R07, R51): the database api, by
// the pooler's connection string, and the folder of the packages, as serve reads
// it — of which the job learns what the image no longer gives, and builds the
// evidence of the attempts it closes. The rights and the sign-in service it does
// not open. The level of its log is the process's Config, read as serve reads
// it.
type After struct {
	// DatabaseURL is the pooler's connection string of the database api.
	DatabaseURL Secret
	// PackagesDir is the absolute path of the folder of the packages.
	PackagesDir string
}

// AfterFrom reads what the job after a rollout opens from a set of KEY=VALUE
// strings, as SweepFrom reads sweep's, and validates it.
func AfterFrom(environ []string) (*After, error) {
	env := read(environ)

	cfg := &After{}
	problems := databaseURLs(env, []namedSecret{{VarDatabaseURL, &cfg.DatabaseURL}})
	var err error
	if cfg.PackagesDir, err = packagesDir(env); err != nil {
		problems = append(problems, err)
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return cfg, nil
}
