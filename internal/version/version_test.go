package version_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// symbols is the path a build hands the linker. Every build of the service
// spells it out, and each is held to this one below.
const symbols = "github.com/altnovatrail/api/internal/version"

// The linker sets the values by a path nothing checks: a wrong one is silent,
// the build succeeds and the binary reports "dev" forever. So a program is
// linked with that path, and every value is read back.
func TestTheLinkerSetsEveryValue(t *testing.T) {
	t.Parallel()

	binary := filepath.Join(t.TempDir(), "stamped")
	flags := "-X " + symbols + ".Version=v1.2.3" +
		" -X " + symbols + ".Commit=abc1234" +
		" -X " + symbols + ".Date=2026-10-12T00:00:00Z"
	build := exec.CommandContext(t.Context(), "go", "build", "-ldflags", flags, "-o", binary, "./testdata/stamped")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build error = %v, want nil\n%s", err, out)
	}

	out, err := exec.CommandContext(t.Context(), binary).Output()
	if err != nil {
		t.Fatalf("running the stamped program error = %v, want nil", err)
	}
	if got, want := strings.TrimSpace(string(out)), "v1.2.3 abc1234 2026-10-12T00:00:00Z"; got != want {
		t.Errorf("the program says %q, want %q: a value the linker did not set", got, want)
	}
}

// Every build of the service hands the linker this same path. The justfile is
// the one build so far; the release's Dockerfile joins it here when it comes.
func TestEveryBuildHandsTheLinkerThisPath(t *testing.T) {
	t.Parallel()

	module := strings.TrimSuffix(symbols, "/internal/version")
	written, err := os.ReadFile("../../justfile")
	if err != nil {
		t.Fatalf("read the justfile: %v", err)
	}
	for _, want := range []string{
		`MODULE := "` + module + `"`,
		`SYMBOLS := MODULE + "/internal/version"`,
		"-X {{SYMBOLS}}.Version=", "-X {{SYMBOLS}}.Commit=", "-X {{SYMBOLS}}.Date=",
	} {
		if !strings.Contains(string(written), want) {
			t.Errorf("the justfile does not hand the linker %q", want)
		}
	}
}
