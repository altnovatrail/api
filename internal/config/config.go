// Package config reads the service's configuration from the environment.
//
// Environment variables are the only source: there is no flag and no remote
// source, and a secret may come from a file only by a variable that names it
// (see Secret). They are read here and nowhere else, so that every value has
// one origin and one validation.
//
// Only the variables the service actually uses are here. A variable arrives
// with the code that consumes it, because a required variable with no reader
// is a deployment that fails for no reason.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// The names of the variables. The service's own carry the prefix API_; PORT
// and K_SERVICE are the names Cloud Run sets.
const (
	VarPort              = "PORT"
	VarLogLevel          = "API_LOG_LEVEL"
	VarReadHeaderTimeout = "API_HTTP_READ_HEADER_TIMEOUT"
	VarReadTimeout       = "API_HTTP_READ_TIMEOUT"
	VarWriteTimeout      = "API_HTTP_WRITE_TIMEOUT"
	VarIdleTimeout       = "API_HTTP_IDLE_TIMEOUT"
	VarShutdownTimeout   = "API_SHUTDOWN_TIMEOUT"
	VarRequestTimeout    = "API_REQUEST_TIMEOUT"
	VarService           = "K_SERVICE"

	// VarMigrateDatabaseURL is the direct connection string of the database api,
	// which migrate takes: a secret, read from the variable or from the file its
	// twin with the suffix _FILE names (R29).
	VarMigrateDatabaseURL = "API_MIGRATE_DATABASE_URL"
	// VarMigrateOpenFGADatabaseURL is the direct connection string of the database
	// openfga, the store of the rights, which migrate takes the same way (R31).
	VarMigrateOpenFGADatabaseURL = "API_MIGRATE_OPENFGA_DATABASE_URL"
)

// declared is every variable the service reads. Nothing else the process
// carries — credentials of other systems, tokens, the shell's own variables —
// is read, so a future dump of what was read can carry none of them.
var declared = map[string]struct{}{
	VarPort:              {},
	VarLogLevel:          {},
	VarReadHeaderTimeout: {},
	VarReadTimeout:       {},
	VarWriteTimeout:      {},
	VarIdleTimeout:       {},
	VarShutdownTimeout:   {},
	VarRequestTimeout:    {},
	VarService:           {},

	VarMigrateDatabaseURL:                     {},
	VarMigrateDatabaseURL + fileSuffix:        {},
	VarMigrateOpenFGADatabaseURL:              {},
	VarMigrateOpenFGADatabaseURL + fileSuffix: {},
}

// Defaults for every optional variable. They are named constants rather than
// literals in LoadFrom so that the documentation and the code cannot drift.
const (
	DefaultPort              = "8080"
	DefaultLogLevel          = "info"
	DefaultReadHeaderTimeout = 5 * time.Second
	DefaultReadTimeout       = 30 * time.Second
	DefaultWriteTimeout      = 60 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
	DefaultShutdownTimeout   = 9 * time.Second
	DefaultRequestTimeout    = 30 * time.Second
)

// MinShutdownTimeout is the shortest way out a service may be given: the close
// after the drain has a quarter of it, and a quarter of less is no time to
// close anything in.
const MinShutdownTimeout = time.Second

// CloudRunStopGrace is how long Cloud Run waits after it asks an instance to
// stop before it kills it. A deployed way out has to end before then, or the
// close it keeps time for never runs.
const CloudRunStopGrace = 10 * time.Second

// ErrInvalid is returned by LoadFrom; callers branch on it with errors.Is.
var ErrInvalid = errors.New("config: invalid")

// Config is the whole configuration of the process.
type Config struct {
	// Port is the TCP port to listen on, as plain digits.
	Port string
	// LogLevel is the lowest level the log writes.
	LogLevel slog.Level

	// ReadHeaderTimeout is how long a client may take to send its headers.
	ReadHeaderTimeout time.Duration
	// ReadTimeout is how long a client may take to send its whole request.
	ReadTimeout time.Duration
	// WriteTimeout is how long the server may take to send its answer.
	WriteTimeout time.Duration
	// IdleTimeout is how long a connection is kept open between requests.
	IdleTimeout time.Duration
	// ShutdownTimeout is the whole way out once a stop is asked for: the
	// server's drain and the close after it together. The platform stops the
	// process ten seconds after asking it to, so the default stops a second
	// short of it.
	ShutdownTimeout time.Duration
	// RequestTimeout is the longest a request may run, its calls of the ports
	// among it; a client's Connect-Timeout-Ms may shorten it (R33). It is under
	// WriteTimeout, so that the answer of a request cut short still goes out.
	RequestTimeout time.Duration

	// Service is the name K_SERVICE carries. Cloud Run sets it on every
	// service and a developer's machine does not, which is what makes it the
	// gate for what a deployment must not be given.
	Service string
}

// Deployed reports whether the process is running as a deployed service.
func (c *Config) Deployed() bool { return c.Service != "" }

// DrainTimeout is the part of the way out the server waits for the requests
// still in flight: three quarters of it. The drain and the close after it
// share one budget in fixed shares, so that a slow request cannot eat the time
// the close needs.
func (c *Config) DrainTimeout() time.Duration { return c.ShutdownTimeout - c.CloseTimeout() }

// CloseTimeout is the part of the way out kept for closing what the service ran
// on: a quarter of it.
func (c *Config) CloseTimeout() time.Duration { return c.ShutdownTimeout / 4 }

// LoadFrom reads the configuration from a set of KEY=VALUE strings, in the
// shape os.Environ returns them, applies the defaults and validates the
// result. The source is an argument — the command hands over os.Environ — so
// that a test can hand over an environment instead of changing the one the
// process runs in.
//
// A variable set to an empty string counts as unset: an empty value is what a
// deployment leaves behind when it stops setting something, and it should not
// silently override a default with nothing.
func LoadFrom(environ []string) (*Config, error) {
	env := read(environ)

	cfg := &Config{
		Port:    env.or(VarPort, DefaultPort),
		Service: env[VarService],
	}
	var problems []error
	var err error
	if cfg.LogLevel, err = level(env.or(VarLogLevel, DefaultLogLevel)); err != nil {
		problems = append(problems, err)
	}
	for _, timeout := range []struct {
		name  string
		into  *time.Duration
		value time.Duration
	}{
		{VarReadHeaderTimeout, &cfg.ReadHeaderTimeout, DefaultReadHeaderTimeout},
		{VarReadTimeout, &cfg.ReadTimeout, DefaultReadTimeout},
		{VarWriteTimeout, &cfg.WriteTimeout, DefaultWriteTimeout},
		{VarIdleTimeout, &cfg.IdleTimeout, DefaultIdleTimeout},
		{VarShutdownTimeout, &cfg.ShutdownTimeout, DefaultShutdownTimeout},
		{VarRequestTimeout, &cfg.RequestTimeout, DefaultRequestTimeout},
	} {
		if *timeout.into, err = duration(env, timeout.name, timeout.value); err != nil {
			problems = append(problems, err)
		}
	}
	// Every variable that could not be read is named, not only the first, so
	// that one failed start shows everything a deployment has to fix.
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// environment is the declared variables that are set, by name.
type environment map[string]string

// read keeps the declared variables of environ that are set. A name is matched
// as it is written: the environment tells PORT from port, and so does this.
func read(environ []string) environment {
	env := environment{}
	for _, entry := range environ {
		name, value, found := strings.Cut(entry, "=")
		if !found || value == "" {
			continue
		}
		if _, ours := declared[name]; ours {
			env[name] = value
		}
	}
	return env
}

// or is the variable's value, or fallback when it is unset.
func (e environment) or(name, fallback string) string {
	if value, set := e[name]; set {
		return value
	}
	return fallback
}

// level reads a level by its name in slog's words.
func level(name string) (slog.Level, error) {
	switch name {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("%w: %s must be debug, info, warn or error, got %q", ErrInvalid, VarLogLevel, name)
}

// duration reads a variable written as Go writes a duration, "5s" or "1m30s".
func duration(env environment, name string, fallback time.Duration) (time.Duration, error) {
	value, set := env[name]
	if !set {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%w: %s must be a duration such as 5s, got %q", ErrInvalid, name, value)
	}
	return parsed, nil
}

// validate reports the first problem it finds, naming the variable behind it.
func (c *Config) validate() error {
	for _, check := range []func() error{c.validatePort, c.validateTimeouts} {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

// validatePort refuses a port the server could not listen on, and one written
// in any other way than plain digits: a sign or a leading zero would be read
// as the same port and logged as something else. Port 0 asks the kernel for a
// free port, which the startup line names; that is for this machine, where tests
// run beside each other, and a deployment, where the platform names the port, is
// refused it.
func (c *Config) validatePort() error {
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 0 || port > 65535 || strconv.Itoa(port) != c.Port {
		return fmt.Errorf("%w: %s must be a number from 1 to 65535, got %q", ErrInvalid, VarPort, c.Port)
	}
	if port == 0 && c.Deployed() {
		return fmt.Errorf("%w: %s must be a port from 1 to 65535 when %s is set, got 0", ErrInvalid, VarPort, VarService)
	}
	return nil
}

// validateTimeouts refuses a duration nothing could run under, naming its
// variable: every one has to be positive, and the way out long enough to be
// shared and, deployed, short enough to end before the platform's grace does.
func (c *Config) validateTimeouts() error {
	for _, timeout := range []struct {
		name  string
		value time.Duration
	}{
		{VarReadHeaderTimeout, c.ReadHeaderTimeout},
		{VarReadTimeout, c.ReadTimeout},
		{VarWriteTimeout, c.WriteTimeout},
		{VarIdleTimeout, c.IdleTimeout},
		{VarShutdownTimeout, c.ShutdownTimeout},
		{VarRequestTimeout, c.RequestTimeout},
	} {
		if timeout.value <= 0 {
			return fmt.Errorf("%w: %s must be a positive duration, got %v", ErrInvalid, timeout.name, timeout.value)
		}
	}
	if c.RequestTimeout >= c.WriteTimeout {
		return fmt.Errorf("%w: %s must be under %s, %v, got %v",
			ErrInvalid, VarRequestTimeout, VarWriteTimeout, c.WriteTimeout, c.RequestTimeout)
	}
	if c.ShutdownTimeout < MinShutdownTimeout {
		return fmt.Errorf("%w: %s must be at least %v, got %v",
			ErrInvalid, VarShutdownTimeout, MinShutdownTimeout, c.ShutdownTimeout)
	}
	if c.Deployed() && c.ShutdownTimeout >= CloudRunStopGrace {
		return fmt.Errorf("%w: %s must be under %v when %s is set, got %v",
			ErrInvalid, VarShutdownTimeout, CloudRunStopGrace, VarService, c.ShutdownTimeout)
	}
	return nil
}
