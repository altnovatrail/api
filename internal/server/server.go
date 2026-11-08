// Package server is the HTTP server of the service and its graceful stop.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/logging"
)

// ErrShutdown marks a stop that failed other than by running out of time:
// what Run returns then is a failure of the shutdown, not of serving.
var ErrShutdown = errors.New("server: graceful shutdown")

// Server is the HTTP server and its graceful stop.
type Server struct {
	http *http.Server
	log  *slog.Logger
	// drainTimeout is how long a stop waits for the requests in flight: the
	// server's share of the way out, not the whole of it.
	drainTimeout time.Duration

	// mu guards listener: Run binds it on one goroutine while Addr may be
	// read from another, which is how a test finds out which port the kernel
	// picked.
	mu       sync.RWMutex
	listener net.Listener
}

// New builds the server of cfg, serving handler and writing to log.
func New(cfg *config.Config, log *slog.Logger, handler http.Handler) *Server {
	return &Server{
		log:          log,
		drainTimeout: cfg.DrainTimeout(),
		http: &http.Server{
			// Empty host, so the server listens on every interface. The
			// configuration guarantees a bare port number, and JoinHostPort is
			// what keeps the address well formed if that ever stops being true.
			Addr:              net.JoinHostPort("", cfg.Port),
			Handler:           handler,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
			// The server's own complaints — a bad request line, a TLS
			// handshake — name the client's address, so they go through the
			// log's guard, which drops them as the events it does not know.
			ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn),
		},
	}
}

// Listen binds the port without serving on it yet. Run binds it itself when
// nothing did, but the process calls Listen first: binding early is how a port
// that is already taken is reported as a failed start, before the service says
// it has started, rather than swallowed by the goroutine inside Run.
func (s *Server) Listen(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		return nil
	}
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", s.http.Addr)
	if err != nil {
		return fmt.Errorf("server: listen on %s: %w", s.http.Addr, err)
	}
	s.listener = listener
	return nil
}

// Addr is the address the server is bound to, empty before Listen. It is safe
// to call while the server is starting or serving.
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Run serves until ctx is cancelled, then stops gracefully. The stop is driven
// by the context rather than by a signal handler of its own, so that every
// part of the process stops for the same reason at the same moment.
//
// A request still being handled when the drain's share of the way out is spent
// is cut off rather than waited for, and that is still the stop that was asked
// for: the line saying so is a warning, and Run returns no error. A failure is
// what nobody asked for — a server that stopped serving on its own.
func (s *Server) Run(ctx context.Context) error {
	if err := s.Listen(ctx); err != nil {
		return err
	}

	s.mu.RLock()
	listener := s.listener
	s.mu.RUnlock()

	served := make(chan error, 1)
	go func() { served <- s.http.Serve(listener) }()

	select {
	case err := <-served:
		// Nothing had asked it to stop, so whatever it returned is a failure.
		return fmt.Errorf("server: serve: %w", err)
	case <-ctx.Done():
		s.log.Info(logging.EventStopping)
	}

	// A fresh context, so the drain's deadline starts now rather than at the
	// moment the signal arrived and cancelled the parent.
	drain, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.drainTimeout)
	defer cancel()

	drained := s.http.Shutdown(drain)
	if drained != nil {
		_ = s.http.Close() // the deadline passed: drop what is left rather than hang
	}
	// Shutdown and Close both make Serve return, so this wait is short.
	<-served

	switch {
	case errors.Is(drained, context.DeadlineExceeded):
		s.log.Warn(logging.EventStopped, logging.FieldCutShort, true)
		return nil
	case drained != nil:
		return fmt.Errorf("%w: %w", ErrShutdown, drained)
	}
	s.log.Info(logging.EventStopped, logging.FieldCutShort, false)
	return nil
}
