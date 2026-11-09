package packages

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/altnovatrail/module-kit/moduleindex"
	"github.com/altnovatrail/spec/types/formatv0"
)

// The reasons Check refuses a modules.json, beside ErrIndex.
var (
	// ErrOlderRelease is a release older than the one main pins: a withdrawal is
	// marked only in the indexes of the releases after it (R63 spec).
	ErrOlderRelease = errors.New("packages: the release is older than the one main pins")
	// ErrLostVersion is a module that has a current version on main and would have
	// none, other than by a withdrawal (R07; architecture 3.3).
	ErrLostVersion = errors.New("packages: a module would lose its current version")
)

// Current is the version of a module that a new attempt gets by an index: the
// newest in api's window that the index does not mark withdrawn, by module-kit
// (R07), or none.
type Current struct {
	ID      formatv0.ModuleID
	Version formatv0.ModuleVersion
	// OK is false when the module has no current version.
	OK bool
}

// Check is the check of CI of a modules.json, cfg, whose signature the job has
// checked by release-verify (R07; R77 spec): index must be the bytes of its
// index; and against main's — mainCfg and mainIndex, or nil when main pins none
// yet — its release must not be older, and no module that has a current
// version on main may lose it other than by a withdrawal. It gives the current
// version of each module of either index, so that the author sees what the
// learners get.
func Check(cfg Config, index []byte, mainCfg *Config, mainIndex []byte) ([]Current, error) {
	idx, err := checkIndex(index, cfg.Index)
	if err != nil {
		return nil, err
	}
	ids := maps.Clone(idx.Modules)
	if mainCfg != nil {
		if olderRelease(cfg.Release, mainCfg.Release) {
			return nil, fmt.Errorf("%w: %s, and main pins %s", ErrOlderRelease, cfg.Release, mainCfg.Release)
		}
		mainIdx, err := checkIndex(mainIndex, mainCfg.Index)
		if err != nil {
			return nil, fmt.Errorf("main's: %w", err)
		}
		for id, versions := range mainIdx.Modules {
			ids[id] = versions
			was, ok := moduleindex.Newest(mainIdx, id)
			if !ok {
				continue
			}
			if _, ok := moduleindex.Newest(idx, id); ok {
				continue
			}
			if entry, there := idx.Modules[id][was]; !there || entry.Withdrawn == nil {
				return nil, fmt.Errorf("%w: %s has %s on main and none here", ErrLostVersion, id, was)
			}
		}
	}
	var currents []Current
	for _, id := range slices.Sorted(maps.Keys(ids)) {
		v, ok := moduleindex.Newest(idx, id)
		currents = append(currents, Current{ID: id, Version: v, OK: ok})
	}
	return currents, nil
}
