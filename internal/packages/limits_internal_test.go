package packages

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A file of the release past its limit is refused, and so is an archive whose
// files say they unpack to more than theirs.
func TestTheLimitsHold(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 11))
	}))
	t.Cleanup(server.Close)
	if _, err := fetch(t.Context(), server.Client(), server.URL, "v0.1.0", "f", 10); !errors.Is(err, ErrFetch) {
		t.Errorf("fetch past its limit = %v, want ErrFetch", err)
	}
	if data, err := fetch(t.Context(), server.Client(), server.URL, "v0.1.0", "f", 11); err != nil || len(data) != 11 {
		t.Errorf("fetch at its limit = %d bytes, %v", len(data), err)
	}
	if _, err := fetch(t.Context(), server.Client(), "http://127.0.0.1:1", "v0.1.0", "f", 10); !errors.Is(err, ErrFetch) {
		t.Errorf("fetch from no server = %v, want ErrFetch", err)
	}

	// The header says more than maxUnpacked; the bytes never come.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "inventory.json", Mode: 0o644, Size: maxUnpacked + 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_ = tw.Flush()
	_ = gz.Close()
	if _, err := unpack(buf.Bytes()); !errors.Is(err, ErrArchive) {
		t.Errorf("unpack past its limit = %v, want ErrArchive", err)
	}
}
