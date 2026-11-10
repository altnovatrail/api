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
	"fmt"
	"os"

	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/spec/types/formatv0"
	tutor "github.com/altnovatrail/tutor/admission"

	"github.com/altnovatrail/api/internal/config"
)

// Gate answers whether a task that AI checks is admitted for a learner.
type Gate struct {
	table *tutor.Table
	facts tutor.Facts
	stage tutor.Stage
}

// Report is what a start says of admission to the operator: the version of the
// table, whether the deployment gave a file of facts, and the stage and whether
// it named it. It names no provider, fact or path.
type Report struct {
	TableVersion string
	FactsFile    bool
	Rung         string
	RungSet      bool
}

// Load is the gate of the table of spec and of what cfg declares. A file that
// cannot be read, or that tutor refuses, is a refusal of the configuration,
// config.ErrInvalid, which names the variable and what tutor said, never the
// file's contents beyond the provider or the fact it does not know. A table
// that tutor cannot read is a fault of the release of spec.
func Load(cfg config.Admission) (*Gate, Report, error) {
	table, err := tutor.Platform()
	if err != nil {
		return nil, Report{}, err
	}
	g := &Gate{table: table, stage: tutor.Stage(cfg.Rung)}
	if cfg.FactsFile != "" {
		data, err := os.ReadFile(cfg.FactsFile)
		if err != nil {
			return nil, Report{}, fmt.Errorf("%w: %s: %w", config.ErrInvalid, config.VarAdmissionFactsFile, err)
		}
		if g.facts, err = tutor.ReadFacts(table, data); err != nil {
			return nil, Report{}, fmt.Errorf("%w: %s: %w", config.ErrInvalid, config.VarAdmissionFactsFile, err)
		}
	}
	return g, Report{TableVersion: table.Version(), FactsFile: cfg.FactsFile != "", Rung: cfg.Rung, RungSet: cfg.RungSet}, nil
}

// Handwriting says whether the reading of handwriting is admitted for a learner
// of category: by platform AI, or, when ownKeyProvider names one, by the own
// key of that provider alone. It is admitted when Select chooses an entry, all
// of whose models the table admits at the deployment's stage or above. On
// platform AI no provider is left out yet: which providers the platform holds a
// key of, tutor's reading brings (T36), and then the same list goes to Select
// here as in tutor, and the submission asks again.
func (g *Gate) Handwriting(category formatv0.AgeCategory, ownKeyProvider string) bool {
	route, providers := admissionv0.AiRoutePlatformAi, []admissionv0.Provider(nil)
	if ownKeyProvider != "" {
		route, providers = admissionv0.AiRouteOwnKey, []admissionv0.Provider{admissionv0.Provider(ownKeyProvider)}
	}
	entry, _ := g.table.Select(g.facts, admissionv0.CapabilityHandwritingReading, route,
		admissionv0.AgeCategory(category), g.stage, providers)
	return len(entry.Models) > 0
}
