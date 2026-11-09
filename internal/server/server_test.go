package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/server"
)

// syncBuffer is a log the server's goroutines and the test may share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// events is the event of every line written so far, and the line itself.
func (b *syncBuffer) events(t *testing.T) []map[string]any {
	t.Helper()

	b.mu.Lock()
	defer b.mu.Unlock()
	var lines []map[string]any
	for line := range strings.Lines(b.buf.String()) {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("a line is not JSON: %q", line)
		}
		lines = append(lines, decoded)
	}
	return lines
}

// testConfig is a configuration on a port the kernel picks.
func testConfig(shutdown time.Duration) *config.Config {
	return &config.Config{
		Port:              "0",
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
		IdleTimeout:       time.Second,
		ShutdownTimeout:   shutdown,
	}
}

func TestTheHealthCheckAnswers(t *testing.T) {
	t.Parallel()

	routes := server.Routes(slog.New(slog.DiscardHandler), "", nil)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		answer := httptest.NewRecorder()
		routes.ServeHTTP(answer, httptest.NewRequest(method, "/health", nil))

		if answer.Code != http.StatusOK {
			t.Errorf("%s /health = %d, want 200", method, answer.Code)
		}
		if got := answer.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s /health Cache-Control = %q, want no-store", method, got)
		}
		if got := answer.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("%s /health Content-Type = %q, want application/json", method, got)
		}
	}

	answer := httptest.NewRecorder()
	routes.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, "/health", nil))
	if got := answer.Body.String(); got != `{"status":"ok"}` {
		t.Errorf("GET /health body = %q, want {\"status\":\"ok\"}", got)
	}
}

// Whatever is not a route is not found, and the answer says nothing of what
// was asked for.
func TestAPathThatIsNotARouteIsNotFound(t *testing.T) {
	t.Parallel()

	routes := server.Routes(slog.New(slog.DiscardHandler), "", nil)
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/profiles/learner@example.com", nil),
		httptest.NewRequest(http.MethodPost, "/health", nil),
		httptest.NewRequest(http.MethodGet, "/healthz", nil),
		httptest.NewRequest(http.MethodGet, "//health", nil),
		httptest.NewRequest(http.MethodGet, "/learner/../health", nil),
	} {
		answer := httptest.NewRecorder()
		routes.ServeHTTP(answer, request)
		if answer.Code != http.StatusNotFound || answer.Body.Len() != 0 {
			t.Errorf("%s %s = %d %q, want 404 and no body", request.Method, request.URL.Path, answer.Code, answer.Body)
		}
		if location := answer.Header().Get("Location"); location != "" {
			t.Errorf("%s %s is redirected to %q, which repeats the path", request.Method, request.URL.Path, location)
		}
	}
}

// The server serves until it is asked to stop, and says it has stopped.
func TestTheServerServesAndStops(t *testing.T) {
	t.Parallel()

	var log syncBuffer
	logger := logging.New(&log, slog.LevelInfo)
	srv := server.New(testConfig(time.Second), logger, server.Routes(logger, "", nil))
	if srv.Addr() != "" {
		t.Fatalf("Addr() = %q before Listen, want empty", srv.Addr())
	}
	if err := srv.Listen(t.Context()); err != nil {
		t.Fatalf("Listen error = %v", err)
	}
	if err := srv.Listen(t.Context()); err != nil {
		t.Fatalf("a second Listen error = %v, want nil", err)
	}

	ctx, stop := context.WithCancel(t.Context())
	ran := make(chan error, 1)
	go func() { ran <- srv.Run(ctx) }()

	answer, err := http.Get("http://" + srv.Addr() + "/health")
	if err != nil {
		t.Fatalf("GET /health error = %v", err)
	}
	_ = answer.Body.Close()
	if answer.StatusCode != http.StatusOK {
		t.Errorf("GET /health = %d, want 200", answer.StatusCode)
	}

	stop()
	if err := <-ran; err != nil {
		t.Fatalf("Run error = %v after a stop, want nil", err)
	}
	lines := log.events(t)
	if len(lines) != 2 || lines[0]["message"] != logging.EventStopping ||
		lines[1]["message"] != logging.EventStopped || lines[1][logging.FieldCutShort] != false {
		t.Errorf("the log is %v, want stopping, then stopped with cut_short false", lines)
	}
}

// A request that outlasts the drain is cut off, and that is still the stop
// that was asked for.
func TestADrainThatRunsOutIsCutShort(t *testing.T) {
	t.Parallel()

	var log syncBuffer
	logger := logging.New(&log, slog.LevelInfo)
	inside, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	slow := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(inside)
		<-release
	})
	srv := server.New(testConfig(40*time.Millisecond), logger, slow)
	if err := srv.Listen(t.Context()); err != nil {
		t.Fatalf("Listen error = %v", err)
	}

	ctx, stop := context.WithCancel(t.Context())
	ran := make(chan error, 1)
	go func() { ran <- srv.Run(ctx) }()
	go func() {
		if answer, err := http.Get("http://" + srv.Addr() + "/"); err == nil {
			_ = answer.Body.Close()
		}
	}()
	<-inside

	stop()
	if err := <-ran; err != nil {
		t.Fatalf("Run error = %v after a cut-short stop, want nil", err)
	}
	lines := log.events(t)
	last := lines[len(lines)-1]
	if last["message"] != logging.EventStopped || last[logging.FieldCutShort] != true || last["severity"] != "WARNING" {
		t.Errorf("the last line is %v, want a warning that the stop was cut short", last)
	}
}

func TestATakenPortFailsTheListen(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig
	taken, err := lc.Listen(t.Context(), "tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	_, port, _ := net.SplitHostPort(taken.Addr().String())

	cfg := testConfig(time.Second)
	cfg.Port = port
	srv := server.New(cfg, slog.New(slog.DiscardHandler), http.NotFoundHandler())
	if err := srv.Run(t.Context()); err == nil || !strings.Contains(err.Error(), "server: listen") {
		t.Errorf("Run on a taken port error = %v, want a failed listen", err)
	}
}
