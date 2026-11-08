package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/server"
)

// syncBuffer is a stream the command and the test may share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// lines decodes the log written to b, one JSON line at a time.
func (b *syncBuffer) lines(t *testing.T) []map[string]any {
	t.Helper()

	var lines []map[string]any
	for line := range strings.Lines(b.String()) {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("a line of the log is not JSON: %q", line)
		}
		lines = append(lines, decoded)
	}
	return lines
}

// runCommand runs the binary's command line with environ, and returns its
// exit code and streams.
func runCommand(ctx context.Context, environ []string, args ...string) (int, string, string) {
	var stdout, stderr syncBuffer
	code := run(ctx, args, environ, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// freePort is a port nothing listens on, as far as the kernel knows now.
func freePort(t *testing.T) string {
	t.Helper()

	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	return port
}

func TestTheCommandLine(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		args   []string
		code   int
		stdout string
		stderr string
	}{
		{"no command", nil, exitUsage, "", "Usage: api <command>"},
		{"help", []string{"help"}, exitOK, "Usage: api <command>", ""},
		{"-h", []string{"-h"}, exitOK, "Usage: api <command>", ""},
		{"unknown command", []string{"seed"}, exitUsage, "", `unknown command "seed"`},
		{"version", []string{"version"}, exitOK, "api dev unknown unknown " + runtime.Version() + "\n", ""},
		{"version with an argument", []string{"version", "now"}, exitUsage, "", "takes no arguments"},
		{"version with an unknown flag", []string{"version", "-json"}, exitUsage, "", "flag provided but not defined"},
		{"version -h", []string{"version", "-h"}, exitOK, "", "Usage of api version"},
		{"serve -h", []string{"serve", "-h"}, exitOK, "", "Usage of api serve"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			code, stdout, stderr := runCommand(t.Context(), nil, tc.args...)
			if code != tc.code {
				t.Errorf("exit code = %d, want %d", code, tc.code)
			}
			if !strings.Contains(stdout, tc.stdout) || (tc.stdout == "" && stdout != "") {
				t.Errorf("stdout = %q, want %q", stdout, tc.stdout)
			}
			if !strings.Contains(stderr, tc.stderr) || (tc.stderr == "" && stderr != "") {
				t.Errorf("stderr = %q, want %q", stderr, tc.stderr)
			}
		})
	}
}

// A service configured wrongly says so, naming the variable, and starts
// nothing.
func TestAWrongConfigurationIsRefusedBeforeTheLog(t *testing.T) {
	t.Parallel()

	code, stdout, stderr := runCommand(t.Context(), []string{"PORT=http"}, "serve")
	if code != exitFailure {
		t.Errorf("exit code = %d, want %d", code, exitFailure)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing: there is no log yet", stdout)
	}
	if !strings.HasPrefix(stderr, "api: config: invalid: PORT") {
		t.Errorf("stderr = %q, want the refusal naming PORT", stderr)
	}
}

// The service starts, serves, stops when asked, and every line it writes on
// the way is a line of the table: none of them loses a field.
func TestServeStartsServesAndStops(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	ctx, stop := context.WithCancel(t.Context())
	var stdout, stderr syncBuffer
	exited := make(chan int, 1)
	go func() { exited <- run(ctx, []string{"serve"}, []string{"PORT=" + port}, &stdout, &stderr) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		answer, err := http.Get("http://127.0.0.1:" + port + "/health")
		if err == nil {
			_ = answer.Body.Close()
			if answer.StatusCode != http.StatusOK {
				t.Fatalf("GET /health = %d, want 200", answer.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the service did not answer: %v; log %s", err, stdout.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	stop()
	if code := <-exited; code != exitOK {
		t.Fatalf("exit code = %d, want 0; stderr %q", code, stderr.String())
	}
	if stderr.String() != "" {
		t.Errorf("stderr = %q, want nothing", stderr.String())
	}

	lines := stdout.lines(t)
	var events []any
	for _, line := range lines {
		events = append(events, line["message"])
		if _, lost := line["dropped_fields"]; lost {
			t.Errorf("a line of the service lost fields to the guard: %v", line)
		}
	}
	if got, want := events, []any{"startup", "stopping", "stopped", "shutdown"}; !equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
	startup := lines[0]
	if startup["version"] != "dev" || startup["port"] != number(t, port) ||
		startup["deployed"] != false {
		t.Errorf("startup = %v, want version dev, port %s, deployed false", startup, port)
	}
	if last := lines[len(lines)-1]; last["started"] != true {
		t.Errorf("shutdown = %v, want started true", last)
	}
}

// A taken port is a failed start, logged once, by its kind.
func TestATakenPortIsAFailedStartLoggedOnce(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig
	taken, err := lc.Listen(t.Context(), "tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	_, port, _ := net.SplitHostPort(taken.Addr().String())

	var stdout, stderr syncBuffer
	code := run(t.Context(), []string{"serve"}, []string{"PORT=" + port}, &stdout, &stderr)
	if code != exitFailure {
		t.Errorf("exit code = %d, want %d", code, exitFailure)
	}
	if stderr.String() != "" {
		t.Errorf("stderr = %q, want nothing: the log has the failure", stderr.String())
	}
	lines := stdout.lines(t)
	if len(lines) != 1 {
		t.Fatalf("the log is %v, want one line", lines)
	}
	failure := lines[0]
	if failure["message"] != "failure" || failure["moment"] != "startup" || failure["severity"] != "ERROR" ||
		!strings.HasSuffix(failure["error_kind"].(string), "errno: address already in use") {
		t.Errorf("the failure is logged as %v", failure)
	}
}

// A stop asked for before the service has started is that stop: the process
// says it stopped before it started, and exits as asked.
func TestAStopBeforeTheStartIsNoFailure(t *testing.T) {
	t.Parallel()

	ctx, stop := context.WithCancel(t.Context())
	stop()
	var stdout, stderr syncBuffer
	if code := run(ctx, []string{"serve"}, []string{"PORT=" + freePort(t)}, &stdout, &stderr); code != exitOK {
		t.Errorf("exit code = %d, want 0; log %s", code, stdout.String())
	}
	lines := stdout.lines(t)
	if len(lines) != 1 || lines[0]["message"] != "shutdown" || lines[0]["started"] != false {
		t.Errorf("the log is %v, want one shutdown line with started false", lines)
	}
}

// A start cut short by the stop that was asked for is that stop, not a failure;
// any other failed start is logged, once.
func TestAStartCutShortByTheStopIsNoFailure(t *testing.T) {
	t.Parallel()

	stopped, stop := context.WithCancel(t.Context())
	stop()
	var log syncBuffer
	logger := logging.New(&log, slog.LevelInfo)
	if err := startupFailed(stopped, logger, fmt.Errorf("connect: %w", context.Canceled)); err != nil {
		t.Errorf("startupFailed after the stop = %v, want nil", err)
	}
	if err := startupFailed(t.Context(), logger, context.Canceled); !errors.Is(err, errLogged) {
		t.Errorf("startupFailed with no stop asked for = %v, want a logged failure", err)
	}
	lines := log.lines(t)
	if len(lines) != 2 || lines[0]["message"] != "shutdown" || lines[0]["started"] != false ||
		lines[1]["message"] != "failure" || lines[1]["moment"] != "startup" {
		t.Errorf("the log is %v, want shutdown with started false, then a failure at startup", lines)
	}
}

func TestABoundAddressWithoutAPortIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := boundPort("no port"); err == nil {
		t.Error("boundPort of an address without a port: error = nil")
	}
}

// One signal asks for a stop: the context ends. The test sends it to its own
// process, which the context's handler catches.
func TestASignalEndsTheContext(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		ctx, stop := signalled()
		if err := syscall.Kill(os.Getpid(), sig); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			t.Errorf("%v did not end the context", sig)
		}
		stop()
	}
}

func equal(got, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// number is the port as encoding/json decodes a number into an any.
func number(t *testing.T, port string) float64 {
	t.Helper()

	var decoded float64
	if err := json.Unmarshal([]byte(port), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestAFailureIsPlacedAtItsMoment(t *testing.T) {
	t.Parallel()

	if got := momentOf(fmt.Errorf("%w: %w", server.ErrShutdown, net.ErrClosed)); got != logging.MomentShutdown {
		t.Errorf("a failed shutdown is at %q, want %q", got, logging.MomentShutdown)
	}
	if got := momentOf(net.ErrClosed); got != logging.MomentServe {
		t.Errorf("a server that stopped serving is at %q, want %q", got, logging.MomentServe)
	}
}
