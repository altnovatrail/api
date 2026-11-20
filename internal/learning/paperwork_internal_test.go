package learning

import (
	"testing"

	"github.com/altnovatrail/module-kit/paper"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	paperv1 "github.com/altnovatrail/spec/contracts/novatrail/paper/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/store"
)

// A key the attempt keeps is read back; one of no task is a key, CAUSE_NO_KEY;
// and one of a shape this build knows not, a varint where the tasks are, is
// none, so that the next submission derives it again.
func TestTheKeptKeyOfAnotherShapeIsDerivedAgain(t *testing.T) {
	t.Parallel()

	one := &paperv1.Key{Tasks: []*paperv1.TaskKey{{Statement: "37 + 48 ="}}}
	bytes, err := proto.Marshal(one)
	if err != nil {
		t.Fatal(err)
	}
	k, err := keptKey(store.Attempt{PaperKey: bytes})
	if err != nil || !proto.Equal(k, one) {
		t.Fatalf("a kept key reads as %v, %v", k, err)
	}
	if k, err := keptKey(store.Attempt{PaperKey: []byte{}}); err != nil || k == nil || len(k.GetTasks()) != 0 {
		t.Fatalf("a key of no task reads as %v, %v", k, err)
	}
	other := protowire.AppendTag(nil, 1, protowire.VarintType)
	other = protowire.AppendVarint(other, 1)
	other = protowire.AppendTag(other, 2, protowire.BytesType)
	other = protowire.AppendBytes(other, []byte{0x08, 0x01})
	if k, err := keptKey(store.Attempt{PaperKey: other}); err != nil || k != nil {
		t.Fatalf("a key of another shape reads as %v, %v", k, err)
	}
	if k, err := keptKey(store.Attempt{}); err != nil || k != nil {
		t.Fatalf("no key reads as %v, %v", k, err)
	}
}

// A submission that derived a key is judged by it, against whose statements
// its work was read, and keeps it only while the attempt keeps none: a Check
// that went meanwhile and kept its own leaves it the attempt's. One that
// derived none is judged by a key kept meanwhile when its work was read on as
// many tasks, else by none; and one that took the kept key by that.
func TestASubmissionIsJudgedByTheKeyItsWorkWasReadAgainst(t *testing.T) {
	t.Parallel()

	three := &paperv1.Key{Tasks: make([]*paperv1.TaskKey, 3)}
	two := &paperv1.Key{Tasks: make([]*paperv1.TaskKey, 2)}
	for _, c := range []struct {
		name string
		kept *paperv1.Key
		out  paperSending
		want *paperv1.Key
		keep bool
	}{
		{"derived, none kept", nil, paperSending{derive: true, derived: two}, two, true},
		{"derived, another kept meanwhile", three, paperSending{derive: true, derived: two}, two, false},
		{"derived none, none kept", nil, paperSending{derive: true, readTasks: 3}, nil, false},
		{"derived none, another kept meanwhile", three, paperSending{derive: true, readTasks: 3}, three, false},
		{"derived none, read on other tasks", three, paperSending{derive: true, readTasks: 2}, nil, false},
		{"derived none, read on none", three, paperSending{derive: true}, nil, false},
		{"kept", three, paperSending{key: three}, three, false},
	} {
		k, keep := judgedKey(c.kept, c.out)
		if k != c.want || keep != c.keep {
			t.Errorf("%s: judged by %v, kept %v", c.name, k, keep)
		}
	}
}

// Two readers differ by the value of a slot or why it is unreadable, and by a
// slot one of them gave and the other did not; where each saw a slot does not
// count.
func TestTwoReadersDifferByWhatTheyRead(t *testing.T) {
	t.Parallel()

	place := &observationv1.AnswerSlotPlace{Item: 1, Slot: 1}
	unreadable := func(why observationv1.Unreadable, box *observationv1.CropBox) *observationv1.Observation {
		return &observationv1.Observation{AnswerSlots: []*observationv1.AnswerSlotObservation{{AnswerSlot: place,
			Read: &observationv1.AnswerSlotObservation_Unreadable{Unreadable: why}, CropBox: box}}}
	}
	illegible := unreadable(observationv1.Unreadable_UNREADABLE_ILLEGIBLE, nil)
	for name, tc := range map[string]struct {
		b      *observationv1.Observation
		differ bool
	}{
		"the same reason, seen elsewhere": {unreadable(observationv1.Unreadable_UNREADABLE_ILLEGIBLE, &observationv1.CropBox{Ymax: 10, Xmax: 10}), false},
		"another reason":                  {unreadable(observationv1.Unreadable_UNREADABLE_BLANK, nil), true},
		"no slot":                         {&observationv1.Observation{}, true},
	} {
		if got := readDiffer(illegible, tc.b); got != tc.differ {
			t.Errorf("%s: readDiffer = %v, want %v", name, got, tc.differ)
		}
	}
}

// The value the learner reads is the one a reader wrote that is brought to the
// value judged, the first such, past a reader that gave no slot; with none, it
// is the value judged.
func TestTheValueShownIsTheOneWritten(t *testing.T) {
	t.Parallel()

	key := &paperv1.Answer{Kind: &paperv1.Answer_Number{Number: &paperv1.Number{Value: "85", Unknown: "x"}}}
	judged := paper.BroughtValue(key, "x = 85")
	read := func(value string) *observationv1.Observation {
		return &observationv1.Observation{AnswerSlots: []*observationv1.AnswerSlotObservation{{AnswerSlot: &observationv1.AnswerSlotPlace{Item: 1, Slot: 1},
			Read: &observationv1.AnswerSlotObservation_Value{Value: value}}}}
	}
	if got := writtenAs(key, judged, []*observationv1.Observation{{}, read("x = 85")}); got != "x = 85" {
		t.Errorf("the value shown = %q, want the second reader's x = 85", got)
	}
	if got := writtenAs(key, judged, []*observationv1.Observation{read("86")}); got != judged {
		t.Errorf("the value shown with none brought to it = %q, want %q", got, judged)
	}
}
