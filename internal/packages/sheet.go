package packages

import (
	"slices"

	"github.com/altnovatrail/spec/types/formatv0"
)

// onSheet is the modules solved on the sheet of a task from paper: a model
// checks each of their tasks, as it checks a task from paper, and none is
// evidence of the learner model. The module format has no field for it while
// one module alone is solved so: a field would be a version of the format, and
// of every reader of it, for one module.
var onSheet = map[formatv0.ModuleID]bool{"novatrail/check-board": true}

// OnSheet says whether the module id is solved on the sheet of a task from
// paper.
func (s *Set) OnSheet(id formatv0.ModuleID) bool {
	if s.sheet != nil {
		return s.sheet[id]
	}
	return onSheet[id]
}

// Sheet names ids, in place of the platform's own, as the modules solved on
// the sheet, for a test whose packages are not the platform's. It is called
// before the set is served, as List is.
func (s *Set) Sheet(ids ...formatv0.ModuleID) {
	s.sheet = map[formatv0.ModuleID]bool{}
	for _, id := range ids {
		s.sheet[id] = true
	}
}

// SheetDigests is every digest of a package of a module solved on the sheet,
// in the order of their bytes: the engine begins none of their tasks, so the
// job after a rollout closes the attempts it began on them before.
func (s *Set) SheetDigests() []string {
	var out []string
	for digest, pkg := range s.Packages {
		if s.OnSheet(pkg.View.Manifest.ID) {
			out = append(out, string(digest))
		}
	}
	slices.Sort(out)
	return out
}
