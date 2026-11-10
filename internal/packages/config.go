// Package packages is the module packages api holds (R07, R41): the release of
// modules that modules.json pins and the digest of its index, the command that
// lays the packages of that index which are in api's window into a folder — the
// image's, or the first app's outside git — and the reading of that folder when
// serve starts. module-kit checks each package by the chain of digests from the
// index; the index itself is held by its digest, which the pull request that
// pinned it checked against its signature (R77 spec).
package packages

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"

	"github.com/altnovatrail/spec/types/formatv0"
)

// ErrConfig is a modules.json that is not the form R07 gives it.
var ErrConfig = errors.New("packages: modules.json is not a release and the digest of its index")

// Config is modules.json: the tag of a release of modules and the digest of the
// bytes of its module-index.json (R07; architecture 3.3).
type Config struct {
	// Release is the tag of the release, vX.Y.Z.
	Release string `json:"release"`
	// Index is sha256: and 64 hexadecimal digits in lower case.
	Index formatv0.Digest `json:"index"`
}

var (
	releaseForm = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	digestForm  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// IsDigest says whether s is a digest as the format and the contract write one:
// sha256: and 64 lowercase hexadecimal digits.
func IsDigest(s string) bool { return digestForm.MatchString(s) }

// ParseConfig reads modules.json: one object of the two members alone, each once
// and of its form, and nothing after it — so that what a reviewer reads in the
// file is what the binary pins.
func ParseConfig(data []byte) (Config, error) {
	if err := oneObject(data, "release", "index"); err != nil {
		return Config{}, fmt.Errorf("%w: %v", ErrConfig, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("%w: %v", ErrConfig, err)
	}
	if !releaseForm.MatchString(c.Release) {
		return Config{}, fmt.Errorf("%w: release %q is no tag vX.Y.Z", ErrConfig, c.Release)
	}
	if !IsDigest(string(c.Index)) {
		return Config{}, fmt.Errorf("%w: index %q is no sha256: digest", ErrConfig, c.Index)
	}
	return c, nil
}

// oneObject says whether data is one JSON object whose members are names, each
// once, and nothing after it.
func oneObject(data []byte, names ...string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if t, err := decoder.Token(); err != nil || t != json.Delim('{') {
		return errors.New("not an object")
	}
	seen := map[string]bool{}
	for decoder.More() {
		t, err := decoder.Token()
		if err != nil {
			return err
		}
		name, _ := t.(string)
		if !slices.Contains(names, name) || seen[name] {
			return fmt.Errorf("the member %q is not one of %v, once each", name, names)
		}
		seen[name] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("something after the object")
	}
	if len(seen) != len(names) {
		return fmt.Errorf("want the members %v", names)
	}
	return nil
}

// releaseParts are the three numbers of a tag vX.Y.Z that ParseConfig accepted.
func releaseParts(tag string) [3]int {
	var parts [3]int
	for i, s := range releaseForm.FindStringSubmatch(tag)[1:] {
		parts[i], _ = strconv.Atoi(s)
	}
	return parts
}

// olderRelease says whether the tag a comes before the tag b, by semver.
func olderRelease(a, b string) bool {
	pa, pb := releaseParts(a), releaseParts(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}
