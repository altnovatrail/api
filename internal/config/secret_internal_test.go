package config

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// value is a synthetic secret: no test holds a real one.
const value = "not-a-real-secret"

func TestASecretIsReadFromItsVariable(t *testing.T) {
	t.Parallel()

	got, err := environment{"API_TOKEN": value}.secret("API_TOKEN")
	if err != nil {
		t.Fatalf("secret error = %v, want nil", err)
	}
	if got.Reveal() != value {
		t.Errorf("Reveal() = %q, want %q", got.Reveal(), value)
	}
}

func TestASecretIsReadFromItsFile(t *testing.T) {
	t.Parallel()

	for name, written := range map[string]string{
		"bare": value, "newline": value + "\n", "carriage return": value + "\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(path, []byte(written), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := environment{"API_TOKEN_FILE": path}.secret("API_TOKEN")
			if err != nil {
				t.Fatalf("secret error = %v, want nil", err)
			}
			if got.Reveal() != value {
				t.Errorf("Reveal() = %q, want %q", got.Reveal(), value)
			}
		})
	}
}

func TestASecretRefusal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(dir, "clone")
	inTree := filepath.Join(clone, "config", "token")
	if err := os.MkdirAll(filepath.Join(clone, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(inTree), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inTree, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		env  environment
	}{
		{"neither", environment{}},
		{"both", environment{"API_TOKEN": value, "API_TOKEN_FILE": empty}},
		{"relative path", environment{"API_TOKEN_FILE": "token"}},
		{"missing file", environment{"API_TOKEN_FILE": filepath.Join(dir, "missing")}},
		{"empty file", environment{"API_TOKEN_FILE": empty}},
		{"file in a working tree", environment{"API_TOKEN_FILE": inTree}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := tc.env.secret("API_TOKEN")
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("secret error = %v, want %v", err, ErrInvalid)
			}
			if !strings.Contains(err.Error(), "API_TOKEN") {
				t.Errorf("error %q does not name the variable", err)
			}
			if strings.Contains(err.Error(), value) {
				t.Errorf("error %q holds the secret", err)
			}
		})
	}
}

// However a secret is printed — by fmt, in an error, by the log — the value
// is not in it.
func TestASecretPrintsAsRedacted(t *testing.T) {
	t.Parallel()

	secret := Secret{value: value}
	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("event", "secret", secret)

	for name, printed := range map[string]string{
		"%v":          fmt.Sprintf("%v", secret),
		"%s":          fmt.Sprintf("%s", secret),
		"%+v":         fmt.Sprintf("%+v", secret),
		"%#v":         fmt.Sprintf("%#v", secret),
		"%d":          fmt.Sprintf("%d", secret),
		"%x":          fmt.Sprintf("%x", secret),
		"%q":          fmt.Sprintf("%q", secret),
		"%10s":        fmt.Sprintf("%10s", secret),
		"String":      secret.String(),
		"GoString":    secret.GoString(),
		"in a struct": fmt.Sprintf("%+v", struct{ S Secret }{secret}),
		"error":       fmt.Errorf("with %v", secret).Error(),
		"slog":        logged.String(),
	} {
		if strings.Contains(printed, value) {
			t.Errorf("%s prints %q, which holds the secret", name, printed)
		}
		if !strings.Contains(printed, redacted) {
			t.Errorf("%s prints %q, want %q in it", name, printed, redacted)
		}
	}
}
