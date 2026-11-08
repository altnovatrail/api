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

// Version is the release or tag the binary was built from. A build between
// tags says how far past one it is, and whether its tree held changes.
var Version = "dev"

// Commit is the abbreviated hash of the git commit the binary was built from.
var Commit = "unknown"

// Date is when the binary was built, in RFC 3339, or "unknown" when the build
// did not say.
var Date = "unknown"
