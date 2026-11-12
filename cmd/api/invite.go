package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/invite"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/store"
)

// createInvite makes one invite code with a tariff, keeps its SHA-256 in the
// database api, and prints the code, once: nothing keeps it after (R04, R34).
// stdout carries the code alone, so that it can be passed on as it is; the log
// goes to stderr.
func createInvite(ctx context.Context, args, environ []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("api invite", flag.ContinueOnError)
	set.SetOutput(stderr)
	tariff := set.String("tariff", quota.ClosedSlice,
		"the tariff the code passes to the account that redeems it: "+strings.Join(quota.Tariffs(), ", "))
	switch err := set.Parse(args); {
	case errors.Is(err, flag.ErrHelp):
		return err
	case err != nil:
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if set.NArg() > 0 {
		fmt.Fprintf(stderr, "api invite takes no arguments, got %q\n", set.Args())
		return errUsage
	}
	if !quota.Known(*tariff) {
		fmt.Fprintf(stderr, "api invite: -tariff must be one of %s, got %q\n", strings.Join(quota.Tariffs(), ", "), *tariff)
		return errUsage
	}
	// The refusal of the configuration names the variable and never the string.
	cfg, err := config.OperatorFrom(environ)
	if err != nil {
		return err
	}
	log := logging.New(stderr, cfg.LogLevel)

	pool, err := store.Open(ctx, cfg.DatabaseURL.Reveal())
	if err != nil {
		return logged(log, logging.MomentInvite, err, logging.FieldDatabase, logging.DatabaseAPI)
	}
	defer pool.Close()
	code, hash := invite.New()
	if err := store.CreateInvite(ctx, pool, hash[:], *tariff); err != nil {
		return logged(log, logging.MomentInvite, err, logging.FieldDatabase, logging.DatabaseAPI)
	}
	fmt.Fprintln(stdout, code)
	return nil
}
