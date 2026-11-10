package packages

import (
	"maps"
	"slices"

	"github.com/altnovatrail/module-kit/moduleindex"
	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/spec/types/formatv0"
)

// Status is whether the image gives a package (R07; contract, PackageStatus).
type Status int

// The statuses of a digest.
const (
	// Unknown is a digest the index of the image does not know: a deployment is
	// going on, or the digest is no package's.
	Unknown Status = iota
	// Given is a package of a version the index does not mark withdrawn, whose
	// format version is in api's window.
	Given
	// NotGiven is a package the index knows, of a version it marks withdrawn or
	// out of the window.
	NotGiven
)

// Status is whether the image gives the package of digest. A digest that two
// entries name is given when either gives it.
func (s *Set) Status(digest formatv0.Digest) Status {
	return s.statuses[digest]
}

// Current is the package of the current version of the module id: the newest by
// semver of its versions the index does not mark withdrawn and whose format
// version is in the window, by module-kit (R07). ok is false when it has none.
func (s *Set) Current(id formatv0.ModuleID) (pkg *modulepackage.Package, ok bool) {
	version, ok := moduleindex.Newest(s.Index, id)
	if !ok {
		return nil, false
	}
	pkg, ok = s.Packages[s.Index.Modules[id][version].PackageDigest]
	return pkg, ok
}

// CurrentFor is the package of the current version of the module id for a
// learner of category: none when the module has no current version, or one whose
// audience the learner is not in — the version is one for every learner, and an
// older one is never given instead (R07).
func (s *Set) CurrentFor(id formatv0.ModuleID, category formatv0.AgeCategory) (*modulepackage.Package, bool) {
	pkg, ok := s.Current(id)
	if !ok || !slices.Contains(pkg.View.Manifest.Audience.AgeCategories, category) {
		return nil, false
	}
	return pkg, true
}

// Modules is the ID of each module the index names, in the order of their bytes.
func (s *Set) Modules() []formatv0.ModuleID {
	return slices.Sorted(maps.Keys(s.Index.Modules))
}

// Names says whether the index names the module id.
func (s *Set) Names(id formatv0.ModuleID) bool {
	_, ok := s.Index.Modules[id]
	return ok
}

// DeviceFile is the file of the device part of the package of digest at path, as
// its listing names it — LICENSE, NOTICE or media/<path> — by its path in the
// folder and its own digest. ok is false when the package is none the set
// holds, or its device part has no such path.
func (s *Set) DeviceFile(digest formatv0.Digest, path string) (name string, file formatv0.Digest, ok bool) {
	d, ok := s.files[digest][path]
	if !ok {
		return "", "", false
	}
	return s.File(d.Digest), d.Digest, true
}

// statusesOf is the status of each digest the index names.
func statusesOf(index formatv0.ModuleIndex) map[formatv0.Digest]Status {
	statuses := map[formatv0.Digest]Status{}
	for _, versions := range index.Modules {
		for _, entry := range versions {
			if statuses[entry.PackageDigest] == Given {
				continue
			}
			statuses[entry.PackageDigest] = NotGiven
			if entry.Withdrawn == nil && readable(entry) {
				statuses[entry.PackageDigest] = Given
			}
		}
	}
	return statuses
}
