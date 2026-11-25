package packages_test

import (
	"slices"
	"testing"

	"github.com/altnovatrail/api/internal/packages"
	"github.com/altnovatrail/api/internal/packages/packagestest"
)

// The platform's board of checks is solved on the sheet and no other module is,
// until a test names its own modules in place of the platform's.
func TestTheModulesOnTheSheet(t *testing.T) {
	t.Parallel()

	var s packages.Set
	if !s.OnSheet("novatrail/check-board") || s.OnSheet("example/other") {
		t.Errorf("the platform's modules on the sheet: board %v, another %v; want the board alone",
			s.OnSheet("novatrail/check-board"), s.OnSheet("example/other"))
	}
	s.Sheet("example/other")
	if s.OnSheet("novatrail/check-board") || !s.OnSheet("example/other") {
		t.Error("a test's modules on the sheet stand in place of the platform's: want the other alone")
	}
}

// The packages of a module solved on the sheet are named by their digests, of
// each version, withdrawn too, and those of another module are not.
func TestTheDigestsOfTheModulesOnTheSheet(t *testing.T) {
	t.Parallel()

	first := packagestest.OwnEdited(t, "choices", "1.0.0", packagestest.As("example/board", "[adult]"))
	second := packagestest.OwnEdited(t, "choices", "1.1.0", packagestest.As("example/board", "[adult]"))
	other := packagestest.Built(t, "smallest")
	set := packagestest.Set(t,
		packagestest.Entry{Pkg: first, Release: "v0.1.0"},
		packagestest.Entry{Pkg: second, Release: "v0.2.0", Withdrawn: "wrong_key"},
		packagestest.Entry{Pkg: other, Release: "v0.1.0"},
	)
	set.Sheet("example/board")
	want := slices.Sorted(slices.Values([]string{string(first.Digest), string(second.Digest)}))
	if got := set.SheetDigests(); !slices.Equal(got, want) {
		t.Errorf("SheetDigests = %v, want %v", got, want)
	}
}
