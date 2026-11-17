package learning_test

import (
	"bytes"
	"context"
	"slices"
	"sync"
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/quota"
)

// page is the work of a workbook page, with the crops of its filled boxes.
func page(boxes ...*v1.BoxCrop) *v1.Work {
	return &v1.Work{Surface: &v1.Work_Workbook{Workbook: &v1.WorkbookWork{Boxes: boxes}}}
}

// boxCrop is the crop of the box at the slot of the item, each from 1.
func boxCrop(item, slot int32, crop []byte) *v1.BoxCrop {
	return &v1.BoxCrop{AnswerSlot: &observationv1.AnswerSlotPlace{Item: item, Slot: slot}, Crop: crop}
}

// blankBox is the observation of a reader of the box of table left blank, as
// tutor gives it for a box that sent no crop.
func blankBox() *observationv1.Observation {
	return &observationv1.Observation{AnswerSlots: []*observationv1.AnswerSlotObservation{{
		AnswerSlot: &observationv1.AnswerSlotPlace{Item: 1, Slot: 1},
		Read:       &observationv1.AnswerSlotObservation_Unreadable{Unreadable: observationv1.Unreadable_UNREADABLE_BLANK},
	}}}
}

// pageReaders are the readers of a test's workbook pages: what they give next,
// and what they were given.
type pageReaders struct {
	mu    sync.Mutex
	next  tutor.WorkbookRead
	given []tutor.WorkbookReading
}

func (ps *pageReaders) read(_ context.Context, r tutor.WorkbookReading) tutor.WorkbookRead {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.given = append(ps.given, r)
	return ps.next
}

func (ps *pageReaders) give(got tutor.WorkbookRead) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.next = got
}

func (ps *pageReaders) count() int {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return len(ps.given)
}

// readPage is a reading of a workbook page by the two readers.
func readPage(first, second *observationv1.Observation) tutor.WorkbookRead {
	return tutor.WorkbookRead{Observations: []*observationv1.Observation{first, second}, Outcome: tutor.Outcome{Kind: tutor.Answered}}
}

// newBookWorld is the world of a deployment that admits the reading of an
// adult's handwriting on a sheet, and, by the test's own admission, on a
// workbook page too, read by ps, for want of a provider. opts are more of the world's options.
func newBookWorld(t *testing.T, ps *pageReaders, opts ...learning.Option) *world {
	t.Helper()
	return worldOf(t, gate(t, adultFacts, config.RungAuthor), append([]learning.Option{
		learning.WithAdmits(func(tutor.Call) bool { return true }), learning.WithWorkbookReader(ps.read)}, opts...))
}

// A workbook page begins where its kind of call is admitted, and is read by its
// filled boxes: each box's verdict is module-kit's on what the two readers
// agree, a page left blank is unreadable and no error, and a call that gives no
// reading leaves the boxes unchecked with its outcome. The readers get the crops
// by the places of their items and boxes, the open view and the route.
func TestAWorkbookPageIsReadByItsBoxes(t *testing.T) {
	t.Parallel()

	ps := &pageReaders{}
	w := newBookWorld(t, ps)
	l := w.learner(t)
	png := pngOf(t, 40, 30)
	var attempt string
	for _, tc := range []struct {
		name    string
		got     tutor.WorkbookRead
		work    *v1.Work
		own     *v1.OwnKey
		verdict string
		wrong   int32
		cause   v1.Cause
	}{
		{"not yet", readPage(read("5"), read("5")), page(boxCrop(1, 1, png)), nil, "not_yet[not_yet 5]", 1, v1.Cause_CAUSE_UNSPECIFIED},
		{"two readings that differ", readPage(read("4"), read("9")), page(boxCrop(1, 1, png)), nil,
			"unreadable[unreadable ambiguous]", 0, v1.Cause_CAUSE_UNSPECIFIED},
		{"a page left blank", readPage(blankBox(), blankBox()), page(), nil, "unreadable[unreadable blank]", 0, v1.Cause_CAUSE_UNSPECIFIED},
		{"not admitted", tutor.WorkbookRead{Outcome: tutor.Outcome{Kind: tutor.NotAdmitted}}, page(boxCrop(1, 1, png)), nil,
			"unchecked[unchecked ]", 0, v1.Cause_CAUSE_NOT_ADMITTED},
		{"the own key's limit", tutor.WorkbookRead{Outcome: tutor.Outcome{Kind: tutor.LimitReached, Limit: tutor.LimitTier}},
			page(boxCrop(1, 1, png)), &v1.OwnKey{Provider: "anthropic", Key: "sk-own"}, "unchecked[unchecked ]", 0, v1.Cause_CAUSE_LIMIT_REACHED},
		{"one reader alone", tutor.WorkbookRead{Observations: []*observationv1.Observation{read("4")}, Outcome: tutor.Outcome{Kind: tutor.Answered}},
			page(boxCrop(1, 1, png)), nil, "unchecked[unchecked ]", 0, v1.Cause_CAUSE_AI_FAILURE},
		{"a reading of no box", readPage(&observationv1.Observation{}, read("4")), page(boxCrop(1, 1, png)), nil,
			"unchecked[unchecked ]", 0, v1.Cause_CAUSE_AI_FAILURE},
		{"correct", readPage(read("4"), read("4")), page(boxCrop(1, 1, png)), nil, "correct[correct 4]", 0, v1.Cause_CAUSE_UNSPECIFIED},
	} {
		attempt = w.begin(t, l, l.adult, "table").GetAttemptId()
		ps.give(tc.got)
		res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: attempt,
			IdempotencyKey: uuid.NewString(), Work: tc.work, OwnKey: tc.own})
		t.Run(tc.name, func(t *testing.T) {
			if err != nil {
				t.Fatal(err)
			}
			if got := verdicts(res); got != tc.verdict || res.GetWrongAnswers() != tc.wrong || res.GetOutcome().GetCause() != tc.cause {
				t.Errorf("SubmitWork = %s, %d wrong, %v; want %s, %d, %v", got, res.GetWrongAnswers(), res.GetOutcome().GetCause(),
					tc.verdict, tc.wrong, tc.cause)
			}
		})
	}
	if res := w.closeOf(t, attempt); res != "correct" {
		t.Errorf("the last attempt is closed %q, want correct", res)
	}
	blank, last := ps.given[2], ps.given[len(ps.given)-1]
	if len(blank.Boxes) != 0 {
		t.Errorf("the page left blank went with %d boxes, want none", len(blank.Boxes))
	}
	if len(last.Boxes) != 1 || last.Boxes[0].Item != 1 || last.Boxes[0].Slot != 1 || !bytes.Equal(last.Boxes[0].Crop, png) ||
		last.Task.ID != "table" || last.Task.Canvas.Kind != formatv0.CanvasKindWorkbook ||
		last.Route.AI != admissionv0.AiRoutePlatformAi || last.Route.Category != admissionv0.AgeCategoryAdult {
		t.Errorf("the readers were given %+v of %s by %v, want the box 1.1, table and platform AI for an adult",
			last.Boxes, last.Task.ID, last.Route)
	}
	if own := ps.given[4]; own.Route.AI != admissionv0.AiRouteOwnKey || own.Route.Own.Provider() != "anthropic" {
		t.Errorf("the own key's reading went by %v, want the own key of anthropic", own.Route)
	}
}

// Each submission of a workbook page whose form tutor does not take is refused
// before any reading, as is the work of a sheet on a workbook and of a page on a
// sheet.
func TestAWorkbookPageTheTaskDoesNotTakeIsRefused(t *testing.T) {
	t.Parallel()

	ps := &pageReaders{}
	ps.give(readPage(read("4"), read("4")))
	w := newBookWorld(t, ps)
	l := w.learner(t)
	png := pngOf(t, 12, 12)
	many := make([]*v1.BoxCrop, 21)
	for i := range many {
		many[i] = boxCrop(1, 1, png)
	}
	for name, work := range map[string]*v1.Work{
		"a sheet's crop":         sheet(png),
		"no answer slot":         page(&v1.BoxCrop{Crop: png}),
		"an item the page lacks": page(boxCrop(2, 1, png)),
		"a box the item lacks":   page(boxCrop(1, 2, png)),
		"item 0":                 page(boxCrop(0, 1, png)),
		"slot 0":                 page(boxCrop(1, 0, png)),
		"a box twice":            page(boxCrop(1, 1, png), boxCrop(1, 1, png)),
		"more boxes than tutor":  page(many...),
		"a crop that is no PNG":  page(boxCrop(1, 1, []byte{1, 2, 3})),
		"a crop cut short":       page(boxCrop(1, 1, png[:40])),
		"a crop wider than 512":  page(boxCrop(1, 1, pngOf(t, 513, 4))),
		"a crop taller than 512": page(boxCrop(1, 1, pngOf(t, 4, 513))),
	} {
		attempt := w.begin(t, l, l.adult, "table").GetAttemptId()
		_, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: attempt,
			IdempotencyKey: uuid.NewString(), Work: work})
		t.Run(name, func(t *testing.T) { refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED) })
	}
	attempt := w.begin(t, l, l.adult, "apples").GetAttemptId()
	_, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: attempt,
		IdempotencyKey: uuid.NewString(), Work: page(boxCrop(1, 1, png))})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	if ps.count() != 0 {
		t.Errorf("the readers read %d pages refused, want none", ps.count())
	}
	// The largest page tutor reads goes: a box of 512 px a side.
	attempt = w.begin(t, l, l.adult, "table").GetAttemptId()
	if res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: attempt,
		IdempotencyKey: uuid.NewString(), Work: page(boxCrop(1, 1, pngOf(t, 512, 512)))}); err != nil || verdicts(res) != "correct[correct 4]" {
		t.Errorf("a box of 512 × 512 = %v, %v; want it read", res, err)
	}
}

// The reading of a workbook page reserves the value of a page's kind for each
// of its filled boxes when tutor expects no price, and a page left blank, which
// goes to no model, reserves and spends nothing (R81).
func TestAWorkbookPageReservesByItsBoxes(t *testing.T) {
	t.Parallel()

	ps := &pageReaders{}
	var mu sync.Mutex
	var during []quota.Counter
	var w *world
	var l learner
	ps.give(readPage(read("5"), read("5")))
	priced := []tutor.Call{}
	w = newBookWorld(t, ps, learning.WithPrice(func(call tutor.Call, _ tutor.Route) tutor.CallPrice {
		mu.Lock()
		defer mu.Unlock()
		priced = append(priced, call)
		return tutor.CallPrice{}
	}), learning.WithWorkbookReader(func(ctx context.Context, r tutor.WorkbookReading) tutor.WorkbookRead {
		profile, _ := w.counted(t, l.accountID, l.adult)
		mu.Lock()
		during = append(during, profile)
		mu.Unlock()
		return ps.read(ctx, r)
	}))
	l = w.learner(t)
	attempt := w.begin(t, l, l.adult, "table").GetAttemptId()
	if _, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: attempt,
		IdempotencyKey: uuid.NewString(), Work: page(boxCrop(1, 1, pngOf(t, 9, 9)))}); err != nil {
		t.Fatal(err)
	}
	ps.give(readPage(blankBox(), blankBox()))
	if _, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: attempt,
		IdempotencyKey: uuid.NewString(), Work: page()}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := (quota.Counter{Reserved: 14_000}); len(during) != 2 || during[0] != want {
		t.Errorf("the profile's counter during the readings = %+v, want %+v first", during, want)
	}
	// The blank page reserved nothing: the first reading's reserve is spent.
	if want := (quota.Counter{Spent: 14_000}); during[1] != want {
		t.Errorf("the profile's counter during the blank page = %+v, want %+v", during[1], want)
	}
	if !slices.Equal(priced, []tutor.Call{tutor.CallWorkbook}) {
		t.Errorf("the calls priced = %v, want the workbook page's once", priced)
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile != (quota.Counter{Spent: 14_000}) {
		t.Errorf("the profile's counter after = %+v, want 14 000 spent", profile)
	}
	if got := quota.Reserve(tutor.CallWorkbook, tutor.CallPrice{}, 3); got != 42_000 {
		t.Errorf("three boxes reserve %d, want 42 000", got)
	}
}

// A workbook page begins, and enters the engine's pool, by the admission of the
// reading of a workbook page, CallWorkbook, and that of a sheet admits none.
func TestAWorkbookPageBeginsByItsKindOfCall(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		admitted tutor.Call
		begins   bool
	}{
		"the workbook's call admitted": {tutor.CallWorkbook, true},
		"the sheet's alone":            {tutor.CallSheet, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := worldOf(t, gate(t, adultFacts, config.RungAuthor), []learning.Option{
				learning.WithAdmits(func(call tutor.Call) bool { return call == tc.admitted })})
			l := w.learner(t)
			_, err := w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString(),
				Start: &v1.BeginAttemptRequest_ChoiceId{ChoiceId: w.choice(t, l.adult, "table")}})
			if tc.begins && err != nil {
				t.Errorf("the workbook page = %v, want it begun", err)
			}
			if !tc.begins {
				refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED)
			}
			pool, _, err := w.service.Candidates(w.current, formatv0.AgeCategoryAdult, "")
			if err != nil || slices.Contains(pool, module+"/task/table") != tc.begins {
				t.Errorf("the pool = %v, %v; want table in it %v", pool, err, tc.begins)
			}
		})
	}
}

// The words of a rung and those after a correct answer take no work of a
// workbook page: with it they are malformed, and without it they go as for a
// task a tap checks, by the hint without the work.
func TestTheWordsOfAWorkbookPageGoWithoutItsWork(t *testing.T) {
	t.Parallel()

	ps, ts := &pageReaders{}, &tellers{}
	ps.give(readPage(read("5"), read("5")))
	ts.say("What do you add to 3 to make 7?", nil)
	w := newBookWorld(t, ps, learning.WithWords(ts.tell))
	l := w.learner(t)
	png := pngOf(t, 9, 9)
	attempt := w.begin(t, l, l.adult, "table").GetAttemptId()
	res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: attempt,
		IdempotencyKey: uuid.NewString(), Work: page(boxCrop(1, 1, png))})
	if err != nil || res.GetWrongAnswers() != 1 {
		t.Fatalf("SubmitWork = %v, %v; want a wrong answer", res, err)
	}
	wrong := int32(1)
	_, err = w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: attempt,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong, Work: page(boxCrop(1, 1, png))})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	words, err := w.rungWords(l, l.adult, attempt, 1)
	if err != nil || words.GetWords() != "What do you add to 3 to make 7?" {
		t.Fatalf("GetRungWords with no work = %v, %v; want the tutor's words", words, err)
	}
	if asked := ts.last(); len(asked.Crop) != 0 || asked.Task.ID != "table" {
		t.Errorf("the words were asked with %d bytes of crop on %s, want none on table", len(asked.Crop), asked.Task.ID)
	}

	ps.give(readPage(read("4"), read("4")))
	if res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: attempt,
		IdempotencyKey: uuid.NewString(), Work: page(boxCrop(1, 1, png))}); err != nil || res.GetSolved() == nil {
		t.Fatalf("SubmitWork = %v, %v; want it solved", res, err)
	}
	_, err = w.attempts.GetCorrectAnswerWords(l.ctx, &v1.GetCorrectAnswerWordsRequest{ProfileId: l.adult, AttemptId: attempt,
		IdempotencyKey: uuid.NewString(), Work: page(boxCrop(1, 1, png))})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	if _, err := w.attempts.GetCorrectAnswerWords(l.ctx, &v1.GetCorrectAnswerWordsRequest{ProfileId: l.adult, AttemptId: attempt,
		IdempotencyKey: uuid.NewString()}); err != nil {
		t.Errorf("GetCorrectAnswerWords with no work = %v, want an answer", err)
	}
}

// The gate's adapter reads a workbook page by tutor, which calls no model for a
// page with no task, and admits by the gate.
func TestTheGateTutorReadsAWorkbookPage(t *testing.T) {
	t.Parallel()

	g := learning.GateTutor(gate(t, adultFacts, config.RungAuthor))
	read := g.ReadWorkbook(t.Context(), tutor.WorkbookReading{
		Route: tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategoryAdult},
	}, formatv0.Task{}, "")
	if read.Outcome.Kind != tutor.Failed || len(read.Outcome.Usage) != 0 {
		t.Errorf("ReadWorkbook with no task = %v, want a failure with no call", read.Outcome.Kind)
	}
	if !g.Reads(tutor.CallSheet, formatv0.AgeCategoryAdult, "") || !g.Reads(tutor.CallWorkbook, formatv0.AgeCategoryAdult, "") {
		t.Error("the gate's adapter admits otherwise than the gate: the sheet and the workbook page")
	}
	if full := learning.GateTutor(gate(t, adultFacts, config.RungFull)); full.Reads(tutor.CallWorkbook, formatv0.AgeCategoryAdult, "") {
		t.Error("the gate's adapter admits a workbook page at the stage full, which admits none (R90)")
	}
}

// In the mode single a workbook page is read by one reader (R88): each box's
// verdict is module-kit's on its one observation, which the submission keeps,
// and two observations are a failure of AI.
func TestAWorkbookPageIsReadByOneReaderInTheModeSingle(t *testing.T) {
	t.Parallel()

	ps := &pageReaders{}
	w := worldOf(t, gate(t, adultFacts, config.RungAuthor, "API_READING_MODE=single"), []learning.Option{
		learning.WithAdmits(func(tutor.Call) bool { return true }), learning.WithWorkbookReader(ps.read)})
	l := w.learner(t)
	png := pngOf(t, 40, 30)
	one := func(o *observationv1.Observation) tutor.WorkbookRead {
		return tutor.WorkbookRead{Observations: []*observationv1.Observation{o}, Outcome: tutor.Outcome{Kind: tutor.Answered}}
	}
	for _, tc := range []struct {
		name    string
		got     tutor.WorkbookRead
		work    *v1.Work
		verdict string
		cause   v1.Cause
	}{
		{"not yet", one(read("5")), page(boxCrop(1, 1, png)), "not_yet[not_yet 5]", v1.Cause_CAUSE_UNSPECIFIED},
		{"a page left blank", one(blankBox()), page(), "unreadable[unreadable blank]", v1.Cause_CAUSE_UNSPECIFIED},
		{"two readings", readPage(read("4"), read("4")), page(boxCrop(1, 1, png)), "unchecked[unchecked ]", v1.Cause_CAUSE_AI_FAILURE},
		{"correct", one(read("4")), page(boxCrop(1, 1, png)), "correct[correct 4]", v1.Cause_CAUSE_UNSPECIFIED},
	} {
		attempt := w.begin(t, l, l.adult, "table").GetAttemptId()
		ps.give(tc.got)
		res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: attempt,
			IdempotencyKey: uuid.NewString(), Work: tc.work})
		t.Run(tc.name, func(t *testing.T) {
			if err != nil {
				t.Fatal(err)
			}
			if got := verdicts(res); got != tc.verdict || res.GetOutcome().GetCause() != tc.cause {
				t.Errorf("SubmitWork = %s, %v; want %s, %v", got, res.GetOutcome().GetCause(), tc.verdict, tc.cause)
			}
		})
	}
	var observed []byte
	if err := w.pool.QueryRow(t.Context(), `SELECT observation FROM submissions ORDER BY submitted_at DESC, number DESC LIMIT 1`).
		Scan(&observed); err != nil {
		t.Fatal(err)
	}
	o := &observationv1.Observation{}
	if err := proto.Unmarshal(observed, o); err != nil || !proto.Equal(o, read("4")) {
		t.Errorf("the submission keeps %v (%v), want the one reader's observation", o, err)
	}
}
