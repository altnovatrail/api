package server

import (
	"net/http"
)

// The answers of CORS to app's origin (R18, R33; contracts README, section 15):
// the methods Connect and the files of a package take, the headers a request of
// app carries — Authorization by its name, which * does not cover — and how
// long a browser may keep a preflight.
const (
	corsMethods = "GET, POST"
	corsHeaders = "Authorization, Content-Type, Connect-Protocol-Version, Connect-Timeout-Ms"
	corsExposed = "Retry-After"
	corsMaxAge  = "7200"
)

// cors answers CORS to origin, the exact origin of app, on every path and every
// answer — an error, a 404, a panic's 500 among them. The headers are put on the
// answer as it begins, so that what clears the handler's own headers, as the
// answer of a panic does, leaves them. An empty origin is api on app's own
// origin, which needs no CORS.
func cors(origin string, next http.Handler) http.Handler {
	if origin == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		if r.Header.Get("Origin") != origin {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h := w.Header()
			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Methods", corsMethods)
			h.Set("Access-Control-Allow-Headers", corsHeaders)
			h.Set("Access-Control-Max-Age", corsMaxAge)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(&allowed{ResponseWriter: w, origin: origin}, r)
	})
}

// allowed is a ResponseWriter that puts CORS's headers on its answer as the
// answer begins.
type allowed struct {
	http.ResponseWriter
	origin string
	begun  bool
}

func (a *allowed) begin() {
	if a.begun {
		return
	}
	a.begun = true
	h := a.ResponseWriter.Header()
	h.Set("Access-Control-Allow-Origin", a.origin)
	h.Set("Access-Control-Expose-Headers", corsExposed)
	if len(h.Values("Vary")) == 0 {
		h.Add("Vary", "Origin")
	}
}

func (a *allowed) WriteHeader(code int) {
	a.begin()
	a.ResponseWriter.WriteHeader(code)
}

func (a *allowed) Write(b []byte) (int, error) {
	a.begin()
	return a.ResponseWriter.Write(b)
}

// Flush begins the answer, as tracked's does.
func (a *allowed) Flush() {
	a.begin()
	_ = http.NewResponseController(a.ResponseWriter).Flush()
}

// Unwrap lets http.ResponseController reach the writer below.
func (a *allowed) Unwrap() http.ResponseWriter { return a.ResponseWriter }
