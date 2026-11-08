package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/altnovatrail/api/internal/logging"
)

// The values below are synthetic: no test holds a real identifier.
const (
	email   = "learner@example.com"
	uid     = "Xq3pVt9sYb2LmN8rKd4wZc6hJf1a"
	address = "203.0.113.7"
	work    = "the answer is 42"
)

// written logs through New at debug and returns the raw output and its lines,
// each decoded.
func written(t *testing.T, write func(*slog.Logger)) (string, []map[string]any) {
	t.Helper()

	var out bytes.Buffer
	write(logging.New(&out, slog.LevelDebug))
	var lines []map[string]any
	for line := range strings.Lines(out.String()) {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("a line is not JSON: %q: %v", line, err)
		}
		lines = append(lines, decoded)
	}
	return out.String(), lines
}

// only is the one line written, or a failure.
func only(t *testing.T, lines []map[string]any) map[string]any {
	t.Helper()

	if len(lines) != 1 {
		t.Fatalf("%d lines written, want 1: %v", len(lines), lines)
	}
	return lines[0]
}

func TestALineUsesTheKeysOfCloudLogging(t *testing.T) {
	t.Parallel()

	_, lines := written(t, func(log *slog.Logger) {
		log.Info(logging.EventStartup,
			logging.FieldVersion, "v0.1.0", logging.FieldCommit, "abc1234",
			logging.FieldDate, "2026-11-08T00:00:00Z", logging.FieldPort, 8080, logging.FieldDeployed, false)
	})
	line := only(t, lines)
	want := map[string]any{
		"severity": "INFO", "message": "startup", "version": "v0.1.0", "commit": "abc1234",
		"date": "2026-11-08T00:00:00Z", "port": float64(8080), "deployed": false,
	}
	for key, value := range want {
		if line[key] != value {
			t.Errorf("%s = %v, want %v", key, line[key], value)
		}
	}
	if _, timed := line["time"]; !timed {
		t.Error("the line has no time")
	}
	if len(line) != len(want)+1 {
		t.Errorf("the line is %v, want only %v and the time", line, want)
	}
}

func TestSeverityNames(t *testing.T) {
	t.Parallel()

	for level, want := range map[slog.Level]string{
		slog.LevelDebug: "DEBUG", slog.LevelDebug + 1: "DEBUG",
		slog.LevelInfo: "INFO", slog.LevelInfo + 2: "INFO",
		slog.LevelWarn: "WARNING", slog.LevelError: "ERROR", slog.LevelError + 4: "ERROR",
	} {
		_, lines := written(t, func(log *slog.Logger) { log.Log(t.Context(), level, logging.EventStopping) })
		if got := only(t, lines)["severity"]; got != want {
			t.Errorf("level %v is written as %v, want %s", level, got, want)
		}
	}
}

func TestALineUnderTheLevelIsNotWritten(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	log := logging.New(&out, slog.LevelInfo)
	log.Debug(logging.EventStopping)
	if out.Len() != 0 {
		t.Errorf("a debug line at level info wrote %q", out.String())
	}
	if log.Enabled(t.Context(), slog.LevelDebug) {
		t.Error("Enabled(debug) = true at level info, want false")
	}
}

// The test the log rests on: a field that would carry an identifier, an
// address or a learner's work does not reach the line, whatever its author
// meant, and the line says how many it lost.
func TestAFieldTheEventDoesNotCarryIsDropped(t *testing.T) {
	t.Parallel()

	raw, lines := written(t, func(log *slog.Logger) {
		log.Info(logging.EventStopping,
			"email", email, "uid", uid, "ip", address, "remote_addr", address, "answer", work)
		log.With("uid", uid).With(slog.String("email", email)).Info(logging.EventStopping)
		// A field of the table, on an event that does not carry it.
		log.Info(logging.EventStopping, logging.FieldPort, 8080)
	})
	for _, value := range []string{email, uid, address, work} {
		if strings.Contains(raw, value) {
			t.Errorf("the log holds %q:\n%s", value, raw)
		}
	}
	if len(lines) != 3 {
		t.Fatalf("%d lines written, want 3", len(lines))
	}
	for i, want := range []float64{5, 2, 1} {
		if got := lines[i]["dropped_fields"]; got != want {
			t.Errorf("line %d: dropped_fields = %v, want %v", i, got, want)
		}
	}
}

func TestAValueOutsideItsRuleIsDropped(t *testing.T) {
	t.Parallel()

	for _, field := range []slog.Attr{
		slog.String(logging.FieldMoment, email),
		slog.String(logging.FieldVersion, uid),
		slog.String(logging.FieldVersion, "v1.2.3 "+uid),
		slog.String(logging.FieldCommit, "abc1234\n"),
		slog.String(logging.FieldDate, "yesterday"),
		slog.String(logging.FieldPort, "8080"),
		slog.String(logging.FieldDeployed, "true"),
		slog.String(logging.FieldErrorKind, "*fs.PathError"),
		slog.Any(logging.FieldErrorKind, logging.Kind{}),
		slog.Any(logging.FieldPanicKind, work),
	} {
		raw, lines := written(t, func(log *slog.Logger) {
			log.Info(logging.EventStartup, field)
			log.Info(logging.EventFailure, field)
			log.Info(logging.EventPanic, field)
		})
		for _, line := range lines {
			if _, kept := line[field.Key]; kept {
				t.Errorf("%s = %v is written: %s", field.Key, field.Value, raw)
			}
			if line["dropped_fields"] != float64(1) {
				t.Errorf("%s = %v: dropped_fields = %v, want 1", field.Key, field.Value, line["dropped_fields"])
			}
		}
	}
}

func TestAValueOfItsRuleIsKept(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"dev", "v0.1.0", "v0.1.0-3-gabc1234", "v0.1.0-3-gabc1234-dirty", "v0.2.0-rc.1", "v0.2.0-rc.1-2-gabc1234",
		"abc1234", "abc1234-dirty",
	} {
		_, lines := written(t, func(log *slog.Logger) { log.Info(logging.EventStartup, logging.FieldVersion, version) })
		if got := only(t, lines)[logging.FieldVersion]; got != version {
			t.Errorf("version %q is written as %v", version, got)
		}
	}
	_, lines := written(t, func(log *slog.Logger) {
		log.With(logging.FieldMoment, logging.MomentServe).Error(logging.EventFailure,
			logging.FieldErrorKind, logging.ErrorKind(errSynthetic{}))
	})
	line := only(t, lines)
	if line[logging.FieldMoment] != logging.MomentServe || line[logging.FieldErrorKind] != "logging_test.errSynthetic" {
		t.Errorf("the failure is written as %v", line)
	}
	if _, counted := line["dropped_fields"]; counted {
		t.Errorf("a line that dropped nothing says dropped_fields = %v", line["dropped_fields"])
	}
}

// An event the table does not know is written, so that the line is not lost,
// but its message is not: it may be anything, an identifier among it.
func TestAnUnknownEventLosesItsMessage(t *testing.T) {
	t.Parallel()

	raw, lines := written(t, func(log *slog.Logger) {
		log.Warn("sign-in of "+email+"\n{\"severity\":\"ERROR\"}", logging.FieldPort, 8080)
	})
	line := only(t, lines)
	if line["message"] != "unregistered_event" || line["dropped_fields"] != float64(1) {
		t.Errorf("the line is %v, want message unregistered_event and dropped_fields 1", line)
	}
	if strings.Contains(raw, email) {
		t.Errorf("the log holds %q:\n%s", email, raw)
	}
}

func TestAGroupIsDropped(t *testing.T) {
	t.Parallel()

	raw, lines := written(t, func(log *slog.Logger) {
		log.Info(logging.EventStartup, slog.Group("request", "uid", uid, logging.FieldPort, 8080))
		log.WithGroup("request").Info(logging.EventStartup, logging.FieldPort, 8080, "uid", uid)
		log.WithGroup("").Info(logging.EventStartup, logging.FieldPort, 8080)
	})
	if strings.Contains(raw, uid) {
		t.Errorf("the log holds %q:\n%s", uid, raw)
	}
	if len(lines) != 3 {
		t.Fatalf("%d lines written, want 3", len(lines))
	}
	for i, want := range []float64{1, 2} {
		if lines[i]["dropped_fields"] != want {
			t.Errorf("line %d: dropped_fields = %v, want %v", i, lines[i]["dropped_fields"], want)
		}
	}
	if lines[2][logging.FieldPort] != float64(8080) {
		t.Errorf("an empty group dropped the port: %v", lines[2])
	}
}

func TestAnEmptyFieldIsNoField(t *testing.T) {
	t.Parallel()

	_, lines := written(t, func(log *slog.Logger) { log.Info(logging.EventStopping, slog.Attr{}) })
	if _, counted := only(t, lines)["dropped_fields"]; counted {
		t.Error("an empty field was counted as dropped")
	}
}

// errSynthetic is an error whose text holds what an error must never carry
// into the log.
type errSynthetic struct{}

func (errSynthetic) Error() string { return "no profile for " + email }
