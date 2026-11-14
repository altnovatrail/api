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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/altnovatrail/spec"
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
	// providers are the providers of the table, in its order.
	providers []Provider
}

// Provider is a provider of the admission table: its code and its name, as
// the table writes them.
type Provider struct {
	Code admissionv0.Provider
	Name string
}

// ownKeyCalls are the kinds of call of tutor that api makes: the reading of a
// sheet, the hint without the work and the words with it (R54, R68) — the
// hint before the words, whose choice asks the hint's too. An own key is open
// when tutor admits it for one of them at least (R72).
var ownKeyCalls = []tutor.Call{tutor.CallSheet, tutor.CallHint, tutor.CallWords}

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
	// Stub says whether tutor's calls go to the stub of the dev mode (R66).
	Stub bool
}

// stubKey stands for the platform's key of Anthropic in the dev mode's stub,
// so that tutor's choice takes the route of platform AI: the stub's adapter
// takes no key, and this one goes nowhere (R66).
const stubKey = "stub"

// errStub is what the stub's adapter answers a call: it calls no provider.
var errStub = errors.New("admission: the stub of tutor calls no provider")

// StubAdapters are the adapters of the dev mode's stub (R66): Anthropic's, by
// the client of its catalog made with no key, whose names and prices tutor
// reads for the choice and the price as it does from any adapter, while the
// calls are refused before any network. So tutor admits, chooses and prices on
// the real table, and nothing of it reaches a provider. The check of an own
// key answers by how the key ends (stubModel.Check), so that Tutor.CheckKey
// gives every end of a check with no call.
func StubAdapters() map[admissionv0.Provider]provider.Dial {
	return map[admissionv0.Provider]provider.Dial{"anthropic": func(id, key string) (provider.Model, error) {
		m, err := claude.Dial(id, "")
		if err != nil {
			return nil, err
		}
		return stubModel{Model: m, checked: stubCheck(key)}, nil
	}}
}

// The endings of a synthetic key that the stub's check of a key reads (R66);
// a key that ends in none of them is accepted. README.md names them for app.
const (
	StubKeyInvalid = "-invalid"
	StubKeyNoPerm  = "-noperm"
	StubKeySilent  = "-silent"
)

// stubCheck is how the stub's check of key ends, by its ending, as a provider
// answers the check: the key refused, a key with no rights to the model, a
// failure of neither kind — a provider that gave no answer —, or nil, the key
// taken.
func stubCheck(key string) error {
	switch {
	case strings.HasSuffix(key, StubKeyInvalid):
		return fmt.Errorf("%w: %w", errStub, provider.ErrKeyRefused)
	case strings.HasSuffix(key, StubKeyNoPerm):
		return fmt.Errorf("%w: %w", errStub, provider.ErrNoRights)
	case strings.HasSuffix(key, StubKeySilent):
		return errStub
	}
	return nil
}

// stubModel is a model of the catalog whose calls are refused, and whose check
// of a key ends as stubCheck read the key it was dialled with: the model keeps
// the end, never the key.
type stubModel struct {
	provider.Model
	checked error
}

func (stubModel) Ask(context.Context, provider.Request) (provider.Response, error) {
	return provider.Response{}, errStub
}

func (m stubModel) Check(context.Context) error { return m.checked }

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
	adapters := Adapters()
	if cfg.Stub {
		// The configuration holds no platform key beside the stub (R66).
		adapters, keys = StubAdapters(), map[admissionv0.Provider]string{"anthropic": stubKey}
	}
	g := &Gate{}
	if g.providers, err = tableProviders(); err != nil {
		return nil, Report{}, err
	}
	if g.tutor, err = tutor.New(tutor.Config{Table: t, Facts: facts, Least: table.Stage(cfg.Rung), Keys: keys,
		Adapters: adapters}); err != nil {
		// What tutor.New refuses is what cfg declares: a stage, or a key of a
		// provider api gives no adapter of.
		return nil, Report{}, fmt.Errorf("%w: %w", config.ErrInvalid, err)
	}
	return g, Report{TableVersion: t.Version(), FactsFile: cfg.FactsFile != "", Rung: cfg.Rung, RungSet: cfg.RungSet,
		PlatformKeys: len(cfg.Keys.ByProvider()), Stub: cfg.Stub}, nil
}

// tableProviders are the providers of the table that tutor reads,
// admission-table.json of spec.Admission (readProviders). A table it cannot
// read is a fault of the release of spec, as table.Platform's.
func tableProviders() ([]Provider, error) {
	data, err := fs.ReadFile(spec.Admission, "admission-table.json")
	if err != nil {
		return nil, fmt.Errorf("admission: %w", err)
	}
	return readProviders(data)
}

// readProviders reads the providers of a table, in the order its JSON writes
// them: tutor's table keeps them in a map, and the contract gives them in the
// table's order (ListOwnKeyProviders). The table follows its schema already,
// as tutor read it, so a member of another form is a fault of the release.
func readProviders(data []byte) ([]Provider, error) {
	var raw struct {
		Providers json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("admission: the table: %w", err)
	}
	d := json.NewDecoder(bytes.NewReader(raw.Providers))
	switch open, err := d.Token(); {
	case err != nil:
		return nil, fmt.Errorf("admission: the providers of the table: %w", err)
	case open != json.Delim('{'):
		return nil, errors.New("admission: the providers of the table are no object")
	}
	var out []Provider
	for d.More() {
		// The key of a member is a string, or Token fails.
		code, err := d.Token()
		var p admissionv0.AiProvider
		if err == nil {
			err = d.Decode(&p)
		}
		if err != nil {
			return nil, fmt.Errorf("admission: the providers of the table: %w", err)
		}
		key, _ := code.(string)
		out = append(out, Provider{Code: admissionv0.Provider(key), Name: p.Name})
	}
	return out, nil
}

// OwnKeyOpen says whether the own key of provider is open at a learner of
// category: whether tutor chooses, on the route of that key, an entry for one
// of the kinds of call api makes at least (R72). A provider api gives no
// adapter of, or the table does not know, is closed; so is none. The list of
// ListOwnKeyProviders and the check of a key ask it alike, so that a provider
// out of the list is closed to the check with no call.
func (g *Gate) OwnKeyOpen(category formatv0.AgeCategory, provider string) bool {
	if provider == "" {
		return false
	}
	route := g.route(category, provider)
	for _, call := range ownKeyCalls {
		if g.chooses(call, route) {
			return true
		}
	}
	return false
}

// checkReserve is the part of a request's time kept after the check of a key,
// to answer how it ended: the check ends that much before the request's
// deadline, so that a provider that does not answer in time is an outcome, not
// a deadline exceeded.
const checkReserve = time.Second

// ErrTooLittleTime is a check of a key that would not begin: the request has
// less than twice checkReserve left, and the provider would be blamed for the
// client's short deadline.
var ErrTooLittleTime = errors.New("admission: too little time to check the key")

// CheckKey checks the own key of provider at a learner of category through
// tutor, which checks it for every model it would call with it, each by a
// request with nothing of a learner's (Tutor.CheckKey; R84 spec, R96 tutor).
// A provider closed to the key (OwnKeyOpen) is KeyProviderClosed with no call,
// so that the check and the list of ListOwnKeyProviders do not part (R72). The
// check ends checkReserve before ctx's deadline, and with less than twice that
// left it does not begin: KeyUnanswered and ErrTooLittleTime. The error says
// what failed, for the log, and never holds the key.
func (g *Gate) CheckKey(ctx context.Context, category formatv0.AgeCategory, provider, key string) (tutor.KeyCheck, error) {
	if !g.OwnKeyOpen(category, provider) {
		return tutor.KeyProviderClosed, nil
	}
	if deadline, ok := ctx.Deadline(); ok {
		if time.Until(deadline) < 2*checkReserve {
			return tutor.KeyUnanswered, ErrTooLittleTime
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline.Add(-checkReserve))
		defer cancel()
	}
	return g.tutor.CheckKey(ctx, tutor.Route{AI: admissionv0.AiRouteOwnKey, Category: admissionv0.AgeCategory(category),
		Own: tutor.NewOwnKey(admissionv0.Provider(provider), key)})
}

// OwnKeyProviders are the providers of the table, in its order, whose own key
// is open at a learner of category (OwnKeyOpen): none at a category the table
// opens no own key to.
func (g *Gate) OwnKeyProviders(category formatv0.AgeCategory) []Provider {
	var out []Provider
	for _, p := range g.providers {
		if g.OwnKeyOpen(category, string(p.Code)) {
			out = append(out, p)
		}
	}
	return out
}

// Handwriting says whether the reading of handwriting is admitted for a learner
// of category: by platform AI, or, when ownKeyProvider names one, by the own
// key of that provider alone. It is admitted when tutor chooses, for the call
// that reads a sheet, an entry all of whose models the table admits at the
// deployment's stage or above. On platform AI only the providers the platform
// holds a key of serve, and on either route only those api gives an adapter
// of; the reading asks again (R46).
func (g *Gate) Handwriting(category formatv0.AgeCategory, ownKeyProvider string) bool {
	return g.chooses(tutor.CallSheet, g.route(category, ownKeyProvider))
}

// Words says whether the tutor's words are admitted for a learner of category,
// on the route Handwriting takes: the hint of a rung by the call of the hint
// without the work, and, with the work, the call of words, which serves the
// analysis of its steps and the hint at once, and which tutor admits only with
// the hint on the model of the analysis (R84 spec; R106, R111 tutor). tutor
// asks again, of the very model.
func (g *Gate) Words(withWork bool, category formatv0.AgeCategory, ownKeyProvider string) bool {
	return g.chooses(WordsCall(withWork), g.route(category, ownKeyProvider))
}

// WordsCall is tutor's kind of call of the words of a rung: the words with the
// work, which analyse its steps too, or the hint without it — "explain
// differently" and a task a tap checks (R106 tutor). The gate admits, and the
// quota reserves, by it.
func WordsCall(withWork bool) tutor.Call {
	if withWork {
		return tutor.CallWords
	}
	return tutor.CallHint
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

// Price is what tutor expects a call of the kind to cost on the route, by the
// models it would choose for it and their prices (Tutor.Price; R94 tutor): the
// quota reserves by it before the call (R12). A route tutor admits no model for,
// or a price it cannot give, is no price — CallPrice's Expected is false — and
// the quota reserves its own value of the kind then.
func (g *Gate) Price(call tutor.Call, route tutor.Route) tutor.CallPrice {
	price, answer, err := g.tutor.Price(call, route)
	if err != nil || !answer.Admitted {
		return tutor.CallPrice{}
	}
	return price
}

// chooses says whether tutor chooses, for the call on the route, an entry with
// models: what the gate means by "admitted".
func (g *Gate) chooses(call tutor.Call, route tutor.Route) bool {
	entry, _, err := g.tutor.ChooseCall(call, route)
	return err == nil && len(entry.Models) > 0
}
