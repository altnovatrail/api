package export

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"

	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/exportv2"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/store"
)

// Whatever work the check of PutWork lets through, the export writes as the
// JSON it is and reads back as it was put: its history and the context of its
// submission the same JSON, its count of events, and its picture in base64
// (R70). The check is what lets the export write a work that a device sent.
func FuzzStoredWork(f *testing.F) {
	const png = "\x89PNG\r\n\x1a\n"
	f.Add([]byte(`[]`), []byte(`{}`), int32(0), []byte(png))
	f.Add([]byte(` [{"kind":"write","t":12,"points":[{"x":1.5,"y":2,"t":3,"p":0.5}]}] `), []byte(`{"answer_slot":1}`),
		int32(3), []byte(png+"IHDR"))
	f.Add([]byte(`["<&> ", 1e400, -0]`), []byte(`{"a":1,"a":2}`), int32(1), []byte(png))
	f.Add([]byte(`["\ud800"]`), []byte(`{"a":{"b":[null,true]}}`), int32(-1), []byte("GIF89a"))
	f.Add([]byte("[\"\xff\"]"), []byte(`[]`), int32(0), []byte(png))
	f.Fuzz(func(t *testing.T, history, context []byte, length int32, picture []byte) {
		sent := &v1.StoredWork{AttemptId: "0b8f6c1e-2a4d-4d6b-9c55-6f2a8e7d1c3b", History: history,
			Submissions: []*v1.WorkSubmission{{Context: context, HistoryLength: length}}, Picture: picture}
		if learning.WorkMalformed(sent) != "" {
			return
		}
		data, err := proto.Marshal(sent)
		if err != nil {
			t.Fatal(err)
		}
		work, err := storedWork(store.ExportedWork{Bytes: data})
		if err != nil {
			t.Fatalf("storedWork = %v", err)
		}
		written, err := json.Marshal(work)
		if err != nil {
			t.Fatalf("the export cannot write the work %q, %q: %v", history, context, err)
		}
		var back exportv2.Work
		if err := json.Unmarshal(written, &back); err != nil {
			t.Fatalf("the export cannot read back %s: %v", written, err)
		}
		if !sameJSON(back.History, history) || len(back.Submissions) != 1 || !sameJSON(back.Submissions[0].Context, context) ||
			back.Submissions[0].HistoryLength != int64(length) {
			t.Fatalf("the export reads back %s, want the history %q and the context %q", written, history, context)
		}
		if got, err := base64.StdEncoding.DecodeString(back.Picture); err != nil || !bytes.Equal(got, picture) {
			t.Fatalf("the picture reads back %q, %v; want %q", got, err, picture)
		}
	})
}

// sameJSON says whether a and b are the same JSON value, numbers by how they
// are written.
func sameJSON(a, b []byte) bool {
	value := func(data []byte) (any, error) {
		d := json.NewDecoder(bytes.NewReader(data))
		d.UseNumber()
		var v any
		return v, d.Decode(&v)
	}
	x, errA := value(a)
	y, errB := value(b)
	return errA == nil && errB == nil && reflect.DeepEqual(x, y)
}
