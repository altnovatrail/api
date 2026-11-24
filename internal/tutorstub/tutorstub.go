// Package tutorstub is the stub of tutor in api's dev mode: it stands behind
// the ports through which api calls tutor — the readings of a sheet, a workbook
// page and an essay, the assessment of an essay, the tutor's words, the finding
// of the tasks on the photo of a page and the check of a task from paper — so
// that a run of the path on the dev projects calls no provider and costs
// nothing. Admission and the prices stay tutor's own, on the real table, by the
// gate the dev mode builds with the stub's adapter (admission.StubAdapters);
// only the calls are the stub's.
//
// The stub reads a sheet by the shape of its crop: wider than tall is the right
// value, taller than wide a wrong one — the slot's first trap value, or the
// first value its check holds not right —, and square is illegible. It reads
// each filled box of a workbook page the same way, by the shape of the box's
// crop. It finds the values by module-kit's verdict on the instance, the key
// with it, which the stub alone is given: tutor sees the open view. It reads an
// essay by the shape of the sheet's crop too — wider than tall a text of its
// own, taller than wide a blank sheet, square illegible —, and assesses the
// text every criterion met but the last. It finds the tasks on the photo of a
// page by its shape as well: taller than wide five tasks, one under another,
// wider than tall none, and square a failure of AI. A task from paper it
// reads for "Recognise" by the shape of its statement — wider than tall one
// task, at least twice as tall as wide three, otherwise one word problem — and
// checks by the shape of the work, which takes in the tracing paper, on as many
// tasks as it is told, or, told none, as many as "Recognise" reads: more than
// twice as wide as tall every task correct, wider but not twice the first not
// yet, square every task so far correct, taller but not twice unreadable, and
// at least twice as tall nothing written. Its words — a rung's questions, the
// Check's words and ladder, the praise after a correct answer and the feedback
// on an essay — are its own, which pass the leak check and the form check.
// Each call spends what tutor expects a call of its kind to cost on its route,
// so that the quota runs out as on the models.
//
// On an own key a call ends by how the key ends, as a provider would refuse it
// (KeyRefused and the endings beside it): api hands the stub the key's value
// beside the call, which tutor takes from the route alone, and the stub reads
// its ending and keeps nothing of it.
//
// api takes the stub off a deployment alone: the configuration refuses it
// there, as api refuses the emulator of the sign-in service.
package tutorstub

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	// The JPEG of the photo of a page, which image.DecodeConfig reads.
	_ "image/jpeg"
	"image/png"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/altnovatrail/module-kit/verdict"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	tutorprovider "github.com/altnovatrail/tutor/provider"
	"github.com/altnovatrail/tutor/words"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
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

// mostTried is the largest whole number the stub tries as a value, from 0, when
// neither the key nor an option gives one.
const mostTried = 999

// wholeCrop is the far edge of a crop box, in thousandths of the crop: a box
// from 0 to it is the whole crop.
const wholeCrop = 1000

// The endings of a synthetic own key that a call of AI on the stub reads, past
// the checks of the request's form, as a provider would answer it: the key
// refused, or a limit — named by none of tutor's reasons, or by one; the
// ceiling of the tier with when access returns, as tutor's adapter of Claude
// gives it. A key that ends in none of them calls the stub as platform AI does.
// docs/build.md names them for app; CheckOwnKey takes them all.
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
// gives a provider's refusal: KeyRefused, or LimitReached with tutor's reason
// of the ending and, for the tier, when access returns by the wall clock — no
// other limit of Anthropic's says it but in its words —, with no usage, since a
// refused call has no tokens; ok is false for a key of no such ending, and on
// platform AI, whose own is empty. A request whose context ended fails instead,
// as a call cut short does. Neither the outcome nor its error holds the key.
func refusal(ctx context.Context, own string) (o tutor.Outcome, ok bool) {
	switch i := slices.IndexFunc(limits, func(l limitEnding) bool { return strings.HasSuffix(own, l.ending) }); {
	case strings.HasSuffix(own, KeyRefused):
		o = tutor.Outcome{Kind: tutor.KeyRefused, Err: fmt.Errorf("%w: %w", errRefused, tutorprovider.ErrKeyRefused)}
	case i >= 0:
		var returns time.Time
		if limits[i].limit == tutor.LimitTier {
			returns = nextMonth(time.Now())
		}
		o = tutor.Outcome{Kind: tutor.LimitReached, Limit: limits[i].limit, AccessReturns: returns,
			Err: &tutorprovider.LimitError{Limit: limits[i].limit, Returns: returns, Err: errRefused}}
	default:
		return tutor.Outcome{}, false
	}
	if err := ctx.Err(); err != nil {
		return tutor.Outcome{Kind: tutor.Failed, Err: err}, true
	}
	return o, true
}

// nextMonth is 00:00 UTC on the first day of the month after the one at falls
// on, in UTC: when the ceiling of Anthropic's tier lets a key go, as tutor's
// adapter of Claude reckons it.
func nextMonth(at time.Time) time.Time {
	at = at.UTC()
	return time.Date(at.Year(), at.Month()+1, 1, 0, 0, 0, 0, time.UTC)
}

// Gate is what the stub asks of the gate of admission: whether a reading and
// the tutor's words are admitted, what a call is expected to cost, and the mode
// tutor reads handwriting by, which says how many readers read a sheet or a
// workbook page.
type Gate interface {
	Reads(call tutor.Call, category formatv0.AgeCategory, ownKeyProvider string) bool
	Words(withWork bool, category formatv0.AgeCategory, ownKeyProvider string) bool
	PaperWords(category formatv0.AgeCategory, ownKeyProvider string) bool
	CorrectWords(category formatv0.AgeCategory, ownKeyProvider string) bool
	Price(call tutor.Call, route tutor.Route) tutor.CallPrice
	Reading() tutor.Reading
}

// Stub reads sheets, workbook pages and essays, assesses essays and words the
// tutor's words in place of tutor.
type Stub struct {
	gate Gate
}

// New is the stub over gate, the dev mode's gate of admission.
func New(gate Gate) *Stub { return &Stub{gate: gate} }

// ReadSheet reads the crop of r by its shape, on task, the instance whose open
// view r holds: each reader of the gate's mode reads the same, so module-kit
// joins the pair's as they are. A value stands with the crop box of the whole
// crop, in thousandths, and a reason with none, as an observation has them. A
// task with no answer slot bound anywhere, a crop that is no PNG, or a request
// whose context ended while the stub looked for a value, fails, as tutor's
// reading does. Past those checks an own key of an ending of the stub's is
// refused by it, own being its value.
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
	n := s.readers()
	read := tutor.SheetRead{Outcome: tutor.Outcome{Kind: tutor.Answered,
		Usage: s.usage(tutor.CallSheet, r.Route, n)}}
	for range n {
		read.Observations = append(read.Observations, &observationv1.Observation{
			AnswerSlots: []*observationv1.AnswerSlotObservation{answer}})
	}
	return read
}

// Reads says whether the reading by the call of the kind is admitted for a
// learner of category, on the route of ownKeyProvider, as the gate says:
// admission is tutor's, so the stub admits no kind of call itself.
func (s *Stub) Reads(call tutor.Call, category formatv0.AgeCategory, ownKeyProvider string) bool {
	return s.gate.Reads(call, category, ownKeyProvider)
}

// ReadEssay reads the essay of r, on task, by the shape of the crop of its
// sheet, as ReadSheet reads a sheet: wider than tall the stub's text,
// essayText; taller than wide a blank sheet, UNREADABLE_BLANK; square
// illegible, UNREADABLE_ILLEGIBLE. One reader reads it, as tutor's one model
// does, and the observation is of the one answer slot bound to the sheet, a
// value with no crop box, as tutor's. Of tutor's checks before its call the
// stub keeps those of the request's form: a task that is no handwriting sheet,
// has no slot bound to the sheet, more than one, or a slot bound otherwise, no
// open view of the task, a crop that is no PNG or is larger than maxCropSide on
// a side, and a request whose context ended, fail. Past those checks an own key
// of an ending of the stub's, own being its value, is refused by it. The call
// spends the expected price of CallEssay on the route, tutor's by the entry the
// author's word admits; with none, nothing priced, so the quota keeps its
// reserve by the value of the kind as spent.
func (s *Stub) ReadEssay(ctx context.Context, r tutor.EssayReading, task formatv0.Task, own string) tutor.EssayRead {
	fail := func(err error) tutor.EssayRead {
		return tutor.EssayRead{Outcome: tutor.Outcome{Kind: tutor.Failed, Err: err}}
	}
	if r.Task == nil {
		return fail(errors.New("tutorstub: the essay has no open view of its task"))
	}
	place, err := essayPlace(task)
	if err != nil {
		return fail(err)
	}
	cfg, err := sized(r.Crop, maxCropSide)
	if err != nil {
		return fail(fmt.Errorf("tutorstub: the crop of the essay: %w", err))
	}
	if o, refused := refusal(ctx, own); refused {
		return tutor.EssayRead{Outcome: o}
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	slot := &observationv1.AnswerSlotObservation{AnswerSlot: place}
	switch {
	case cfg.Width > cfg.Height:
		slot.Read = &observationv1.AnswerSlotObservation_Value{Value: essayText}
	case cfg.Height > cfg.Width:
		slot.Read = &observationv1.AnswerSlotObservation_Unreadable{Unreadable: observationv1.Unreadable_UNREADABLE_BLANK}
	default:
		slot.Read = &observationv1.AnswerSlotObservation_Unreadable{Unreadable: observationv1.Unreadable_UNREADABLE_ILLEGIBLE}
	}
	return tutor.EssayRead{Observation: &observationv1.Observation{AnswerSlots: []*observationv1.AnswerSlotObservation{slot}},
		Outcome: tutor.Outcome{Kind: tutor.Answered, Usage: s.usage(tutor.CallEssay, r.Route, 1)}}
}

// maxCropSide is the longest side in pixels of the crop of a sheet that tutor
// reads as an essay.
const maxCropSide = 4096

// sized is the size of crop, a PNG no larger than side pixels on a side.
func sized(crop []byte, side int) (image.Config, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(crop))
	if err == nil && (cfg.Width > side || cfg.Height > side) {
		err = fmt.Errorf("%d × %d pixels, more than %d on a side", cfg.Width, cfg.Height, side)
	}
	return cfg, err
}

// essayText is the text the stub reads on the sheet of an essay: synthetic, of
// no learner, with no number, in English, since a reading of an essay names no
// language.
const essayText = "We walked to the old harbour on a grey morning. The boats were quiet, " +
	"and the gulls kept watch over the water. When the sun came out, we sat on the wall and shared our bread."

// essayPlace is the place of the one answer slot of task bound to the sheet,
// from 1, as tutor reads an essay: a task that is no handwriting sheet, has no
// such slot or more than one, or has a slot bound otherwise, is refused, as
// tutor's reading refuses it.
func essayPlace(task formatv0.Task) (*observationv1.AnswerSlotPlace, error) {
	if task.Canvas.Kind != formatv0.CanvasKindHandwritingSheet {
		return nil, errors.New("tutorstub: the task of an essay is no handwriting sheet")
	}
	var place *observationv1.AnswerSlotPlace
	for i, item := range task.Items {
		for j, slot := range item.AnswerSlots {
			if slot.Binding != formatv0.AnswerSlotBindingSheet || place != nil {
				return nil, errors.New("tutorstub: the essay has a slot bound otherwise than to the sheet, or two slots")
			}
			place = &observationv1.AnswerSlotPlace{Item: int32(i + 1), Slot: int32(j + 1)}
		}
	}
	if place == nil {
		return nil, errors.New("tutorstub: the task has no answer slot bound to the sheet")
	}
	return place, nil
}

// AssessEssay assesses the text of r by the criteria of its rubric as the stub
// does: every criterion met but the last, each with the stub's synthetic
// evidence, and the stub's feedback in the language of r — English for a
// language it has no words of —, which passes the leak check and words.Feedback
// against any text, since it quotes none. Of tutor's checks before its call the
// stub keeps those of the request's form: no text, no criterion or a blank one,
// no topic, no style, or a language words.Feedback cannot check, fails, as does
// a request whose context ended, every criterion without a mark. Past those
// checks an own key of an ending of the stub's, own being its value, is refused
// by it. The call spends the expected price of CallRubric on the route, tutor's
// by the entry the author's word admits; with none, nothing priced, so the
// quota keeps its reserve by the value of the kind as spent.
func (s *Stub) AssessEssay(ctx context.Context, r tutor.RubricAssessment, own string) tutor.RubricAssessed {
	none := func(o tutor.Outcome) tutor.RubricAssessed {
		return tutor.RubricAssessed{Criteria: make([]tutor.CriterionMark, len(r.Criteria)), Outcome: o}
	}
	fail := func(err error) tutor.RubricAssessed { return none(tutor.Outcome{Kind: tutor.Failed, Err: err}) }
	switch {
	case strings.TrimSpace(r.Text) == "":
		return fail(errors.New("tutorstub: the essay has no text to assess"))
	case len(r.Criteria) == 0 || slices.ContainsFunc(r.Criteria, func(c string) bool { return strings.TrimSpace(c) == "" }):
		return fail(errors.New("tutorstub: the rubric has no criterion, or a blank one"))
	case strings.TrimSpace(r.Topic) == "":
		return fail(errors.New("tutorstub: the essay has no topic"))
	case strings.TrimSpace(r.Style) == "":
		return fail(errors.New("tutorstub: the assessment names no style of presentation"))
	case !feedbackLanguage(r.Language):
		return fail(errors.New("tutorstub: the assessment names no language of the feedback"))
	}
	if o, refused := refusal(ctx, own); refused {
		return none(o)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	got := tutor.RubricAssessed{Criteria: make([]tutor.CriterionMark, len(r.Criteria)),
		Outcome: tutor.Outcome{Kind: tutor.Answered, Usage: s.usage(tutor.CallRubric, r.Route, 1)}}
	for i := range got.Criteria {
		met := i < len(r.Criteria)-1
		got.Criteria[i] = tutor.CriterionMark{Evidence: evidence, Met: &met}
	}
	got.Feedback = feedback["en"]
	if f, ok := feedback[base(r.Language)]; ok {
		got.Feedback = f
	}
	return got
}

// feedbackLanguage says whether tag names a language of feedback, as tutor's
// assessment takes one: a tag that parses, is not the root, has a name in
// English, and has a first subtag words.Feedback can read.
func feedbackLanguage(tag string) bool {
	t, err := language.Parse(tag)
	return err == nil && !t.IsRoot() && display.English.Tags().Name(t) != "" && words.Readable(tag)
}

// evidence is what the stub says it judged each criterion by: synthetic, and
// never shown, kept or logged, as no evidence is.
const evidence = "The stub's synthetic evidence: no model read the text."

// feedback is the stub's feedback on an essay, by language: two sentences with
// no number and no quote, in the letters of the language's script, which pass
// words.Feedback against any text read, and the leak check of a task none of
// whose leak phrases they hold.
var feedback = map[string]string{
	"en": "The essay keeps to its topic, and its ideas follow one another in a clear order. " +
		"Look again at the last point of the rubric and think how the text could meet it.",
	"ru": "Сочинение держится темы, и мысли в нём идут ясно и по порядку. " +
		"Перечитай последний пункт рубрики и подумай, как текст мог бы ему ответить.",
}

// ReadWorkbook reads the filled boxes of the workbook page of r, on task, the
// instance whose open view r holds, each by the shape of its crop as ReadSheet
// reads a sheet's: wider than tall the box's right value, taller than wide a
// wrong one, square illegible. The observation holds every box of the page in
// the task's order, a box that sent no crop unreadable, blank, and a value with
// no crop box, as tutor's does; each reader of the gate's mode reads the same.
// Of tutor's checks before its call the stub keeps those of the request's form:
// a task that is no workbook page, a crop of a place the page has no box of or
// of one box twice, more than maxBoxes crops, or a crop that is no PNG or is
// larger than maxBoxSide on a side fails, as does a request whose context ended
// while the stub looked for the values. A page with no crop goes to no model
// and spends nothing, as tutor's does; past those checks an own key of an
// ending of the stub's, own being its value, is refused by it. The call spends
// the expected price of CallWorkbook on the route, tutor's by the entry the
// author's word admits; with none, nothing priced, so the quota keeps its
// reserve by the value of the kind as spent.
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
		cfg, err := sized(b.Crop, maxBoxSide)
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
	n := s.readers()
	out := tutor.WorkbookRead{Outcome: tutor.Outcome{Kind: tutor.Answered}}
	if len(r.Boxes) > 0 {
		out.Outcome.Usage = s.usage(tutor.CallWorkbook, r.Route, n)
	}
	for range n {
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

// PaperWords says whether "explain differently" on a task from paper is
// admitted, as the gate says.
func (s *Stub) PaperWords(category formatv0.AgeCategory, ownKeyProvider string) bool {
	return s.gate.PaperWords(category, ownKeyProvider)
}

// TellPaperWords words "explain differently" on a task from paper with a
// question of the stub's own in the language of w, as TellWords does, and
// answers a learner's question with words of its own in that language, which
// name no number, so that app walks the conversation without a model. Of
// tutor's checks before its call the stub keeps those of the request's form:
// another kind of words, a question with no words, no style or a task not in
// the frame fails with no usage. Past them an own key of an ending of the
// stub's, own being its value, is refused by it.
func (s *Stub) TellPaperWords(ctx context.Context, w tutor.PaperWordsRequest, own string) tutor.WordsAnswer {
	fail := func(err error) tutor.WordsAnswer {
		return tutor.WordsAnswer{Outcome: tutor.Outcome{Kind: tutor.Failed, Err: err}}
	}
	switch {
	case w.Ask != tutor.PaperAgain && w.Ask != tutor.PaperQuestion:
		return fail(fmt.Errorf("tutorstub: no kind of words of paper %d", w.Ask))
	case w.Ask == tutor.PaperQuestion && strings.TrimSpace(w.Question) == "":
		return fail(errors.New("tutorstub: the question has no words"))
	case strings.TrimSpace(w.Style) == "":
		return fail(errors.New("tutorstub: the words have no style of presentation"))
	case w.Task < 1 || w.Task > len(w.Tasks):
		return fail(fmt.Errorf("tutorstub: no task %d in the frame", w.Task))
	}
	if o, refused := refusal(ctx, own); refused {
		return tutor.WordsAnswer{Outcome: o}
	}
	if w.Ask == tutor.PaperQuestion {
		said := answers["en"]
		if a, ok := answers[base(w.Language)]; ok {
			said = a
		}
		return tutor.WordsAnswer{Question: said[len(w.Turns)/2%len(said)], Outcome: tutor.Outcome{Kind: tutor.Answered,
			Usage: s.usage(tutor.CallPaperHint, w.Route, 1)}}
	}
	list := questions["en"]
	if l, ok := questions[base(w.Language)]; ok {
		list = l
	}
	answer := tutor.WordsAnswer{Question: list[len(w.Asked)%len(list)], Outcome: tutor.Outcome{Kind: tutor.Answered,
		Usage: s.usage(tutor.CallPaperHint, w.Route, 1)}}
	for _, q := range list {
		if !slices.Contains(w.Asked, q) {
			answer.Question = q
			break
		}
	}
	return answer
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

// answers are the stub's answers to a learner's question, by language, each
// the next by the turns before it: one question each, without a number, as the
// form check and the leak check take them.
var answers = map[string][]string{
	"en": {
		"What did you do in the last line of your work?",
		"Which part of the task is still unclear to you?",
	},
	"ru": {
		"Что происходит в последней строке твоего решения?",
		"Какая часть задачи тебе ещё непонятна?",
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
// names no technique, on how the answer was found, as tutor's prompt asks. They
// hold no number and tell no sex, so they pass the leak check and
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
// with none, nothing priced, and the quota keeps the reserve as spent. So the
// settlement spends the reserve to the micro-dollar, and the quota runs out
// after as many calls as on the models.
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

// pageTasks is how many tasks the stub finds on a page taller than wide, and
// columnTasks how many it finds by the learner's request, in the left column.
const (
	pageTasks   = 5
	columnTasks = 12
)

// FindTasks finds the tasks on the photo of f by its shape: taller than wide,
// pageTasks of them, one under another across the page, in its order, the
// first with the frame of an instruction in the margin above it; wider
// than tall, none, which the learner frames by hand; square, a failure of AI,
// which spends as a call that went. With the learner's request a photo not
// square has columnTasks of them, one under another in the left quarter, less
// those that cover a frame the learner drew, by tutor's own check. Of tutor's
// checks before its call the stub keeps the photo's form: one that is no JPEG
// or PNG fails with no usage. Past it an own key of an ending of the stub's,
// own being its value, is refused by it. The call spends the expected price of
// CallFinding, tutor's; with none, nothing priced, so the quota keeps its
// reserve by the value of the kind as spent. It turns no photo: its turn is 0,
// the page taken as upright.
func (s *Stub) FindTasks(ctx context.Context, f tutor.TaskFinding, own string) tutor.TasksFound {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(f.Photo))
	if err != nil {
		return tutor.TasksFound{Outcome: tutor.Outcome{Kind: tutor.Failed, Err: fmt.Errorf("tutorstub: the photo: %w", err)}}
	}
	if o, refused := refusal(ctx, own); refused {
		return tutor.TasksFound{Outcome: o}
	}
	usage := s.usage(tutor.CallFinding, f.Route, 1)
	found := tutor.TasksFound{Outcome: tutor.Outcome{Kind: tutor.Answered, Usage: usage}}
	switch {
	case cfg.Width == cfg.Height:
		found.Outcome = tutor.Outcome{Kind: tutor.Failed, Usage: usage, Err: errors.New("tutorstub: a square photo fails the finding")}
	case f.Request != "":
		const step, height = (wholeCrop - 40) / columnTasks, (wholeCrop-40)/columnTasks - 10
		for i := range int32(columnTasks) {
			top := 20 + i*step
			box := &observationv1.CropBox{Ymin: top, Xmin: 20, Ymax: top + height, Xmax: wholeCrop/4 - 10}
			if !tutor.CoversDrawn(box, f.Drawn) {
				found.Frames = append(found.Frames, box)
			}
		}
	case cfg.Height > cfg.Width:
		// A fifth of the page each, less a margin, from 20 to 980 thousandths.
		const step, height = (wholeCrop - 40) / pageTasks, (wholeCrop-40)/pageTasks - 20
		for i := range int32(pageTasks) {
			top := 20 + i*step
			found.Frames = append(found.Frames, &observationv1.CropBox{Ymin: top, Xmin: 50, Ymax: top + height, Xmax: wholeCrop - 50})
			found.Instructions = append(found.Instructions, nil)
		}
		found.Instructions[0] = &observationv1.CropBox{Ymin: 2, Xmin: 50, Ymax: 18, Xmax: wholeCrop - 50}
	}
	return found
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

// readers is how many readers read a sheet or a workbook page by the gate's
// mode of reading: the two of the pair, or the one reader.
func (s *Stub) readers() int {
	if s.gate.Reading() == tutor.ReadingSingle {
		return 1
	}
	return 2
}

// stubFormula is the formula the stub reads of a statement wider than tall, and
// stubKey the one answer its Check gives each task from paper, the formula's.
const (
	stubFormula = "37+48"
	stubKey     = "85"
)

// severalTasks are the tasks the stub reads on a crop of several: three sums.
var severalTasks = []tutor.StatementTask{
	{Statement: "37 + 48 =", Formula: "37+48"},
	{Statement: "40 + 45 =", Formula: "40+45"},
	{Statement: "50 + 36 =", Formula: "50+36"},
}

// ReadStatement reads the statement of a task from paper by the shape of its
// crop: wider than tall, one task of the formula stubFormula; at least twice as
// tall as wide, the three tasks severalTasks; otherwise taller than wide or
// square, one task of no formula. Of tutor's checks before its call the stub keeps the
// crop's form: one that is no JPEG or PNG fails with no usage. Past it an own
// key of an ending of the stub's, own being its value, is refused by it.
func (s *Stub) ReadStatement(ctx context.Context, p tutor.PaperTask, own string) tutor.StatementRead {
	cfg, o, ok := s.statement(ctx, tutor.CallStatement, p, own)
	if !ok {
		return tutor.StatementRead{Outcome: o}
	}
	read := tutor.StatementRead{Outcome: o}
	switch {
	case cfg.Width > cfg.Height:
		read.Tasks = []tutor.StatementTask{{Statement: "37 + 48 =", Formula: stubFormula}}
	case cfg.Height >= 2*cfg.Width:
		read.Tasks = slices.Clone(severalTasks)
	default:
		read.Tasks = []tutor.StatementTask{{Statement: "Ann has 37 stamps and gets 48 more. How many has she?"}}
	}
	return read
}

// statement is the size of the crop of p and the outcome of the call of its
// kind, which spends the call's expected price: false for a crop that is no
// JPEG or PNG, which fails, or an own key the stub refuses.
func (s *Stub) statement(ctx context.Context, call tutor.Call, p tutor.PaperTask, own string) (image.Config, tutor.Outcome, bool) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(p.Statement))
	if err != nil {
		return cfg, tutor.Outcome{Kind: tutor.Failed, Err: fmt.Errorf("tutorstub: the statement: %w", err)}, false
	}
	if o, refused := refusal(ctx, own); refused {
		return cfg, o, false
	}
	return cfg, tutor.Outcome{Kind: tutor.Answered, Usage: s.usage(call, p.Route, 1)}, true
}

// The verdicts of a slot of the Check, as tutor's SlotChecked names them.
const (
	checkCorrect    = "correct"
	checkIncorrect  = "incorrect"
	checkUnreadable = "unreadable"
	checkNoAnswer   = "no_answer"
)

// stubSolution is the hidden solution the stub's Check gives each task, of the
// answer stubKey, and stubSoFar what it reads of a work with no answer yet.
const (
	stubSolution = "37 + 48 = 85"
	stubSoFar    = "37 + 4"
)

// checkWords are the tutor's words of the stub's Check, by language, without a
// number, as the leak check takes them.
var checkWords = map[string]string{
	"en": "Let us look at your work together.",
	"ru": "Давай посмотрим на твоё решение вместе.",
}

// CheckPaper checks the work of c by the shape of its crop, which takes in the
// tracing paper, the same for each of its c.Tasks tasks — told none, as many as
// ReadStatement reads on its statement, three on one at least twice as tall as
// wide, else one —, each in a band of the crop of its own, one under another:
// more than twice as wide as tall, each task correct, stubKey read; wider but
// not twice, the first task incorrect, a wrong value read, with one error, and
// the others correct; square, each task with no answer yet, a start of the
// solution read and no error; taller but not twice, each unreadable; at least
// twice as tall, each with no answer and nothing read. Each slot has the hidden
// solution stubSolution, and the ladder is the stub's questions in the
// language of c. Of tutor's checks before its call the stub keeps those of the
// request's form: a count of tasks below 0 or above 20, a crop or an
// instruction that is no picture and a work that is no PNG fail with no usage. Past them an own key of an ending of
// the stub's, own being its value, is refused by it.
func (s *Stub) CheckPaper(ctx context.Context, c tutor.PaperCheck, own string) tutor.PaperChecked {
	fail := func(err error) tutor.PaperChecked {
		return tutor.PaperChecked{Outcome: tutor.Outcome{Kind: tutor.Failed, Err: err}}
	}
	if c.Tasks < 0 || c.Tasks > maxCheckTasks {
		return fail(fmt.Errorf("tutorstub: the Check of %d tasks", c.Tasks))
	}
	statement, _, err := image.DecodeConfig(bytes.NewReader(c.Statement))
	if err != nil {
		return fail(fmt.Errorf("tutorstub: the statement: %w", err))
	}
	tasks := c.Tasks
	if tasks == 0 {
		tasks = 1
		if statement.Height >= 2*statement.Width {
			tasks = len(severalTasks)
		}
	}
	if len(c.Instruction) > 0 {
		if _, _, err := image.DecodeConfig(bytes.NewReader(c.Instruction)); err != nil {
			return fail(fmt.Errorf("tutorstub: the instruction: %w", err))
		}
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(c.Work))
	if err != nil {
		return fail(err)
	}
	if o, refused := refusal(ctx, own); refused {
		return tutor.PaperChecked{Outcome: o}
	}
	hints := questions["en"]
	words := checkWords["en"]
	if l, ok := questions[base(c.Language)]; ok {
		hints, words = l, checkWords[base(c.Language)]
	}
	checked := tutor.PaperChecked{TutorWords: words, Hints: slices.Clone(hints),
		Outcome: tutor.Outcome{Kind: tutor.Answered, Usage: s.usage(tutor.CallChecking, c.Route, 1)}}
	for i := range tasks {
		band := &observationv1.CropBox{Ymin: int32(i * wholeCrop / tasks), Ymax: int32((i + 1) * wholeCrop / tasks), Xmax: wholeCrop}
		slot := tutor.SlotChecked{Task: i + 1, Confidence: "high", Solution: stubSolution, Answer: stubKey}
		switch {
		case cfg.Width > 2*cfg.Height:
			slot.Verdict, slot.Read, slot.ReadBox = checkCorrect, stubKey, band
		case cfg.Width > cfg.Height && i == 0:
			slot.Verdict, slot.Read, slot.ReadBox = checkIncorrect, stubKey+"1", band
			slot.Errors = []tutor.CheckError{{Box: band, Where: "the last digit", Kind: "arithmetic", Note: "one digit too many"}}
		case cfg.Width > cfg.Height:
			slot.Verdict, slot.Read, slot.ReadBox = checkCorrect, stubKey, band
		case cfg.Width == cfg.Height:
			slot.Verdict, slot.Read, slot.ReadBox = checkNoAnswer, stubSoFar, band
		case cfg.Height < 2*cfg.Width:
			slot.Verdict = checkUnreadable
		default:
			slot.Verdict = checkNoAnswer
		}
		checked.Slots = append(checked.Slots, slot)
	}
	return checked
}

// maxCheckTasks is the most tasks of a frame tutor's Check takes.
const maxCheckTasks = 20
