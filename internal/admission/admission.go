// Package admission is the function of admission to AI as api asks it before
// an attempt (R25, R45): the admission table of the release of spec that tutor
// reads, the facts the deployment declares in its file, and the least stage of
// tutor's rule of admission of models. What a task that AI checks may be given
// is what tutor chooses for the call it will make, by the call's prompt
// (Tutor.ChooseCall), and tutor asks the same before its call (R54).
//
// tutor calls a provider only through an adapter the service gives it, and api
// gives only Anthropic's: the admission table admits no model of another
// provider, and an adapter api does not give is an SDK its binary does not hold
// (R54).
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

	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	table "github.com/altnovatrail/tutor/admission"
	"github.com/altnovatrail/tutor/provider"
	"github.com/altnovatrail/tutor/provider/claude"

	"github.com/altnovatrail/api/internal/config"
)

// Adapters are the adapters of the providers api gives tutor: Anthropic's alone
// (R54). Each call makes the map anew, so that no caller changes what a later
// gate gives.
func Adapters() map[admissionv0.Provider]provider.Dial {
	return map[admissionv0.Provider]provider.Dial{"anthropic": claude.Dial}
}

// Gate answers whether a task that AI checks is admitted for a learner, and
// reads its work, by tutor made once with the table, the facts, the stage, the
// platform keys and the adapters: the gate asks the very choice tutor makes
// before its call.
type Gate struct {
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
	var facts table.Facts
	if cfg.FactsFile != "" {
		data, err := os.ReadFile(cfg.FactsFile)
		if err != nil {
			return nil, Report{}, fmt.Errorf("%w: %s: %w", config.ErrInvalid, config.VarAdmissionFactsFile, err)
		}
		if facts, err = table.ReadFacts(t, data); err != nil {
			return nil, Report{}, fmt.Errorf("%w: %s: %w", config.ErrInvalid, config.VarAdmissionFactsFile, err)
		}
	}
	keys := map[admissionv0.Provider]string{}
	for p, key := range cfg.Keys.ByProvider() {
		keys[admissionv0.Provider(p)] = key.Reveal()
	}
	g := &Gate{}
	if g.tutor, err = tutor.New(tutor.Config{Table: t, Facts: facts, Least: table.Stage(cfg.Rung), Keys: keys,
		Adapters: Adapters()}); err != nil {
		// What tutor.New refuses is what cfg declares: a stage, or a key of a
		// provider api gives no adapter of.
		return nil, Report{}, fmt.Errorf("%w: %w", config.ErrInvalid, err)
	}
	return g, Report{TableVersion: t.Version(), FactsFile: cfg.FactsFile != "", Rung: cfg.Rung, RungSet: cfg.RungSet,
		PlatformKeys: len(keys)}, nil
}

// Handwriting says whether the reading of handwriting is admitted for a learner
// of category: by platform AI, or, when ownKeyProvider names one, by the own
// key of that provider alone. It is admitted when tutor chooses, for the call
// that reads a sheet, an entry all of whose models the table admits at the
// deployment's stage or above. On platform AI only the providers the platform
// holds a key of serve, and on either route only those api gives an adapter
// of; the reading asks again (R46).
func (g *Gate) Handwriting(category formatv0.AgeCategory, ownKeyProvider string) bool {
	entry, _, err := g.tutor.ChooseCall(tutor.CallSheet, g.route(category, ownKeyProvider))
	return err == nil && len(entry.Models) > 0
}

// Words says whether the tutor's words are admitted for a learner of category,
// on the route Handwriting takes: the hint of a rung, and, with the work, the
// analysis of its steps too, which the one call of words serves by the very
// model the hint is admitted for (R47). tutor asks again, of the very model.
func (g *Gate) Words(withWork bool, category formatv0.AgeCategory, ownKeyProvider string) bool {
	route := g.route(category, ownKeyProvider)
	hint, _, err := g.tutor.Choose(admissionv0.CapabilityHint, route)
	if err != nil || len(hint.Models) == 0 {
		return false
	}
	if !withWork {
		return true
	}
	analysis, _, err := g.tutor.ChooseCall(tutor.CallWords, route)
	return err == nil && len(analysis.Models) > 0 && analysis.Models[0] == hint.Models[0]
}

// route is tutor's route for a learner of category: platform AI, or the own key
// of ownKeyProvider. The choice reads only the key's provider, never the key,
// which the gate does not hold: a stand-in stands for it.
func (g *Gate) route(category formatv0.AgeCategory, ownKeyProvider string) tutor.Route {
	r := tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategory(category)}
	if ownKeyProvider != "" {
		r.AI, r.Own = admissionv0.AiRouteOwnKey, tutor.NewOwnKey(admissionv0.Provider(ownKeyProvider), "own key")
	}
	return r
}

// ReadSheet reads the work of a handwriting sheet by tutor's two readers: the
// observation of each, or the outcome of a call that gave none.
func (g *Gate) ReadSheet(ctx context.Context, r tutor.SheetReading) tutor.SheetRead {
	return g.tutor.ReadSheet(ctx, r)
}

// TellWords asks tutor for the words of a rung: its question, with the box of
// the first mistake when the work went, or the outcome of a call that gave none.
func (g *Gate) TellWords(ctx context.Context, w tutor.WordsRequest) tutor.WordsAnswer {
	return g.tutor.Words(ctx, w)
}
