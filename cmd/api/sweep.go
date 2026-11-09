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
	"github.com/altnovatrail/api/internal/sweep"
)

// defaultFor is how long a pass of sweep may run: under the 10 minutes Cloud Run
// gives a task of a job by default, so that the pass stops between its items and
// says so, rather than being stopped in one (R38).
const defaultFor = 9 * time.Minute

// sweepPass runs one pass of the daily job (R21, R38): it finishes the deletions
// the requests left and deletes what is past its term, then exits. A deployment
// runs it on a schedule as a Cloud Run job; here it is run by hand. The log goes
// to stdout, a line for each step with its counts; the process exits 1 unless the
// pass was clean, since a term that depends on the job runs to its next clean
// pass.
func sweepPass(ctx context.Context, args, environ []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("api sweep", flag.ContinueOnError)
	set.SetOutput(stderr)
	within := set.Duration("for", defaultFor, "how long the pass may run; it stops between items after that")
	switch err := set.Parse(args); {
	case errors.Is(err, flag.ErrHelp):
		return err
	case err != nil:
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if set.NArg() > 0 {
		fmt.Fprintf(stderr, "api sweep takes no arguments, got %q\n", set.Args())
		return errUsage
	}
	if *within <= 0 {
		fmt.Fprintf(stderr, "api sweep: -for must be a positive duration, got %v\n", *within)
		return errUsage
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

	ctx, cancel := context.WithTimeout(ctx, *within)
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

	report := sweep.New(p.pool, p.rights, p.signin, cfg.RequestTimeout, log).Run(ctx, time.Now())
	if !report.Clean() {
		// Each step's line has said what it did and failed.
		return fmt.Errorf("%w: the pass was not clean", errLogged)
	}
	return nil
}
