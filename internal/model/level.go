package model

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	learnermodel "github.com/altnovatrail/learner-model"
	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/store"
)

// Level is an adult's last correction of a module's level, as the journal holds
// it: the module, the ID of the band set, and when, in whole seconds.
type Level struct {
	Module string
	Band   string
	SetAt  time.Time
}

// Correct appends an adult's correction of the level of the module of pkg — its
// current version — to band, at at, to the settled state s and to the profile's
// journal in tx, and writes the cache, as Settle does: the evidence
// learner-model's LevelCorrection gives, with the start band by the learner's
// grade, which every evidence of the module has. It gives the state after it.
// That the band is next to the level's is the caller's to check.
func (m *Model) Correct(ctx context.Context, tx pgx.Tx, profileID string, s Settled, pkg *modulepackage.Package, band learnermodel.BandRef, at time.Time) (Settled, error) {
	module := string(pkg.View.Manifest.ID)
	start, err := learnermodel.BandByGrade(s.Grade, bandsOf(pkg))
	if err != nil {
		return Settled{}, fmt.Errorf("model: the start band: %w", err)
	}
	// The module may have no evidence yet, and so no graph among those settled.
	graphs := maps.Clone(s.Graphs)
	if graphs == nil {
		graphs = learnermodel.Graphs{}
	}
	graphs[module] = m.Graphs([]string{module})[module]
	next, e, err := learnermodel.Append(s.State, learnermodel.LevelCorrection(module, band, start, at.Unix()), graphs)
	if err != nil {
		return Settled{}, fmt.Errorf("model: append the correction: %w", err)
	}
	data, err := learnermodel.EncodeEvidence(e)
	if err != nil {
		return Settled{}, fmt.Errorf("model: write the correction: %w", err)
	}
	seq, err := store.AppendCorrection(ctx, tx, profileID, string(pkg.Digest), module, data, at)
	if err != nil {
		return Settled{}, err
	}
	s.State, s.Graphs, s.LastSeq = next, graphs, seq
	return s, m.writeCache(ctx, tx, profileID, s)
}

// Levels is the adult's last correction of the level of the module of the
// profile's journal, or with module empty of each module that has one,
// connected or not since, in the order of the modules' IDs byte by byte.
func Levels(ctx context.Context, db store.Lister, profileID, module string) ([]Level, error) {
	corrections, err := store.Corrections(ctx, db, profileID, module)
	if err != nil {
		return nil, err
	}
	return lastLevels(corrections), nil
}

// LevelsOf is Levels of a journal already read, whole, in its order.
func LevelsOf(journal []store.Entry) []Level {
	var corrections []store.Correction
	for _, e := range journal {
		if e.AttemptID == "" {
			corrections = append(corrections, store.Correction{ModuleID: e.ModuleID, Evidence: e.Evidence})
		}
	}
	slices.SortStableFunc(corrections, func(a, b store.Correction) int { return strings.Compare(a.ModuleID, b.ModuleID) })
	return lastLevels(corrections)
}

// lastLevels is the last correction of each module of corrections, which come
// by module and, within one, in the order of the journal. A correction that
// does not decode is left out, as the fold leaves it out, and the one before it
// is the last.
func lastLevels(corrections []store.Correction) []Level {
	var out []Level
	for _, c := range corrections {
		e, err := learnermodel.DecodeEvidence(c.Evidence)
		if err != nil || !e.LevelSetByAdult {
			continue
		}
		l := Level{Module: c.ModuleID, Band: e.Task.Band.ID, SetAt: time.Unix(e.PracticeTime, 0).UTC()}
		if n := len(out); n > 0 && out[n-1].Module == l.Module {
			out[n-1] = l
		} else {
			out = append(out, l)
		}
	}
	return out
}
