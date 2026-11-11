package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/store"
)

// consentsUsage is what api consents says of itself.
const consentsUsage = `Usage: api consents list | confirm <code> | reject <code>

The parents' consents whose signed form waits to be checked: list them, by the
code of each request and the object its scan lies at in the store of the forms;
confirm one whose scan matches its code; or reject one, whose adult then sends a
new scan.
`

// reportsUsage is what api reports says of itself.
const reportsUsage = `Usage: api reports list | review <id>

The problem reports of adults on the replies of the tutor, which the operator
reviews within 3 days: list those not reviewed, with what each says, or mark one
reviewed, which keeps it a year more.
`

// requestCodeForm is a code of a request: two groups of five characters of
// Crockford's base32.
var requestCodeForm = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{5}-[0-9A-HJKMNP-TV-Z]{5}$`)

// judgeConsents is the operator's check of the parents' signed forms (R116
// spec): it lists the forms sent, confirms a consent whose scan the operator
// matched to its code, or rejects one. What it prints goes to the operator's
// terminal, and the log names no code.
func judgeConsents(ctx context.Context, args, environ []string, stdout, stderr io.Writer) error {
	action, code, err := operatorArgs("consents", consentsUsage, args, stderr, func(rest []string) (string, string, error) {
		switch {
		case len(rest) == 1 && rest[0] == "list":
			return "list", "", nil
		case len(rest) != 2 || (rest[0] != "confirm" && rest[0] != "reject"):
			return "", "", errors.New("give list, confirm <code> or reject <code>")
		case !requestCodeForm.MatchString(rest[1]):
			return "", "", fmt.Errorf("%q is no code of a request, as ABCDE-FGHJK", rest[1])
		}
		return rest[0], rest[1], nil
	})
	if err != nil {
		return err
	}
	pool, log, err := operatorPool(ctx, environ, stderr, logging.MomentConsents)
	if err != nil {
		return err
	}
	defer pool.Close()
	if action == "list" {
		forms, err := store.SentForms(ctx, pool)
		if err != nil {
			return logged(log, logging.MomentConsents, err, logging.FieldDatabase, logging.DatabaseAPI)
		}
		for _, f := range forms {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", f.RequestCode, f.Object, f.MediaType, f.SentAt.UTC().Format(time.RFC3339))
		}
		return nil
	}
	switch err := store.JudgeForm(ctx, pool, code, action == "confirm"); {
	case errors.Is(err, store.ErrNoForm):
		fmt.Fprintf(stdout, "no consent of %s waits with its form sent\n", code)
		return errLogged
	case err != nil:
		return logged(log, logging.MomentConsents, err, logging.FieldDatabase, logging.DatabaseAPI)
	case action == "confirm":
		fmt.Fprintf(stdout, "%s confirmed\n", code)
	default:
		fmt.Fprintf(stdout, "%s rejected: its adult sends a new scan\n", code)
	}
	return nil
}

// reviewReports is the operator's reading of the problem reports (R123 spec):
// it lists those not reviewed, with what each says, or marks one reviewed.
func reviewReports(ctx context.Context, args, environ []string, stdout, stderr io.Writer) error {
	action, id, err := operatorArgs("reports", reportsUsage, args, stderr, func(rest []string) (string, string, error) {
		switch {
		case len(rest) == 1 && rest[0] == "list":
			return "list", "", nil
		case len(rest) != 2 || rest[0] != "review":
			return "", "", errors.New("give list or review <id>")
		case !store.CanonicalID(rest[1]):
			return "", "", fmt.Errorf("%q is no ID of a report", rest[1])
		}
		return rest[0], rest[1], nil
	})
	if err != nil {
		return err
	}
	pool, log, err := operatorPool(ctx, environ, stderr, logging.MomentReports)
	if err != nil {
		return err
	}
	defer pool.Close()
	if action == "list" {
		reports, err := store.ReportsToReview(ctx, pool, store.ReviewWithin)
		if err != nil {
			return logged(log, logging.MomentReports, err, logging.FieldDatabase, logging.DatabaseAPI)
		}
		for _, r := range reports {
			rung := ""
			if r.Rung != nil {
				rung = fmt.Sprintf(" %d", *r.Rung)
			}
			fmt.Fprintf(stdout, "%s\tcame %s, review by %s\t%s%s\t%s\n\twords: %q\n\ttext: %q\n", r.ID,
				r.CreatedAt.UTC().Format(time.RFC3339), r.ReviewBy.UTC().Format(time.RFC3339), r.Reply, rung, r.Kind,
				r.Words, r.Text)
		}
		return nil
	}
	switch err := store.ReviewReport(ctx, pool, id); {
	case errors.Is(err, store.ErrNotFound):
		fmt.Fprintf(stdout, "no report %s waits to be reviewed\n", id)
		return errLogged
	case err != nil:
		return logged(log, logging.MomentReports, err, logging.FieldDatabase, logging.DatabaseAPI)
	}
	fmt.Fprintf(stdout, "%s reviewed\n", id)
	return nil
}

// operatorArgs reads the action and its argument of an operator's command by
// read, and refuses what may not be done before the database is asked.
func operatorArgs(name, usage string, args []string, stderr io.Writer,
	read func(rest []string) (string, string, error)) (string, string, error) {
	set := flag.NewFlagSet("api "+name, flag.ContinueOnError)
	set.SetOutput(stderr)
	set.Usage = func() { fmt.Fprint(stderr, usage) }
	switch err := set.Parse(args); {
	case errors.Is(err, flag.ErrHelp):
		return "", "", err
	case err != nil:
		return "", "", fmt.Errorf("%w: %w", errUsage, err)
	}
	action, arg, err := read(set.Args())
	if err != nil {
		fmt.Fprintf(stderr, "api %s: %v\n\n%s", name, err, usage)
		return "", "", errUsage
	}
	return action, arg, nil
}

// operatorPool opens the database api by the pooler, as a command of the
// operator does, with its log on stderr.
func operatorPool(ctx context.Context, environ []string, stderr io.Writer, moment string) (*pgxpool.Pool, *slog.Logger, error) {
	// The refusal of the configuration names the variable and never the string.
	cfg, err := config.OperatorFrom(environ)
	if err != nil {
		return nil, nil, err
	}
	log := logging.New(stderr, cfg.LogLevel)
	pool, err := store.Open(ctx, cfg.DatabaseURL.Reveal())
	if err != nil {
		return nil, nil, logged(log, moment, err, logging.FieldDatabase, logging.DatabaseAPI)
	}
	return pool, log, nil
}
