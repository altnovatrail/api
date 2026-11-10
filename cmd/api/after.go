package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/model"
	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/sweep"
	"github.com/altnovatrail/api/internal/version"
)

// afterPass runs one pass of the job after a rollout (R07, R51): it closes
// "unchecked" the open attempts and lesson runs on the packages this image no
// longer gives — withdrawn, or out of the window — and writes the evidence the
// attempts it closes owe, then exits. The rollout runs it as a Cloud Run job of
// the new image once its revision takes all traffic (the amendment of 2026-11-08
// to R106 spec). It opens the database api alone, by the pooler, and reads the
// packages as serve does. The log goes to stdout, a line for each step with its
// counts; the process exits 1 unless the pass was clean, and what it left the
// next request on it, or the daily job, closes.
func afterPass(ctx context.Context, args, environ []string, stdout, stderr io.Writer) error {
	within, err := passFlags("after", args, stderr)
	if err != nil {
		return err
	}
	// The refusal of the configuration names the variable and never the string.
	// The level of the log and the time to close are read as serve reads them.
	cfg, err := config.LoadFrom(environ)
	if err != nil {
		return err
	}
	opens, err := config.AfterFrom(environ)
	if err != nil {
		return err
	}
	log := logging.New(stdout, cfg.LogLevel)
	pkgs, err := readPackages(opens.PackagesDir)
	if err != nil {
		return logged(log, logging.MomentAfter, err)
	}
	m, err := model.New(pkgs, version.LearnerModel())
	if err != nil {
		return logged(log, logging.MomentAfter, err)
	}

	ctx, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	pool, err := store.Open(ctx, opens.DatabaseURL.Reveal())
	if err == nil {
		if err = pool.Ping(ctx); err != nil {
			pool.Close()
		}
	}
	if err != nil {
		return logged(log, logging.MomentAfter, err, logging.FieldDatabase, logging.DatabaseAPI)
	}
	// A pass cut short may leave a connection in use, which the pool's Close
	// would wait for: the close is bounded as sweep's is.
	defer closeWithin(cfg.CloseTimeout(), pool.Close)

	if report := sweep.After(pool, pkgs, m, log).Run(ctx, time.Now()); !report.Clean() {
		// Each step's line has said what it did and failed.
		return fmt.Errorf("%w: the pass was not clean", errLogged)
	}
	return nil
}
