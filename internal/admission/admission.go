// Package admission is the function of admission to AI as api asks it before
// an attempt (R25, R45): the admission table of the release of spec that tutor
// reads, the facts the deployment declares in its file, and the least stage of
// tutor's rule of admission of models. What a task that AI checks may be given
// is what tutor's Table.Select chooses for it, and tutor asks the same before
// its call.
//
// The file of facts and the stage are the deployment's, not the image's: with
// no file no fact holds, and the table closes platform AI while it leaves an
// adult's own key open; with no stage it is "full", closed by default (R25).
// No model has passed the stage full yet, so there no reading is admitted at
// all, by an own key either: the first app passes "author". A fact or a
// provider the table does not name is a refusal of the configuration, since a
// typo would close a route without a word.
package admission

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	table "github.com/altnovatrail/tutor/admission"

	"github.com/altnovatrail/api/internal/config"
)

// Gate answers whether a task that AI checks is admitted for a learner, and
// reads its work by tutor, made once with the same table, facts, stage and
// platform keys, so that the gate and tutor choose alike.
type Gate struct {
	table *table.Table
	facts table.Facts
	stage table.Stage
	// keyed are the providers the platform holds a key of, the only ones whose
	// models platform AI may call.
	keyed []admissionv0.Provider
	tutor *tutor.Tutor
}

// Report is what a start says of admission to the operator: the version of the
// table, whether the deployment gave a file of facts, and the stage and whether
// it named it. It names no provider, fact or path.
type Report struct {
	TableVersion string
	FactsFile    bool
	Rung         string
	RungSet      bool
	// PlatformKeys is how many providers the platform holds a key of.
	PlatformKeys int
}

// Load is the gate of the table of spec and of what cfg declares. A file that
// cannot be read, or that tutor refuses, is a refusal of the configuration,
// config.ErrInvalid, which names the variable and what tutor said, never the
// file's contents beyond the provider or the fact it does not know. A table
// that tutor cannot read is a fault of the release of spec.
func Load(cfg config.Admission) (*Gate, Report, error) {
	t, err := table.Platform()
	if err != nil {
		return nil, Report{}, err
	}
	// Not nil: Select reads nil providers as no bound, and no key is no provider.
	g := &Gate{table: t, stage: table.Stage(cfg.Rung), keyed: []admissionv0.Provider{}}
	if cfg.FactsFile != "" {
		data, err := os.ReadFile(cfg.FactsFile)
		if err != nil {
			return nil, Report{}, fmt.Errorf("%w: %s: %w", config.ErrInvalid, config.VarAdmissionFactsFile, err)
		}
		if g.facts, err = table.ReadFacts(t, data); err != nil {
			return nil, Report{}, fmt.Errorf("%w: %s: %w", config.ErrInvalid, config.VarAdmissionFactsFile, err)
		}
	}
	keys := map[admissionv0.Provider]string{}
	for p, key := range cfg.Keys.ByProvider() {
		keys[admissionv0.Provider(p)] = key.Reveal()
		g.keyed = append(g.keyed, admissionv0.Provider(p))
	}
	slices.Sort(g.keyed)
	if g.tutor, err = tutor.New(tutor.Config{Table: t, Facts: g.facts, Least: g.stage, Keys: keys}); err != nil {
		return nil, Report{}, err
	}
	return g, Report{TableVersion: t.Version(), FactsFile: cfg.FactsFile != "", Rung: cfg.Rung, RungSet: cfg.RungSet,
		PlatformKeys: len(g.keyed)}, nil
}

// Handwriting says whether the reading of handwriting is admitted for a learner
// of category: by platform AI, or, when ownKeyProvider names one, by the own
// key of that provider alone. It is admitted when Select chooses an entry, all
// of whose models the table admits at the deployment's stage or above. On
// platform AI only the providers the platform holds a key of serve, as tutor's
// Choose has it; the reading asks again (R46).
func (g *Gate) Handwriting(category formatv0.AgeCategory, ownKeyProvider string) bool {
	route, providers := admissionv0.AiRoutePlatformAi, g.keyed
	if ownKeyProvider != "" {
		route, providers = admissionv0.AiRouteOwnKey, []admissionv0.Provider{admissionv0.Provider(ownKeyProvider)}
	}
	entry, _ := g.table.Select(g.facts, admissionv0.CapabilityHandwritingReading, route,
		admissionv0.AgeCategory(category), g.stage, providers)
	return len(entry.Models) > 0
}

// ReadSheet reads the work of a handwriting sheet by tutor's two readers: the
// observation of each, or the outcome of a call that gave none.
func (g *Gate) ReadSheet(ctx context.Context, r tutor.SheetReading) tutor.SheetRead {
	return g.tutor.ReadSheet(ctx, r)
}
