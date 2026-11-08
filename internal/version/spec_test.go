package version_test

import (
	"runtime/debug"
	"strings"
	"testing"

	"github.com/altnovatrail/spec"
	apiv1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
)

// The modules spec's code comes in: the root, with the format and its
// vectors, and the nested module of the contracts' code.
const (
	specModule      = "github.com/altnovatrail/spec"
	contractsModule = "github.com/altnovatrail/spec/contracts"
)

// spec releases both modules from one commit, the contracts' tag carrying the
// number of spec's own (R83 spec), and api takes both at the tag of its spec
// submodule. A go.mod that moved one without the other would build the
// service against a contract of one tag and a format of another. The versions
// are read from this test binary, which links both, and not from the
// submodule: CI checks out none.
func TestSpecAndItsContractsAreAtOneTag(t *testing.T) {
	t.Parallel()

	if spec.Format == nil {
		t.Fatal("spec.Format is nil: the format's files are missing from the module")
	}
	if got, want := string(apiv1.File_novatrail_api_v1_account_proto.Package()), "novatrail.api.v1"; got != want {
		t.Fatalf("the contract's package is %q, want %q", got, want)
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("the test binary carries no build information")
	}
	versions := map[string]string{}
	for _, dep := range info.Deps {
		if dep.Replace != nil {
			t.Errorf("%s is replaced by %s: api takes spec at its tag, from the module proxy", dep.Path, dep.Replace.Path)
		}
		versions[dep.Path] = dep.Version
	}

	root, contracts := versions[specModule], versions[contractsModule]
	if root == "" || contracts == "" {
		t.Fatalf("the test binary links %s %q and %s %q, want both", specModule, root, contractsModule, contracts)
	}
	if root != contracts {
		t.Errorf("%s is at %s and %s at %s, want one tag", specModule, root, contractsModule, contracts)
	}
	if !strings.HasPrefix(root, "v") || strings.Contains(root, "-") {
		t.Errorf("%s is at %s, want a release tag rather than a pseudo-version", specModule, root)
	}
}
