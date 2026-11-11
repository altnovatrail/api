package forms_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/altnovatrail/api/internal/forms"
)

// A folder keeps a scan by its name, one in the place of the one before, and
// erases it; one not there is erased already.
func TestAFolderKeepsAndErasesAScan(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := forms.Dir(dir)
	for _, scan := range []string{"first", "second"} {
		if err := store.Put(context.Background(), "consent-1", []byte(scan), "image/png"); err != nil {
			t.Fatal(err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(dir, "consent-1")); err != nil || string(data) != "second" {
		t.Errorf("the scan = %q, %v; want the second", data, err)
	}
	for range 2 {
		if err := store.Delete(context.Background(), "consent-1"); err != nil {
			t.Errorf("Delete = %v, want nil", err)
		}
	}
	if err := forms.Dir(filepath.Join(dir, "none")).Put(context.Background(), "x", nil, "image/png"); !errors.Is(err, forms.ErrFailed) {
		t.Errorf("a folder not there: %v, want ErrFailed", err)
	}
	if err := forms.Dir(filepath.Join(dir, "consent-1x")).Delete(context.Background(), "x/y"); err != nil {
		t.Errorf("Delete of nothing = %v, want nil", err)
	}
}

// bucket is a fake of the JSON API of Cloud Storage: what it was sent, and the
// status it answers.
type bucket struct {
	mu      sync.Mutex
	objects map[string][]byte
	types   map[string]string
	status  int
}

func (b *bucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status != 0 {
		w.WriteHeader(b.status)
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/upload/storage/v1/b/forms-bucket/o" && r.URL.Query().Get("uploadType") == "media":
		data, _ := io.ReadAll(r.Body)
		name := r.URL.Query().Get("name")
		b.objects[name], b.types[name] = data, r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodDelete && len(r.URL.Path) > len("/storage/v1/b/forms-bucket/o/"):
		name := r.URL.Path[len("/storage/v1/b/forms-bucket/o/"):]
		if _, ok := b.objects[name]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(b.objects, name)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

// A bucket gets the scan under the prefix, with its media type, and erases it;
// an answer of another status is a failure.
func TestABucketKeepsAndErasesAScan(t *testing.T) {
	t.Parallel()

	fake := &bucket{objects: map[string][]byte{}, types: map[string]string{}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	store := forms.Bucket{Client: server.Client(), Base: server.URL, Name: "forms-bucket", Prefix: "api/consent-forms/"}
	if err := store.Put(context.Background(), "consent-1", []byte("scan"), "application/pdf"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fake.objects["api/consent-forms/consent-1"], []byte("scan")) || fake.types["api/consent-forms/consent-1"] != "application/pdf" {
		t.Errorf("the bucket holds %v %v, want the scan under the prefix", fake.objects, fake.types)
	}
	for range 2 {
		if err := store.Delete(context.Background(), "consent-1"); err != nil {
			t.Errorf("Delete = %v, want nil", err)
		}
	}
	fake.status = http.StatusForbidden
	if err := store.Put(context.Background(), "consent-2", []byte("scan"), "image/jpeg"); !errors.Is(err, forms.ErrFailed) {
		t.Errorf("a refusal: %v, want ErrFailed", err)
	}
	if err := store.Delete(context.Background(), "consent-2"); !errors.Is(err, forms.ErrFailed) {
		t.Errorf("a refusal of the erasure: %v, want ErrFailed", err)
	}
	server.Close()
	if err := store.Put(context.Background(), "consent-3", nil, "image/png"); !errors.Is(err, forms.ErrFailed) {
		t.Errorf("no server: %v, want ErrFailed", err)
	}
	if err := (forms.Bucket{Base: "http://%zz", Name: "b"}).Put(context.Background(), "x", nil, "image/png"); err == nil {
		t.Error("a request that cannot be made: nil error")
	}
}
