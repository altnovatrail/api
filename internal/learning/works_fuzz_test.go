package learning_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"reflect"
	"testing"

	"github.com/altnovatrail/spec"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/exportv4"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/store"
)

// workSchema is work.schema.json of the part export/ of spec's tag, with the
// schemas it refers to.
func workSchema(tb testing.TB) *jsonschema.Schema {
	tb.Helper()
	compiler := jsonschema.NewCompiler()
	entries, err := fs.ReadDir(spec.Export, "schemas")
	if err != nil {
		tb.Fatal(err)
	}
	var id string
	for _, e := range entries {
		data, err := fs.ReadFile(spec.Export, "schemas/"+e.Name())
		if err != nil {
			tb.Fatal(err)
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			tb.Fatal(err)
		}
		docID := doc.(map[string]any)["$id"].(string)
		if err := compiler.AddResource(docID, doc); err != nil {
			tb.Fatal(err)
		}
		if e.Name() == "work.schema.json" {
			id = docID
		}
	}
	s, err := compiler.Compile(id)
	if err != nil {
		tb.Fatal(err)
	}
	return s
}

// Whatever work the check of PutWork lets through, the file of the export that
// app writes of it, as GetWork gives it, passes its schema and reads back as it
// was put: its history and the context of its submission the same JSON, its
// count of events, and its picture in base64. The check is what lets the export
// hold a work that a device sent.
func FuzzWorkExported(f *testing.F) {
	schema := workSchema(f)
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
		work := exportv4.Work{Format: store.ExportFormat, ProfileID: "5d0e9a43-7c1b-4f2e-8a6d-3b9f1c2e4a70",
			AttemptID: exportv4.FileID(sent.GetAttemptId()), Kind: exportv4.WorkKindHandwriting, StoredAt: "2026-11-16T00:00:00Z",
			History:     json.RawMessage(history),
			Submissions: []exportv4.WorkSubmission{{Context: json.RawMessage(context), HistoryLength: int64(length)}},
			Picture:     base64.StdEncoding.EncodeToString(picture)}
		written, err := json.Marshal(work)
		if err != nil {
			t.Fatalf("the export cannot write the work %q, %q: %v", history, context, err)
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(written))
		if err != nil {
			t.Fatalf("the file of the work %s is no JSON: %v", written, err)
		}
		if err := schema.Validate(doc); err != nil {
			t.Fatalf("the file of the work breaks its schema: %v\n%s", err, written)
		}
		var back exportv4.Work
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
