// Package catalog is api's way from a module to its package, in the contract
// novatrail.api.v1 (R07, R17, R18, R42): PackageService, the open views of a
// package by its digest and which digests the image still gives; ModuleService,
// the catalog of a profile and the modules connected to it; and the files of the
// device parts, by HTTP GET, which are no method of Connect. All of it is read
// from the packages serve read at its start, packages.Set, and the open views
// are built once, there: a request reads them, and a package whose views do not
// build is a failed start.
package catalog

import (
	"encoding/json"
	"fmt"

	"github.com/altnovatrail/module-kit/openview"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"

	"github.com/altnovatrail/api/internal/packages"
)

// The caching of the answers (R18): what a digest gives never changes, so an
// open view and a file are kept for a day, and a withdrawal reaches the
// learners only with the author's pull request anyway (Д9 in RUN.md). A refusal
// is kept by nobody: the next request may find the version given, or the
// deployment over.
const (
	cacheFor = "public, max-age=86400"
	noStore  = "no-store"
)

// Catalog is the packages of the image with the open views of each.
type Catalog struct {
	set   *packages.Set
	views map[formatv0.Digest]views
}

// views is what PackageService gives of a package: the open view of each part
// it has, nil for a part it has not, and the tiers of its skills.
type views struct {
	manifest, skillGraph, techniques, solvingSteps *v1.OpenView
	tiers                                          map[string]int32
}

// New is the catalog of set, with the open views of each of its packages built
// by module-kit, by the marks x-open of the format (R72 spec).
func New(set *packages.Set) (*Catalog, error) {
	c := &Catalog{set: set, views: map[formatv0.Digest]views{}}
	for digest, pkg := range set.Packages {
		v, err := viewsOf(pkg.View)
		if err != nil {
			return nil, fmt.Errorf("catalog: the open views of %s: %w", digest, err)
		}
		c.views[digest] = v
	}
	return c, nil
}

func viewsOf(view formatv0.CanonicalView) (views, error) {
	version := string(view.Manifest.ModuleFormatVersion)
	var v views
	var err error
	if v.manifest, err = openOf(&view.Manifest, openview.Manifest, version); err != nil {
		return views{}, err
	}
	if v.skillGraph, err = openOf(view.SkillGraph, openview.SkillGraph, version); err != nil {
		return views{}, err
	}
	if v.techniques, err = openOf(view.Techniques, openview.Techniques, version); err != nil {
		return views{}, err
	}
	if v.solvingSteps, err = openOf(view.SolvingSteps, openview.SolvingSteps, version); err != nil {
		return views{}, err
	}
	if len(view.SkillTiers) > 0 {
		v.tiers = map[string]int32{}
		for skill, tier := range view.SkillTiers {
			v.tiers[string(skill)] = int32(tier)
		}
	}
	return v, nil
}

// openOf is the open view of part as the contract carries it, the JSON of the
// format and its version; none when the module has no such part.
func openOf[P, O any](part *P, open func(P) (O, error), version string) (*v1.OpenView, error) {
	if part == nil {
		return nil, nil
	}
	view, err := open(*part)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	return &v1.OpenView{Json: data, ModuleFormatVersion: version}, nil
}
