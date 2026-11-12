package packages

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/altnovatrail/module-kit/moduleindex"
	"github.com/altnovatrail/spec/types/formatv0"
)

// The reasons Check refuses a modules.json, beside ErrIndex, and the one of
// Withdrawals.
var (
	// ErrOlderRelease is a release older than the one main pins: a withdrawal is
	// marked only in the indexes of the releases after it (R63 spec).
	ErrOlderRelease = errors.New("packages: the release is older than the one main pins")
	// ErrLostVersion is a module that has a current version on main and would have
	// none, other than by a withdrawal (R07; architecture 3.3).
	ErrLostVersion = errors.New("packages: a module would lose its current version")
	// ErrNewestIndex is a file of the newest release of modules that is no module
	// index (R61).
	ErrNewestIndex = errors.New("packages: the newest release gives no module index")
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

// Withdrawal is a version that the pinned release gives and the newest release
// withdraws: until modules.json pins a release that marks it, it stays given,
// and new attempts take it when it is the current version (R07; R61).
type Withdrawal struct {
	ID      formatv0.ModuleID
	Version formatv0.ModuleVersion
	Reason  formatv0.WithdrawalReason
	// Current is true when the version is the current version by the pinned index.
	Current bool
}

// Withdrawals is the check of the scheduled job Withdrawals (R61): index must be
// the bytes of the index cfg pins, and latest the index of the newest release
// of modules, whose signature the job has checked by release-verify (R77 spec).
// It gives each version that the pinned index gives — in api's window, and not
// withdrawn — and that latest withdraws, by module ID and then by version, each
// in the order of its bytes. Only the newest index knows of every withdrawal so
// far, and module-kit joins them to the pinned index (moduleindex.WithLatest):
// an index that does not keep every entry of the pinned one, as one of an older
// release, gives an error that is moduleindex.ErrLatest.
func Withdrawals(cfg Config, index, latest []byte) ([]Withdrawal, error) {
	pinned, err := checkIndex(index, cfg.Index)
	if err != nil {
		return nil, err
	}
	newest, err := moduleindex.Read(latest)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNewestIndex, err)
	}
	joined, err := moduleindex.WithLatest(pinned, newest)
	if err != nil {
		return nil, fmt.Errorf("packages: %w", err)
	}
	var withdrawals []Withdrawal
	for _, id := range slices.Sorted(maps.Keys(pinned.Modules)) {
		current, _ := moduleindex.Newest(pinned, id)
		versions := pinned.Modules[id]
		for _, v := range slices.Sorted(maps.Keys(versions)) {
			entry := versions[v]
			later := joined.Modules[id][v]
			if !readable(entry) || entry.Withdrawn != nil || later.Withdrawn == nil {
				continue
			}
			withdrawals = append(withdrawals, Withdrawal{ID: id, Version: v, Reason: *later.Withdrawn, Current: v == current})
		}
	}
	return withdrawals, nil
}
