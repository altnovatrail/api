package main

import (
	"context"
	"fmt"
	"io"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/store"
)

// reopenPlatformAI opens platform AI again for all, once the operator has
// lifted the limit of its provider that closed it (R11, R59): a refusal by that
// limit on the platform's key sets the flag "platform AI closed" of the
// deployment, and nothing but this command clears it. It says on stdout whether
// platform AI was closed, and its log line says the same, with no ID.
func reopenPlatformAI(ctx context.Context, args, environ []string, stdout, stderr io.Writer) error {
	if err := flags("reopen", args, stderr); err != nil {
		return err
	}
	cfg, err := config.OperatorFrom(environ)
	if err != nil {
		return err
	}
	log := logging.New(stderr, cfg.LogLevel)

	pool, err := store.Open(ctx, cfg.DatabaseURL.Reveal())
	if err != nil {
		return logged(log, logging.MomentReopen, err, logging.FieldDatabase, logging.DatabaseAPI)
	}
	defer pool.Close()
	was, err := store.OpenPlatformAI(ctx, pool)
	if err != nil {
		return logged(log, logging.MomentReopen, err, logging.FieldDatabase, logging.DatabaseAPI)
	}
	log.Info(logging.EventPlatformAIOpened, logging.FieldWasClosed, was)
	if was {
		fmt.Fprintln(stdout, "platform AI was closed by its provider's limit, and is open again")
	} else {
		fmt.Fprintln(stdout, "platform AI was open already")
	}
	return nil
}
