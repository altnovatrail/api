package version_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// read is a file of the repository, by its path from the root.
func read(t *testing.T, path string) string {
	t.Helper()

	written, err := os.ReadFile("../../" + path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(written)
}

// The Dockerfile builds with the Go that go.mod names and the tests run on, and
// on bases pinned by their digests (R105 spec): a builder that drifted from
// go.mod would ship a binary its tests never ran on.
func TestTheDockerfileBuildsWithTheGoOfGoMod(t *testing.T) {
	t.Parallel()

	goMod := regexp.MustCompile(`(?m)^go (\d+\.\d+\.\d+)$`).FindStringSubmatch(read(t, "go.mod"))
	if goMod == nil {
		t.Fatal("go.mod names no go version of three numbers")
	}
	dockerfile := read(t, "Dockerfile")

	froms := regexp.MustCompile(`(?m)^FROM (\S+)`).FindAllStringSubmatch(dockerfile, -1)
	var pinned []string
	for _, from := range froms {
		if strings.Contains(from[1], "@") {
			pinned = append(pinned, from[1])
		} else if from[1] != "runtime" {
			t.Errorf("FROM %s is no image pinned by its digest, nor the stage runtime", from[1])
		}
	}
	digest := `@sha256:[0-9a-f]{64}$`
	for i, want := range []string{
		`^golang:` + regexp.QuoteMeta(goMod[1]) + `-trixie` + digest,
		`^gcr\.io/distroless/static-debian13:nonroot` + digest,
	} {
		if i >= len(pinned) {
			t.Fatalf("the Dockerfile pins %d images, want the builder and the runtime", len(pinned))
		}
		if !regexp.MustCompile(want).MatchString(pinned[i]) {
			t.Errorf("image %d is %s, want %s: the builder has the Go of go.mod, %s", i+1, pinned[i], want, goMod[1])
		}
	}
	if !strings.Contains(dockerfile, "FROM runtime AS release\n") || !strings.Contains(dockerfile, "COPY --from=binary --chmod=0555 api /api") {
		t.Error("the Dockerfile has no stage release around the binary of the context binary, which a deployment builds")
	}
	// The packages of modules are laid by the binary the image holds, and copied in
	// owned by root (R41).
	for _, line := range []string{
		`RUN ["/api", "packages", "/tmp/packages"]`,
		"COPY --from=release-packages --chown=0:0 /tmp/packages /packages",
		"COPY --from=source-packages --chown=0:0 /tmp/packages /packages",
	} {
		if !strings.Contains(dockerfile, line) {
			t.Errorf("the Dockerfile has no line %s, which lays the packages of modules into the image", line)
		}
	}
}

// git archive writes the commit and its time into archive.txt, which
// release-files reads (R28): the attribute that makes it do so, and the format
// it fills in, are held here.
func TestTheArchiveNamesItsCommit(t *testing.T) {
	t.Parallel()

	if !regexp.MustCompile(`(?m)^/internal/version/archive\.txt export-subst$`).MatchString(read(t, ".gitattributes")) {
		t.Error(".gitattributes does not give internal/version/archive.txt export-subst")
	}
	archive := strings.TrimSpace(read(t, "internal/version/archive.txt"))
	// In a checkout the format is as written; in an archive, git has filled it in.
	if archive != "$Format:%H %ct$" && !regexp.MustCompile(`^[0-9a-f]{40} \d+$`).MatchString(archive) {
		t.Errorf("internal/version/archive.txt is %q, want $Format:%%H %%ct$ or what git archive makes of it", archive)
	}
}
