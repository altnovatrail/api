package config_test

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/altnovatrail/api/internal/config"
)

func TestDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := config.LoadFrom(nil)
	if err != nil {
		t.Fatalf("LoadFrom(nil) error = %v, want nil", err)
	}
	want := config.Config{
		Port:              config.DefaultPort,
		LogLevel:          slog.LevelInfo,
		ReadHeaderTimeout: config.DefaultReadHeaderTimeout,
		ReadTimeout:       config.DefaultReadTimeout,
		WriteTimeout:      config.DefaultWriteTimeout,
		IdleTimeout:       config.DefaultIdleTimeout,
		ShutdownTimeout:   config.DefaultShutdownTimeout,
		RequestTimeout:    config.DefaultRequestTimeout,
	}
	if *cfg != want {
		t.Errorf("LoadFrom(nil) = %+v, want %+v", *cfg, want)
	}
	if cfg.Deployed() {
		t.Error("Deployed() = true without K_SERVICE, want false")
	}
}

func TestValuesAreRead(t *testing.T) {
	t.Parallel()

	cfg, err := config.LoadFrom([]string{
		"PORT=9090",
		"API_LOG_LEVEL=debug",
		"API_HTTP_READ_HEADER_TIMEOUT=1s",
		"API_HTTP_READ_TIMEOUT=2s",
		"API_HTTP_WRITE_TIMEOUT=3s",
		"API_HTTP_IDLE_TIMEOUT=4s",
		"API_SHUTDOWN_TIMEOUT=8s",
		"API_REQUEST_TIMEOUT=2500ms",
		"K_SERVICE=api",
	})
	if err != nil {
		t.Fatalf("LoadFrom error = %v, want nil", err)
	}
	want := config.Config{
		Port:              "9090",
		LogLevel:          slog.LevelDebug,
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       4 * time.Second,
		ShutdownTimeout:   8 * time.Second,
		RequestTimeout:    2500 * time.Millisecond,
		Service:           "api",
	}
	if *cfg != want {
		t.Errorf("LoadFrom = %+v, want %+v", *cfg, want)
	}
	if !cfg.Deployed() {
		t.Error("Deployed() = false with K_SERVICE set, want true")
	}
}

func TestEveryLevelIsRead(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]slog.Level{
		"debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError,
	} {
		cfg, err := config.LoadFrom([]string{"API_LOG_LEVEL=" + name})
		if err != nil {
			t.Fatalf("API_LOG_LEVEL=%s: error = %v, want nil", name, err)
		}
		if cfg.LogLevel != want {
			t.Errorf("API_LOG_LEVEL=%s: level = %v, want %v", name, cfg.LogLevel, want)
		}
	}
}

func TestEmptyValueCountsAsUnset(t *testing.T) {
	t.Parallel()

	cfg, err := config.LoadFrom([]string{"PORT=", "API_LOG_LEVEL=", "API_SHUTDOWN_TIMEOUT=", "K_SERVICE="})
	if err != nil {
		t.Fatalf("LoadFrom error = %v, want nil", err)
	}
	if cfg.Port != config.DefaultPort || cfg.LogLevel != slog.LevelInfo ||
		cfg.ShutdownTimeout != config.DefaultShutdownTimeout || cfg.Deployed() {
		t.Errorf("LoadFrom with empty values = %+v, want the defaults", *cfg)
	}
}

// Only the declared names are read, exactly as they are written: a variable
// the service does not know is not read, whatever it holds.
func TestOnlyDeclaredVariablesAreRead(t *testing.T) {
	t.Parallel()

	cfg, err := config.LoadFrom([]string{
		"port=1", "k_service=api", "API_PORT=1", "DB_PASSWORD=x", "noequals",
	})
	if err != nil {
		t.Fatalf("LoadFrom error = %v, want nil", err)
	}
	if cfg.Port != config.DefaultPort || cfg.Deployed() {
		t.Errorf("LoadFrom = %+v, want the defaults: no declared variable was set", *cfg)
	}
}

// Every variable that cannot be read is named in the one error, so that a
// failed start shows all a deployment has to fix.
func TestReadFailuresNameEveryBadVariable(t *testing.T) {
	t.Parallel()

	_, err := config.LoadFrom([]string{
		"API_LOG_LEVEL=loud", "API_HTTP_READ_TIMEOUT=soon", "API_SHUTDOWN_TIMEOUT=9",
	})
	if !errors.Is(err, config.ErrInvalid) {
		t.Fatalf("LoadFrom error = %v, want %v", err, config.ErrInvalid)
	}
	for _, name := range []string{"API_LOG_LEVEL", "API_HTTP_READ_TIMEOUT", "API_SHUTDOWN_TIMEOUT"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
}

func TestADeploymentStarts(t *testing.T) {
	t.Parallel()

	if _, err := config.LoadFrom([]string{"K_SERVICE=api", "PORT=8080"}); err != nil {
		t.Errorf("a deployment with the defaults: error = %v, want nil", err)
	}
}

func TestRefusals(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		environ []string
		names   string
	}{
		{"port not a number", []string{"PORT=http"}, "PORT"},
		{"port zero deployed", []string{"PORT=0", "K_SERVICE=api"}, "PORT"},
		{"a negative port", []string{"PORT=-1"}, "PORT"},
		{"port too high", []string{"PORT=65536"}, "PORT"},
		{"port with a sign", []string{"PORT=+8080"}, "PORT"},
		{"port with a leading zero", []string{"PORT=08080"}, "PORT"},
		{"unknown level", []string{"API_LOG_LEVEL=trace"}, "API_LOG_LEVEL"},
		{"zero header timeout", []string{"API_HTTP_READ_HEADER_TIMEOUT=0s"}, "API_HTTP_READ_HEADER_TIMEOUT"},
		{"negative read timeout", []string{"API_HTTP_READ_TIMEOUT=-1s"}, "API_HTTP_READ_TIMEOUT"},
		{"zero write timeout", []string{"API_HTTP_WRITE_TIMEOUT=0s"}, "API_HTTP_WRITE_TIMEOUT"},
		{"zero idle timeout", []string{"API_HTTP_IDLE_TIMEOUT=0s"}, "API_HTTP_IDLE_TIMEOUT"},
		{"zero shutdown timeout", []string{"API_SHUTDOWN_TIMEOUT=0s"}, "API_SHUTDOWN_TIMEOUT"},
		{"shutdown under a second", []string{"API_SHUTDOWN_TIMEOUT=999ms"}, "API_SHUTDOWN_TIMEOUT"},
		{"deployed shutdown at the grace", []string{"K_SERVICE=api", "API_SHUTDOWN_TIMEOUT=10s"}, "API_SHUTDOWN_TIMEOUT"},
		{"zero request timeout", []string{"API_REQUEST_TIMEOUT=0s"}, "API_REQUEST_TIMEOUT"},
		{"a request as long as the write", []string{"API_REQUEST_TIMEOUT=60s"}, "API_REQUEST_TIMEOUT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := config.LoadFrom(tc.environ)
			if !errors.Is(err, config.ErrInvalid) {
				t.Fatalf("LoadFrom(%q) = %+v, %v; want %v", tc.environ, cfg, err, config.ErrInvalid)
			}
			if !strings.Contains(err.Error(), tc.names) {
				t.Errorf("error %q does not name %s", err, tc.names)
			}
		})
	}
}

// A long way out is a developer's choice on their own machine, where no
// platform kills the process after ten seconds.
func TestALocalShutdownMayBeLong(t *testing.T) {
	t.Parallel()

	cfg, err := config.LoadFrom([]string{"API_SHUTDOWN_TIMEOUT=30s"})
	if err != nil {
		t.Fatalf("LoadFrom error = %v, want nil", err)
	}
	if got, want := cfg.DrainTimeout()+cfg.CloseTimeout(), 30*time.Second; got != want {
		t.Errorf("drain %v + close %v = %v, want the whole %v", cfg.DrainTimeout(), cfg.CloseTimeout(), got, want)
	}
	if got, want := cfg.CloseTimeout(), 7500*time.Millisecond; got != want {
		t.Errorf("CloseTimeout() = %v, want a quarter, %v", got, want)
	}
}

// Port 0, on this machine, asks the kernel for a free port.
func TestPortZeroIsAFreePortHere(t *testing.T) {
	t.Parallel()

	cfg, err := config.LoadFrom([]string{"PORT=0"})
	if err != nil || cfg.Port != "0" {
		t.Errorf("LoadFrom(PORT=0) = %+v, %v; want the port 0", cfg, err)
	}
}
