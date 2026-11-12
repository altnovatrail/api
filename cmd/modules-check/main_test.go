package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// index is an index of one version out of api's window, as v0.1.4 of modules.
const index = `{"module_format_version":"0.4","modules":{"example/older":{"0.1.0":{"module_format_version":"0.3","package_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000003","release":"v0.1.4"}}}}`

// files writes a modules.json of release for index and the index, and gives
// their paths.
func files(t *testing.T, release, index string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	sum := sha256.Sum256([]byte(index))
	config := `{"release": "` + release + `", "index": "sha256:` + hex.EncodeToString(sum[:]) + `"}`
	configFile, indexFile := filepath.Join(dir, "modules.json"), filepath.Join(dir, "module-index.json")
	if err := os.WriteFile(configFile, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexFile, []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	return configFile, indexFile
}

func TestTheCheckPrintsTheCurrentVersionsAndRefuses(t *testing.T) {
	t.Parallel()

	config, idx := files(t, "v0.1.4", index)
	mainConfig, mainIdx := files(t, "v0.1.4", index)
	older, olderIdx := files(t, "v0.1.3", index)
	for _, tc := range []struct {
		args   []string
		code   int
		stdout string
	}{
		{[]string{"-config", config, "-index", idx}, 0, "example/older: no current version\n"},
		{[]string{"-config", config, "-index", idx, "-main-config", mainConfig, "-main-index", mainIdx}, 0, "example/older: no current version\n"},
		{[]string{"-config", older, "-index", olderIdx, "-main-config", mainConfig, "-main-index", mainIdx}, 1, ""},
		{[]string{"-config", config, "-index", olderIdx + ".missing"}, 1, ""},
		{[]string{"-config", config + ".missing", "-index", idx}, 1, ""},
		{[]string{"-config", config, "-index", idx, "-main-config", mainConfig + ".missing", "-main-index", mainIdx}, 1, ""},
		{[]string{"-config", idx, "-index", idx}, 1, ""},
		{[]string{"-config", config}, 2, ""},
		{[]string{"-config", config, "-index", idx, "-main-config", mainConfig}, 2, ""},
		{[]string{"-x"}, 2, ""},
		{[]string{"-h"}, 0, ""},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(tc.args, &stdout, &stderr); code != tc.code || stdout.String() != tc.stdout {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q; want %d and %q", strings.Join(tc.args, " "), code, stdout.String(), stderr.String(), tc.code, tc.stdout)
		}
	}
}

// pinned is an index of two versions of a module in api's window, the newer
// current; withdrawn, the same with both withdrawn by a later release.
const (
	pinned    = `{"module_format_version":"0.4","modules":{"example/smallest":{"0.1.0":{"module_format_version":"0.4","package_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000001","release":"v0.1.0"},"0.2.0":{"module_format_version":"0.4","package_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000002","release":"v0.2.0"}}}}`
	withdrawn = `{"module_format_version":"0.4","modules":{"example/smallest":{"0.1.0":{"module_format_version":"0.4","package_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000001","release":"v0.1.0","withdrawn":"answer_leak"},"0.2.0":{"module_format_version":"0.4","package_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000002","release":"v0.2.0","withdrawn":"wrong_key"}}}}`
)

func TestTheCheckOfTheNewestReleasePrintsItsWithdrawalsAndRefuses(t *testing.T) {
	t.Parallel()

	config, idx := files(t, "v0.2.0", pinned)
	later, newest := files(t, "v0.3.0", withdrawn)
	mainConfig, mainIdx := files(t, "v0.2.0", pinned)
	for _, tc := range []struct {
		args   []string
		code   int
		stdout string
	}{
		{[]string{"-config", config, "-index", idx, "-latest", idx}, 0, "no version that v0.2.0 gives is withdrawn\n"},
		{[]string{"-config", config, "-index", idx, "-latest", newest}, 1, "example/smallest 0.1.0: withdrawn, answer_leak\nexample/smallest 0.2.0: withdrawn, wrong_key, the current version\n"},
		// The newest of an older release, which does not keep the withdrawals.
		{[]string{"-config", later, "-index", newest, "-latest", idx}, 1, ""},
		{[]string{"-config", config, "-index", newest, "-latest", newest}, 1, ""},
		{[]string{"-config", config, "-index", idx, "-latest", newest + ".missing"}, 1, ""},
		{[]string{"-config", config + ".missing", "-index", idx, "-latest", newest}, 1, ""},
		{[]string{"-config", config, "-index", idx, "-latest", config}, 1, ""},
		{[]string{"-config", config, "-index", idx, "-latest", newest, "-main-config", mainConfig, "-main-index", mainIdx}, 2, ""},
		{[]string{"-config", config, "-index", idx, "-latest", ""}, 2, ""},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(tc.args, &stdout, &stderr); code != tc.code || stdout.String() != tc.stdout {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q; want %d and %q", strings.Join(tc.args, " "), code, stdout.String(), stderr.String(), tc.code, tc.stdout)
		}
	}
}
