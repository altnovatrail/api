package config

import (
	"fmt"
	"slices"
	"strings"
)

// VarCatalogModules is the deployment's list of modules: the IDs of the modules
// whose current version the catalog may give, separated by commas. Unset or
// empty, the catalog gives the whole pinned index. On novatrail, infra sets it.
const VarCatalogModules = "API_CATALOG_MODULES"

func init() {
	declared[VarCatalogModules] = struct{}{}
}

// Catalog is what api check reads of the deployment's list of modules.
type Catalog struct {
	// Modules is each ID of the list, in its order, or nil when the variable is
	// unset or empty: the whole index.
	Modules []string
	// PackagesDir is the absolute path of the folder of the packages, whose
	// index the list is checked against; empty with no list.
	PackagesDir string
}

// CatalogFrom reads the deployment's list of modules from a set of KEY=VALUE
// strings, with the folder of the packages whose index the caller checks it
// against: whether the pinned index holds each ID is no matter of the
// variables. With no list the folder is neither read nor checked.
func CatalogFrom(environ []string) (Catalog, error) {
	env := read(environ)
	modules, err := catalogModules(env)
	if err != nil || modules == nil {
		return Catalog{}, err
	}
	dir, err := packagesDir(env)
	if err != nil {
		return Catalog{}, err
	}
	return Catalog{Modules: modules, PackagesDir: dir}, nil
}

// catalogModules is the list env names, nil when it names none. The list is
// refused when it has an empty ID or names one twice: each would be a
// typo, and a typo hides a module without a word.
func catalogModules(env environment) ([]string, error) {
	list, set := env[VarCatalogModules]
	if !set {
		return nil, nil
	}
	ids := []string{}
	for id := range strings.SplitSeq(list, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("%w: %s must list module IDs separated by commas, got %q", ErrInvalid, VarCatalogModules, list)
		}
		if slices.Contains(ids, id) {
			return nil, fmt.Errorf("%w: %s names %q twice", ErrInvalid, VarCatalogModules, id)
		}
		ids = append(ids, id)
	}
	return ids, nil
}
