// Command modules-check is the check of CI of modules.json (R07, R41): just
// ci-modules runs it once release-verify has accepted the signature of the
// index. It is no part of the binary of a release: the release builds
// ./cmd/api alone.
//
//	modules-check -config modules.json -index <index> [-main-config <file> -main-index <file>]
//
// The index must be the bytes of the digest -config pins. Given main's, the
// release must not be older, and no module that has a current version on main
// may lose it other than by a withdrawal. It prints the current version of each
// module, and exits 1 on a refusal, 2 on a wrong command line.
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
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *indexFile == "" || set.NArg() > 0 || (*mainConfig == "") != (*mainIndex == "") {
		fmt.Fprintln(stderr, "modules-check: -index is required, and -main-config and -main-index go together")
		return 2
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
