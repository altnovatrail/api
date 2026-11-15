// Package tutorstub is the stub of tutor in api's dev mode (R66; R146 spec): it
// stands behind the ports through which api calls tutor — the reading of a
// sheet and the tutor's words — so that a run of the path on the dev projects
// calls no provider and costs nothing. Admission and the prices stay tutor's
// own, on the real table, by the gate the dev mode builds with the stub's
// adapter (admission.StubAdapters); only the calls are the stub's.
//
// The stub reads a sheet by the shape of its crop: wider than tall is the right
// value, taller than wide a wrong one — the slot's first trap value, or the
// first value its check holds not right —, and square is illegible. It reads
// each filled box of a workbook page the same way, by the shape of the box's
// crop, and admits that reading itself wherever the gate admits a sheet's, for
// the same category and route: tutor v0.1.35 admits it nowhere (В72). It finds
// the values by module-kit's verdict on the instance, the key with it, which
// the stub alone is given: tutor sees the open view (R67 spec). Its words — a
// rung's questions and the praise after a correct answer — are its own, which
// pass the leak check and the form check. Each call spends what tutor expects a
// call of its kind to cost on its route, so that the quota runs out as on the
// models.
//
// On an own key a call ends by how the key ends, as a provider would refuse it
// (KeyRefused and the endings beside it): api hands the stub the key's value
// beside the call, which tutor takes from the route alone, and the stub reads
// its ending and keeps nothing of it.
//
// api takes the stub off a deployment alone: the configuration refuses it
// there, as R32 refuses the emulator of the sign-in service.
package tutorstub

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"slices"
	"strconv"
	"strings"

	"github.com/altnovatrail/module-kit/verdict"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	tutorprovider "github.com/altnovatrail/tutor/provider"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/admission"
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

// The endings of a synthetic own key that a call of AI on the stub reads, past
// the checks of the request's form, as a provider would answer it (the
// 2026-11-15 amendment to R66): the key refused, or a limit — named by none of
// tutor's reasons, or by one. A key that ends in none of them calls the stub as
// platform AI does. README.md names them for app; CheckOwnKey takes them all.
const (
	KeyRefused      = "-refused"
	KeyLimit        = "-limit"
	KeyLimitOwner   = "-limit-owner"
	KeyLimitTier    = "-limit-tier"
	KeyLimitBalance = "-limit-balance"
	KeyLimitPayment = "-limit-payment"
)

// limits is tutor's reason of each ending of a limit, the longest ending first,
// so that an ending is never taken for a shorter one it ends in.
var limits = []limitEnding{
	{KeyLimitBalance, tutor.LimitBalance},
	{KeyLimitPayment, tutor.LimitBilling},
	{KeyLimitOwner, tutor.LimitOwner},
	{KeyLimitTier, tutor.LimitTier},
	{KeyLimit, ""},
}

// limitEnding is an ending of a limit and tutor's reason of it.
type limitEnding struct {
	ending string
	limit  tutor.LimitReason
}

// errRefused is what the stub's provider says when it refuses a call.
var errRefused = errors.New("tutorstub: the stub refused the call by the ending of the own key")

// refusal is what a call on the own key own comes to by its ending, as tutor
// gives a provider's refusal: KeyRefused, or LimitReached with tutor's reason of
// the ending, with no usage, since a refused call has no tokens; ok is false
// for a key of no such ending, and on platform AI, whose own is empty. A request
// whose context ended fails instead, as a call cut short does. Neither the
// outcome nor its error holds the key.
func refusal(ctx context.Context, own string) (o tutor.Outcome, ok bool) {
	switch i := slices.IndexFunc(limits, func(l limitEnding) bool { return strings.HasSuffix(own, l.ending) }); {
	case strings.HasSuffix(own, KeyRefused):
		o = tutor.Outcome{Kind: tutor.KeyRefused, Err: fmt.Errorf("%w: %w", errRefused, tutorprovider.ErrKeyRefused)}
	case i >= 0:
		o = tutor.Outcome{Kind: tutor.LimitReached, Limit: limits[i].limit,
			Err: &tutorprovider.LimitError{Limit: limits[i].limit, Err: errRefused}}
	default:
		return tutor.Outcome{}, false
	}
	if err := ctx.Err(); err != nil {
		return tutor.Outcome{Kind: tutor.Failed, Err: err}, true
	}
	return o, true
}

// Gate is what the stub asks of the gate of admission: whether a reading and
// the tutor's words are admitted, and what a call is expected to cost.
type Gate interface {
	Reads(call tutor.Call, category formatv0.AgeCategory, ownKeyProvider string) bool
	Words(withWork bool, category formatv0.AgeCategory, ownKeyProvider string) bool
	CorrectWords(category formatv0.AgeCategory, ownKeyProvider string) bool
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
// while the stub looked for a value, fails, as tutor's reading does. Past those
// checks an own key of an ending of the stub's is refused by it, own being its
// value.
func (s *Stub) ReadSheet(ctx context.Context, r tutor.SheetReading, task formatv0.Task, own string) tutor.SheetRead {
	cfg, err := png.DecodeConfig(bytes.NewReader(r.Crop))
	if err != nil {
		return failed(err)
	}
	item, slot, ok := anywhere(task)
	if !ok {
		return failed(errors.New("tutorstub: the task has no answer slot bound anywhere"))
	}
	if o, refused := refusal(ctx, own); refused {
		return tutor.SheetRead{Outcome: o}
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

// Reads says whether the reading by the call of the kind is admitted for a
// learner of category, on the route of ownKeyProvider: the reading of a
// workbook page wherever the gate admits a sheet's, CallSheet, for the same
// category and route, since no entry of tutor v0.1.35 admits it (В72; the
// 2026-11-15 amendment to R66), and any other kind as the gate says.
func (s *Stub) Reads(call tutor.Call, category formatv0.AgeCategory, ownKeyProvider string) bool {
	if call == tutor.CallWorkbook {
		call = tutor.CallSheet
	}
	return s.gate.Reads(call, category, ownKeyProvider)
}

// ReadWorkbook reads the filled boxes of the workbook page of r, on task, the
// instance whose open view r holds, each by the shape of its crop as ReadSheet
// reads a sheet's: wider than tall the box's right value, taller than wide a
// wrong one, square illegible. The observation holds every box of the page in
// the task's order, a box that sent no crop unreadable, blank, and a value with
// no crop box, as tutor's does; both readers read the same. Of tutor's checks
// before its call the stub keeps those of the request's form: a task that is no
// workbook page, a crop of a place the page has no box of or of one box twice,
// more than maxBoxes crops, or a crop that is no PNG or is larger than
// maxBoxSide on a side fails, as does a request whose context ended while the
// stub looked for the values. A page with no crop goes to no model and spends
// nothing, as tutor's does; past those checks an own key of an ending of the
// stub's, own being its value, is refused by it. The call spends the expected
// price of CallWorkbook on the route, which tutor v0.1.35 does not give: none,
// so the quota keeps its reserve by the value of the kind as spent (R12, R81).
func (s *Stub) ReadWorkbook(ctx context.Context, r tutor.WorkbookReading, task formatv0.Task, own string) tutor.WorkbookRead {
	fail := func(err error) tutor.WorkbookRead {
		return tutor.WorkbookRead{Outcome: tutor.Outcome{Kind: tutor.Failed, Err: err}}
	}
	places, boxes, err := boxesOf(task)
	if err != nil {
		return fail(err)
	}
	if len(r.Boxes) > maxBoxes {
		return fail(fmt.Errorf("tutorstub: %d filled boxes, more than the %d a reading shows", len(r.Boxes), maxBoxes))
	}
	read := make(map[box]*observationv1.AnswerSlotObservation, len(r.Boxes))
	shapes := make(map[box]image.Config, len(r.Boxes))
	for _, b := range r.Boxes {
		at := box{b.Item, b.Slot}
		_, onPage := boxes[at]
		if _, twice := shapes[at]; !onPage || twice {
			return fail(fmt.Errorf("tutorstub: no box %d of item %d on the page, or two crops of it", b.Slot, b.Item))
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(b.Crop))
		if err == nil && (cfg.Width > maxBoxSide || cfg.Height > maxBoxSide) {
			err = fmt.Errorf("%d × %d pixels, more than %d on a side of a box", cfg.Width, cfg.Height, maxBoxSide)
		}
		if err != nil {
			return fail(fmt.Errorf("tutorstub: the crop of item %d, box %d: %w", b.Item, b.Slot, err))
		}
		shapes[at] = cfg
	}
	if len(r.Boxes) > 0 {
		if o, refused := refusal(ctx, own); refused {
			return tutor.WorkbookRead{Outcome: o}
		}
	}
	for at, cfg := range shapes {
		value, found := "", false
		switch slot := boxes[at]; {
		case cfg.Width > cfg.Height:
			value, found = right(ctx, slot)
		case cfg.Height > cfg.Width:
			value, found = wrong(ctx, slot)
		}
		answer := &observationv1.AnswerSlotObservation{Read: &observationv1.AnswerSlotObservation_Unreadable{
			Unreadable: observationv1.Unreadable_UNREADABLE_ILLEGIBLE}}
		if found {
			answer.Read = &observationv1.AnswerSlotObservation_Value{Value: value}
		}
		read[at] = answer
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	page := &observationv1.Observation{}
	for _, at := range places {
		answer, ok := read[at]
		if !ok {
			answer = &observationv1.AnswerSlotObservation{Read: &observationv1.AnswerSlotObservation_Unreadable{
				Unreadable: observationv1.Unreadable_UNREADABLE_BLANK}}
		}
		answer.AnswerSlot = &observationv1.AnswerSlotPlace{Item: int32(at.item), Slot: int32(at.slot)}
		page.AnswerSlots = append(page.AnswerSlots, answer)
	}
	out := tutor.WorkbookRead{Outcome: tutor.Outcome{Kind: tutor.Answered}}
	if len(r.Boxes) > 0 {
		out.Outcome.Usage = s.usage(tutor.CallWorkbook, r.Route, readers)
	}
	for range readers {
		out.Observations = append(out.Observations, proto.CloneOf(page))
	}
	return out
}

// The bounds of a reading of a workbook page, as tutor's ReadWorkbook takes
// them: the most filled boxes, and the longest side of a box's crop in pixels.
const (
	maxBoxes   = 20
	maxBoxSide = 512
)

// box is the place of a box of a workbook page: its item and its place among
// the item's boxes, from 1, as tutor's BoxCrop names it.
type box struct{ item, slot int }

// boxesOf is the places of the boxes of task, a workbook page, in the task's
// order, and the answer slot of each: each answer slot of an item bound box,
// counted among the item's boxes, as tutor counts them. A task that is no
// workbook page, or has no box, is refused, as tutor's reading refuses it.
func boxesOf(task formatv0.Task) ([]box, map[box]verdict.AnswerSlot, error) {
	if task.Canvas.Kind != formatv0.CanvasKindWorkbook {
		return nil, nil, errors.New("tutorstub: the task is no workbook page")
	}
	var places []box
	slots := map[box]verdict.AnswerSlot{}
	for i, item := range task.Items {
		n := 0
		for _, slot := range verdict.AnswerSlots(item) {
			if slot.Binding == string(formatv0.LinePartBindingBox) {
				n++
				places = append(places, box{i + 1, n})
				slots[box{i + 1, n}] = slot
			}
		}
	}
	if len(places) == 0 {
		return nil, nil, errors.New("tutorstub: the workbook page has no box")
	}
	return places, slots, nil
}

// Words says whether the tutor's words are admitted, as the gate says.
func (s *Stub) Words(withWork bool, category formatv0.AgeCategory, ownKeyProvider string) bool {
	return s.gate.Words(withWork, category, ownKeyProvider)
}

// TellWords words a question of the stub's own in the language of w: the first
// one of its list that w has not asked yet, so "explain differently" gets other
// words, and, once all were asked, the list again from its start. A language the
// stub has no list of gets English, which the form check turns down into the
// rung's fallback text. An own key of an ending of the stub's, own being its
// value, is refused by it.
func (s *Stub) TellWords(ctx context.Context, w tutor.WordsRequest, own string) tutor.WordsAnswer {
	if o, refused := refusal(ctx, own); refused {
		return tutor.WordsAnswer{Outcome: o}
	}
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
		Usage: s.usage(admission.WordsCall(len(w.Crop) > 0), w.Route, 1)}}
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

// CorrectWords says whether the tutor's words after a correct answer are
// admitted, as the gate says.
func (s *Stub) CorrectWords(category formatv0.AgeCategory, ownKeyProvider string) bool {
	return s.gate.CorrectWords(category, ownKeyProvider)
}

// TellCorrectWords words the stub's praise in the language of c — of a step of
// the work when the work went, of the option chosen when it did not — with a
// question on the technique that helped as its last sentence, or, when the task
// names no technique, on how the answer was found, as tutor's prompt asks (R112
// tutor). They hold no number and tell no sex, so they pass the leak check and
// words.Correct. A language the stub has no words of gets English. Of tutor's
// checks before its call the stub keeps those of the request's form: no task,
// no style, no language, or a crop that is no PNG fails with no usage; the
// words of the task and the size of the crop it does not read. The call spends
// the expected price of CallCorrect, with the work or without it. Past those
// checks an own key of an ending of the stub's, own being its value, is refused
// by it.
func (s *Stub) TellCorrectWords(ctx context.Context, c tutor.CorrectRequest, own string) tutor.CorrectAnswer {
	fail := func(err error) tutor.CorrectAnswer {
		return tutor.CorrectAnswer{Outcome: tutor.Outcome{Kind: tutor.Failed, Err: err}}
	}
	switch {
	case c.Task == nil:
		return fail(errors.New("tutorstub: the words after a correct answer have no task"))
	case strings.TrimSpace(c.Style) == "":
		return fail(errors.New("tutorstub: the words after a correct answer have no style of presentation"))
	case strings.TrimSpace(c.Language) == "":
		return fail(errors.New("tutorstub: the words after a correct answer have no language"))
	}
	if len(c.Crop) > 0 {
		if _, err := png.DecodeConfig(bytes.NewReader(c.Crop)); err != nil {
			return fail(err)
		}
	}
	if o, refused := refusal(ctx, own); refused {
		return tutor.CorrectAnswer{Outcome: o}
	}
	words, ok := praise[base(c.Language)]
	if !ok {
		words = praise["en"]
	}
	first, question := words.choice, words.technique
	if len(c.Crop) > 0 {
		first = words.step
	}
	if len(c.Techniques) == 0 {
		question = words.found
	}
	return tutor.CorrectAnswer{Words: first + " " + question, Outcome: tutor.Outcome{Kind: tutor.Answered,
		Usage: s.usage(tutor.CallCorrect, c.Route, 1)}}
}

// praise are the stub's words after a correct answer, by language: the praise
// of a step of the work, or of the option chosen, then the question on the
// technique, or on how the answer was found when the task names none.
var praise = map[string]struct{ step, choice, technique, found string }{
	"en": {"This step of the solution is clear and in good order.", "The right option is chosen.",
		"Which technique helped you here?", "How did you find the answer?"},
	"ru": {"Этот шаг решения записан ясно и по порядку.", "Верный вариант выбран.",
		"Какой приём тебе здесь помог?", "Каким путём удалось найти ответ?"},
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
