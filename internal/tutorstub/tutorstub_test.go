package tutorstub_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"

	"github.com/altnovatrail/module-kit/observation"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
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

func (g gate) Price(call tutor.Call, _ tutor.Route) tutor.CallPrice { return g.prices[call] }

// priced is a gate that admits, and expects a sheet to cost $0.020001, which
// the two readers do not share evenly, and the words a cent.
var priced = gate{admitted: true, prices: map[tutor.Call]tutor.CallPrice{
	tutor.CallSheet: {ExpectedUSD: 0.020001, Expected: true},
	tutor.CallWords: {ExpectedUSD: 0.01, Expected: true},
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
	r := tutorstub.New(priced).ReadSheet(t.Context(), tutor.SheetReading{Route: adult, Crop: crop(t, width, height)}, tk)
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
	r := tutorstub.New(priced).ReadSheet(t.Context(), tutor.SheetReading{Route: adult, Crop: crop(t, 40, 10)}, tk)
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
	unpriced := tutorstub.New(gate{}).ReadSheet(t.Context(), tutor.SheetReading{Route: adult, Crop: crop(t, 40, 10)}, tk)
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
		r := tutorstub.New(priced).ReadSheet(t.Context(), tutor.SheetReading{Route: adult, Crop: tc.crop}, tc.task)
		if r.Outcome.Kind != tutor.Failed || len(r.Observations) != 0 || len(r.Outcome.Usage) != 0 {
			t.Errorf("%s: ReadSheet = %v with %d observations, want a failure", name, r.Outcome.Kind, len(r.Observations))
		}
	}
}

// The words are the stub's questions in the learner's language, the first not
// asked yet, so "explain differently" gets other words, and the list again once
// all were asked; another language gets English. A call spends the expected
// price of the words. Admission is the gate's.
func TestTheStubWordsItsOwnQuestions(t *testing.T) {
	t.Parallel()

	s := tutorstub.New(priced)
	first := s.TellWords(t.Context(), tutor.WordsRequest{Route: adult, Language: "ru-RU"})
	if first.Outcome.Kind != tutor.Answered || first.Question == "" {
		t.Fatalf("TellWords = %+v, want a question", first)
	}
	if u := first.Outcome.Usage; len(u) != 1 || u[0].Model != tutorstub.Model || u[0].USD != 0.01 || !u[0].Priced {
		t.Errorf("the words spent %+v, want the expected 0.01 by the model stub", u)
	}
	asked := []string{first.Question}
	for range 2 {
		next := s.TellWords(t.Context(), tutor.WordsRequest{Route: adult, Language: "ru", Asked: asked})
		for _, q := range asked {
			if next.Question == q {
				t.Errorf("TellWords after %q gave %q again", asked, q)
			}
		}
		asked = append(asked, next.Question)
	}
	if again := s.TellWords(t.Context(), tutor.WordsRequest{Route: adult, Language: "ru", Asked: asked}); again.Question != first.Question {
		t.Errorf("TellWords after all were asked = %q, want the first, %q", again.Question, first.Question)
	}
	en := s.TellWords(t.Context(), tutor.WordsRequest{Route: adult, Language: "en_US"})
	other := s.TellWords(t.Context(), tutor.WordsRequest{Route: adult, Language: "de"})
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
	r := tutorstub.New(priced).ReadSheet(ctx, tutor.SheetReading{Route: adult, Crop: crop(t, 40, 10)}, tk)
	if r.Outcome.Kind != tutor.Failed || len(r.Observations) != 0 || len(r.Outcome.Usage) != 0 {
		t.Errorf("ReadSheet cut short = %v with %d observations, want a failure", r.Outcome.Kind, len(r.Observations))
	}
}
