package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/logging"
)

// work is synthetic: what a panic might carry from a request.
const work = "the answer is 42"

// A server that stops serving with nobody asking has failed.
func TestAServerThatStopsOnItsOwnFails(t *testing.T) {
	t.Parallel()

	srv := New(&config.Config{Port: "0", ShutdownTimeout: time.Second}, slog.New(slog.DiscardHandler), http.NotFoundHandler())
	if err := srv.Listen(t.Context()); err != nil {
		t.Fatal(err)
	}
	_ = srv.listener.Close()
	if err := srv.Run(t.Context()); err == nil || !strings.Contains(err.Error(), "server: serve") {
		t.Errorf("Run on a closed listener error = %v, want a failed serve", err)
	}
}

// recoveredBy is the handler that panics with fault, after writing when begin
// is set, recovered, and the log it writes to.
func recoveredBy(fault any, begin bool) (http.Handler, *bytes.Buffer) {
	var log bytes.Buffer
	return recovered(logging.New(&log, slog.LevelInfo), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Own", "set by the handler")
		if begin {
			_, _ = w.Write([]byte("partial"))
		}
		panic(fault)
	})), &log
}

func TestAPanicIsAnsweredWithA500AndLoggedByItsKind(t *testing.T) {
	t.Parallel()

	handler, log := recoveredBy(work, false)
	answer := httptest.NewRecorder()
	handler.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, "/", nil))

	if answer.Code != http.StatusInternalServerError || answer.Body.Len() != 0 {
		t.Errorf("a panic is answered %d %q, want 500 and no body", answer.Code, answer.Body)
	}
	if got := answer.Header().Get("X-Own"); got != "" {
		t.Errorf("the handler's own header %q is in the answer", got)
	}
	var line map[string]any
	if err := json.Unmarshal(log.Bytes(), &line); err != nil {
		t.Fatalf("the log is %q: %v", log, err)
	}
	if line["message"] != logging.EventPanic || line[logging.FieldPanicKind] != "a value of type string" {
		t.Errorf("the panic is logged as %v", line)
	}
	if strings.Contains(log.String(), work) {
		t.Errorf("the log holds what the panic carried: %s", log)
	}
}

// An answer already begun cannot be taken back: the connection is cut.
func TestAPanicAfterTheAnswerBeganCutsTheConnection(t *testing.T) {
	t.Parallel()

	handler, log := recoveredBy(work, true)
	defer func() {
		if fault := recover(); fault != http.ErrAbortHandler {
			t.Errorf("the recovery panicked with %v, want http.ErrAbortHandler", fault)
		}
		if !strings.Contains(log.String(), logging.EventPanic) {
			t.Errorf("the panic is not logged: %q", log)
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

// A handler that aborts on purpose is let through, unlogged.
func TestAnAbortIsLetThrough(t *testing.T) {
	t.Parallel()

	handler, log := recoveredBy(http.ErrAbortHandler, false)
	defer func() {
		if fault := recover(); fault != http.ErrAbortHandler {
			t.Errorf("the recovery panicked with %v, want http.ErrAbortHandler", fault)
		}
		if log.Len() != 0 {
			t.Errorf("an abort is logged: %q", log)
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestTheTrackedWriterUnwraps(t *testing.T) {
	t.Parallel()

	below := httptest.NewRecorder()
	if (&tracked{ResponseWriter: below}).Unwrap() != below {
		t.Error("Unwrap does not reach the writer below")
	}
}

// A flush begins the answer, as a write does: a panic after it cuts the
// connection rather than answering a second time.
func TestAFlushBeginsTheAnswer(t *testing.T) {
	t.Parallel()

	var log bytes.Buffer
	handler := recovered(logging.New(&log, slog.LevelInfo), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("Flush error = %v", err)
		}
		panic(work)
	}))
	defer func() {
		if fault := recover(); fault != http.ErrAbortHandler {
			t.Errorf("the recovery panicked with %v, want http.ErrAbortHandler", fault)
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}
