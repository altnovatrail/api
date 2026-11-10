// Package model is the learner model of api (R06, R08, R49): it builds the
// evidence of a closed attempt, appends it to the profile's journal, keeps the
// cache of the skill state that learner-model folds of the journal, and reads
// the state for the skill map. The journal is the truth, and the cache is
// learner-model's state under a mark: the version of learner-model, the release
// of modules and the window of the format's versions it was computed on.
//
// Evidence is owed, rather than written by each close: Settle, under the lock of
// the profile's row of the cache, writes the evidence of every closed attempt of
// the profile that owes it, in the order they closed — those a request closed,
// and those the daily job, a trigger or the image before closed too (R49).
package model

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	learnermodel "github.com/altnovatrail/learner-model"
	"github.com/altnovatrail/module-kit/module"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/packages"
	"github.com/altnovatrail/api/internal/store"
)

// Model is the learner model on the packages the image holds.
type Model struct {
	set  *packages.Set
	mark store.Mark
	// digests is the packages the image holds, of which evidence can be built.
	digests []string
	// wrongKey is the packages the index withdrew for a wrong key, whose
	// evidence the state leaves out (R63 spec); in the window or not.
	wrongKey map[string]bool
	// graphs is the graph of the current version of each module that has one
	// (the 2026-11-02 amendment to R07): the index's, for every learner.
	graphs map[formatv0.ModuleID]learnermodel.Graph
}

// New is the model of the packages of set, by the version of learner-model the
// binary is built with. A binary that recorded none is refused, since the mark
// of the cache names it, and so is a current version whose graph learner-model
// refuses: no state could be computed of it.
func New(set *packages.Set, learnerModel string) (*Model, error) {
	if learnerModel == "" {
		return nil, errors.New("model: the build recorded no version of learner-model")
	}
	m := &Model{
		set:      set,
		mark:     store.Mark{LearnerModel: learnerModel, Release: set.Release, Window: module.FormatVersion},
		wrongKey: map[string]bool{},
		graphs:   map[formatv0.ModuleID]learnermodel.Graph{},
	}
	for digest := range set.Packages {
		m.digests = append(m.digests, string(digest))
	}
	slices.Sort(m.digests)
	for id, versions := range set.Index.Modules {
		for _, v := range versions {
			if v.Withdrawn != nil && *v.Withdrawn == formatv0.WithdrawalReasonWrongKey {
				m.wrongKey[string(v.PackageDigest)] = true
			}
		}
		pkg, ok := set.Current(id)
		if !ok {
			continue
		}
		g, err := graphOf(pkg)
		if err != nil {
			return nil, err
		}
		m.graphs[id] = g
	}
	return m, nil
}

// Settled is a profile's state once its owed evidence is in its journal: the
// state, the graphs it was computed on, the learner's grade, which reading it
// needs, the number of the last evidence of the journal, which an engine's
// choice keeps (R06), and how many pieces of evidence the settling wrote.
type Settled struct {
	State   learnermodel.State
	Graphs  learnermodel.Graphs
	Grade   learnermodel.Grade
	LastSeq int64
	Written int
}

// Settle writes, in tx, the evidence the profile's closed attempts owe, and
// gives its state (R06, R49). It holds the profile's row of the cache, which
// numbers the evidence one piece after another, and takes the cache by its mark:
//   - the mark this image works on: the tail of the journal the cache has not
//     taken yet is appended first, in order;
//   - an older one, or none, or a cache that does not decode: the state is
//     recomputed of the whole journal;
//   - a newer one — the image after this one, during a deployment, wrote it —:
//     the state is recomputed for the answer, and the cache is not written
//     (the 2026-11-02 amendment to R06).
//
// Then each owed attempt's evidence is appended to the state and to the journal,
// with the start band the fold took. The evidence of a package withdrawn for a
// wrong key goes to the journal and not to the state, as the recomputing leaves
// it out, so that appending gives what recomputing does (R63 spec).
//
// What cannot be read or built stops nothing: a piece of the journal that does
// not decode, or that the fold refuses, is left out of the state, as both ways
// of folding leave it out; an owed attempt whose evidence cannot be built, or
// that the fold refuses, stays owed and is not written, so that a fixed release
// builds it. One such attempt would otherwise refuse every close and every read
// of its profile.
func (m *Model) Settle(ctx context.Context, tx pgx.Tx, profileID string, at time.Time) (Settled, error) {
	held, err := store.HoldSkillState(ctx, tx, profileID)
	if err != nil {
		return Settled{}, err
	}
	grade, err := gradeOf(ctx, tx, profileID)
	if err != nil {
		return Settled{}, err
	}
	owed, err := store.OwedAttempts(ctx, tx, profileID, m.digests)
	if err != nil {
		return Settled{}, err
	}
	modules, err := store.JournalModules(ctx, tx, profileID)
	if err != nil {
		return Settled{}, err
	}
	for _, o := range owed {
		modules = append(modules, o.ModuleID)
	}
	out := Settled{Graphs: m.Graphs(modules), Grade: grade, State: learnermodel.State{Kind: learnermodel.StateKind}}

	after := int64(0)
	if held.Mark == m.mark {
		if cached, err := learnermodel.DecodeState(held.State); err == nil {
			out.State, after = cached, held.LastSeq
		}
	}
	journal, err := store.Journal(ctx, tx, profileID, after)
	if err != nil {
		return Settled{}, err
	}
	lastSeq := after
	for _, entry := range journal {
		lastSeq = entry.Seq
		if m.wrongKey[entry.PackageDigest] {
			continue
		}
		if e, err := learnermodel.DecodeEvidence(entry.Evidence); err == nil {
			out.State = fold(out.State, e, out.Graphs)
		}
	}

	for _, o := range owed {
		e, ok, err := evidence(ctx, m.set.Packages[formatv0.Digest(o.PackageDigest)], o, grade)
		if err != nil || !ok {
			continue
		}
		if !m.wrongKey[o.PackageDigest] {
			next, taken, err := learnermodel.Append(out.State, e, out.Graphs)
			if err != nil {
				continue
			}
			out.State, e = next, taken
		}
		data, err := learnermodel.EncodeEvidence(e)
		if err != nil {
			continue
		}
		if lastSeq, err = store.AppendEvidence(ctx, tx, profileID, o, data, at); err != nil {
			return Settled{}, err
		}
		out.Written++
	}
	out.LastSeq = lastSeq
	if newer(held.Mark, m.mark) {
		return out, nil
	}
	// A state the fold gave always encodes.
	state, _ := learnermodel.EncodeState(out.State)
	return out, store.WriteSkillState(ctx, tx, profileID, store.SkillState{State: state, Mark: m.mark, LastSeq: lastSeq})
}

// fold is the step of the fold of a piece of the journal on s, or s as it is
// when the fold refuses the piece: Recompute is this step over the journal from
// the empty state, so the cache and the state recomputed leave out the same.
func fold(s learnermodel.State, e learnermodel.Evidence, g learnermodel.Graphs) learnermodel.State {
	next, _, err := learnermodel.Append(s, e, g)
	if err != nil {
		return s
	}
	return next
}

// Owing is up to n of the profiles that owe evidence this image can build, by ID
// after the ID after: those with an attempt closed with no request after it — by
// the daily job, a trigger or the image before (R49).
func (m *Model) Owing(ctx context.Context, db store.Lister, after string, n int) ([]string, error) {
	return store.ProfilesOwingEvidence(ctx, db, m.digests, after, n)
}

// Graphs gives each of the modules the graph of its current version, or no
// current version, whose evidence then waits in the journal (R08).
func (m *Model) Graphs(modules []string) learnermodel.Graphs {
	g := learnermodel.Graphs{}
	for _, id := range modules {
		graph, ok := m.graphs[formatv0.ModuleID(id)]
		g[id] = learnermodel.ModuleGraph{Graph: graph, Current: ok}
	}
	return g
}

// Read reads the settled state at at, against its graphs and those of the
// modules of the engine — the modules connected and fit for the learner — with
// the grade (R08).
func (m *Model) Read(s Settled, at time.Time, engineModules []string) (learnermodel.Readout, error) {
	g := m.Graphs(engineModules)
	for id, graph := range s.Graphs {
		g[id] = graph
	}
	r, err := learnermodel.Read(s.State, g, at.Unix(), s.Grade, engineModules)
	if err != nil {
		return learnermodel.Readout{}, fmt.Errorf("model: read the state: %w", err)
	}
	return r, nil
}

// gradeOf is the profile's grade, as learner-model takes it: none is a case of
// its own, not grade 0.
func gradeOf(ctx context.Context, tx pgx.Tx, profileID string) (learnermodel.Grade, error) {
	grade, err := store.ProfileGrade(ctx, tx, profileID)
	if err != nil || grade == nil {
		return learnermodel.Grade{}, err
	}
	return learnermodel.Grade{Value: int32(*grade), Known: true}, nil
}

// newer says whether the cache's mark is newer than the image's in a part — a
// later version of learner-model, a later release of modules or a later window
// —, as the image after this one writes it during a deployment.
func newer(cache, image store.Mark) bool {
	return later(cache.LearnerModel, image.LearnerModel) || later(cache.Release, image.Release) ||
		later(cache.Window, image.Window)
}

// later says whether the version a is later than b, both X.Y.Z, with a v or
// without, or X.Y; a version either cannot read is no later.
func later(a, b string) bool {
	x, okA := numbers(a)
	y, okB := numbers(b)
	return okA && okB && slices.Compare(x, y) > 0
}

// numbers is the numbers of a version, without its v and what follows a - or a
// +, and whether it is one.
func numbers(v string) ([]int, bool) {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out []int
	for part := range strings.SplitSeq(v, ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, len(out) > 0
}

// Mark is the mark this image computes the cache on: the version of
// learner-model, the release of modules and the window of the format.
func (m *Model) Mark() store.Mark { return m.mark }
