package learning

import (
	"testing"

	"github.com/altnovatrail/spec/types/formatv0"

	"github.com/altnovatrail/api/internal/store"
)

// A work is an essay when an item of its task has a rubric, handwriting when an
// answer slot is checked otherwise, and a drawing when none is checked: a task
// of no item, of items with no answer slot, or with boxes that hold no check.
func TestTheKindOfAWorkIsByItsTask(t *testing.T) {
	t.Parallel()

	box := formatv0.LinePartBindingBox
	slot := func(kind formatv0.CheckKind) formatv0.AnswerSlot {
		return formatv0.AnswerSlot{Binding: formatv0.AnswerSlotBindingSheet, Check: formatv0.Check{Kind: kind}}
	}
	for name, c := range map[string]struct {
		items []formatv0.Item
		want  string
	}{
		"no item":           {nil, store.WorkDrawing},
		"no answer slot":    {[]formatv0.Item{{}}, store.WorkDrawing},
		"a box of no check": {[]formatv0.Item{{Line: []formatv0.LinePart{{Binding: &box}}}}, store.WorkDrawing},
		"a key":             {[]formatv0.Item{{AnswerSlots: []formatv0.AnswerSlot{slot(formatv0.CheckKindNumber)}}}, store.WorkHandwriting},
		"a predicate":       {[]formatv0.Item{{}, {AnswerSlots: []formatv0.AnswerSlot{slot(formatv0.CheckKindPredicate)}}}, store.WorkHandwriting},
		"a key and a rubric": {[]formatv0.Item{{AnswerSlots: []formatv0.AnswerSlot{slot(formatv0.CheckKindChoice)}},
			{AnswerSlots: []formatv0.AnswerSlot{slot(formatv0.CheckKindRubric)}}}, store.WorkEssay},
	} {
		if got := workKind(formatv0.Task{Items: c.items}); got != c.want {
			t.Errorf("%s: the kind = %s, want %s", name, got, c.want)
		}
	}
}
