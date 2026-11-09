package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/altnovatrail/api/internal/account"
	"github.com/altnovatrail/api/internal/authn"
	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/server"
	"github.com/altnovatrail/api/internal/store"
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
	opens, err := config.ServeFrom(environ, cfg)
	if err != nil {
		return err
	}
	log := logging.New(stdout, cfg.LogLevel)

	// What the requests run on is opened first: a database that does not answer
	// is a failed start, not a failed first request.
	deps, handlers, closeDeps, err := open(ctx, opens)
	if err != nil {
		var fields []any
		if failed := (*openFailed)(nil); errors.As(err, &failed) {
			fields = failed.fields()
		}
		return startupFailed(ctx, log, err, fields...)
	}
	// Closed in the part of the way out kept for it: a request the drain cut short
	// may still hold a connection, and the pool's Close waits for it, past the
	// platform's grace if nothing bounded it.
	defer closeWithin(cfg.CloseTimeout(), closeDeps)
	deps.Log, deps.RequestTimeout = log, cfg.RequestTimeout
	routes := server.Routes(log, opens.AppOrigin, func(mux *http.ServeMux) {
		rpc.Mount(mux, rpc.NewServer(deps, handlers))
	})

	// The port is taken before the service says it has started, so that a port
	// already in use is the failed start it is, and the startup line never
	// names a port the process does not hold.
	srv := server.New(cfg, log, routes)
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
// a platform may take an instance away while it is still starting. fields are
// what else the line says: the database that did not open.
func startupFailed(ctx context.Context, log *slog.Logger, err error, fields ...any) error {
	if ctx.Err() != nil && errors.Is(err, context.Canceled) {
		log.Info(logging.EventShutdown, logging.FieldStarted, false)
		return nil
	}
	return logged(log, logging.MomentStartup, err, fields...)
}

// newSignin builds the port to the sign-in service; a test of serve puts a fake
// of its own here, for want of a Firebase project.
var newSignin = func(ctx context.Context, o authn.Options) (authn.Port, error) { return authn.New(ctx, o) }

// openFailed is what did not open, with the database it is of; none for the
// sign-in service.
type openFailed struct {
	database string
	err      error
}

func (o *openFailed) Error() string { return "serve: open " + o.database + ": " + o.err.Error() }

func (o *openFailed) Unwrap() error { return o.err }

// fields is what the line of the failure says beside its kind: the database.
func (o *openFailed) fields() []any {
	if o.database == "" {
		return nil
	}
	return []any{logging.FieldDatabase, o.database}
}

// open opens what the requests run on (R29, R31, R32): the database api through
// the pooler, checked by a ping so that a wrong string fails the start and not
// the first request; the rights, which find their store and model or refuse;
// and the port to the sign-in service, with the handlers on them. What opened
// before a failure is closed; close closes the rest.
func open(ctx context.Context, opens *config.Serve) (rpc.Deps, rpc.Handlers, func(), error) {
	pool, err := store.Open(ctx, opens.DatabaseURL.Reveal())
	if err == nil {
		if err = pool.Ping(ctx); err != nil {
			pool.Close()
		}
	}
	if err != nil {
		return rpc.Deps{}, rpc.Handlers{}, nil, &openFailed{database: logging.DatabaseAPI, err: err}
	}
	rights, err := authz.Open(ctx, opens.OpenFGADatabaseURL.Reveal())
	if err != nil {
		pool.Close()
		return rpc.Deps{}, rpc.Handlers{}, nil, &openFailed{database: logging.DatabaseOpenFGA, err: err}
	}
	signin, err := newSignin(ctx, authn.Options{
		ProjectID:   opens.FirebaseProjectID,
		Credentials: []byte(opens.FirebaseCredentials.Reveal()),
	})
	if err != nil {
		rights.Close()
		pool.Close()
		return rpc.Deps{}, rpc.Handlers{}, nil, &openFailed{err: err}
	}
	closeAll := func() {
		rights.Close()
		pool.Close()
	}
	// The handlers write the tuples of the profiles they make (R31).
	handlers := rpc.Handlers{Account: account.New(pool, rights, signin)}
	return rpc.Deps{Signin: signin, DB: pool, Rights: rights}, handlers, closeAll, nil
}

// closeWithin runs closeAll, and waits for it no longer than timeout: a process on
// its way out leaves what is still closing to the exit.
func closeWithin(timeout time.Duration, closeAll func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		closeAll()
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}
