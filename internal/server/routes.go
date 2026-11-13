package server

import (
	"log/slog"
	"net/http"
	"reflect"

	"github.com/altnovatrail/api/internal/logging"
)

// healthBody is the whole answer of the health check: that the process
// serves. It names no version, which the startup line and the version command
// give to those who need it.
const healthBody = `{"status":"ok"}`

// redirect is the type of the handler by which the mux redirects a request.
var redirect = reflect.TypeOf(http.RedirectHandler("/", http.StatusTemporaryRedirect))

// Routes is the service's handler: the health check, and whatever mount puts on
// the mux — the methods of the contract, by the Connect server (R33). A panic
// in any of them is answered here, and CORS is answered to appOrigin, when it
// is set, on every path and every answer.
//
// The router is the standard library's: a Connect handler is an http.Handler,
// and a pattern with a method is all the routes need.
func Routes(log *slog.Logger, appOrigin string, mount func(*http.ServeMux)) http.Handler {
	mux := http.NewServeMux()
	// GET /health answers HEAD too. The path does not end in z: Cloud Run's
	// front end keeps paths that do for itself (R30 canvas-api).
	mux.HandleFunc("GET /health", health)
	if mount != nil {
		mount(mux)
	}
	// Whatever else is asked for is not here. The answer has no body, so it
	// repeats nothing of the path, which may carry anything a client put in it.
	mux.HandleFunc("/", notFound)
	return cors(appOrigin, recovered(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A path the mux would redirect — to its clean form, //health or
		// /a/../b, or to the subtree with a slash at its end, as a service of
		// Connect is mounted — is not found either: the redirect would repeat
		// the path and the query in its Location and its body. No handler of
		// api answers by a redirect.
		if h, _ := mux.Handler(r); reflect.TypeOf(h) == redirect {
			notFound(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})))
}

func notFound(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(healthBody))
}

// recovered answers a panic of next with a 500 and no body, and writes it to
// the log by its kind alone: what it was raised with may hold a request's
// data. An answer already begun cannot be taken back, so its connection is
// cut instead, which is how net/http ends one without a word of its own.
func recovered(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		written := &tracked{ResponseWriter: w}
		defer func() {
			fault := recover()
			if fault == nil {
				return
			}
			if fault == http.ErrAbortHandler {
				panic(fault)
			}
			log.ErrorContext(r.Context(), logging.EventPanic, logging.FieldPanicKind, logging.PanicKind(fault))
			if written.begun {
				panic(http.ErrAbortHandler)
			}
			// Whatever the handler set for its own answer is not this one's.
			clear(w.Header())
			w.WriteHeader(http.StatusInternalServerError)
		}()
		next.ServeHTTP(written, r)
	})
}

// tracked is a ResponseWriter that knows whether its answer has begun.
type tracked struct {
	http.ResponseWriter
	begun bool
}

func (t *tracked) WriteHeader(code int) {
	t.begun = true
	t.ResponseWriter.WriteHeader(code)
}

func (t *tracked) Write(b []byte) (int, error) {
	t.begun = true
	return t.ResponseWriter.Write(b)
}

// Flush sends what is written so far, which begins the answer.
func (t *tracked) Flush() {
	t.begun = true
	_ = http.NewResponseController(t.ResponseWriter).Flush()
}

// Unwrap lets http.ResponseController reach the writer below.
func (t *tracked) Unwrap() http.ResponseWriter { return t.ResponseWriter }
