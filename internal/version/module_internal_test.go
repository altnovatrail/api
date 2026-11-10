package version

import (
	"runtime/debug"
	"testing"
)

// A module replaced is of its replacement's version, and one not there of none.
func TestTheVersionOfAModuleIsItsReplacements(t *testing.T) {
	t.Parallel()

	deps := []*debug.Module{
		{Path: "example.com/other", Version: "v1.0.0"},
		{Path: moduleKit, Version: "v0.3.0", Replace: &debug.Module{Path: "../module-kit", Version: "v0.3.1"}},
	}
	if got := moduleVersion(deps, moduleKit); got != "v0.3.1" {
		t.Errorf("a module replaced = %q, want v0.3.1", got)
	}
	deps[1].Replace = &debug.Module{Path: "../module-kit"}
	if got := moduleVersion(deps, moduleKit); got != "(devel)" {
		t.Errorf("a module replaced by a folder = %q, want (devel)", got)
	}
	if got := moduleVersion(deps[:1], moduleKit); got != "" {
		t.Errorf("a module not there = %q, want none", got)
	}
}

// A binary that links no module-kit records no version of it: this test's
// binary, of a package that imports none.
func TestABinaryWithNoModuleKitHasNoVersionOfIt(t *testing.T) {
	t.Parallel()

	if got := ModuleKit(); got != "" {
		t.Errorf("ModuleKit() = %q in a binary that links no module-kit, want none", got)
	}
}
