package learning

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"google.golang.org/protobuf/proto"
)

// twoLines is a workbook page of two items, "3 + [ ] = [ ]" and "[ ] apples",
// with three boxes in all: the first item's two and the second's one.
func twoLines() formatv0.Task {
	box := formatv0.LinePartBindingBox
	key := func(k string) *formatv0.Check { return &formatv0.Check{Kind: formatv0.CheckKindNumber, Key: &k} }
	text := func(s string) formatv0.LinePart {
		t := formatv0.Text(s)
		return formatv0.LinePart{Text: &t}
	}
	return formatv0.Task{ID: "lines", Canvas: formatv0.Canvas{Kind: formatv0.CanvasKindWorkbook}, Items: []formatv0.Item{
		{Line: []formatv0.LinePart{text("3 +"), {Binding: &box, Check: key("4")}, text("="), {Binding: &box, Check: key("7")}}},
		{Line: []formatv0.LinePart{{Binding: &box, Check: key("2")}, text("apples")}},
	}}
}

// grey is a PNG of width by height.
func grey(t testing.TB, width, height int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// The kind of call of a task is by its canvas: a sheet with one answer slot
// bound anywhere is read as a sheet, a workbook as a page, and one a tap checks,
// a zone, the whole sheet or a workbook with no box by none.
func TestTheKindOfCallIsByTheCanvas(t *testing.T) {
	t.Parallel()

	sheetOf := func(bindings ...formatv0.AnswerSlotBinding) formatv0.Task {
		task := formatv0.Task{Canvas: formatv0.Canvas{Kind: formatv0.CanvasKindHandwritingSheet}}
		for _, b := range bindings {
			task.Items = append(task.Items, formatv0.Item{AnswerSlots: []formatv0.AnswerSlot{{Binding: b}}})
		}
		return task
	}
	for name, c := range map[string]struct {
		task formatv0.Task
		call string
	}{
		"anywhere":           {sheetOf(formatv0.AnswerSlotBindingAnywhere, formatv0.AnswerSlotBindingChoice), "sheet"},
		"a tap alone":        {sheetOf(formatv0.AnswerSlotBindingChoice), ""},
		"a zone":             {sheetOf(formatv0.AnswerSlotBindingZone), ""},
		"the whole sheet":    {sheetOf(formatv0.AnswerSlotBindingSheet), ""},
		"a workbook":         {twoLines(), "workbook"},
		"a workbook, no box": {formatv0.Task{Canvas: formatv0.Canvas{Kind: formatv0.CanvasKindWorkbook}, Items: []formatv0.Item{{}}}, ""},
		"a workbook, a zone": {formatv0.Task{Canvas: formatv0.Canvas{Kind: formatv0.CanvasKindWorkbook},
			Items: sheetOf(formatv0.AnswerSlotBindingZone).Items}, ""},
	} {
		call, ok := callOf(c.task)
		if string(call) != c.call || ok != (c.call != "") {
			t.Errorf("%s: the kind of call = %q, %v; want %q", name, call, ok, c.call)
		}
	}
}

// Whatever app sends as the work of a workbook page, boxesTaken does not panic,
// and what it takes is what tutor reads: no more than 20 boxes, each of a box of
// the page, by the places of its item and box, from 1, none twice, its crop the
// PNG sent, whole and no larger than 512 px a side.
func FuzzWorkbookWork(f *testing.F) {
	task := twoLines()
	small, wide := grey(f, 30, 20), grey(f, 513, 2)
	place := func(item, slot int32) *observationv1.AnswerSlotPlace {
		return &observationv1.AnswerSlotPlace{Item: item, Slot: slot}
	}
	many := make([]*v1.BoxCrop, 21)
	for i := range many {
		many[i] = &v1.BoxCrop{AnswerSlot: place(1, 1), Crop: small}
	}
	for _, seed := range []*v1.WorkbookWork{
		{},
		{Boxes: []*v1.BoxCrop{{AnswerSlot: place(1, 1), Crop: small}, {AnswerSlot: place(1, 2), Crop: small}, {AnswerSlot: place(2, 1), Crop: small}}},
		{Boxes: []*v1.BoxCrop{{AnswerSlot: place(2, 1), Crop: small}, {AnswerSlot: place(1, 1), Crop: small}}},
		{Boxes: []*v1.BoxCrop{{AnswerSlot: place(1, 1), Crop: small}, {AnswerSlot: place(1, 1), Crop: small}}},
		{Boxes: []*v1.BoxCrop{{AnswerSlot: place(2, 2), Crop: small}}},
		{Boxes: []*v1.BoxCrop{{AnswerSlot: place(3, 1), Crop: small}}},
		{Boxes: []*v1.BoxCrop{{AnswerSlot: place(-1, 1), Crop: small}}},
		{Boxes: []*v1.BoxCrop{{Crop: small}}},
		{Boxes: []*v1.BoxCrop{{AnswerSlot: place(1, 1), Crop: wide}}},
		{Boxes: []*v1.BoxCrop{{AnswerSlot: place(1, 1), Crop: small[:len(small)-5]}}},
		{Boxes: []*v1.BoxCrop{{AnswerSlot: place(1, 1)}}},
		{Boxes: many},
	} {
		data, err := proto.Marshal(seed)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		work := &v1.WorkbookWork{}
		if proto.Unmarshal(data, work) != nil {
			return
		}
		boxes, ok := boxesTaken(task, work.GetBoxes())
		if !ok {
			if boxes != nil {
				t.Fatalf("a page refused gives %d boxes", len(boxes))
			}
			return
		}
		if len(boxes) != len(work.GetBoxes()) || len(boxes) > maxBoxes {
			t.Fatalf("%d boxes taken of %d sent, more than %d or not all", len(boxes), len(work.GetBoxes()), maxBoxes)
		}
		seen := map[[2]int]bool{}
		for i, b := range boxes {
			if b.Item < 1 || b.Item > len(task.Items) || b.Slot < 1 || b.Slot > 2 || (b.Item == 2 && b.Slot > 1) || seen[[2]int{b.Item, b.Slot}] {
				t.Fatalf("a box taken at item %d, box %d, which the page has not, or twice", b.Item, b.Slot)
			}
			seen[[2]int{b.Item, b.Slot}] = true
			sent := work.GetBoxes()[i]
			if int(sent.GetAnswerSlot().GetItem()) != b.Item || int(sent.GetAnswerSlot().GetSlot()) != b.Slot || !bytes.Equal(sent.GetCrop(), b.Crop) {
				t.Fatalf("the box taken %d.%d is not the one sent", b.Item, b.Slot)
			}
			cfg, err := png.DecodeConfig(bytes.NewReader(b.Crop))
			if err != nil || cfg.Width > maxBoxSide || cfg.Height > maxBoxSide {
				t.Fatalf("a crop taken of %d × %d (%v)", cfg.Width, cfg.Height, err)
			}
			if _, err := png.Decode(bytes.NewReader(b.Crop)); err != nil {
				t.Fatalf("a crop taken that is not whole: %v", err)
			}
		}
	})
}
