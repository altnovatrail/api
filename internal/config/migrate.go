package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// Migrate is the configuration of the command migrate: the level of its log
// and the direct connection string of the database it migrates (R29).
type Migrate struct {
	// LogLevel is the lowest level the log writes.
	LogLevel slog.Level
	// DatabaseURL is the database's direct connection string. Neon's pooler
	// keeps no session, and goose locks the migration by an advisory lock of
	// the session, so migrate never goes through the pooler.
	DatabaseURL Secret
}

// MigrateFrom reads the configuration of migrate from a set of KEY=VALUE
// strings, as LoadFrom reads the server's, and validates it.
func MigrateFrom(environ []string) (*Migrate, error) {
	env := read(environ)

	var problems []error
	cfg := &Migrate{}
	var err error
	if cfg.LogLevel, err = level(env.or(VarLogLevel, DefaultLogLevel)); err != nil {
		problems = append(problems, err)
	}
	if cfg.DatabaseURL, err = env.secret(VarMigrateDatabaseURL); err != nil {
		problems = append(problems, err)
	} else if err := checkDatabaseURL(VarMigrateDatabaseURL, cfg.DatabaseURL); err != nil {
		problems = append(problems, err)
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return cfg, nil
}

// checkDatabaseURL refuses a connection string that pgx cannot read, or one by
// which pgx would reach a server off this machine without checking its
// certificate. The check is of what pgx makes of the string, not of the string:
// pgx reads a host, an sslmode or a user where a URL parser reads none, and turns
// sslmode=require into encryption with no check of whom it talks to. So every host
// pgx would try — the first and each fallback — is on this machine, as the tests'
// server is, or is reached by TLS that verifies the server, which sslmode=
// verify-full asks for. The refusal names the variable and never the value.
func checkDatabaseURL(name string, secret Secret) error {
	parsed, err := pgconn.ParseConfig(secret.Reveal())
	if err != nil {
		return fmt.Errorf("%w: %s is no connection string pgx can read", ErrInvalid, name)
	}
	tries := []*pgconn.FallbackConfig{{Host: parsed.Host, TLSConfig: parsed.TLSConfig}}
	tries = append(tries, parsed.Fallbacks...)
	for _, try := range tries {
		if local(try.Host) {
			continue
		}
		if try.TLSConfig == nil || try.TLSConfig.InsecureSkipVerify {
			return fmt.Errorf("%w: %s must ask for sslmode=verify-full, which checks the server's certificate", ErrInvalid, name)
		}
	}
	return nil
}

// local says whether a host is this machine: the loopback, or a Unix socket.
func local(host string) bool {
	if host == "localhost" || strings.HasPrefix(host, "/") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
