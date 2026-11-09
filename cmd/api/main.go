// Command api is NovaTrail's platform service, one binary with a command for
// each thing it is run for: serve, migrate, invite, countries, sweep, packages
// and version. The image it runs in has no shell, so whatever an operator or
// its build runs — the migrations as a job, an invite code from a terminal of
// their own (R34), the list of countries (R36), the daily job (R38), the
// packages of modules laid into the image (R41) — is a command of this binary
// too (R105 spec).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// The codes the process exits with.
const (
	exitOK      = 0
	exitFailure = 1
	// exitUsage is a command line the binary cannot run, as flag has it.
	exitUsage = 2
)

// usage is what the binary says when it is asked how to run it, or run in a
// way it does not know.
const usage = `Usage: api <command>

Commands:
  serve     serve the API until a stop is asked for
  migrate   apply the migrations of the databases api and openfga
  invite    make an invite code and print it, once
  countries list, allow or remove a country learners come from
  sweep     finish the deletions and delete what is past its term, once
  packages  lay the packages of the pinned release of modules into a folder
  version   print the build of this binary
`

// main owns the two things a process owns and a function should not: the
// signals it listens for and the code it exits with.
func main() {
	ctx, stop := signalled()
	code := run(ctx, os.Args[1:], os.Environ(), os.Stdout, os.Stderr)
	stop()

	// The exit is the last statement of the process on purpose: os.Exit runs
	// no deferred call, so nothing may be left to one after this point.
	os.Exit(code)
}

// signalled is a context that ends when a stop is asked for, by either of the
// signals that ask for one. One signal asks; a second ends the process at once.
// Once the first has arrived the signals go back to what they do by default,
// rather than being caught for the whole of the way out.
func signalled() (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	context.AfterFunc(ctx, stop)
	return ctx, stop
}

// commands are the binary's commands, by name. Each takes the arguments after
// its name and the process's environment and streams.
var commands = map[string]func(ctx context.Context, args, environ []string, stdout, stderr io.Writer) error{
	"serve":     serve,
	"migrate":   migrate,
	"invite":    createInvite,
	"countries": listCountries,
	"sweep":     sweepPass,
	"packages":  layPackages,
	"version":   printVersion,
}

// run runs the command args name and returns the code to exit with.
func run(ctx context.Context, args, environ []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, usage)
		return exitOK
	}
	command, known := commands[args[0]]
	if !known {
		fmt.Fprintf(stderr, "api: unknown command %q\n\n%s", args[0], usage)
		return exitUsage
	}
	return exitCode(stderr, command(ctx, args[1:], environ, stdout, stderr))
}

// errUsage marks a command line a command cannot run; flag has said why.
var errUsage = errors.New("usage")

// errLogged marks a failure the log has already recorded, so that it is
// reported once.
var errLogged = errors.New("logged")

// exitCode is the code the process exits with once a command has returned
// err, and it prints the one failure nobody else will have: a failure the log
// has recorded is not printed again, and one from before there was a log has
// nowhere else to go.
func exitCode(stderr io.Writer, err error) int {
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, flag.ErrHelp):
		return exitOK
	case errors.Is(err, errUsage):
		return exitUsage
	case !errors.Is(err, errLogged):
		fmt.Fprintf(stderr, "api: %v\n", err)
	}
	return exitFailure
}

// flags is the flag set of a command that takes no flags yet, which still
// answers -h and refuses what it does not know.
func flags(name string, args []string, stderr io.Writer) error {
	set := flag.NewFlagSet("api "+name, flag.ContinueOnError)
	set.SetOutput(stderr)
	switch err := set.Parse(args); {
	case errors.Is(err, flag.ErrHelp):
		// The help it asked for is printed, and the command is not run.
		return err
	case err != nil:
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if set.NArg() > 0 {
		fmt.Fprintf(stderr, "api %s takes no arguments, got %q\n", name, set.Args())
		return errUsage
	}
	return nil
}
