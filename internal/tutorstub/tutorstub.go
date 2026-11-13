// Package tutorstub is the stub of tutor in api's dev mode (R66; R146 spec): it
// stands behind the ports through which api calls tutor — the reading of a
// sheet and the tutor's words — so that a run of the path on the dev projects
// calls no provider and costs nothing. Admission and the prices stay tutor's
// own, on the real table, by the gate the dev mode builds with the stub's
// adapter (admission.StubAdapters); only the calls are the stub's.
//
// The stub reads a sheet by the shape of its crop: wider than tall is the right
// value, taller than wide a wrong one — the slot's first trap value, or the
// first value its check holds not right —, and square is illegible. It finds
// the values by module-kit's verdict on the instance, the key with it, which
// the stub alone is given: tutor sees the open view (R67 spec). Its words are
// questions of its own, which pass the leak check and the form check. Each call
// spends what tutor expects a call of its kind to cost on its route, so that the
// quota runs out as on the models.
//
// api takes the stub off a deployment alone: the configuration refuses it
// there, as R32 refuses the emulator of the sign-in service.
package tutorstub

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"slices"
	"strconv"
	"strings"

	"github.com/altnovatrail/module-kit/verdict"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"

	"github.com/altnovatrail/api/internal/quota"
)

// Model is the model the stub's usage names in the log's line ai_usage, so that
// no line of it is taken for a model's.
const Model = "stub"

// provider is the provider of the models the admission table admits, which the
// stub stands for.
const provider = "anthropic"

// readers is how many readers read a sheet: tutor's pair (R68 spec).
const readers = 2

// mostTried is the largest whole number the stub tries as a value, from 0, when
// neither the key nor an option gives one.
const mostTried = 999

// wholeCrop is the far edge of a crop box, in thousandths of the crop: a box
// from 0 to it is the whole crop.
const wholeCrop = 1000

// Gate is what the stub asks of the gate of admission: whether the tutor's
// words are admitted, and what a call is expected to cost.
type Gate interface {
	Words(withWork bool, category formatv0.AgeCategory, ownKeyProvider string) bool
	Price(call tutor.Call, route tutor.Route) tutor.CallPrice
}

// Stub reads sheets and words the tutor's words in place of tutor.
type Stub struct {
	gate Gate
}

// New is the stub over gate, the dev mode's gate of admission.
func New(gate Gate) *Stub { return &Stub{gate: gate} }

// ReadSheet reads the crop of r by its shape, on task, the instance whose open
// view r holds: both readers read the same, so module-kit joins them as they
// are. A value stands with the crop box of the whole crop, in thousandths, and a
// reason with none, as an observation has them (R53 spec). A task with no answer
// slot bound anywhere, a crop that is no PNG, or a request whose context ended
// while the stub looked for a value, fails, as tutor's reading does.
func (s *Stub) ReadSheet(ctx context.Context, r tutor.SheetReading, task formatv0.Task) tutor.SheetRead {
	cfg, err := png.DecodeConfig(bytes.NewReader(r.Crop))
	if err != nil {
		return failed(err)
	}
	item, slot, ok := anywhere(task)
	if !ok {
		return failed(errors.New("tutorstub: the task has no answer slot bound anywhere"))
	}
	value, found := "", false
	switch checked := verdict.AnswerSlots(task.Items[item])[slot]; {
	case cfg.Width > cfg.Height:
		value, found = right(ctx, checked)
	case cfg.Height > cfg.Width:
		value, found = wrong(ctx, checked)
	}
	if err := ctx.Err(); err != nil {
		return failed(err)
	}
	answer := &observationv1.AnswerSlotObservation{
		AnswerSlot: &observationv1.AnswerSlotPlace{Item: int32(item + 1), Slot: int32(slot + 1)}}
	if found {
		answer.Read = &observationv1.AnswerSlotObservation_Value{Value: value}
		answer.CropBox = &observationv1.CropBox{Ymax: wholeCrop, Xmax: wholeCrop}
	} else {
		answer.Read = &observationv1.AnswerSlotObservation_Unreadable{Unreadable: observationv1.Unreadable_UNREADABLE_ILLEGIBLE}
	}
	read := tutor.SheetRead{Outcome: tutor.Outcome{Kind: tutor.Answered,
		Usage: s.usage(tutor.CallSheet, r.Route, readers)}}
	for range readers {
		read.Observations = append(read.Observations, &observationv1.Observation{
			AnswerSlots: []*observationv1.AnswerSlotObservation{answer}})
	}
	return read
}

// Words says whether the tutor's words are admitted, as the gate says.
func (s *Stub) Words(withWork bool, category formatv0.AgeCategory, ownKeyProvider string) bool {
	return s.gate.Words(withWork, category, ownKeyProvider)
}

// TellWords words a question of the stub's own in the language of w: the first
// one of its list that w has not asked yet, so "explain differently" gets other
// words, and, once all were asked, the list again from its start. A language the
// stub has no list of gets English, which the form check turns down into the
// rung's fallback text.
func (s *Stub) TellWords(_ context.Context, w tutor.WordsRequest) tutor.WordsAnswer {
	list := questions["en"]
	if l, ok := questions[base(w.Language)]; ok {
		list = l
	}
	question := list[len(w.Asked)%len(list)]
	for _, q := range list {
		if !slices.Contains(w.Asked, q) {
			question = q
			break
		}
	}
	return tutor.WordsAnswer{Question: question, Outcome: tutor.Outcome{Kind: tutor.Answered,
		Usage: s.usage(tutor.CallWords, w.Route, 1)}}
}

// questions are the stub's words, by language: one question each, without a
// number, as the form check and the leak check take them.
var questions = map[string][]string{
	"en": {
		"Which step of your work would you check first?",
		"What does the task ask you to find?",
		"What is given in the task, in your own words?",
	},
	"ru": {
		"Какой шаг своего решения ты проверишь первым?",
		"Что нужно найти в этой задаче?",
		"Что дано в задаче, если сказать своими словами?",
	},
}

// usage is what the call spends: what tutor expects a call of its kind to cost
// on route, the reserve the quota took for it, shared by the models that answer
// in whole micro-dollars, each billed, and priced when tutor gave the price —
// with none, nothing priced, and the quota keeps the reserve as spent (R12). So
// the settlement spends the reserve to the micro-dollar, and the quota runs out
// after as many calls as on the models (R58).
func (s *Stub) usage(call tutor.Call, route tutor.Route, models int) []tutor.Usage {
	price := s.gate.Price(call, route)
	var total quota.Micro
	if price.Expected {
		total = quota.USD(price.ExpectedUSD)
	}
	usage := make([]tutor.Usage, models)
	for i := range usage {
		share := total / quota.Micro(models)
		if i < int(total%quota.Micro(models)) {
			share++
		}
		usage[i] = tutor.Usage{Provider: provider, Model: Model, Billed: true, USD: share.Dollars(), Priced: price.Expected}
	}
	return usage
}

// failed is a reading that gave no observation.
func failed(err error) tutor.SheetRead {
	return tutor.SheetRead{Outcome: tutor.Outcome{Kind: tutor.Failed, Err: err}}
}

// anywhere is the place of the one answer slot of task bound anywhere, from 0:
// the slot tutor reads.
func anywhere(task formatv0.Task) (item, slot int, ok bool) {
	for i, it := range task.Items {
		for j, s := range it.AnswerSlots {
			if s.Binding == formatv0.AnswerSlotBindingAnywhere {
				return i, j, true
			}
		}
	}
	return 0, 0, false
}

// right is the first value the check of slot holds right, of candidates.
func right(ctx context.Context, slot verdict.AnswerSlot) (string, bool) {
	for _, value := range candidates(slot) {
		if v, err := verdict.OfSlot(ctx, slot, &value); err == nil && v.Verdict == verdict.Correct {
			return value, true
		}
	}
	return "", false
}

// wrong is the first trap value the check of slot holds not right, and with
// none, the first value of candidates it holds so.
func wrong(ctx context.Context, slot verdict.AnswerSlot) (string, bool) {
	var traps []string
	for _, t := range slot.Check.TrapValues {
		traps = append(traps, t.Value)
	}
	for _, values := range [][]string{traps, candidates(slot)} {
		for _, value := range values {
			if v, err := verdict.OfSlot(ctx, slot, &value); err == nil && v.Verdict == verdict.NotYet {
				return value, true
			}
		}
	}
	return "", false
}

// candidates are the values the stub tries, in order: the key, the options of a
// choice written by hand, and the whole numbers from 0 to mostTried, which a
// predicate is written on.
func candidates(slot verdict.AnswerSlot) []string {
	var values []string
	if slot.Check.Key != nil {
		values = append(values, *slot.Check.Key)
	}
	for _, o := range slot.Check.Options {
		values = append(values, string(o))
	}
	for n := range mostTried + 1 {
		values = append(values, strconv.Itoa(n))
	}
	return values
}

// base is the first subtag of a language's tag, lowercase: "ru" of "ru-RU".
func base(language string) string {
	tag, _, _ := strings.Cut(strings.ReplaceAll(language, "_", "-"), "-")
	return strings.ToLower(tag)
}
