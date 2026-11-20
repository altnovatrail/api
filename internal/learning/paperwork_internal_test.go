package learning

import (
	"testing"

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
