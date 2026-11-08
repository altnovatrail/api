package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/server"
	"github.com/altnovatrail/api/internal/version"
)

// serve starts the service, serves until ctx is cancelled and stops it again.
// The log goes to stdout, where Cloud Run reads it.
func serve(ctx context.Context, args, environ []string, stdout, stderr io.Writer) error {
	if err := flags("serve", args, stderr); err != nil {
		return err
	}

	// The configuration is read before anything else exists, so a service that
	// is configured wrongly says so and exits instead of half-starting. Its
	// refusal names the variable and never a secret's value.
	cfg, err := config.LoadFrom(environ)
	if err != nil {
		return err
	}
	log := logging.New(stdout, cfg.LogLevel)

	// The port is taken before the service says it has started, so that a port
	// already in use is the failed start it is, and the startup line never
	// names a port the process does not hold.
	srv := server.New(cfg, log, server.Routes(log))
	if err := srv.Listen(ctx); err != nil {
		return startupFailed(ctx, log, err)
	}
	// Taking a port waits for nothing, so a stop asked for meanwhile is seen
	// here: the process says it stopped before it started, not that it started.
	if ctx.Err() != nil {
		log.Info(logging.EventShutdown, logging.FieldStarted, false)
		return nil
	}
	port, err := boundPort(srv.Addr())
	if err != nil {
		return startupFailed(ctx, log, err)
	}

	// The delivery finds the release a revision runs by this line.
	log.Info(logging.EventStartup,
		logging.FieldVersion, version.Version,
		logging.FieldCommit, version.Commit,
		logging.FieldDate, version.Date,
		logging.FieldPort, port,
		logging.FieldDeployed, cfg.Deployed(),
	)

	if err := srv.Run(ctx); err != nil {
		return logged(log, momentOf(err), err)
	}
	log.Info(logging.EventShutdown, logging.FieldStarted, true)
	return nil
}

// momentOf is the moment a failure of the running server happened at: in the
// shutdown that was asked for, or while it served.
func momentOf(err error) string {
	if errors.Is(err, server.ErrShutdown) {
		return logging.MomentShutdown
	}
	return logging.MomentServe
}

// boundPort is the port of the address the server holds.
func boundPort(addr string) (int, error) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, fmt.Errorf("serve: the bound address: %w", err)
	}
	return strconv.Atoi(port)
}

// logged writes a failure to the log at the moment it happened, by its kind
// alone, with what else the table lets the line say — the database of a failed
// migration — and marks it as written.
func logged(log *slog.Logger, moment string, err error, fields ...any) error {
	log.Error(logging.EventFailure, append([]any{logging.FieldMoment, moment, logging.FieldErrorKind, logging.ErrorKind(err)}, fields...)...)
	return fmt.Errorf("%w: %s: %w", errLogged, moment, err)
}

// startupFailed is what serve returns when the service could not start. A
// start cut short by the stop that was asked for is that stop, not a failure:
// a platform may take an instance away while it is still starting.
func startupFailed(ctx context.Context, log *slog.Logger, err error) error {
	if ctx.Err() != nil && errors.Is(err, context.Canceled) {
		log.Info(logging.EventShutdown, logging.FieldStarted, false)
		return nil
	}
	return logged(log, logging.MomentStartup, err)
}
