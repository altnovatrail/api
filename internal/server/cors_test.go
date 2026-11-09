package server_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/altnovatrail/api/internal/server"
)

const appOrigin = "https://app.example"

// routesOf is the routes with app's origin and a few paths of the tests' own: one
// that fails, one that panics and one that flushes.
func routesOf(origin string) http.Handler {
	return server.Routes(slog.New(slog.DiscardHandler), origin, func(mux *http.ServeMux) {
		mux.HandleFunc("POST /refused", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		})
		mux.HandleFunc("POST /panics", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Own", "lost")
			panic("a handler's fault")
		})
		mux.HandleFunc("GET /flushes", func(w http.ResponseWriter, _ *http.Request) {
			http.NewResponseController(w).Flush()
		})
	})
}

func ask(t *testing.T, h http.Handler, method, path, origin string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), method, path, nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	for i := 0; i+1 < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// app's origin gets CORS on every answer: a refusal, a 404, a panic's 500 whose
// headers were cleared, the health check and an answer begun by a flush.
func TestCORSIsOnEveryAnswerToApp(t *testing.T) {
	t.Parallel()

	h := routesOf(appOrigin)
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodPost, "/refused", http.StatusUnauthorized},
		{http.MethodGet, "/nowhere", http.StatusNotFound},
		{http.MethodPost, "/panics", http.StatusInternalServerError},
		{http.MethodGet, "/health", http.StatusOK},
		{http.MethodGet, "/flushes", http.StatusOK},
	} {
		w := ask(t, h, tc.method, tc.path, appOrigin)
		if w.Code != tc.status {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, w.Code, tc.status)
		}
		if w.Header().Get("Access-Control-Allow-Origin") != appOrigin ||
			w.Header().Get("Access-Control-Expose-Headers") != "Retry-After" ||
			w.Header().Get("Vary") != "Origin" {
			t.Errorf("%s %s: headers %v, want CORS to app", tc.method, tc.path, w.Header())
		}
		if w.Header().Get("X-Own") != "" {
			t.Errorf("%s %s kept the handler's own header on the answer of its panic", tc.method, tc.path)
		}
	}
}

func TestAPreflightOfAppIsAnswered(t *testing.T) {
	t.Parallel()

	w := ask(t, routesOf(appOrigin), http.MethodOptions, "/novatrail.api.v1.AccountService/GetAccount", appOrigin,
		"Access-Control-Request-Method", "POST", "Access-Control-Request-Headers", "authorization, content-type")
	want := map[string]string{
		"Access-Control-Allow-Origin":  appOrigin,
		"Access-Control-Allow-Methods": "GET, POST",
		"Access-Control-Allow-Headers": "Authorization, Content-Type, Connect-Protocol-Version, Connect-Timeout-Ms",
		"Access-Control-Max-Age":       "7200",
	}
	if w.Code != http.StatusNoContent {
		t.Errorf("preflight = %d, want 204", w.Code)
	}
	for name, value := range want {
		if got := w.Header().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
}

// Another origin, and a request with none, get no CORS; and api on app's own
// origin answers none to anyone.
func TestNoCORSForAnyoneElse(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		h      http.Handler
		origin string
	}{
		"another origin":          {routesOf(appOrigin), "https://other.example"},
		"no origin":               {routesOf(appOrigin), ""},
		"api on app's own origin": {routesOf(""), appOrigin},
	} {
		for _, method := range []string{http.MethodGet, http.MethodOptions} {
			w := ask(t, tc.h, method, "/health", tc.origin, "Access-Control-Request-Method", "POST")
			if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Errorf("%s, %s: Access-Control-Allow-Origin = %q, want none", name, method, got)
			}
		}
	}
}
