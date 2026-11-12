// Command modules-check is the check of CI of modules.json (R07, R41): just
// ci-modules runs it once release-verify has accepted the signature of the
// index, and just ci-withdrawals, the scheduled job, with the index of the
// newest release of modules, once release-verify has accepted its signature too
// (R61). It is no part of the binary of a release: the release builds ./cmd/api
// alone.
//
//	modules-check -config modules.json -index <index> [-main-config <file> -main-index <file>]
//	modules-check -config modules.json -index <index> -latest <newest index>
//
// The index must be the bytes of the digest -config pins. Given main's, the
// release must not be older, and no module that has a current version on main
// may lose it other than by a withdrawal; it prints the current version of each
// module. Given the newest index instead, it prints each version that the
// pinned index gives and the newest withdraws, and refuses when there is one,
// so that the job fails until modules.json pins a release that marks it. It
// exits 1 on a refusal, 2 on a wrong command line.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/altnovatrail/api/internal/packages"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	set := flag.NewFlagSet("modules-check", flag.ContinueOnError)
	set.SetOutput(stderr)
	configFile := set.String("config", "modules.json", "the modules.json to check")
	indexFile := set.String("index", "", "the module-index.json of its release, whose signature was checked")
	mainConfig := set.String("main-config", "", "main's modules.json, when main has one")
	mainIndex := set.String("main-index", "", "the module-index.json of main's release")
	latestIndex := set.String("latest", "", "the module-index.json of the newest release of modules, whose signature was checked")
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	// -latest given empty, as an empty variable gives it, is no check of the
	// newest release: it must not fall back to the check of a pull request.
	latest := false
	set.Visit(func(f *flag.Flag) { latest = latest || f.Name == "latest" })
	if *indexFile == "" || set.NArg() > 0 || (*mainConfig == "") != (*mainIndex == "") ||
		(latest && (*latestIndex == "" || *mainConfig != "")) {
		fmt.Fprintln(stderr, "modules-check: -index is required, -main-config and -main-index go together, and -latest names a file and goes with neither")
		return 2
	}
	if latest {
		return withdrawals(*configFile, *indexFile, *latestIndex, stdout, stderr)
	}
	currents, err := check(*configFile, *indexFile, *mainConfig, *mainIndex)
	if err != nil {
		fmt.Fprintf(stderr, "modules-check: %v\n", err)
		return 1
	}
	for _, c := range currents {
		if c.OK {
			fmt.Fprintf(stdout, "%s: %s\n", c.ID, c.Version)
		} else {
			fmt.Fprintf(stdout, "%s: no current version\n", c.ID)
		}
	}
	return 0
}

// withdrawals prints the versions the pinned index gives that the newest
// withdraws, and refuses when there is one.
func withdrawals(configFile, indexFile, latestFile string, stdout, stderr io.Writer) int {
	cfg, index, err := read(configFile, indexFile)
	if err != nil {
		fmt.Fprintf(stderr, "modules-check: %v\n", err)
		return 1
	}
	latest, err := os.ReadFile(latestFile)
	if err != nil {
		fmt.Fprintf(stderr, "modules-check: %v\n", err)
		return 1
	}
	found, err := packages.Withdrawals(cfg, index, latest)
	if err != nil {
		fmt.Fprintf(stderr, "modules-check: %v\n", err)
		return 1
	}
	if len(found) == 0 {
		fmt.Fprintf(stdout, "no version that %s gives is withdrawn\n", cfg.Release)
		return 0
	}
	for _, w := range found {
		current := ""
		if w.Current {
			current = ", the current version"
		}
		fmt.Fprintf(stdout, "%s %s: withdrawn, %s%s\n", w.ID, w.Version, w.Reason, current)
	}
	fmt.Fprintf(stderr, "modules-check: the newest release of modules withdraws versions that %s gives: pin it in modules.json (R07)\n", cfg.Release)
	return 1
}

// check reads the files and checks them.
func check(configFile, indexFile, mainConfigFile, mainIndexFile string) ([]packages.Current, error) {
	cfg, index, err := read(configFile, indexFile)
	if err != nil {
		return nil, err
	}
	if mainConfigFile == "" {
		return packages.Check(cfg, index, nil, nil)
	}
	mainCfg, mainIndex, err := read(mainConfigFile, mainIndexFile)
	if err != nil {
		return nil, fmt.Errorf("main's: %w", err)
	}
	return packages.Check(cfg, index, &mainCfg, mainIndex)
}

func read(configFile, indexFile string) (packages.Config, []byte, error) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return packages.Config{}, nil, err
	}
	cfg, err := packages.ParseConfig(data)
	if err != nil {
		return packages.Config{}, nil, err
	}
	index, err := os.ReadFile(indexFile)
	if err != nil {
		return packages.Config{}, nil, err
	}
	return cfg, index, nil
}
