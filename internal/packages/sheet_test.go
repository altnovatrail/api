package packages_test

import (
	"testing"

	"github.com/altnovatrail/api/internal/packages"
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
