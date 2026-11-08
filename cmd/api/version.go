package main

import (
	"context"
	"fmt"
	"io"
	"runtime"

	"github.com/altnovatrail/api/internal/version"
)

// printVersion prints the build of the binary on one line: its version,
// commit, build date and the Go it was built with.
func printVersion(_ context.Context, args, _ []string, stdout, stderr io.Writer) error {
	if err := flags("version", args, stderr); err != nil {
		return err
	}
	_, err := fmt.Fprintf(stdout, "api %s %s %s %s\n", version.Version, version.Commit, version.Date, runtime.Version())
	return err
}
