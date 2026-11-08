package main

import (
	"context"
	"io"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/store"
)

// migrate applies the migrations of the database api that it has not had, over
// the direct connection, and exits (R29). A deployment runs it before a new
// revision takes requests (В38); here it is run by hand.
func migrate(ctx context.Context, args, environ []string, stdout, stderr io.Writer) error {
	if err := flags("migrate", args, stderr); err != nil {
		return err
	}
	// The refusal of the configuration names the variable and never the string.
	cfg, err := config.MigrateFrom(environ)
	if err != nil {
		return err
	}
	log := logging.New(stdout, cfg.LogLevel)

	applied, version, err := store.Migrate(ctx, cfg.DatabaseURL.Reveal())
	if err != nil {
		return logged(log, logging.MomentMigrate, err)
	}
	log.Info(logging.EventMigrated, logging.FieldApplied, applied, logging.FieldSchemaVersion, version)
	return nil
}
