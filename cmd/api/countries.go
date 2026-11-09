package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/countries"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/store"
)

// countriesUsage is what api countries says of itself.
const countriesUsage = `Usage: api countries list | allow <XX> | remove <XX>

The countries the deployment takes learners from before the DPIA, by ISO 3166-1
alpha-2. None is taken until it is allowed; one of the EU, the EEA or the United
Kingdom is never allowed before the DPIA.
`

// listCountries keeps the operator's list of the countries a deployment takes
// learners from (R118, R119 spec; R36): it lists them, allows one, or removes one. A
// country removed takes no new profile; the profiles of it stay.
func listCountries(ctx context.Context, args, environ []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("api countries", flag.ContinueOnError)
	set.SetOutput(stderr)
	set.Usage = func() { fmt.Fprint(stderr, countriesUsage) }
	switch err := set.Parse(args); {
	case errors.Is(err, flag.ErrHelp):
		return err
	case err != nil:
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	action, country, err := countriesAction(set.Args())
	if err != nil {
		fmt.Fprintf(stderr, "api countries: %v\n\n%s", err, countriesUsage)
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
		return logged(log, logging.MomentCountries, err, logging.FieldDatabase, logging.DatabaseAPI)
	}
	defer pool.Close()

	switch action {
	case "list":
		list, err := store.Countries(ctx, pool)
		if err != nil {
			return logged(log, logging.MomentCountries, err, logging.FieldDatabase, logging.DatabaseAPI)
		}
		for _, c := range list {
			fmt.Fprintln(stdout, c)
		}
	case "allow":
		added, err := store.AllowCountry(ctx, pool, country)
		if err != nil {
			return logged(log, logging.MomentCountries, err, logging.FieldDatabase, logging.DatabaseAPI)
		}
		say(stdout, added, country+" allowed", country+" was allowed already")
	case "remove":
		removed, err := store.RemoveCountry(ctx, pool, country)
		if err != nil {
			return logged(log, logging.MomentCountries, err, logging.FieldDatabase, logging.DatabaseAPI)
		}
		say(stdout, removed, country+" removed", country+" was not allowed")
	}
	return nil
}

// countriesAction reads the action and its country, and refuses what may not be
// done before the database is asked.
func countriesAction(args []string) (action, country string, err error) {
	switch {
	case len(args) == 1 && args[0] == "list":
		return "list", "", nil
	case len(args) != 2 || (args[0] != "allow" && args[0] != "remove"):
		return "", "", errors.New("give list, allow <XX> or remove <XX>")
	case !countries.Valid(args[1]):
		return "", "", fmt.Errorf("%q is no country of ISO 3166-1 alpha-2, two capital letters", args[1])
	case args[0] == "allow" && countries.IsClosed(args[1]):
		return "", "", fmt.Errorf("%s is of the EU, the EEA or the United Kingdom, which no learner comes from before the DPIA (R119 spec)", args[1])
	}
	return args[0], args[1], nil
}

func say(w io.Writer, done bool, yes, no string) {
	if done {
		fmt.Fprintln(w, yes)
		return
	}
	fmt.Fprintln(w, no)
}
