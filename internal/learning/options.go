package learning

import (
	"context"
	"math/rand/v2"
	"slices"
	"strconv"

	"github.com/altnovatrail/module-kit/observation"
	"github.com/altnovatrail/module-kit/verdict"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// fewestOptions and mostOptions bound the answer options: fewer than two are
// no choice, and the key with four wrong values, or a number's five, are all
// that is shown.
const (
	fewestOptions = 2
	mostOptions   = 5
)

// optionsStream keeps the shuffle of the answer options apart from the other
// uses of an attempt's seed, so that the order says nothing of the instance.
const optionsStream = 0x6f7074696f6e73

// answerOptions is the options of the answer of task, an instance, in the
// order the attempt of seed shows them, the same at every read: none unless
// the task has exactly one answer slot and it is not bound sheet. A slot bound
// choice, or a check choice with options, has its options; any other check
// its key and its trap values not equal to it, the first four distinct ones in
// the module's order, and a check number with fewer than five in all also the
// whole numbers next to the key until there are five. Each value is as the
// module writes it; fewer than two are none.
func answerOptions(task formatv0.Task, seed uint64) []string {
	slot, _, ok := oneSlot(task)
	if !ok || slot.Binding == string(formatv0.AnswerSlotBindingSheet) {
		return nil
	}
	var out []string
	if slot.Binding == string(formatv0.AnswerSlotBindingChoice) ||
		(slot.Check.Kind == formatv0.CheckKindChoice && len(slot.Check.Options) > 0) {
		for _, o := range slot.ChoiceOptions() {
			out = append(out, string(o))
		}
	} else {
		out = keyAndWrong(slot)
	}
	if len(out) < fewestOptions {
		return nil
	}
	r := rand.New(rand.NewPCG(seed, optionsStream))
	r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// oneSlot is the one answer slot of task and its place, when it has exactly
// one.
func oneSlot(task formatv0.Task) (verdict.AnswerSlot, *observationv1.AnswerSlotPlace, bool) {
	var slot verdict.AnswerSlot
	var place *observationv1.AnswerSlotPlace
	n := 0
	for i, item := range task.Items {
		for j, s := range verdict.AnswerSlots(item) {
			slot, place = s, placeOf(i, j)
			n++
		}
	}
	return slot, place, n == 1
}

// keyAndWrong is the key of slot's check and its wrong values, distinct as the
// check compares them, the key first: none without a key.
func keyAndWrong(slot verdict.AnswerSlot) []string {
	if slot.Check.Key == nil {
		return nil
	}
	key := *slot.Check.Key
	out := []string{key}
	add := func(v string) {
		if len(out) < mostOptions && !slices.ContainsFunc(out, func(o string) bool { return verdict.Same(slot, o, v) }) {
			out = append(out, v)
		}
	}
	for _, t := range slot.Check.TrapValues {
		add(t.Value)
	}
	if slot.Check.Kind != formatv0.CheckKindNumber {
		return out
	}
	n, err := strconv.ParseInt(key, 10, 64)
	if err != nil {
		// A key that is no whole number has no whole numbers next to it.
		return out
	}
	for d := int64(1); len(out) < mostOptions && d <= mostOptions*2; d++ {
		for _, v := range []int64{n + d, n - d} {
			if v >= 0 || n < 0 {
				add(strconv.FormatInt(v, 10))
			}
		}
	}
	return out
}

// optionTapped is the value req taps of the answer options of the attempt h:
// a tap on the one answer slot of a task, not bound choice, with no work. Not
// such a request is false, for the other checks of a submission; one that taps
// more than once, another slot or a value that is none of the options is
// malformed.
func optionTapped(h held, req *v1.SubmitWorkRequest) (string, bool, error) {
	slot, place, ok := oneSlot(h.task)
	taps := req.GetTappedChoices()
	if !ok || slot.Binding == string(formatv0.AnswerSlotBindingChoice) || req.GetWork() != nil || len(taps) == 0 {
		return "", false, nil
	}
	tap := taps[0]
	if len(taps) != 1 || placeKey(tap.GetAnswerSlot()) != placeKey(place) ||
		!slices.Contains(answerOptions(h.task, h.row.Seed), tap.GetValue()) {
		return "", false, rpc.Malformed("tapped_choices")
	}
	return tap.GetValue(), true, nil
}

// verdictsOf is the verdicts of the submission out on task: of an option
// tapped, by the key as the value of the one answer slot; else of the options
// tapped of the slots bound choice and of what AI read, joined.
func verdictsOf(ctx context.Context, task formatv0.Task, joined *observationv1.Observation, out sending) ([]verdict.ItemVerdict, error) {
	if out.option == nil {
		return observation.Verdicts(ctx, task.Items, joined, out.tapped)
	}
	v, err := verdict.OfItem(ctx, task.Items[0], []*string{out.option})
	return []verdict.ItemVerdict{v}, err
}

// optionAnswered says whether the submission of the attempt row, on task, that
// picks finds in its answers was an option of its answer options tapped: one on
// a task that has them, which kept no observation, as only a tap keeps none of
// a task whose one slot AI reads and gives a verdict.
func optionAnswered(ctx context.Context, tx pgx.Tx, profileID string, task formatv0.Task, row store.Attempt, picks func([]store.Answer) (store.Answer, bool)) (bool, error) {
	if len(answerOptions(task, row.Seed)) == 0 {
		return false, nil
	}
	answers, err := store.Answers(ctx, tx, profileID, row.ID)
	if err != nil {
		return false, err
	}
	a, ok := picks(answers)
	return ok && !a.Observed, nil
}

// wrongOf picks the submission whose wrong answer brought the attempt to wrong
// wrong answers: the first whose answer counts them, as only a wrong answer
// raises the count.
func wrongOf(wrong int) func([]store.Answer) (store.Answer, bool) {
	return func(answers []store.Answer) (store.Answer, bool) {
		for _, a := range answers {
			answer := &v1.SubmitWorkResponse{}
			if proto.Unmarshal(a.Answer, answer) == nil && answer.GetWrongAnswers() == int32(wrong) {
				return a, true
			}
		}
		return store.Answer{}, false
	}
}

// lastOf picks the last submission: of an attempt closed correct, the correct
// one.
func lastOf(answers []store.Answer) (store.Answer, bool) {
	if len(answers) == 0 {
		return store.Answer{}, false
	}
	return answers[len(answers)-1], true
}
