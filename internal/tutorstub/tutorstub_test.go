package tutorstub_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/altnovatrail/module-kit/leak"
	"github.com/altnovatrail/module-kit/observation"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/spec/types/formatv0/open"
	"github.com/altnovatrail/tutor"
	"github.com/altnovatrail/tutor/provider"
	"github.com/altnovatrail/tutor/words"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/tutorstub"
)

// gate admits what admitted says, and prices each kind of call by prices.
type gate struct {
	admitted bool
	prices   map[tutor.Call]tutor.CallPrice
}

func (g gate) Words(bool, formatv0.AgeCategory, string) bool { return g.admitted }

func (g gate) CorrectWords(formatv0.AgeCategory, string) bool { return g.admitted }

func (g gate) Price(call tutor.Call, _ tutor.Route) tutor.CallPrice { return g.prices[call] }

// priced is a gate that admits, and expects a sheet to cost $0.020001, which
// the two readers do not share evenly, the words with the work a cent, the
// hint without it half a cent, and the words after a correct answer $0.013524.
var priced = gate{admitted: true, prices: map[tutor.Call]tutor.CallPrice{
	tutor.CallSheet:   {ExpectedUSD: 0.020001, Expected: true},
	tutor.CallWords:   {ExpectedUSD: 0.01, Expected: true},
	tutor.CallHint:    {ExpectedUSD: 0.005, Expected: true},
	tutor.CallCorrect: {ExpectedUSD: 0.013524, Expected: true},
}}

var adult = tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategoryAdult}

// crop is a white PNG of width by height.
func crop(t *testing.T, width, height int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func ptr[T any](v T) *T { return &v }

// task is a handwriting task whose second item holds the slot written anywhere,
// checked by check, after an item tapped.
func task(check formatv0.Check) formatv0.Task {
	return formatv0.Task{Items: []formatv0.Item{
		{AnswerSlots: []formatv0.AnswerSlot{{Binding: formatv0.AnswerSlotBindingChoice,
			Check: formatv0.Check{Kind: formatv0.CheckKindChoice, Key: ptr("yes")}, Options: []formatv0.Text{"yes", "no"}}}},
		{AnswerSlots: []formatv0.AnswerSlot{{Binding: formatv0.AnswerSlotBindingAnywhere, Check: check}}},
	}}
}

// read is what the stub read of the crop of width by height on tk: the value,
// or the reason it could not, of the one slot both readers read alike, which
// module-kit joins as it joins tutor's pair. A value has the crop box of the
// whole crop, in thousandths, and a reason none.
func read(t *testing.T, tk formatv0.Task, width, height int) *observationv1.AnswerSlotObservation {
	t.Helper()
	r := tutorstub.New(priced).ReadSheet(t.Context(), tutor.SheetReading{Route: adult, Crop: crop(t, width, height)}, tk, "")
	if r.Outcome.Kind != tutor.Answered || len(r.Observations) != 2 {
		t.Fatalf("ReadSheet = %v with %d observations, want both readers' answers", r.Outcome.Kind, len(r.Observations))
	}
	joined, err := observation.Join(tk.Items, r.Observations[0], r.Observations[1])
	if err != nil {
		t.Fatalf("module-kit does not join the readings: %v", err)
	}
	slots := joined.GetAnswerSlots()
	if len(slots) != 1 {
		t.Fatalf("the joined reading holds %v, want one slot", slots)
	}
	slot := slots[0]
	if p := slot.GetAnswerSlot(); p.GetItem() != 2 || p.GetSlot() != 1 {
		t.Errorf("the slot read is %d.%d, want 2.1, the one written anywhere", p.GetItem(), p.GetSlot())
	}
	whole := &observationv1.CropBox{Ymax: 1000, Xmax: 1000}
	if b := slot.GetCropBox(); slot.GetUnreadable() == 0 && !proto.Equal(b, whole) || slot.GetUnreadable() != 0 && b != nil {
		t.Errorf("the crop box is %v, want the whole crop beside a value and none beside a reason", b)
	}
	return slot
}

// The shape of the crop says what the stub reads (R66): wider than tall, the
// right value; taller than wide, the first trap value, or with none, the first
// value the check holds not right; square, illegible. A predicate's right and
// wrong values are whole numbers, and a choice written by hand reads its
// options.
func TestTheStubReadsByTheShapeOfTheCrop(t *testing.T) {
	t.Parallel()

	illegible := observationv1.Unreadable_UNREADABLE_ILLEGIBLE
	for name, tc := range map[string]struct {
		check        formatv0.Check
		right, wrong string
	}{
		"a number with a trap": {formatv0.Check{Kind: formatv0.CheckKindNumber, Key: ptr("12"),
			TrapValues: []formatv0.TrapValue{{Value: "7", Trap: "adds"}}}, "12", "7"},
		"a number of no trap": {formatv0.Check{Kind: formatv0.CheckKindNumber, Key: ptr("0")}, "0", "1"},
		"a predicate":         {formatv0.Check{Kind: formatv0.CheckKindPredicate, Predicate: ptr("value > 2 and value % 2 == 1")}, "3", "0"},
		"a choice by hand": {formatv0.Check{Kind: formatv0.CheckKindChoice, Key: ptr(">"),
			Options: []formatv0.Text{"<", ">", "="}}, ">", "<"},
	} {
		t.Run(name, func(t *testing.T) {
			tk := task(tc.check)
			if got := read(t, tk, 1200, 300).GetValue(); got != tc.right {
				t.Errorf("a wide crop reads %q, want %q", got, tc.right)
			}
			if got := read(t, tk, 300, 1200).GetValue(); got != tc.wrong {
				t.Errorf("a tall crop reads %q, want %q", got, tc.wrong)
			}
			if got := read(t, tk, 10, 10).GetUnreadable(); got != illegible {
				t.Errorf("a square crop reads %v, want %v", got, illegible)
			}
		})
	}
	// A predicate no whole number up to the stub's last satisfies has no right
	// value to read: illegible.
	never := task(formatv0.Check{Kind: formatv0.CheckKindPredicate, Predicate: ptr("value > 100000")})
	if got := read(t, never, 40, 10).GetUnreadable(); got != illegible {
		t.Errorf("a right value no candidate gives reads %v, want %v", got, illegible)
	}
}

// A reading spends what tutor expects a sheet to cost, shared by the two
// readers in whole micro-dollars, so the quota settles the very reserve it
// took, each billed and priced, by the model "stub"; a price tutor did not give
// is unpriced.
func TestTheStubSpendsTheExpectedPrice(t *testing.T) {
	t.Parallel()

	tk := task(formatv0.Check{Kind: formatv0.CheckKindNumber, Key: ptr("12")})
	r := tutorstub.New(priced).ReadSheet(t.Context(), tutor.SheetReading{Route: adult, Crop: crop(t, 40, 10)}, tk, "")
	var spent quota.Micro
	for _, u := range r.Outcome.Usage {
		if u.Model != tutorstub.Model || u.Provider != "anthropic" || !u.Billed || !u.Priced {
			t.Errorf("usage %+v, want Anthropic's model stub, billed and priced", u)
		}
		spent += quota.USD(u.USD)
	}
	if want := quota.USD(0.020001); len(r.Outcome.Usage) != 2 || spent != want {
		t.Errorf("the reading spent %d over %d rows, want the reserve, %d, over the two readers", spent, len(r.Outcome.Usage), want)
	}
	unpriced := tutorstub.New(gate{}).ReadSheet(t.Context(), tutor.SheetReading{Route: adult, Crop: crop(t, 40, 10)}, tk, "")
	for _, u := range unpriced.Outcome.Usage {
		if u.Priced || u.USD != 0 {
			t.Errorf("usage %+v of a price tutor did not give, want unpriced", u)
		}
	}
}

// A crop that is no PNG, or a task with no slot written anywhere, fails the
// reading with no observation and no usage, as tutor's does.
func TestTheStubFailsWhatTutorFails(t *testing.T) {
	t.Parallel()

	tapped := formatv0.Task{Items: task(formatv0.Check{}).Items[:1]}
	for name, tc := range map[string]struct {
		crop []byte
		task formatv0.Task
	}{
		"no PNG":           {[]byte("not a picture"), task(formatv0.Check{Kind: formatv0.CheckKindNumber, Key: ptr("1")})},
		"no slot anywhere": {crop(t, 40, 10), tapped},
	} {
		r := tutorstub.New(priced).ReadSheet(t.Context(), tutor.SheetReading{Route: adult, Crop: tc.crop}, tc.task, "")
		if r.Outcome.Kind != tutor.Failed || len(r.Observations) != 0 || len(r.Outcome.Usage) != 0 {
			t.Errorf("%s: ReadSheet = %v with %d observations, want a failure", name, r.Outcome.Kind, len(r.Observations))
		}
	}
}

// The words are the stub's questions in the learner's language, the first not
// asked yet, so "explain differently" gets other words, and the list again once
// all were asked; another language gets English. A call spends the expected
// price of its kind: the hint without the work, or the words with it when the
// work goes (R106 tutor). Admission is the gate's.
func TestTheStubWordsItsOwnQuestions(t *testing.T) {
	t.Parallel()

	s := tutorstub.New(priced)
	first := s.TellWords(t.Context(), tutor.WordsRequest{Route: adult, Language: "ru-RU"}, "")
	if first.Outcome.Kind != tutor.Answered || first.Question == "" {
		t.Fatalf("TellWords = %+v, want a question", first)
	}
	if u := first.Outcome.Usage; len(u) != 1 || u[0].Model != tutorstub.Model || u[0].USD != 0.005 || !u[0].Priced {
		t.Errorf("the hint without the work spent %+v, want the expected 0.005 by the model stub", u)
	}
	withWork := s.TellWords(t.Context(), tutor.WordsRequest{Route: adult, Language: "ru", Crop: crop(t, 40, 20)}, "")
	if u := withWork.Outcome.Usage; withWork.Outcome.Kind != tutor.Answered || len(u) != 1 || u[0].USD != 0.01 {
		t.Errorf("the words with the work spent %+v, want the expected 0.01", u)
	}
	asked := []string{first.Question}
	for range 2 {
		next := s.TellWords(t.Context(), tutor.WordsRequest{Route: adult, Language: "ru", Asked: asked}, "")
		for _, q := range asked {
			if next.Question == q {
				t.Errorf("TellWords after %q gave %q again", asked, q)
			}
		}
		asked = append(asked, next.Question)
	}
	if again := s.TellWords(t.Context(), tutor.WordsRequest{Route: adult, Language: "ru", Asked: asked}, ""); again.Question != first.Question {
		t.Errorf("TellWords after all were asked = %q, want the first, %q", again.Question, first.Question)
	}
	en := s.TellWords(t.Context(), tutor.WordsRequest{Route: adult, Language: "en_US"}, "")
	other := s.TellWords(t.Context(), tutor.WordsRequest{Route: adult, Language: "de"}, "")
	if en.Question == first.Question || other.Question != en.Question {
		t.Errorf("TellWords in English = %q and in German = %q, want English for both, not Russian", en.Question, other.Question)
	}
	if !s.Words(true, formatv0.AgeCategoryAdult, "") || tutorstub.New(gate{}).Words(true, formatv0.AgeCategoryAdult, "") {
		t.Error("Words is not the gate's admission")
	}
}

// A request whose context ended while the stub looked for a value fails, with
// no usage to bill, as a reading cut short does.
func TestTheStubFailsAReadingCutShort(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	tk := task(formatv0.Check{Kind: formatv0.CheckKindPredicate, Predicate: ptr("value > 2")})
	r := tutorstub.New(priced).ReadSheet(ctx, tutor.SheetReading{Route: adult, Crop: crop(t, 40, 10)}, tk, "")
	if r.Outcome.Kind != tutor.Failed || len(r.Observations) != 0 || len(r.Outcome.Usage) != 0 {
		t.Errorf("ReadSheet cut short = %v with %d observations, want a failure", r.Outcome.Kind, len(r.Observations))
	}
}

// The words after a correct answer are the stub's praise — of a step with the
// work, of the option chosen without it — and a question as their last
// sentence, on the technique when the task names one and on how the answer was
// found when it names none, in English and Russian, and English in another
// language, which the form check of that language takes too. On both languages
// they pass the leak check of module-kit on the task and words.Correct of
// tutor, as api checks them (R112 tutor; R76). A call spends the expected price
// of CallCorrect, with the work and without it; admission is the gate's.
func TestTheStubPraisesACorrectAnswer(t *testing.T) {
	t.Parallel()

	tk := task(formatv0.Check{Kind: formatv0.CheckKindNumber, Key: ptr("12"),
		TrapValues: []formatv0.TrapValue{{Value: "7", Trap: "adds"}}})
	search, err := leak.For(tk)
	if err != nil {
		t.Fatal(err)
	}
	view := &open.Task{}
	s := tutorstub.New(priced)
	seen := map[string]bool{}
	for _, language := range []string{"en", "ru", "ru-RU", "en_US"} {
		for _, techniques := range [][]string{nil, {"count-on"}} {
			for _, work := range [][]byte{nil, crop(t, 40, 20)} {
				c := tutor.CorrectRequest{Route: adult, Task: view, Crop: work, Key: "12", Solution: []string{"7 + 5 = 12"},
					Techniques: techniques, Language: language, Style: "an adult"}
				got := s.TellCorrectWords(t.Context(), c, "")
				if got.Outcome.Kind != tutor.Answered || got.Words == "" {
					t.Fatalf("TellCorrectWords(%s, %v) = %+v, want words", language, techniques, got)
				}
				if found := search.In(got.Words); len(found) > 0 {
					t.Errorf("the words %q leak %v", got.Words, found)
				}
				if breaches := words.Correct(got.Words, language, ""); len(breaches) > 0 {
					t.Errorf("the words %q in %s break %v of their form", got.Words, language, breaches)
				}
				if u := got.Outcome.Usage; len(u) != 1 || u[0].Model != tutorstub.Model || u[0].USD != 0.013524 || !u[0].Priced {
					t.Errorf("the words after a correct answer spent %+v, want the expected 0.013524 by the model stub", u)
				}
				seen[got.Words] = true
			}
		}
	}
	if len(seen) != 8 {
		t.Errorf("the stub gave %d different words, want 8: two languages, with a technique and without, with the work and without", len(seen))
	}
	if other := s.TellCorrectWords(t.Context(), tutor.CorrectRequest{Route: adult, Task: view, Language: "de",
		Style: "an adult"}, ""); !seen[other.Words] || words.Correct(other.Words, "de", "") != nil {
		t.Errorf("the words in German = %q, want English", other.Words)
	}
	if !s.CorrectWords(formatv0.AgeCategoryAdult, "") || tutorstub.New(gate{}).CorrectWords(formatv0.AgeCategoryAdult, "") {
		t.Error("CorrectWords is not the gate's admission")
	}
}

// A request with no task, no style of presentation or no language, or a crop
// that is no PNG, fails the words after a correct answer with no usage, as
// tutor's call does.
func TestTheStubFailsTheWordsTutorFails(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]tutor.CorrectRequest{
		"no task":     {Route: adult, Language: "en", Style: "an adult"},
		"no style":    {Route: adult, Task: &open.Task{}, Language: "en", Style: " "},
		"no PNG":      {Route: adult, Task: &open.Task{}, Crop: []byte("not a picture"), Language: "en", Style: "an adult"},
		"no language": {Route: adult, Task: &open.Task{}, Language: " ", Style: "an adult"},
	} {
		got := tutorstub.New(priced).TellCorrectWords(t.Context(), c, "")
		if got.Outcome.Kind != tutor.Failed || got.Words != "" || len(got.Outcome.Usage) != 0 {
			t.Errorf("%s: TellCorrectWords = %+v, want a failure with no usage", name, got)
		}
	}
}

// On an own key whose value ends in an ending of the stub's, the reading, the
// words of a rung and the words after a correct answer are refused as a
// provider refuses them: the key refused, or a limit with tutor's reason of the
// ending, or none, with no usage and an error that holds no key. A key of no
// such ending, an ending of the stub's check of a key, and platform AI, with no
// key, are answered; a request tutor fails before its call fails so on any key,
// and so does one cut short.
func TestTheStubRefusesAnOwnKeyByItsEnding(t *testing.T) {
	t.Parallel()

	own := tutor.Route{AI: admissionv0.AiRouteOwnKey, Category: admissionv0.AgeCategoryAdult}
	tk := task(formatv0.Check{Kind: formatv0.CheckKindNumber, Key: ptr("12")})
	s := tutorstub.New(priced)
	sheet := tutor.SheetReading{Route: own, Crop: crop(t, 40, 10)}
	rung := tutor.WordsRequest{Route: own, Language: "en"}
	praised := tutor.CorrectRequest{Route: own, Task: &open.Task{}, Language: "en", Style: "an adult"}
	for ending, want := range map[string]struct {
		kind  tutor.Kind
		limit tutor.LimitReason
	}{
		tutorstub.KeyRefused:      {kind: tutor.KeyRefused},
		tutorstub.KeyLimit:        {kind: tutor.LimitReached},
		tutorstub.KeyLimitOwner:   {kind: tutor.LimitReached, limit: tutor.LimitOwner},
		tutorstub.KeyLimitTier:    {kind: tutor.LimitReached, limit: tutor.LimitTier},
		tutorstub.KeyLimitBalance: {kind: tutor.LimitReached, limit: tutor.LimitBalance},
		tutorstub.KeyLimitPayment: {kind: tutor.LimitReached, limit: tutor.LimitBilling},
	} {
		key := "sk-ant-synthetic" + ending
		for call, o := range map[string]tutor.Outcome{
			"ReadSheet":        s.ReadSheet(t.Context(), sheet, tk, key).Outcome,
			"TellWords":        s.TellWords(t.Context(), rung, key).Outcome,
			"TellCorrectWords": s.TellCorrectWords(t.Context(), praised, key).Outcome,
		} {
			if o.Kind != want.kind || o.Limit != want.limit || len(o.Usage) != 0 || o.Err == nil || strings.Contains(o.Err.Error(), key) {
				t.Errorf("%s on %q = %+v, want %v, limit %q, no usage, and an error with no key", call, ending, o, want.kind, want.limit)
			}
			var limit *provider.LimitError
			if refused, limited := errors.Is(o.Err, provider.ErrKeyRefused), errors.As(o.Err, &limit); refused != (want.kind == tutor.KeyRefused) ||
				limited != (want.kind == tutor.LimitReached) || limited && limit.Limit != want.limit {
				t.Errorf("%s on %q failed with %v, want the provider's error of the outcome", call, ending, o.Err)
			}
		}
		if r := s.ReadSheet(t.Context(), tutor.SheetReading{Route: own, Crop: []byte("not a picture")}, tk, key); r.Outcome.Kind != tutor.Failed {
			t.Errorf("ReadSheet of no PNG on %q = %v, want a failure before the call", ending, r.Outcome.Kind)
		}
		if w := s.TellCorrectWords(t.Context(), tutor.CorrectRequest{Route: own, Language: "en", Style: "an adult"}, key); w.Outcome.Kind != tutor.Failed {
			t.Errorf("TellCorrectWords with no task on %q = %v, want a failure before the call", ending, w.Outcome.Kind)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for call, o := range map[string]tutor.Outcome{
		"ReadSheet":        s.ReadSheet(ctx, sheet, tk, "sk-ant-synthetic"+tutorstub.KeyLimitTier).Outcome,
		"TellWords":        s.TellWords(ctx, rung, "sk-ant-synthetic"+tutorstub.KeyRefused).Outcome,
		"TellCorrectWords": s.TellCorrectWords(ctx, praised, "sk-ant-synthetic"+tutorstub.KeyLimit).Outcome,
	} {
		if o.Kind != tutor.Failed || !errors.Is(o.Err, context.Canceled) {
			t.Errorf("%s cut short on an ending of the stub's = %+v, want a failure, as a call cut short", call, o)
		}
	}
	for _, key := range []string{"sk-ant-synthetic", "sk-ant-synthetic-invalid", "sk-ant-refused-not-at-the-end", "sk-ant-synthetic-limited", ""} {
		if r := s.ReadSheet(t.Context(), sheet, tk, key); r.Outcome.Kind != tutor.Answered || len(r.Observations) != 2 {
			t.Errorf("ReadSheet on %q = %v, want both readers' answers", key, r.Outcome.Kind)
		}
		if w := s.TellWords(t.Context(), rung, key); w.Outcome.Kind != tutor.Answered || w.Question == "" {
			t.Errorf("TellWords on %q = %+v, want a question", key, w)
		}
		if c := s.TellCorrectWords(t.Context(), praised, key); c.Outcome.Kind != tutor.Answered || c.Words == "" {
			t.Errorf("TellCorrectWords on %q = %+v, want words", key, c)
		}
	}
}
