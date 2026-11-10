// Package version holds the build identity of the binary.
//
// The values are placeholders in a plain `go build` and are set at link time,
// so that a running instance can say which build it is:
//
//	go build -ldflags "-X github.com/altnovatrail/api/internal/version.Version=v0.1.0 ..."
//
// The symbol path is spelled out because a wrong one is silent: the linker
// writes nothing, the build succeeds, and the binary reports "dev" forever. So
// the path is checked by linking a program with it and reading every value
// back.
package version

import "runtime/debug"

// Version is the release or tag the binary was built from. A build between
// tags says how far past one it is, and whether its tree held changes.
var Version = "dev"

// Commit is the abbreviated hash of the git commit the binary was built from.
var Commit = "unknown"

// Date is when the binary was built, in RFC 3339, or "unknown" when the build
// did not say.
var Date = "unknown"

// moduleKit is the path of module-kit's module.
const moduleKit = "github.com/altnovatrail/module-kit"

// ModuleKit is the version of module-kit the binary is built with, which every
// attempt keeps beside its package, so that its verdict repeats
// (architecture 3.2). It is read from the list of modules the build records, as
// go.mod pins it, and is empty in a binary that recorded none or links no
// module-kit.
func ModuleKit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return moduleVersion(info.Deps, moduleKit)
}

// moduleVersion is the version of the module path among deps, as replaced
// when it is — "(devel)" for a folder, which has none; empty when deps do not
// hold it.
func moduleVersion(deps []*debug.Module, path string) string {
	for _, dep := range deps {
		if dep.Path != path {
			continue
		}
		if dep.Replace != nil && dep.Replace.Version != "" {
			return dep.Replace.Version
		}
		if dep.Replace != nil {
			// A folder of the module's code, with no version: what Go's own
			// build calls a build of no version.
			return "(devel)"
		}
		return dep.Version
	}
	return ""
}
