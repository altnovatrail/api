package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/model"
	"github.com/altnovatrail/api/internal/sweep"
	"github.com/altnovatrail/api/internal/version"
)

// defaultFor is how long a pass of sweep or after may run: under the 10 minutes Cloud Run
// gives a task of a job by default, so that the pass stops between its items and
// says so, rather than being stopped in one (R38).
const defaultFor = 9 * time.Minute

// sweepPass runs one pass of the daily job (R21, R38, R49): it finishes the deletions
// the requests left and deletes what is past its term, writes the evidence the closed
// attempts owe, then exits. A deployment
// runs it on a schedule as a Cloud Run job; here it is run by hand. The log goes
// to stdout, a line for each step with its counts; the process exits 1 unless the
// pass was clean, since a term that depends on the job runs to its next clean
// pass.
func sweepPass(ctx context.Context, args, environ []string, stdout, stderr io.Writer) error {
	within, err := passFlags("sweep", args, stderr)
	if err != nil {
		return err
	}
	// The refusal of the configuration names the variable and never the string.
	// The level of the log and the longest a request may run are read as serve
	// reads them.
	cfg, err := config.LoadFrom(environ)
	if err != nil {
		return err
	}
	opens, err := config.SweepFrom(environ)
	if err != nil {
		return err
	}
	log := logging.New(stdout, cfg.LogLevel)
	// The packages, read as serve reads them at its start: the evidence the pass
	// writes is built of them (R49).
	pkgs, err := readPackages(opens.PackagesDir)
	if err != nil {
		return logged(log, logging.MomentSweep, err)
	}
	m, err := model.New(pkgs, version.LearnerModel())
	if err != nil {
		return logged(log, logging.MomentSweep, err)
	}

	ctx, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	p, err := openPorts(ctx, opens.DatabaseURL, opens.OpenFGADatabaseURL, opens.FirebaseProjectID, opens.FirebaseCredentials)
	if err != nil {
		var fields []any
		if failed := (*openFailed)(nil); errors.As(err, &failed) {
			fields = failed.fields()
		}
		return logged(log, logging.MomentSweep, err, fields...)
	}
	// A pass cut short may leave a connection in use, which the pool's Close
	// would wait for: the close is bounded as serve's is.
	defer closeWithin(cfg.CloseTimeout(), p.close)

	store, err := formsStore(ctx, opens.Agreements)
	if err != nil {
		return logged(log, logging.MomentSweep, err)
	}
	report := sweep.New(p.pool, pkgs, m, p.rights, p.signin, cfg.RequestTimeout, log).WithForms(store).Run(ctx, time.Now())
	if !report.Clean() {
		// Each step's line has said what it did and failed.
		return fmt.Errorf("%w: the pass was not clean", errLogged)
	}
	return nil
}

// passFlags is how long a pass of the job name may run, by its flag -for: a
// pass takes no arguments, and a duration that is not positive is refused.
func passFlags(name string, args []string, stderr io.Writer) (time.Duration, error) {
	set := flag.NewFlagSet("api "+name, flag.ContinueOnError)
	set.SetOutput(stderr)
	within := set.Duration("for", defaultFor, "how long the pass may run; it stops between items after that")
	switch err := set.Parse(args); {
	case errors.Is(err, flag.ErrHelp):
		return 0, err
	case err != nil:
		return 0, fmt.Errorf("%w: %w", errUsage, err)
	}
	if set.NArg() > 0 {
		fmt.Fprintf(stderr, "api %s takes no arguments, got %q\n", name, set.Args())
		return 0, errUsage
	}
	if *within <= 0 {
		fmt.Fprintf(stderr, "api %s: -for must be a positive duration, got %v\n", name, *within)
		return 0, errUsage
	}
	return *within, nil
}
