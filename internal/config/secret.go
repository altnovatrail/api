package config

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// redacted is what a Secret prints as, wherever it is printed.
const redacted = "[redacted]"

// Secret is a value that must reach only the code it is for: a key, a token, a
// connection string. Printing it — by fmt with any verb, by the log, in an
// error — gives "[redacted]"; only Reveal gives the value, and the one caller
// that needs it calls that.
type Secret struct{ value string }

// Reveal is the value itself.
func (s Secret) Reveal() string { return s.value }

// String is what fmt prints for %v and %s.
func (Secret) String() string { return redacted }

// GoString is what fmt prints for %#v.
func (Secret) GoString() string { return redacted }

// Format is what fmt prints for every verb, so that none of them — %d, %x,
// %q — reflects its way to the value.
func (Secret) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, redacted) }

// LogValue is what slog writes.
func (Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// fileSuffix names the variable that holds the path of a secret's file.
const fileSuffix = "_FILE"

// secret reads the secret called name: from the variable itself, or from the
// file whose absolute path the variable name_FILE holds. Both are declared
// when the secret is. The file lies outside the repository — a key in the
// working tree is one commit away from git (RULES.md, Hard rules) — so a file
// in any git working tree is refused; on Cloud Run it is a secret mounted as a
// volume. One trailing newline, which an
// editor or echo leaves, is not part of the value.
//
// Both at once is refused: which one wins would be a guess. So is an empty
// value or file, since a secret that is empty is a deployment that lost it.
// The refusal names the variables and the path, never the value.
func (e environment) secret(name string) (Secret, error) {
	value, inline := e[name]
	path, fromFile := e[name+fileSuffix]
	switch {
	case inline && fromFile:
		return Secret{}, fmt.Errorf("%w: set %s or %s%s, not both", ErrInvalid, name, name, fileSuffix)
	case inline:
		return Secret{value: value}, nil
	case !fromFile:
		return Secret{}, fmt.Errorf("%w: %s or %s%s must be set", ErrInvalid, name, name, fileSuffix)
	case !filepath.IsAbs(path):
		return Secret{}, fmt.Errorf("%w: %s%s must be an absolute path, got %q", ErrInvalid, name, fileSuffix, path)
	case inWorkingTree(path):
		return Secret{}, fmt.Errorf("%w: %s%s names a file in a git working tree, %q: keep it outside any repository",
			ErrInvalid, name, fileSuffix, path)
	}

	read, err := os.ReadFile(path)
	if err != nil {
		// The error of the file system names the path and why, and holds none
		// of the file's contents.
		return Secret{}, fmt.Errorf("%w: %s%s: %w", ErrInvalid, name, fileSuffix, err)
	}
	value = strings.TrimSuffix(strings.TrimSuffix(string(read), "\n"), "\r")
	if value == "" {
		return Secret{}, fmt.Errorf("%w: %s%s names an empty file, %q", ErrInvalid, name, fileSuffix, path)
	}
	return Secret{value: value}, nil
}

// inWorkingTree says whether path lies in a git working tree: whether a folder
// it stands in, up to the root, holds .git — a folder in a clone, a file in a
// worktree or a submodule.
func inWorkingTree(path string) bool {
	for dir := filepath.Dir(filepath.Clean(path)); ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		if parent := filepath.Dir(dir); parent == dir {
			return false
		}
	}
}
