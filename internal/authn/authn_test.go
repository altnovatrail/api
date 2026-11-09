package authn_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/altnovatrail/api/internal/authn"
)

func TestASignInIsFreshForFiveMinutesEitherWay(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		authTime time.Time
		fresh    bool
	}{
		{"now", now, true},
		{"300 s ago", now.Add(-300 * time.Second), true},
		{"301 s ago", now.Add(-301 * time.Second), false},
		{"300 s ahead", now.Add(300 * time.Second), true},
		{"301 s ahead", now.Add(301 * time.Second), false},
		{"no time of sign-in", time.Time{}, false},
	} {
		if got := authn.Fresh(tc.authTime, now); got != tc.fresh {
			t.Errorf("%s: Fresh = %v, want %v", tc.name, got, tc.fresh)
		}
	}
}

func TestEachClassHasItsName(t *testing.T) {
	t.Parallel()

	names := map[authn.Class]string{
		authn.Expired: "expired", authn.Invalid: "invalid", authn.Revoked: "revoked",
		authn.NoUser: "no_user", authn.Disabled: "disabled", authn.Failed: "failed",
		authn.Class(0): "unknown",
	}
	for class, name := range names {
		if class.String() != name {
			t.Errorf("Class(%d) = %q, want %q", int(class), class.String(), name)
		}
	}
}

// api reads no email of a token or a user (R29 canvas-api): what the port gives
// has no field for one, and the code reads no field or claim of the SDK's
// beyond the ones it names.
func TestNoEmailIsRead(t *testing.T) {
	t.Parallel()

	for _, value := range []any{authn.Token{}, authn.User{}} {
		kind := reflect.TypeOf(value)
		for i := range kind.NumField() {
			if name := kind.Field(i).Name; strings.Contains(strings.ToLower(name), "email") {
				t.Errorf("%s has the field %s", kind.Name(), name)
			}
		}
	}

	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	more, err := filepath.Glob("authntest/*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range append(sources, more...) {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		code, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		for _, read := range []string{".Email", ".Claims", ".ProviderUserInfo", ".PhoneNumber", ".DisplayName"} {
			if strings.Contains(string(code), read) {
				t.Errorf("%s reads %s", source, read)
			}
		}
	}
}
