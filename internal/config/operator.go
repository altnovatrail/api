package config

import (
	"errors"
	"log/slog"
)

// Operator is the configuration of a command of the operator that writes to the
// database api, such as invite (R34): the level of its log and the pooler's
// connection string, which serve takes too. Its writes keep no session, so the
// pooler serves them as it serves the requests (R29).
type Operator struct {
	// LogLevel is the lowest level the log writes.
	LogLevel slog.Level
	// DatabaseURL is the pooler's connection string of the database api.
	DatabaseURL Secret
}

// OperatorFrom reads the configuration of a command of the operator from a set
// of KEY=VALUE strings, as MigrateFrom reads migrate's, and validates it.
func OperatorFrom(environ []string) (*Operator, error) {
	env := read(environ)

	var problems []error
	cfg := &Operator{}
	var err error
	if cfg.LogLevel, err = level(env.or(VarLogLevel, DefaultLogLevel)); err != nil {
		problems = append(problems, err)
	}
	if cfg.DatabaseURL, err = env.secret(VarDatabaseURL); err != nil {
		problems = append(problems, err)
	} else if err := checkDatabaseURL(VarDatabaseURL, cfg.DatabaseURL); err != nil {
		problems = append(problems, err)
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return cfg, nil
}
