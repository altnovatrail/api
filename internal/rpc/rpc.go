// Package rpc is api's server of the contract novatrail.api.v1, on Connect v2
// (R17, R18, R33): the methods of the contract, its codecs, and the interceptors
// every request goes through — its line of the log, its time limit and the
// checks of architecture 3.1, which run before any handler. The handlers of the
// methods join it task by task; until then a method answers unimplemented.
package rpc

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"

	"github.com/altnovatrail/api/internal/authn"
	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/deletion"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/packages"
)

// Deps is what the server runs on.
type Deps struct {
	// Signin is the sign-in service (R32).
	Signin authn.Port
	// DB is the pool of the database api (R29).
	DB DB
	// Rights is api's rights in OpenFGA (R31).
	Rights Rights
	// Log is the service's log.
	Log *slog.Logger
	// RequestTimeout is the longest a request may run (R33).
	RequestTimeout time.Duration
	// Packages is the packages of modules serve read at its start (R07, R41),
	// which the package and module services serve from (T32).
	Packages *packages.Set
}

// Handlers are the methods of the contract that api serves. A service left
// unset answers unimplemented; the tests set the one they need.
type Handlers struct {
	Account  apiv1connect.AccountServiceHandler
	Attempt  apiv1connect.AttemptServiceHandler
	Learning apiv1connect.LearningServiceHandler
	Module   apiv1connect.ModuleServiceHandler
	Package  apiv1connect.PackageServiceHandler
	Progress apiv1connect.ProgressServiceHandler
}

// NewServer is the server of the contract, with every method registered and
// every interceptor in its order: the log's line outermost, so that it sees
// every refusal, then the time limit, then the checks.
func NewServer(d Deps, h Handlers) *connect.Server {
	gone := deletion.New(d.DB, d.Rights, d.Signin)
	c := &checks{signin: d.Signin, db: d.DB, rights: d.Rights, userGone: gone.Account, now: time.Now}
	server := connect.NewServer(logged(d.Log), limited(d.RequestTimeout), c.intercept)
	register(server, h)
	return server
}

func register(server *connect.Server, h Handlers) {
	apiv1connect.RegisterAccountServiceHandler(server, or[apiv1connect.AccountServiceHandler](h.Account, apiv1connect.UnimplementedAccountServiceHandler{}))
	apiv1connect.RegisterAttemptServiceHandler(server, or[apiv1connect.AttemptServiceHandler](h.Attempt, apiv1connect.UnimplementedAttemptServiceHandler{}))
	apiv1connect.RegisterLearningServiceHandler(server, or[apiv1connect.LearningServiceHandler](h.Learning, apiv1connect.UnimplementedLearningServiceHandler{}))
	apiv1connect.RegisterModuleServiceHandler(server, or[apiv1connect.ModuleServiceHandler](h.Module, apiv1connect.UnimplementedModuleServiceHandler{}))
	apiv1connect.RegisterPackageServiceHandler(server, or[apiv1connect.PackageServiceHandler](h.Package, apiv1connect.UnimplementedPackageServiceHandler{}))
	apiv1connect.RegisterProgressServiceHandler(server, or[apiv1connect.ProgressServiceHandler](h.Progress, apiv1connect.UnimplementedProgressServiceHandler{}))
}

// or is the handler set, or the one that answers unimplemented.
func or[H any](set, unimplemented H) H {
	if any(set) == nil {
		return unimplemented
	}
	return set
}

// Mount puts the server's methods on mux, with the contract's codecs: binary
// protobuf, and JSON for debugging, written with the protobuf names (R18;
// contracts README, sections 6 and 9). The JSON codec of Connect discards
// unknown fields already.
func Mount(mux *http.ServeMux, server *connect.Server) {
	json := connectproto.NewJSONCodec()
	json.MarshalOptions.UseProtoNames = true
	connecthttp.Mount(mux, server, connecthttp.WithCodecs(connectproto.NewBinaryCodec(), json))
}

// limited cuts a request short at its time limit; a shorter Connect-Timeout-Ms
// of the client's still holds (R33). No limit given is the default one, never
// none, and never one that has run out before the request began.
func limited(timeout time.Duration) connect.ServerInterceptor {
	if timeout <= 0 {
		timeout = config.DefaultRequestTimeout
	}
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			return next(ctx, spec, stream)
		}
	}
}

// note is what a request's line of the log is told by the code that refused it.
type note struct {
	cause   string
	class   authn.Class
	failure error
}

type noteKey struct{}

// noteOf is the request's note; outside a request, one nobody reads.
func noteOf(ctx context.Context) *note {
	if n, ok := ctx.Value(noteKey{}).(*note); ok {
		return n
	}
	return &note{}
}

// logged writes the request's line: its method, code, cause, token class, time
// and the kind of its failure — nothing of the request itself (R18, R33).
func logged(log *slog.Logger) connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			n := &note{}
			start := time.Now()
			// A handler's panic is answered by the server's recovery; its line says
			// which method it was, and it goes on up.
			defer func() {
				if fault := recover(); fault != nil {
					log.ErrorContext(ctx, logging.EventRequest, logging.FieldMethod, spec.Procedure,
						logging.FieldCode, connect.CodeInternal.String(),
						logging.FieldDurationMS, time.Since(start).Milliseconds())
					panic(fault)
				}
			}()
			err := next(context.WithValue(ctx, noteKey{}, n), spec, stream)
			code := "ok"
			if err != nil {
				code = connect.CodeOf(err).String()
			}
			attrs := []any{
				logging.FieldMethod, spec.Procedure,
				logging.FieldCode, code,
				logging.FieldDurationMS, time.Since(start).Milliseconds(),
			}
			if n.cause != "" {
				attrs = append(attrs, logging.FieldCause, n.cause)
			}
			if n.class != 0 {
				attrs = append(attrs, logging.FieldTokenClass, n.class.String())
			}
			level := slog.LevelInfo
			switch {
			case n.failure != nil:
				attrs = append(attrs, logging.FieldErrorKind, logging.ErrorKind(n.failure))
				level = slog.LevelError
			case code == connect.CodeInternal.String() || code == connect.CodeUnknown.String():
				attrs = append(attrs, logging.FieldErrorKind, logging.ErrorKind(err))
				level = slog.LevelError
			}
			log.Log(ctx, level, logging.EventRequest, attrs...)
			return err
		}
	}
}
