// Command stamped prints the build identity it was linked with, so that what
// the linker set can be read back.
package main

import (
	"fmt"

	"github.com/altnovatrail/api/internal/version"
)

func main() { fmt.Println(version.Version, version.Commit, version.Date) }
