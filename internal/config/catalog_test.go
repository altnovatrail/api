package config_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/altnovatrail/api/internal/config"
)

// The deployment's list of modules is each ID between the commas, in its order,
// and none at all when the variable is unset or empty, as every variable reads
// empty: the whole index (R150 spec).
func TestCatalogReadsTheListOfModules(t *testing.T) {
	t.Parallel()

	for environ, want := range map[string][]string{
		"":                             nil,
		"API_CATALOG_MODULES=":         nil,
		"API_CATALOG_MODULES=a/b":      {"a/b"},
		"API_CATALOG_MODULES=a/b,c/d":  {"a/b", "c/d"},
		"API_CATALOG_MODULES=c/d,_a/b": {"c/d", "_a/b"},
	} {
		got, err := config.CatalogFrom(strings.Fields(environ))
		wantDir := config.DefaultPackagesDir
		if want == nil {
			wantDir = ""
		}
		if err != nil || !slices.Equal(got.Modules, want) || (want == nil) != (got.Modules == nil) || got.PackagesDir != wantDir {
			t.Errorf("CatalogFrom(%s) = %+v, %v; want %q in %q", environ, got, err, want, wantDir)
		}
	}
	got, err := config.CatalogFrom([]string{"API_CATALOG_MODULES= a/b , c/d", "API_PACKAGES_DIR=/srv/packages"})
	if err != nil || !slices.Equal(got.Modules, []string{"a/b", "c/d"}) || got.PackagesDir != "/srv/packages" {
		t.Errorf("CatalogFrom with spaces = %+v, %v; want the IDs without them, in /srv/packages", got, err)
	}
}

// A list that has an empty ID or names one twice is a typo, refused by
// the variable, and so is a folder of the packages by a relative path beside a
// list; with no list the folder is not checked. serve refuses them with the
// rest.
func TestCatalogRefusesAListWithATypo(t *testing.T) {
	t.Parallel()

	for _, environ := range [][]string{
		{"API_CATALOG_MODULES=,"},
		{"API_CATALOG_MODULES=a/b,"},
		{"API_CATALOG_MODULES=a/b,,c/d"},
		{"API_CATALOG_MODULES=a/b, a/b"},
		{"API_CATALOG_MODULES=a/b", "API_PACKAGES_DIR=packages"},
	} {
		_, err := config.CatalogFrom(environ)
		want := "API_CATALOG_MODULES"
		if len(environ) == 2 {
			want = "API_PACKAGES_DIR"
		}
		if !errors.Is(err, config.ErrInvalid) || !strings.Contains(err.Error(), want) {
			t.Errorf("CatalogFrom(%q) error = %v, want a refusal naming %s", environ, err, want)
		}
	}
	if got, err := config.CatalogFrom([]string{"API_PACKAGES_DIR=packages"}); err != nil || got.Modules != nil {
		t.Errorf("CatalogFrom with no list and a relative folder = %+v, %v; want no list and no refusal", got, err)
	}
	if _, err := config.ServeFrom(append(serveEnviron, "API_CATALOG_MODULES=a/b,a/b"), loaded(t, nil)); !errors.Is(err, config.ErrInvalid) ||
		!strings.Contains(err.Error(), "API_CATALOG_MODULES") {
		t.Errorf("ServeFrom with an ID twice: error = %v, want a refusal of API_CATALOG_MODULES", err)
	}
	serve, err := config.ServeFrom(append(serveEnviron, "API_CATALOG_MODULES=a/b,c/d"), loaded(t, nil))
	if err != nil || !slices.Equal(serve.Catalog, []string{"a/b", "c/d"}) {
		t.Errorf("ServeFrom = %+v, %v; want the list a/b, c/d", serve, err)
	}
}
