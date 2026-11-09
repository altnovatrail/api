package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altnovatrail/spec/types/formatv0"

	"github.com/altnovatrail/api/internal/packages"
)

// testIndex is the index of the release the tests pin: one version of format
// 0.3, out of api's window, as v0.1.4 of modules is.
const testIndex = `{"module_format_version":"0.4","modules":{"example/older":{"0.1.0":{"module_format_version":"0.3","package_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000003","release":"v0.1.4"}}}}`

// testModules is the configuration the tests pin: the release v0.1.4 of
// testIndex.
func testModules() (packages.Config, error) {
	sum := sha256.Sum256([]byte(testIndex))
	return packages.Config{Release: "v0.1.4", Index: formatv0.Digest("sha256:" + hex.EncodeToString(sum[:]))}, nil
}

// releases serves testIndex as the release v0.1.4 of modules, and nothing else.
func releases() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0.1.4/"+packages.IndexFile {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(testIndex))
	}))
}

// laidPackages is a folder of the pinned release as api packages lays it, for
// serve.
func laidPackages(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, packages.IndexFile), []byte(testIndex), 0o444); err != nil {
		t.Fatal(err)
	}
	return dir
}

// api packages lays the pinned release into the folder it names, and says what
// it laid; a folder that holds something is refused, and so is a command line
// that names no absolute folder.
func TestPackagesLaysThePinnedRelease(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "packages")
	var stdout, stderr syncBuffer
	if code := run(t.Context(), []string{"packages", dir}, nil, &stdout, &stderr); code != exitOK {
		t.Fatalf("packages: exit code = %d, stderr %q, stdout %q", code, stderr.String(), stdout.String())
	}
	lines := stdout.lines(t)
	if len(lines) != 1 || lines[0]["message"] != "packages_laid" || lines[0]["modules_release"] != "v0.1.4" ||
		lines[0]["packages"] != float64(0) || lines[0]["files"] != float64(0) {
		t.Errorf("the log = %v, want one line of what was laid", lines)
	}
	if data, err := os.ReadFile(filepath.Join(dir, packages.IndexFile)); err != nil || string(data) != testIndex {
		t.Errorf("the index laid = %q, %v", data, err)
	}

	var again syncBuffer
	code := run(t.Context(), []string{"packages", dir}, nil, &again, &stderr)
	lines = again.lines(t)
	if code != exitFailure || len(lines) != 1 || lines[0]["message"] != "failure" || lines[0]["moment"] != "packages" ||
		lines[0]["error_kind"] == nil {
		t.Errorf("packages into a folder laid already: exit code %d, log %v; want a failure of the moment packages", code, lines)
	}
	for _, args := range [][]string{{"packages"}, {"packages", "relative"}, {"packages", dir, dir}, {"packages", "-x"}} {
		if code, _, _ := runCommand(t.Context(), nil, args...); code != exitUsage {
			t.Errorf("%v: exit code = %d, want %d", args, code, exitUsage)
		}
	}
	if code, _, stderr := runCommand(t.Context(), nil, "packages", "-h"); code != exitOK || !strings.Contains(stderr, "Usage: api packages") {
		t.Errorf("packages -h: exit code %d, stderr %q", code, stderr)
	}
	if code, _, _ := runCommand(t.Context(), []string{"API_LOG_LEVEL=loud"}, "packages", dir); code != exitFailure {
		t.Errorf("packages with a wrong log level: exit code = %d, want %d", code, exitFailure)
	}
}
