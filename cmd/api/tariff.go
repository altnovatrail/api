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

// setTariff gives the account that redeemed an invite code another tariff
// (R11): the operator finds the account by its code, so that the account's ID
// never leaves the platform (R04), and neither its output nor its log names
// it. The code is the one argument, written any way invite.Read takes it; the
// tariff, its flag. stdout says the tariff the account has now.
func setTariff(ctx context.Context, args, environ []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("api tariff", flag.ContinueOnError)
	set.SetOutput(stderr)
	tariff := set.String("tariff", "", "the tariff the account of the code gets: "+strings.Join(quota.Tariffs(), ", "))
	set.Usage = func() {
		fmt.Fprintln(stderr, "Usage: api tariff -tariff <tariff> <invite code>")
		set.PrintDefaults()
	}
	switch err := set.Parse(args); {
	case errors.Is(err, flag.ErrHelp):
		return err
	case err != nil:
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if !quota.Known(*tariff) {
		fmt.Fprintf(stderr, "api tariff: -tariff must be one of %s, got %q\n", strings.Join(quota.Tariffs(), ", "), *tariff)
		return errUsage
	}
	if set.NArg() != 1 {
		fmt.Fprintln(stderr, "api tariff takes one argument, the invite code of the account")
		return errUsage
	}
	hash, ok := invite.Read(set.Arg(0))
	if !ok {
		// The argument is not echoed: it may be a code mistyped.
		fmt.Fprintln(stderr, "api tariff: the argument is no invite code")
		return errUsage
	}
	cfg, err := config.OperatorFrom(environ)
	if err != nil {
		return err
	}
	log := logging.New(stderr, cfg.LogLevel)

	pool, err := store.Open(ctx, cfg.DatabaseURL.Reveal())
	if err != nil {
		return logged(log, logging.MomentTariff, err, logging.FieldDatabase, logging.DatabaseAPI)
	}
	defer pool.Close()
	switch err := store.SetTariff(ctx, pool, hash[:], *tariff); {
	case errors.Is(err, store.ErrNoAccount):
		fmt.Fprintln(stderr, "api tariff: no account redeemed this code, or it is being deleted")
		return errLogged
	case err != nil:
		return logged(log, logging.MomentTariff, err, logging.FieldDatabase, logging.DatabaseAPI)
	}
	fmt.Fprintln(stdout, *tariff)
	return nil
}
