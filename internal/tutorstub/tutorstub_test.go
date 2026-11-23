package tutorstub_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"slices"
	"strings"
	"testing"
	"time"

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

// gate admits the words as admitted says, a reading of the kinds of reads, and
// prices each kind of call by prices, and reads handwriting by reading, the pair
// when empty, as the gate of the dev mode names it.
type gate struct {
	admitted bool
	reads    map[tutor.Call]bool
	prices   map[tutor.Call]tutor.CallPrice
	reading  tutor.Reading
}

func (g gate) Reads(call tutor.Call, _ formatv0.AgeCategory, _ string) bool { return g.reads[call] }

func (g gate) Words(bool, formatv0.AgeCategory, string) bool { return g.admitted }

func (g gate) PaperWords(bool, formatv0.AgeCategory, string) bool { return g.admitted }

func (g gate) CorrectWords(formatv0.AgeCategory, string) bool { return g.admitted }

func (g gate) Price(call tutor.Call, _ tutor.Route) tutor.CallPrice { return g.prices[call] }

func (g gate) Reading() tutor.Reading { return g.reading }

// priced is a gate that admits, and expects a sheet to cost $0.020001, which
// the two readers do not share evenly, the words with the work a cent, the
// hint without it half a cent, and the words after a correct answer $0.013524.
var priced = gate{admitted: true, prices: map[tutor.Call]tutor.CallPrice{
	tutor.CallSheet:      {ExpectedUSD: 0.020001, Expected: true},
	tutor.CallWords:      {ExpectedUSD: 0.01, Expected: true},
	tutor.CallHint:       {ExpectedUSD: 0.005, Expected: true},
	tutor.CallCorrect:    {ExpectedUSD: 0.013524, Expected: true},
	tutor.CallPaperWords: {ExpectedUSD: 0.02, Expected: true},
	tutor.CallPaperHint:  {ExpectedUSD: 0.015, Expected: true},
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

// The shape of the crop says what the stub reads: wider than tall, the right
// value; taller than wide, the first trap value, or with none, the first value
// the check holds not right; square, illegible. A predicate's right and wrong
// values are whole numbers, and a choice written by hand reads its options.
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
// work goes. Admission is the gate's.
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

// The words of a task from paper are the stub's own questions too: a rung's
// with the box of the band of its task, as ReadPaper reads the tasks of a crop,
// the opening question and a rung put another way with none, each spending the
// expected price of its kind of call. A request tutor refuses before its call
// fails with no usage.
func TestTheStubWordsATaskFromPaper(t *testing.T) {
	t.Parallel()

	s := tutorstub.New(priced)
	tasks := []tutor.PaperWordsTask{{Statement: "37 + 48 ="}, {Statement: "40 + 45 ="}, {Statement: "50 + 36 ="}}
	rung := s.TellPaperWords(t.Context(), tutor.PaperWordsRequest{Route: adult, Ask: tutor.PaperRung, Crop: crop(t, 40, 90),
		Tasks: tasks, Task: 2, Rung: 1, Language: "ru", Style: "a child"}, "")
	if b := rung.Box; rung.Outcome.Kind != tutor.Answered || len(words.Hint(rung.Question, "ru", "")) > 0 ||
		b.GetYmin() != 333 || b.GetYmax() != 666 || b.GetXmin() != 0 || b.GetXmax() != 1000 {
		t.Errorf("the rung's words = %+v, want a Russian question with the band of the second task", rung)
	}
	if u := rung.Outcome.Usage; len(u) != 1 || u[0].USD != 0.02 {
		t.Errorf("the rung's words spent %+v, want the expected 0.02", u)
	}
	opening := s.TellPaperWords(t.Context(), tutor.PaperWordsRequest{Route: adult, Ask: tutor.PaperOpening, Statement: photo(t, 40, 30),
		Language: "en", Style: "an adult", Asked: []string{rung.Question}}, "")
	if u := opening.Outcome.Usage; opening.Outcome.Kind != tutor.Answered || opening.Box != nil || len(u) != 1 || u[0].USD != 0.015 {
		t.Errorf("the opening question = %+v, want a question with no box at the expected 0.015", opening)
	}
	again := s.TellPaperWords(t.Context(), tutor.PaperWordsRequest{Route: adult, Ask: tutor.PaperAgain, Tasks: tasks, Task: 1, Rung: 2,
		Language: "ru", Style: "a child", Asked: []string{rung.Question}}, "")
	if again.Outcome.Kind != tutor.Answered || again.Question == rung.Question || again.Box != nil {
		t.Errorf("the rung put another way = %+v, want another question with no box", again)
	}
	for name, w := range map[string]tutor.PaperWordsRequest{
		"no style":           {Route: adult, Ask: tutor.PaperAgain, Tasks: tasks, Task: 1, Rung: 1, Language: "en"},
		"a rung with no PNG": {Route: adult, Ask: tutor.PaperRung, Crop: []byte("no picture"), Tasks: tasks, Task: 1, Rung: 1, Language: "en", Style: "s"},
		"no crop of a task":  {Route: adult, Ask: tutor.PaperOpening, Language: "en", Style: "s"},
	} {
		if got := s.TellPaperWords(t.Context(), w, ""); got.Outcome.Kind != tutor.Failed || len(got.Outcome.Usage) != 0 {
			t.Errorf("%s: %+v, want a failure before the call", name, got)
		}
	}
	if !s.PaperWords(true, formatv0.AgeCategoryAdult, "") || tutorstub.New(gate{}).PaperWords(false, formatv0.AgeCategoryAdult, "") {
		t.Error("PaperWords is not the gate's admission")
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
// tutor, as api checks them. A call spends the expected price of CallCorrect,
// with the work and without it; admission is the gate's.
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

// firstOfAMonthAfter tells whether at is 00:00 UTC on the first day of the
// month after the one of before, or of the one after it, should the month turn
// while the test runs.
func firstOfAMonthAfter(at, before time.Time) bool {
	next := time.Date(before.Year(), before.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	return at.Equal(next) || at.Equal(next.AddDate(0, 1, 0))
}

// On an own key whose value ends in an ending of the stub's, the reading of a
// sheet, of a workbook page and of an essay, the assessment of an essay, the
// words of a rung and the words after a correct answer are refused as a provider
// refuses them: the key refused, or a limit with tutor's reason of the ending,
// or none, and when access returns for the ceiling of the tier alone, with no
// usage and an error that holds no key. A key of no such ending, an ending of the
// stub's check of a key, and platform AI, with no key, are answered; a request
// tutor fails before its call fails so on any key, and so does one cut short.
func TestTheStubRefusesAnOwnKeyByItsEnding(t *testing.T) {
	t.Parallel()

	own := tutor.Route{AI: admissionv0.AiRouteOwnKey, Category: admissionv0.AgeCategoryAdult}
	tk := task(formatv0.Check{Kind: formatv0.CheckKindNumber, Key: ptr("12")})
	s := tutorstub.New(priced)
	sheet := tutor.SheetReading{Route: own, Crop: crop(t, 40, 10)}
	page := tutor.WorkbookReading{Route: own, Boxes: []tutor.BoxCrop{boxOf(1, 1, crop(t, 40, 10))}}
	rung := tutor.WordsRequest{Route: own, Language: "en"}
	praised := tutor.CorrectRequest{Route: own, Task: &open.Task{}, Language: "en", Style: "an adult"}
	sheetOfEssay := tutor.EssayReading{Route: own, Task: &open.Task{}, Crop: crop(t, 40, 10)}
	assessed := assessment(own, "en", 2)
	photographed := tutor.TaskFinding{Route: own, Photo: photo(t, 30, 40)}
	opening := tutor.PaperWordsRequest{Route: own, Ask: tutor.PaperOpening, Statement: photo(t, 40, 30), Language: "en", Style: "an adult"}
	before := time.Now().UTC()
	for ending, want := range map[string]struct {
		kind    tutor.Kind
		limit   tutor.LimitReason
		returns bool
	}{
		tutorstub.KeyRefused:      {kind: tutor.KeyRefused},
		tutorstub.KeyLimit:        {kind: tutor.LimitReached},
		tutorstub.KeyLimitOwner:   {kind: tutor.LimitReached, limit: tutor.LimitOwner},
		tutorstub.KeyLimitTier:    {kind: tutor.LimitReached, limit: tutor.LimitTier, returns: true},
		tutorstub.KeyLimitBalance: {kind: tutor.LimitReached, limit: tutor.LimitBalance},
		tutorstub.KeyLimitPayment: {kind: tutor.LimitReached, limit: tutor.LimitBilling},
	} {
		key := "sk-ant-synthetic" + ending
		for call, o := range map[string]tutor.Outcome{
			"ReadSheet":        s.ReadSheet(t.Context(), sheet, tk, key).Outcome,
			"ReadWorkbook":     s.ReadWorkbook(t.Context(), page, book(), key).Outcome,
			"TellWords":        s.TellWords(t.Context(), rung, key).Outcome,
			"TellCorrectWords": s.TellCorrectWords(t.Context(), praised, key).Outcome,
			"ReadEssay":        s.ReadEssay(t.Context(), sheetOfEssay, essay(), key).Outcome,
			"AssessEssay":      s.AssessEssay(t.Context(), assessed, key).Outcome,
			"FindTasks":        s.FindTasks(t.Context(), photographed, key).Outcome,
			"TellPaperWords":   s.TellPaperWords(t.Context(), opening, key).Outcome,
		} {
			if o.Kind != want.kind || o.Limit != want.limit || len(o.Usage) != 0 || o.Err == nil || strings.Contains(o.Err.Error(), key) {
				t.Errorf("%s on %q = %+v, want %v, limit %q, no usage, and an error with no key", call, ending, o, want.kind, want.limit)
			}
			var limit *provider.LimitError
			if refused, limited := errors.Is(o.Err, provider.ErrKeyRefused), errors.As(o.Err, &limit); refused != (want.kind == tutor.KeyRefused) ||
				limited != (want.kind == tutor.LimitReached) || limited && (limit.Limit != want.limit || !limit.Returns.Equal(o.AccessReturns)) {
				t.Errorf("%s on %q failed with %v, want the provider's error of the outcome", call, ending, o.Err)
			}
			if returns := !o.AccessReturns.IsZero(); returns != want.returns || returns && !firstOfAMonthAfter(o.AccessReturns, before) {
				t.Errorf("%s on %q: access returns %v, want %t: 00:00 UTC on the first of the month after %v", call, ending, o.AccessReturns, want.returns, before)
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
		"ReadWorkbook":     s.ReadWorkbook(ctx, page, book(), "sk-ant-synthetic"+tutorstub.KeyLimitOwner).Outcome,
		"TellWords":        s.TellWords(ctx, rung, "sk-ant-synthetic"+tutorstub.KeyRefused).Outcome,
		"TellCorrectWords": s.TellCorrectWords(ctx, praised, "sk-ant-synthetic"+tutorstub.KeyLimit).Outcome,
		"ReadEssay":        s.ReadEssay(ctx, sheetOfEssay, essay(), "sk-ant-synthetic"+tutorstub.KeyLimitBalance).Outcome,
		"AssessEssay":      s.AssessEssay(ctx, assessed, "sk-ant-synthetic"+tutorstub.KeyLimitPayment).Outcome,
		"FindTasks":        s.FindTasks(ctx, photographed, "sk-ant-synthetic"+tutorstub.KeyRefused).Outcome,
	} {
		if o.Kind != tutor.Failed || !errors.Is(o.Err, context.Canceled) {
			t.Errorf("%s cut short on an ending of the stub's = %+v, want a failure, as a call cut short", call, o)
		}
	}
	for _, key := range []string{"sk-ant-synthetic", "sk-ant-synthetic-invalid", "sk-ant-refused-not-at-the-end", "sk-ant-synthetic-limited", ""} {
		if r := s.ReadSheet(t.Context(), sheet, tk, key); r.Outcome.Kind != tutor.Answered || len(r.Observations) != 2 {
			t.Errorf("ReadSheet on %q = %v, want both readers' answers", key, r.Outcome.Kind)
		}
		if r := s.ReadWorkbook(t.Context(), page, book(), key); r.Outcome.Kind != tutor.Answered || len(r.Observations) != 2 {
			t.Errorf("ReadWorkbook on %q = %v, want both readers' answers", key, r.Outcome.Kind)
		}
		if w := s.TellWords(t.Context(), rung, key); w.Outcome.Kind != tutor.Answered || w.Question == "" {
			t.Errorf("TellWords on %q = %+v, want a question", key, w)
		}
		if c := s.TellCorrectWords(t.Context(), praised, key); c.Outcome.Kind != tutor.Answered || c.Words == "" {
			t.Errorf("TellCorrectWords on %q = %+v, want words", key, c)
		}
		if r := s.ReadEssay(t.Context(), sheetOfEssay, essay(), key); r.Outcome.Kind != tutor.Answered || r.Observation == nil {
			t.Errorf("ReadEssay on %q = %v, want the text read", key, r.Outcome.Kind)
		}
		if a := s.AssessEssay(t.Context(), assessed, key); a.Outcome.Kind != tutor.Answered || a.Feedback == "" {
			t.Errorf("AssessEssay on %q = %+v, want marks and feedback", key, a)
		}
		if f := s.FindTasks(t.Context(), photographed, key); f.Outcome.Kind != tutor.Answered || len(f.Frames) == 0 || f.Turn != 0 {
			t.Errorf("FindTasks on %q = %+v, want frames on the photo unturned", key, f)
		}
	}
}

// With the learner's request the stub finds twelve tasks one under another in
// the left quarter, whatever the shape, and none that covers a frame drawn.
func TestTheStubFindsTheLeftColumnByARequest(t *testing.T) {
	t.Parallel()

	s := tutorstub.New(gate{})
	ask := tutor.TaskFinding{Route: adult, Photo: photo(t, 400, 300), Request: "only the first column, each task apart"}
	found := s.FindTasks(t.Context(), ask, "")
	if found.Outcome.Kind != tutor.Answered || len(found.Frames) != 12 {
		t.Fatalf("a request: %+v, want twelve frames", found)
	}
	for i, f := range found.Frames {
		if f.GetXmax() > 250 || f.GetYmin() >= f.GetYmax() || i > 0 && f.GetYmin() < found.Frames[i-1].GetYmax() {
			t.Errorf("frame %d is %v, want one under another in the left quarter", i+1, f)
		}
	}
	third := found.Frames[2]
	ask.Drawn = []*observationv1.CropBox{{Ymin: third.Ymin + 2, Xmin: third.Xmin, Ymax: third.Ymax, Xmax: third.Xmax - 5}}
	if again := s.FindTasks(t.Context(), ask, ""); len(again.Frames) != 11 || slices.ContainsFunc(again.Frames, func(f *observationv1.CropBox) bool { return f.Ymin == third.Ymin }) {
		t.Errorf("a request with a frame drawn: %v, want the eleven others", again.Frames)
	}
}

// photo is a grey JPEG of width by height, as the camera of app takes it.
func photo(t *testing.T, width, height int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, width, height)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// The shape of the photo says what the stub finds: taller than wide, five tasks
// one under another, in the order of the page; wider than tall, none; square, a
// failure of AI that spent. A PNG is a photo too, and what is no picture fails
// before the call, with no usage.
func TestTheStubFindsTheTasksByTheShapeOfThePhoto(t *testing.T) {
	t.Parallel()

	s := tutorstub.New(gate{prices: map[tutor.Call]tutor.CallPrice{tutor.CallFinding: {ExpectedUSD: 0.04, Expected: true}}})
	found := s.FindTasks(t.Context(), tutor.TaskFinding{Route: adult, Photo: photo(t, 300, 400)}, "")
	if found.Outcome.Kind != tutor.Answered || len(found.Frames) != 5 || len(found.Outcome.Usage) != 1 || found.Outcome.Usage[0].USD != 0.04 {
		t.Fatalf("a portrait photo: %+v", found)
	}
	for i, f := range found.Frames {
		if f.GetYmin() >= f.GetYmax() || f.GetXmin() >= f.GetXmax() || f.GetYmax() > 1000 || i > 0 && f.GetYmin() < found.Frames[i-1].GetYmax() {
			t.Errorf("frame %d is %v, want a box below the one before it, within the page", i+1, f)
		}
	}
	if found := s.FindTasks(t.Context(), tutor.TaskFinding{Route: adult, Photo: crop(t, 400, 300)}, ""); found.Outcome.Kind != tutor.Answered || len(found.Frames) != 0 {
		t.Errorf("a landscape PNG: %+v, want no task", found)
	}
	if found := s.FindTasks(t.Context(), tutor.TaskFinding{Route: adult, Photo: photo(t, 300, 300)}, ""); found.Outcome.Kind != tutor.Failed || len(found.Outcome.Usage) != 1 {
		t.Errorf("a square photo: %+v, want a failure that spent", found)
	}
	if found := s.FindTasks(t.Context(), tutor.TaskFinding{Route: adult, Photo: []byte("a photo")}, "sk-ant-synthetic"+tutorstub.KeyRefused); found.Outcome.Kind != tutor.Failed || len(found.Outcome.Usage) != 0 {
		t.Errorf("no picture: %+v, want a failure before the call", found)
	}
}

// asking is a gate that records the reading it was asked of, and admits it as
// gate does.
type asking struct {
	gate
	asked *[]string
}

func (g asking) Reads(call tutor.Call, category formatv0.AgeCategory, ownKeyProvider string) bool {
	*g.asked = append(*g.asked, string(call)+" "+string(category)+" "+ownKeyProvider)
	return g.gate.Reads(call, category, ownKeyProvider)
}

// The stub admits each kind of call as the gate does, on its category and
// route: admission is tutor's, so the stub admits none of them in a sheet's
// stead.
func TestTheStubAdmitsAsTheGateDoes(t *testing.T) {
	t.Parallel()

	var asked []string
	books := tutorstub.New(asking{gate{reads: map[tutor.Call]bool{tutor.CallWorkbook: true, tutor.CallEssay: true}}, &asked})
	if !books.Reads(tutor.CallWorkbook, formatv0.AgeCategoryUnder13, "anthropic") || !books.Reads(tutor.CallEssay, formatv0.AgeCategoryAdult, "") ||
		books.Reads(tutor.CallSheet, formatv0.AgeCategoryAdult, "") || books.Reads(tutor.CallRubric, formatv0.AgeCategoryFrom13ToConsent, "") {
		t.Error("the stub admits otherwise than the gate")
	}
	if want := []string{"workbook under_13 anthropic", "essay adult ", "sheet adult ", "rubric from_13_to_consent "}; !slices.Equal(asked, want) {
		t.Errorf("the gate was asked %q, want %q: each kind on its own category and route", asked, want)
	}
}

// book is a workbook page of two items: the first with a box of a number with a
// trap, between words, and a box of a choice written by hand; the second with a
// box of a number of no trap.
func book() formatv0.Task {
	number := func(key string, traps ...formatv0.TrapValue) *formatv0.Check {
		return &formatv0.Check{Kind: formatv0.CheckKindNumber, Key: ptr(key), TrapValues: traps}
	}
	box := ptr(formatv0.LinePartBindingBox)
	return formatv0.Task{Canvas: formatv0.Canvas{Kind: formatv0.CanvasKindWorkbook}, Items: []formatv0.Item{
		{Line: []formatv0.LinePart{{Text: ptr(formatv0.Text("3 +"))}, {Binding: box, Check: number("4", formatv0.TrapValue{Value: "10", Trap: "adds"})},
			{Text: ptr(formatv0.Text("= 7, and 1/4"))},
			{Binding: box, Check: &formatv0.Check{Kind: formatv0.CheckKindChoice, Key: ptr("<"), Options: []formatv0.Text{"<", ">", "="}}},
			{Text: ptr(formatv0.Text("3/4"))}}},
		{Line: []formatv0.LinePart{{Binding: box, Check: number("6")}, {Text: ptr(formatv0.Text("× 2 = 12"))}}},
	}}
}

// boxOf is the crop of the box at the slot of the item, each from 1.
func boxOf(item, slot int, crop []byte) tutor.BoxCrop {
	return tutor.BoxCrop{Item: item, Slot: slot, Crop: crop}
}

// A workbook page is read box by box by the shape of each crop, as a sheet is:
// wider than tall the right value, taller than wide the first trap value or a
// value the check holds not right, square illegible; a box that sent no crop is
// blank. Both readers read the same, each box of the page in the task's order,
// whatever order the crops came in, which module-kit joins as it joins tutor's
// pair. The reading spends the expected price of the page, shared by the two
// readers, and with no price nothing priced.
// A page left blank goes to no model: every box blank, no usage.
func TestTheStubReadsAWorkbookPageByItsBoxes(t *testing.T) {
	t.Parallel()

	tk := book()
	page := tutor.WorkbookReading{Route: adult, Boxes: []tutor.BoxCrop{
		boxOf(2, 1, crop(t, 20, 30)), boxOf(1, 2, crop(t, 30, 20)), boxOf(1, 1, crop(t, 30, 10))}}
	r := tutorstub.New(priced).ReadWorkbook(t.Context(), page, tk, "")
	if r.Outcome.Kind != tutor.Answered || len(r.Observations) != 2 || !proto.Equal(r.Observations[0], r.Observations[1]) {
		t.Fatalf("ReadWorkbook = %v with %v, want the same reading of both readers", r.Outcome.Kind, r.Observations)
	}
	items, err := observation.Join(tk.Items, r.Observations[0], r.Observations[1])
	if err != nil {
		t.Fatalf("module-kit does not join the readings: %v", err)
	}
	var got []string
	for _, s := range items.GetAnswerSlots() {
		read := s.GetValue()
		if read == "" {
			read = s.GetUnreadable().String()
		}
		got = append(got, fmt.Sprintf("%d.%d %s", s.GetAnswerSlot().GetItem(), s.GetAnswerSlot().GetSlot(), read))
		if s.GetCropBox() != nil {
			t.Errorf("the box %v has the crop box %v, want none, as tutor reads a box", s.GetAnswerSlot(), s.GetCropBox())
		}
	}
	if want := []string{"1.1 4", "1.2 <", "2.1 0"}; !slices.Equal(got, want) {
		t.Errorf("the page reads %q, want %q", got, want)
	}
	for _, u := range r.Outcome.Usage {
		if u.Model != tutorstub.Model || u.Priced || u.USD != 0 {
			t.Errorf("usage %+v of a page tutor gives no price of, want the model stub, unpriced", u)
		}
	}
	if len(r.Outcome.Usage) != 2 {
		t.Errorf("the reading spent over %d rows, want the two readers", len(r.Outcome.Usage))
	}
	pricedPage := gate{prices: map[tutor.Call]tutor.CallPrice{tutor.CallWorkbook: {ExpectedUSD: 0.028001, Expected: true}}}
	var spent quota.Micro
	for _, u := range tutorstub.New(pricedPage).ReadWorkbook(t.Context(), page, tk, "").Outcome.Usage {
		spent += quota.USD(u.USD)
	}
	if want := quota.USD(0.028001); spent != want {
		t.Errorf("a priced page spent %d, want its expected price, %d", spent, want)
	}

	square := tutor.WorkbookReading{Route: adult, Boxes: []tutor.BoxCrop{boxOf(1, 1, crop(t, 10, 10)), boxOf(1, 2, crop(t, 10, 40))}}
	odd := tutorstub.New(priced).ReadWorkbook(t.Context(), square, tk, "")
	slots := odd.Observations[0].GetAnswerSlots()
	if len(slots) != 3 || slots[0].GetUnreadable() != observationv1.Unreadable_UNREADABLE_ILLEGIBLE || slots[1].GetValue() != ">" ||
		slots[2].GetUnreadable() != observationv1.Unreadable_UNREADABLE_BLANK {
		t.Errorf("a square box, a tall box of a choice and a box with no crop read %v, want illegible, >, blank", slots)
	}

	blank := tutorstub.New(priced).ReadWorkbook(t.Context(), tutor.WorkbookReading{Route: adult}, tk, "sk-ant-synthetic"+tutorstub.KeyRefused)
	if blank.Outcome.Kind != tutor.Answered || len(blank.Outcome.Usage) != 0 || len(blank.Observations) != 2 {
		t.Fatalf("a page left blank = %+v, want both readers' answers with no usage and no refusal of the key", blank.Outcome)
	}
	for _, s := range blank.Observations[1].GetAnswerSlots() {
		if s.GetUnreadable() != observationv1.Unreadable_UNREADABLE_BLANK {
			t.Errorf("the box %v of a page left blank reads %v, want blank", s.GetAnswerSlot(), s.GetRead())
		}
	}
}

// What tutor's reading of a workbook page fails before its call the stub fails
// too, with no observation and no usage, on any key: a task that is no workbook
// page or has no box, a crop of a place with no box or of one box twice, more
// than 20 crops, a crop that is no PNG or is larger than 512 px on a side; and
// so does a request cut short.
func TestTheStubFailsAPageTutorFails(t *testing.T) {
	t.Parallel()

	tk := book()
	wide := crop(t, 30, 10)
	// A page of 21 boxes, each in an item of its own, and a crop in every one:
	// one more than a reading shows, none twice.
	long := formatv0.Task{Canvas: formatv0.Canvas{Kind: formatv0.CanvasKindWorkbook}}
	var many []tutor.BoxCrop
	for i := range 21 {
		long.Items = append(long.Items, formatv0.Item{Line: []formatv0.LinePart{{Binding: ptr(formatv0.LinePartBindingBox),
			Check: &formatv0.Check{Kind: formatv0.CheckKindNumber, Key: ptr("1")}}}})
		many = append(many, boxOf(i+1, 1, wide))
	}
	sheet := task(formatv0.Check{Kind: formatv0.CheckKindNumber, Key: ptr("1")})
	empty := formatv0.Task{Canvas: formatv0.Canvas{Kind: formatv0.CanvasKindWorkbook}, Items: []formatv0.Item{{}}}
	for name, tc := range map[string]struct {
		task  formatv0.Task
		boxes []tutor.BoxCrop
	}{
		"a sheet":            {sheet, []tutor.BoxCrop{boxOf(1, 1, wide)}},
		"a page of no box":   {empty, nil},
		"no such box":        {tk, []tutor.BoxCrop{boxOf(2, 2, wide)}},
		"no such item":       {tk, []tutor.BoxCrop{boxOf(3, 1, wide)}},
		"a box twice":        {tk, []tutor.BoxCrop{boxOf(1, 1, wide), boxOf(1, 1, wide)}},
		"more than 20 boxes": {long, many},
		"no PNG":             {tk, []tutor.BoxCrop{boxOf(1, 1, []byte("not a picture"))}},
		"too large a box":    {tk, []tutor.BoxCrop{boxOf(1, 1, crop(t, 513, 10))}},
	} {
		for _, own := range []string{"", "sk-ant-synthetic" + tutorstub.KeyLimit} {
			r := tutorstub.New(priced).ReadWorkbook(t.Context(), tutor.WorkbookReading{Route: adult, Boxes: tc.boxes}, tc.task, own)
			if r.Outcome.Kind != tutor.Failed || len(r.Observations) != 0 || len(r.Outcome.Usage) != 0 {
				t.Errorf("%s on %q: ReadWorkbook = %v with %d observations, want a failure", name, own, r.Outcome.Kind, len(r.Observations))
			}
		}
	}
	if r := tutorstub.New(priced).ReadWorkbook(t.Context(), tutor.WorkbookReading{Route: adult,
		Boxes: []tutor.BoxCrop{boxOf(1, 1, crop(t, 512, 512))}}, tk, ""); r.Outcome.Kind != tutor.Answered {
		t.Errorf("a box of 512 × 512 = %v, want read", r.Outcome.Kind)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, own := range []string{"", "sk-ant-synthetic" + tutorstub.KeyRefused} {
		r := tutorstub.New(priced).ReadWorkbook(ctx, tutor.WorkbookReading{Route: adult, Boxes: []tutor.BoxCrop{boxOf(1, 1, wide)}}, tk, own)
		if r.Outcome.Kind != tutor.Failed || !errors.Is(r.Outcome.Err, context.Canceled) || len(r.Observations) != 0 {
			t.Errorf("ReadWorkbook cut short on %q = %+v, want a failure", own, r.Outcome)
		}
	}
}

// essay is a task with a rubric as tutor reads an essay: a handwriting sheet
// whose second item holds the one answer slot, bound to the sheet, checked by
// three criteria, after an item with no slot.
func essay() formatv0.Task {
	return formatv0.Task{Canvas: formatv0.Canvas{Kind: formatv0.CanvasKindHandwritingSheet},
		Statement: []formatv0.StatementPart{{Text: ptr(formatv0.Text("Write about a walk by the sea."))}},
		Items: []formatv0.Item{{}, {AnswerSlots: []formatv0.AnswerSlot{{Binding: formatv0.AnswerSlotBindingSheet,
			Check: formatv0.Check{Kind: formatv0.CheckKindRubric}}}}}}
}

// assessment is an assessment of a text on route, in language, by n criteria.
func assessment(route tutor.Route, language string, n int) tutor.RubricAssessment {
	a := tutor.RubricAssessment{Route: route, Text: "We walked by the sea.", Topic: "Write about a walk by the sea.",
		Style: "an adult", Language: language}
	for i := range n {
		a.Criteria = append(a.Criteria, fmt.Sprintf("Criterion %c of the rubric.", 'A'+i))
	}
	return a
}

// An essay is read by the shape of its sheet's crop: wider than tall the stub's
// text, taller than wide a blank sheet, square illegible, each an observation
// of the one slot bound to the sheet, with no crop box, which module-kit takes
// as an essay read or unreadable. One reader reads it, spending the expected
// price of the essay, and with no price nothing priced.
func TestTheStubReadsAnEssayByTheShapeOfTheCrop(t *testing.T) {
	t.Parallel()

	tk := essay()
	pricedEssay := gate{prices: map[tutor.Call]tutor.CallPrice{tutor.CallEssay: {ExpectedUSD: 0.012345, Expected: true}}}
	for name, tc := range map[string]struct {
		width, height int
		ended         observation.Submission
		unreadable    observationv1.Unreadable
	}{
		"wide":   {1200, 300, observation.SubmissionAssessment, 0},
		"tall":   {300, 1200, observation.SubmissionUnreadable, observationv1.Unreadable_UNREADABLE_BLANK},
		"square": {10, 10, observation.SubmissionUnreadable, observationv1.Unreadable_UNREADABLE_ILLEGIBLE},
	} {
		r := tutorstub.New(gate{}).ReadEssay(t.Context(), tutor.EssayReading{Route: adult, Task: &open.Task{}, Crop: crop(t, tc.width, tc.height)}, tk, "")
		if r.Outcome.Kind != tutor.Answered {
			t.Fatalf("%s: ReadEssay = %+v, want the essay read", name, r.Outcome)
		}
		ended, err := observation.Essay(tk.Items, r.Observation)
		slots := r.Observation.GetAnswerSlots()
		if err != nil || ended != tc.ended || len(slots) != 1 || slots[0].GetUnreadable() != tc.unreadable || slots[0].GetCropBox() != nil {
			t.Fatalf("%s: the essay reads %v, %v as %v; want %v, unreadable %v, with no crop box", name, slots, err, ended, tc.ended, tc.unreadable)
		}
		if p := slots[0].GetAnswerSlot(); p.GetItem() != 2 || p.GetSlot() != 1 {
			t.Errorf("%s: the slot read is %d.%d, want 2.1, the one bound to the sheet", name, p.GetItem(), p.GetSlot())
		}
		if u := r.Outcome.Usage; len(u) != 1 || u[0].Model != tutorstub.Model || u[0].Priced || u[0].USD != 0 {
			t.Errorf("%s: the reading spent %+v, want one row of the model stub, unpriced", name, u)
		}
	}
	wide := tutorstub.New(gate{}).ReadEssay(t.Context(), tutor.EssayReading{Route: adult, Task: &open.Task{}, Crop: crop(t, 40, 10)}, tk, "")
	if len(wide.Observation.GetAnswerSlots()) != 1 {
		t.Fatalf("a wide essay = %+v, want the one slot read", wide)
	}
	if text := wide.Observation.GetAnswerSlots()[0].GetValue(); strings.TrimSpace(text) == "" || strings.ContainsAny(text, "0123456789") {
		t.Errorf("the stub's text is %q, want words with no number", text)
	}
	if u := tutorstub.New(pricedEssay).ReadEssay(t.Context(), tutor.EssayReading{Route: adult, Task: &open.Task{}, Crop: crop(t, 40, 10)}, tk, "").Outcome.Usage; len(u) != 1 || !u[0].Priced || u[0].USD != 0.012345 {
		t.Errorf("a priced essay spent %+v, want its expected price", u)
	}
}

// What tutor's reading of an essay fails before its call the stub fails too,
// with no observation and no usage, on any key: a task that is no handwriting
// sheet, has no slot bound to the sheet, two, or one bound otherwise, no open
// view of the task, a crop that is no PNG or is larger than 4096 px on a side;
// and so does a request cut short.
func TestTheStubFailsAnEssayTutorFails(t *testing.T) {
	t.Parallel()

	wide := crop(t, 40, 10)
	page := essay()
	page.Canvas.Kind = formatv0.CanvasKindWorkbook
	twice := essay()
	twice.Items[0] = twice.Items[1]
	anywhere := essay()
	anywhere.Items[0].AnswerSlots = []formatv0.AnswerSlot{{Binding: formatv0.AnswerSlotBindingAnywhere}}
	for name, tc := range map[string]struct {
		task formatv0.Task
		crop []byte
	}{
		"a workbook page":        {page, wide},
		"no slot":                {formatv0.Task{Canvas: formatv0.Canvas{Kind: formatv0.CanvasKindHandwritingSheet}, Items: []formatv0.Item{{}}}, wide},
		"two slots of the sheet": {twice, wide},
		"a slot bound anywhere":  {anywhere, wide},
		"no PNG":                 {essay(), []byte("not a picture")},
		"too large a sheet":      {essay(), crop(t, 4097, 10)},
	} {
		for _, own := range []string{"", "sk-ant-synthetic" + tutorstub.KeyRefused} {
			r := tutorstub.New(priced).ReadEssay(t.Context(), tutor.EssayReading{Route: adult, Task: &open.Task{}, Crop: tc.crop}, tc.task, own)
			if r.Outcome.Kind != tutor.Failed || r.Observation != nil || len(r.Outcome.Usage) != 0 {
				t.Errorf("%s on %q: ReadEssay = %+v, want a failure", name, own, r)
			}
		}
	}
	if r := tutorstub.New(priced).ReadEssay(t.Context(), tutor.EssayReading{Route: adult, Crop: wide}, essay(), ""); r.Outcome.Kind != tutor.Failed {
		t.Errorf("an essay with no open view = %v, want a failure", r.Outcome.Kind)
	}
	if r := tutorstub.New(priced).ReadEssay(t.Context(), tutor.EssayReading{Route: adult, Task: &open.Task{}, Crop: crop(t, 4096, 10)}, essay(), ""); r.Outcome.Kind != tutor.Answered {
		t.Errorf("a sheet of 4096 px = %v, want read", r.Outcome.Kind)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if r := tutorstub.New(priced).ReadEssay(ctx, tutor.EssayReading{Route: adult, Task: &open.Task{}, Crop: wide}, essay(), ""); r.Outcome.Kind != tutor.Failed ||
		!errors.Is(r.Outcome.Err, context.Canceled) || r.Observation != nil {
		t.Errorf("ReadEssay cut short = %+v, want a failure", r)
	}
}

// The stub assesses every criterion met but the last, each with its synthetic
// evidence, and gives its feedback in English and Russian, English in another
// language: each passes words.Feedback against the text the stub reads and
// against any other, and the leak check of the task, as api checks it. The
// assessment spends the expected price of the rubric, and with none nothing
// priced.
func TestTheStubAssessesEveryCriterionButTheLast(t *testing.T) {
	t.Parallel()

	read := tutorstub.New(gate{}).ReadEssay(t.Context(), tutor.EssayReading{Route: adult, Task: &open.Task{}, Crop: crop(t, 40, 10)}, essay(), "")
	if len(read.Observation.GetAnswerSlots()) != 1 {
		t.Fatalf("ReadEssay = %+v, want the one slot read", read)
	}
	text := read.Observation.GetAnswerSlots()[0].GetValue()
	search, err := leak.For(essay())
	if err != nil {
		t.Fatal(err)
	}
	s := tutorstub.New(gate{})
	seen := map[string]string{}
	for _, language := range []string{"en", "ru", "ru-RU", "en_US", "de"} {
		a := assessment(adult, language, 3)
		a.Text = text
		got := s.AssessEssay(t.Context(), a, "")
		if got.Outcome.Kind != tutor.Answered || len(got.Criteria) != 3 {
			t.Fatalf("AssessEssay in %s = %+v, want three marks", language, got)
		}
		for i, m := range got.Criteria {
			if m.Met == nil || *m.Met != (i < 2) || m.Evidence == "" {
				t.Errorf("criterion %d in %s is marked %+v, want met but the last, with evidence", i+1, language, m)
			}
		}
		for _, against := range []string{text, "Something else entirely."} {
			if breaches := words.Feedback(got.Feedback, against, language); len(breaches) > 0 {
				t.Errorf("the feedback %q in %s against %q breaks %v", got.Feedback, language, against, breaches)
			}
		}
		if found := search.In(got.Feedback); len(found) > 0 {
			t.Errorf("the feedback %q leaks %v", got.Feedback, found)
		}
		if u := got.Outcome.Usage; len(u) != 1 || u[0].Model != tutorstub.Model || u[0].Priced || u[0].USD != 0 {
			t.Errorf("the assessment spent %+v, want one row of the model stub, unpriced", u)
		}
		seen[language] = got.Feedback
	}
	if seen["en"] == seen["ru"] || seen["ru"] != seen["ru-RU"] || seen["en"] != seen["en_US"] || seen["de"] != seen["en"] {
		t.Errorf("the feedback by language is %q, want English and Russian apart, and English in German", seen)
	}
	one := s.AssessEssay(t.Context(), assessment(adult, "en", 1), "")
	if len(one.Criteria) != 1 || one.Criteria[0].Met == nil || *one.Criteria[0].Met {
		t.Errorf("a rubric of one criterion is marked %+v, want it not met, as the last", one.Criteria)
	}
	pricedRubric := gate{prices: map[tutor.Call]tutor.CallPrice{tutor.CallRubric: {ExpectedUSD: 0.011, Expected: true}}}
	if u := tutorstub.New(pricedRubric).AssessEssay(t.Context(), assessment(adult, "en", 2), "").Outcome.Usage; len(u) != 1 || !u[0].Priced || u[0].USD != 0.011 {
		t.Errorf("a priced assessment spent %+v, want its expected price", u)
	}
}

// What tutor's assessment fails before its call the stub fails too, every
// criterion without a mark, with no feedback and no usage, on any key: no
// text, no criterion or a blank one, no topic, no style, a language the rule of
// feedback cannot check; and so does a request cut short.
func TestTheStubFailsAnAssessmentTutorFails(t *testing.T) {
	t.Parallel()

	for name, change := range map[string]func(*tutor.RubricAssessment){
		"no text":            func(a *tutor.RubricAssessment) { a.Text = " " },
		"no criterion":       func(a *tutor.RubricAssessment) { a.Criteria = nil },
		"a blank criterion":  func(a *tutor.RubricAssessment) { a.Criteria[1] = " " },
		"no topic":           func(a *tutor.RubricAssessment) { a.Topic = "" },
		"no style":           func(a *tutor.RubricAssessment) { a.Style = " " },
		"no language":        func(a *tutor.RubricAssessment) { a.Language = "" },
		"no language's code": func(a *tutor.RubricAssessment) { a.Language = "e1" },
		"the root":           func(a *tutor.RubricAssessment) { a.Language = "und" },
		"a tag that fails":   func(a *tutor.RubricAssessment) { a.Language = "en-not-a-tag-at-all" },
	} {
		for _, own := range []string{"", "sk-ant-synthetic" + tutorstub.KeyLimit} {
			a := assessment(adult, "en", 2)
			change(&a)
			got := tutorstub.New(priced).AssessEssay(t.Context(), a, own)
			if got.Outcome.Kind != tutor.Failed || got.Feedback != "" || len(got.Outcome.Usage) != 0 || len(got.Criteria) != len(a.Criteria) ||
				slices.ContainsFunc(got.Criteria, func(m tutor.CriterionMark) bool { return m.Met != nil }) {
				t.Errorf("%s on %q: AssessEssay = %+v, want a failure with no mark", name, own, got)
			}
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got := tutorstub.New(priced).AssessEssay(ctx, assessment(adult, "en", 2), ""); got.Outcome.Kind != tutor.Failed ||
		!errors.Is(got.Outcome.Err, context.Canceled) || got.Feedback != "" {
		t.Errorf("AssessEssay cut short = %+v, want a failure", got)
	}
}

// By one reader, a sheet and a workbook page read into one observation, which
// module-kit takes as it is, and the reading spends the whole of what tutor
// expects it to cost on one row: the one reader's.
func TestTheStubReadsByOneReaderInTheModeSingle(t *testing.T) {
	t.Parallel()

	single := priced
	single.reading = tutor.ReadingSingle
	tk := task(formatv0.Check{Kind: formatv0.CheckKindNumber, Key: ptr("12")})
	sheet := tutorstub.New(single).ReadSheet(t.Context(), tutor.SheetReading{Route: adult, Crop: crop(t, 40, 10)}, tk, "")
	if sheet.Outcome.Kind != tutor.Answered || len(sheet.Observations) != 1 {
		t.Fatalf("ReadSheet = %v with %d observations, want the one reader's", sheet.Outcome.Kind, len(sheet.Observations))
	}
	if err := observation.Validate(tk.Items, sheet.Observations[0]); err != nil || sheet.Observations[0].GetAnswerSlots()[0].GetValue() != "12" {
		t.Errorf("the one reading %v, %v; want the right value, valid", sheet.Observations[0], err)
	}
	if want := quota.USD(0.020001); len(sheet.Outcome.Usage) != 1 || quota.USD(sheet.Outcome.Usage[0].USD) != want {
		t.Errorf("the reading spent %v, want the reserve, %d, on one row", sheet.Outcome.Usage, want)
	}

	book := book()
	page := tutor.WorkbookReading{Route: adult, Boxes: []tutor.BoxCrop{boxOf(1, 1, crop(t, 30, 10))}}
	read := tutorstub.New(single).ReadWorkbook(t.Context(), page, book, "")
	if read.Outcome.Kind != tutor.Answered || len(read.Observations) != 1 || len(read.Outcome.Usage) != 1 {
		t.Fatalf("ReadWorkbook = %v with %d observations over %d rows, want the one reader's", read.Outcome.Kind,
			len(read.Observations), len(read.Outcome.Usage))
	}
	if err := observation.Validate(book.Items, read.Observations[0]); err != nil {
		t.Errorf("module-kit does not take the one reading of the page: %v", err)
	}
}

// A task from paper by shapes: the statement wider than tall reads into one
// task of the formula the solver takes, at least twice as tall into three, and
// any other into one of none; a square statement has no one answer, any other
// the stub's number for each task asked; the work of one task reads the key
// more than twice as wide as tall, a wrong value wider but not twice, illegible
// square or taller, and of several the key in each, with its band of the crop.
// Each call spends its expected price, and what is no picture fails before it.
func TestTheStubChecksATaskFromPaperByShapes(t *testing.T) {
	t.Parallel()

	s := tutorstub.New(gate{prices: map[tutor.Call]tutor.CallPrice{tutor.CallStatement: {ExpectedUSD: 0.002, Expected: true}}})
	task := func(width, height int) tutor.PaperTask {
		return tutor.PaperTask{Route: adult, Statement: photo(t, width, height)}
	}
	if r := s.ReadStatement(t.Context(), task(400, 300), ""); r.Outcome.Kind != tutor.Answered || len(r.Tasks) != 1 || r.Tasks[0].Formula != "37+48" || r.Outcome.Usage[0].USD != 0.002 {
		t.Errorf("a wide statement: %+v, want 37+48", r)
	}
	if r := s.ReadStatement(t.Context(), task(150, 400), ""); r.Outcome.Kind != tutor.Answered || len(r.Tasks) != 3 || r.Tasks[2].Formula != "50+36" {
		t.Errorf("twice as tall: %+v, want three tasks", r)
	}
	if r := s.ReadStatement(t.Context(), task(300, 400), ""); r.Outcome.Kind != tutor.Answered || len(r.Tasks) != 1 || r.Tasks[0].Formula != "" || r.Tasks[0].Statement == "" {
		t.Errorf("a tall statement: %+v, want one task of no formula", r)
	}
	if r := s.ReadStatement(t.Context(), tutor.PaperTask{Route: adult, Statement: []byte("no")}, ""); r.Outcome.Kind != tutor.Failed || len(r.Outcome.Usage) != 0 {
		t.Errorf("no picture: %+v, want a failure before the call", r)
	}
	asked := []tutor.TaskToSolve{{Number: 1, Statement: "a"}, {Number: 3, Statement: "c"}}
	if r := s.SolveTask(t.Context(), task(300, 400), asked, ""); r.Outcome.Kind != tutor.Answered || len(r.Answers) != 2 || r.Answers[3].GetNumber().GetValue() != "85" {
		t.Errorf("a tall statement solved: %+v, want 85 for each", r)
	}
	if r := s.SolveTask(t.Context(), task(300, 300), asked, ""); r.Outcome.Kind != tutor.Answered || r.Answers[1] != nil {
		t.Errorf("a square statement solved: %+v, want no answer", r)
	}
	if r := s.SolveTask(t.Context(), task(300, 400), asked, "sk-ant-synthetic"+tutorstub.KeyRefused); r.Outcome.Kind != tutor.KeyRefused || len(r.Answers) != 0 {
		t.Errorf("a refused key: %+v", r)
	}
	for _, c := range []struct {
		width, height int
		value         string
	}{{90, 30, "85"}, {40, 30, "851"}, {30, 30, ""}, {30, 40, ""}} {
		r := s.ReadPaper(t.Context(), tutor.PaperReading{Route: adult, Crop: crop(t, c.width, c.height), Tasks: []string{"37 + 48 ="}}, "")
		if r.Outcome.Kind != tutor.Answered || len(r.Observations) != 1 || r.Observations[0].GetAnswerSlots()[0].GetValue() != c.value ||
			(c.value == "") != (r.Observations[0].GetAnswerSlots()[0].GetUnreadable() == observationv1.Unreadable_UNREADABLE_ILLEGIBLE) {
			t.Errorf("a work of %dx%d: %+v, want %q by one reader", c.width, c.height, r, c.value)
		}
	}
	r := s.ReadPaper(t.Context(), tutor.PaperReading{Route: adult, Crop: crop(t, 30, 90), Tasks: []string{"a", "b", "c"}}, "")
	if slots := r.Observations[0].GetAnswerSlots(); r.Outcome.Kind != tutor.Answered || len(slots) != 3 || slots[2].GetAnswerSlot().GetItem() != 3 ||
		slots[2].GetValue() != "85" || slots[1].GetCropBox().GetYmin() != 333 || slots[1].GetCropBox().GetYmax() != 666 {
		t.Errorf("a work of three tasks: %+v, want 85 in each, in its band", r)
	}
	if r := s.ReadPaper(t.Context(), tutor.PaperReading{Route: adult, Crop: crop(t, 40, 30)}, ""); r.Outcome.Kind != tutor.Failed {
		t.Errorf("no task: %+v, want a failure", r)
	}
	if r := s.ReadPaper(t.Context(), tutor.PaperReading{Route: adult, Crop: photo(t, 40, 30), Tasks: []string{"a"}}, ""); r.Outcome.Kind != tutor.Failed {
		t.Errorf("a JPEG of the work: %+v, want a failure", r)
	}
	if r := s.ReadPaper(t.Context(), tutor.PaperReading{Route: adult, Crop: crop(t, 40, 30), Tasks: []string{"a"}}, "sk-ant-synthetic"+tutorstub.KeyRefused); r.Outcome.Kind != tutor.KeyRefused {
		t.Errorf("a refused key: %+v", r)
	}
}
