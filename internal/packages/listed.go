package packages

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/spec/types/formatv0"
)

// ErrNotListed is an ID of the deployment's list of modules that the pinned
// index does not hold (R150 spec): a typo would hide a module without a word,
// so it is a failed start, as a fact the admission table does not name is.
var ErrNotListed = errors.New("packages: a module of the catalog's list is not in the pinned index")

// NotInIndex refuses each of ids that index does not hold, naming them in their
// order: whether the deployment's list names only modules of the pinned index.
func NotInIndex(index formatv0.ModuleIndex, ids []formatv0.ModuleID) error {
	var missing []string
	for _, id := range ids {
		if _, ok := index.Modules[id]; !ok {
			missing = append(missing, string(id))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s", ErrNotListed, strings.Join(missing, ", "))
	}
	return nil
}

// List narrows what the set offers to the modules ids, the deployment's list
// (R150 spec): the catalog gives no other module, and the engine chooses none of
// its lessons and tasks, as of a module with no current version. Their packages
// stay in the set, so an attempt or a lesson run open on one goes on to its end
// on its package (R13). An ID the index does not hold is refused, and the set is
// left as it was; with no list — ids nil — the set offers the whole index. It is
// called before the set is served, as serve calls it at its start: the handlers
// read the list with no lock.
func (s *Set) List(ids []formatv0.ModuleID) error {
	if err := NotInIndex(s.Index, ids); err != nil {
		return err
	}
	if ids == nil {
		s.listed = nil
		return nil
	}
	s.listed = map[formatv0.ModuleID]bool{}
	for _, id := range ids {
		s.listed[id] = true
	}
	return nil
}

// Listed says whether the deployment's list names the module id, or there is
// no list.
func (s *Set) Listed(id formatv0.ModuleID) bool {
	return s.listed == nil || s.listed[id]
}

// OfferedFor is CurrentFor of a module the deployment's list names: the package
// a new attempt, a new lesson run, the catalog and the engine take (R150 spec).
func (s *Set) OfferedFor(id formatv0.ModuleID, category formatv0.AgeCategory) (*modulepackage.Package, bool) {
	if !s.Listed(id) {
		return nil, false
	}
	return s.CurrentFor(id, category)
}

// ReadIndex reads the index of the folder dir that Lay laid for cfg, by the
// digest cfg pins, and none of its packages: what api check needs to check the
// deployment's list of modules against it.
func ReadIndex(dir string, cfg Config) (formatv0.ModuleIndex, error) {
	data, err := os.ReadFile(filepath.Join(dir, IndexFile))
	if err != nil {
		return formatv0.ModuleIndex{}, fmt.Errorf("packages: read the index: %w", err)
	}
	return checkIndex(data, cfg.Index)
}
