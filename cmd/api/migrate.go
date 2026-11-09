package main

import (
	"context"
	"io"

	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/store"
)

// migrate applies the migrations of the database api and of the database openfga
// that they have not had, over the direct connections, makes the store of the
// rights and writes its model when it must, and exits (R29, R31). A deployment
// runs it before a new revision takes requests (В38); here it is run by hand.
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
		return logged(log, logging.MomentMigrate, err, logging.FieldDatabase, logging.DatabaseAPI)
	}
	log.Info(logging.EventMigrated,
		logging.FieldDatabase, logging.DatabaseAPI, logging.FieldApplied, applied, logging.FieldSchemaVersion, version)

	rights, err := authz.Migrate(ctx, cfg.OpenFGADatabaseURL.Reveal())
	if err != nil {
		return logged(log, logging.MomentMigrate, err, logging.FieldDatabase, logging.DatabaseOpenFGA)
	}
	log.Info(logging.EventMigrated,
		logging.FieldDatabase, logging.DatabaseOpenFGA, logging.FieldApplied, rights.Applied,
		logging.FieldSchemaVersion, rights.Version,
		logging.FieldStoreCreated, rights.StoreCreated, logging.FieldModelWritten, rights.ModelWritten)
	return nil
}
