package learning

import (
	"slices"
	"strings"
	"testing"

	"github.com/altnovatrail/spec/types/formatv0"
)

func ptr[T any](v T) *T { return &v }

// slotTask is a task on a handwriting sheet of one item with the answer slots
// given.
func slotTask(slots ...formatv0.AnswerSlot) formatv0.Task {
	return formatv0.Task{Canvas: formatv0.Canvas{Kind: formatv0.CanvasKindHandwritingSheet},
		Items: []formatv0.Item{{AnswerSlots: slots}}}
}

func traps(values ...string) []formatv0.TrapValue {
	var out []formatv0.TrapValue
	for _, v := range values {
		out = append(out, formatv0.TrapValue{Value: v, Trap: "first-seen"})
	}
	return out
}

func number(key string, trapValues ...string) formatv0.AnswerSlot {
	return formatv0.AnswerSlot{Binding: formatv0.AnswerSlotBindingAnywhere,
		Check: formatv0.Check{Kind: formatv0.CheckKindNumber, Key: ptr(key), TrapValues: traps(trapValues...)}}
}

// The answer options are the key, the trap values and, for a number, the whole
// numbers next to the key, each as the module writes it, in any order.
func TestAnswerOptionsOfACheck(t *testing.T) {
	t.Parallel()

	choice := formatv0.Check{Kind: formatv0.CheckKindChoice, Key: ptr("odd"), Options: []formatv0.Text{"even", "odd"}}
	for _, c := range []struct {
		name string
		task formatv0.Task
		want []string
	}{
		{"a number with no trap values", slotTask(number("7")), []string{"7", "8", "6", "9", "5"}},
		{"a number with trap values", slotTask(number("7", "3", "34")), []string{"7", "3", "34", "8", "6"}},
		{"trap values next to the key are not given twice", slotTask(number("7", "8", "6")), []string{"7", "8", "6", "9", "5"}},
		{"the first four trap values", slotTask(number("7", "1", "2", "3", "4", "5")), []string{"7", "1", "2", "3", "4"}},
		{"a trap value equal to the key", slotTask(number("7", "7", "3")), []string{"7", "3", "8", "6", "9"}},
		{"none below 0", slotTask(number("1")), []string{"1", "2", "0", "3", "4"}},
		{"a negative key", slotTask(number("-1")), []string{"-1", "0", "-2", "1", "-3"}},
		{"a number that is no whole number", slotTask(number("2.5", "3.5")), []string{"2.5", "3.5"}},
		{"a fraction", slotTask(formatv0.AnswerSlot{Binding: formatv0.AnswerSlotBindingAnywhere,
			Check: formatv0.Check{Kind: formatv0.CheckKindFraction, Key: ptr("1/2"), TrapValues: traps("1/3", "2/4")}}),
			[]string{"1/2", "1/3"}},
		{"a choice written by hand", slotTask(formatv0.AnswerSlot{Binding: formatv0.AnswerSlotBindingZone, Check: choice}),
			[]string{"even", "odd"}},
		{"a slot bound choice", slotTask(formatv0.AnswerSlot{Binding: formatv0.AnswerSlotBindingChoice,
			Options: []formatv0.Text{"12", "21"}, Check: formatv0.Check{Kind: formatv0.CheckKindChoice, Key: ptr("21")}}),
			[]string{"12", "21"}},
	} {
		got := answerOptions(c.task, 42)
		if !sameSet(got, c.want) {
			t.Errorf("%s: answerOptions = %q, want %q in some order", c.name, got, c.want)
		}
	}
}

// A task of other slots, or a check that gives fewer than two, has none.
func TestNoAnswerOptions(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		task formatv0.Task
	}{
		{"two answer slots", slotTask(number("7"), number("8"))},
		{"two items", formatv0.Task{Items: []formatv0.Item{{AnswerSlots: []formatv0.AnswerSlot{number("7")}},
			{AnswerSlots: []formatv0.AnswerSlot{number("8")}}}}},
		{"a slot bound sheet", slotTask(formatv0.AnswerSlot{Binding: formatv0.AnswerSlotBindingSheet,
			Check: formatv0.Check{Kind: formatv0.CheckKindNumber, Key: ptr("7")}})},
		{"a time with no trap values", slotTask(formatv0.AnswerSlot{Binding: formatv0.AnswerSlotBindingAnywhere,
			Check: formatv0.Check{Kind: formatv0.CheckKindTime, Key: ptr("3:15")}})},
		{"a predicate", slotTask(formatv0.AnswerSlot{Binding: formatv0.AnswerSlotBindingAnywhere,
			Check: formatv0.Check{Kind: formatv0.CheckKindPredicate, Predicate: ptr("value > 2"), TrapValues: traps("1", "2")}})},
		{"a rubric", slotTask(formatv0.AnswerSlot{Binding: formatv0.AnswerSlotBindingAnywhere,
			Check: formatv0.Check{Kind: formatv0.CheckKindRubric}})},
		{"no answer slot", formatv0.Task{}},
	} {
		if got := answerOptions(c.task, 42); got != nil {
			t.Errorf("%s: answerOptions = %q, want none", c.name, got)
		}
	}
}

// The seed shuffles the options, the same at every read.
func TestAnswerOptionsShuffleBySeed(t *testing.T) {
	t.Parallel()

	task := slotTask(number("7", "3", "34"))
	orders := map[string]bool{}
	for seed := range uint64(20) {
		got := answerOptions(task, seed)
		if again := answerOptions(task, seed); !slices.Equal(got, again) {
			t.Fatalf("seed %d: %q, then %q", seed, got, again)
		}
		orders[strings.Join(got, "|")] = true
	}
	if len(orders) < 2 {
		t.Errorf("20 seeds gave %d orders, want several", len(orders))
	}
}

func sameSet(a, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}
