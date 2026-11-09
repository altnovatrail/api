package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/altnovatrail/api"
	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/packages"
)

// packagesUsage is what api packages says of itself.
const packagesUsage = `Usage: api packages <folder>

Lays the packages of the release of modules that modules.json pins into the
folder, which must be empty or not there: the index, checked by its digest, and
each package in the window, checked by module-kit by the chain of digests from
it. The build of the image runs it, and so does just packages for the first app.
`

// The release of modules and where its files come from; a test puts its own
// here, for want of the network.
var (
	modulesConfig = api.Modules
	modulesSource = packages.Source{Base: packages.DefaultBase}
)

// layPackages lays the packages of the pinned release of modules into a folder
// (R07, R41): the stage of the Dockerfile that builds the image runs it, and the
// recipe of the first app, with the one modules.json embedded in the binary.
func layPackages(ctx context.Context, args, environ []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("api packages", flag.ContinueOnError)
	set.SetOutput(stderr)
	set.Usage = func() { fmt.Fprint(stderr, packagesUsage) }
	switch err := set.Parse(args); {
	case errors.Is(err, flag.ErrHelp):
		return err
	case err != nil:
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if set.NArg() != 1 || !filepath.IsAbs(set.Arg(0)) {
		fmt.Fprintf(stderr, "api packages: name one folder, by its absolute path\n\n%s", packagesUsage)
		return errUsage
	}
	cfg, err := config.LoadFrom(environ)
	if err != nil {
		return err
	}
	log := logging.New(stdout, cfg.LogLevel)
	modules, err := modulesConfig()
	if err != nil {
		return logged(log, logging.MomentPackages, err)
	}
	laid, err := packages.Lay(ctx, modulesSource, modules, set.Arg(0))
	if err != nil {
		return logged(log, logging.MomentPackages, err)
	}
	log.Info(logging.EventPackagesLaid,
		logging.FieldModulesRelease, modules.Release,
		logging.FieldPackages, laid.Packages,
		logging.FieldFiles, laid.Files,
	)
	return nil
}
