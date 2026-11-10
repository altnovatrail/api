package model

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	learnermodel "github.com/altnovatrail/learner-model"
	"github.com/altnovatrail/spec/types/formatv0"

	"github.com/altnovatrail/api/internal/store"
)

// Trap is a trap that repeats in a profile's closed attempts (contract,
// RepeatingTrap): its full ID, its description and advice from the trap catalog
// of the version of the last attempt that found it, how many attempts found it,
// and when the last of them closed.
type Trap struct {
	ID           string
	Description  string
	Advice       string
	Attempts     int
	LastClosedAt time.Time
}

// RepeatingTraps is the traps the program found in two of the journal's
// attempts or more, by full ID, the most found first, then the latest. An
// attempt on a version withdrawn for a wrong key counts none: its verdicts are
// wrong (R63 spec). A trap whose last version left the image has no catalog to
// describe it by, and is left out (Д27).
func (m *Model) RepeatingTraps(journal []store.Entry) ([]Trap, error) {
	type found struct {
		attempts int
		last     store.Entry
	}
	traps := map[string]*found{}
	for _, entry := range journal {
		if m.wrongKey[entry.PackageDigest] {
			continue
		}
		e, err := learnermodel.DecodeEvidence(entry.Evidence)
		if err != nil {
			return nil, fmt.Errorf("model: read the evidence %d: %w", entry.Seq, err)
		}
		for _, t := range e.Traps {
			f := traps[t.ID]
			if f == nil {
				f = &found{}
				traps[t.ID] = f
			}
			f.attempts++
			if !entry.ClosedAt.Before(f.last.ClosedAt) {
				f.last = entry
			}
		}
	}
	var out []Trap
	for _, id := range slices.Sorted(maps.Keys(traps)) {
		f := traps[id]
		if f.attempts < 2 {
			continue
		}
		pkg, ok := m.set.Packages[formatv0.Digest(f.last.PackageDigest)]
		if !ok || pkg.View.Traps == nil {
			continue
		}
		local, ok := strings.CutPrefix(id, string(pkg.View.Manifest.ID)+"/trap/")
		trap, known := pkg.View.Traps.Traps[formatv0.TrapID(local)]
		if !ok || !known {
			continue
		}
		out = append(out, Trap{ID: id, Description: string(trap.Description), Advice: string(trap.TrapAdvice),
			Attempts: f.attempts, LastClosedAt: f.last.ClosedAt})
	}
	slices.SortStableFunc(out, func(a, b Trap) int {
		return cmp.Or(cmp.Compare(b.Attempts, a.Attempts), b.LastClosedAt.Compare(a.LastClosedAt))
	})
	return out, nil
}
