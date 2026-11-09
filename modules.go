// Package api holds modules.json, the release of modules that api's packages
// come from and the digest of its index (R07): the file is embedded here, in the
// package at the root of the module, because a //go:embed pattern takes no "..",
// so that CI checks, the build of the image lays and serve reads the one file,
// with no copy of it.
package api

import (
	_ "embed"

	"github.com/altnovatrail/api/internal/packages"
)

//go:embed modules.json
var modulesJSON []byte

// Modules is the configuration modules.json gives: the release and its index.
func Modules() (packages.Config, error) { return packages.ParseConfig(modulesJSON) }
