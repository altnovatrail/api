package catalog_test

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altnovatrail/spec/types/formatv0"

	"github.com/altnovatrail/api/internal/packages"
	"github.com/altnovatrail/api/internal/packages/packagestest"
)

// filePath is the address of the file at path of the device part of digest.
func filePath(digest formatv0.Digest, path string) string {
	return "/packages/" + packages.Name(digest) + "/device/" + path
}

// A file of a device part comes by its package's digest and its path, with its
// media type, kept for a day, and again as 304 by its ETag.
func TestTheFilesOfADevicePartComeByDigestAndPath(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	for path, want := range map[string]string{
		"LICENSE":                "text/plain; charset=utf-8",
		"NOTICE":                 "text/plain; charset=utf-8",
		"media/pictures/dot.png": "image/png",
	} {
		res, body := w.get(t, filePath(w.media.Digest, path))
		if res.StatusCode != http.StatusOK || !bytes.Equal(body, w.media.Files["device/"+path]) {
			t.Errorf("GET %s = %d, %d bytes", path, res.StatusCode, len(body))
		}
		for header, value := range map[string]string{
			"Content-Type":                want,
			"Cache-Control":               "public, max-age=86400",
			"X-Content-Type-Options":      "nosniff",
			"Access-Control-Allow-Origin": appOrigin,
		} {
			if got := res.Header.Get(header); got != value {
				t.Errorf("GET %s: %s = %q, want %q", path, header, got, value)
			}
		}
		etag := res.Header.Get("ETag")
		if etag != `"`+string(packagestest.DigestOf(w.media.Files["device/"+path]))+`"` {
			t.Fatalf("GET %s: ETag %q, want the file's digest", path, etag)
		}
		again, body := w.get(t, filePath(w.media.Digest, path), "If-None-Match", etag)
		if again.StatusCode != http.StatusNotModified || len(body) != 0 {
			t.Errorf("GET %s again by its ETag = %d, %d bytes", path, again.StatusCode, len(body))
		}
	}

	res, body := w.get(t, filePath(w.media.Digest, "LICENSE"), "Range", "bytes=0-3")
	if res.StatusCode != http.StatusPartialContent || !bytes.Equal(body, w.media.Files["device/LICENSE"][:4]) {
		t.Errorf("GET of a range = %d, %q", res.StatusCode, body)
	}
	// A range that misses the file is the error of http.ServeContent, with no
	// body and kept by nobody.
	res, body = w.get(t, filePath(w.media.Digest, "LICENSE"), "Range", "bytes=99999-")
	if res.StatusCode != http.StatusRequestedRangeNotSatisfiable || len(body) != 0 ||
		res.Header.Get("Cache-Control") != "no-store" || res.Header.Get("ETag") != "" {
		t.Errorf("GET of a range that misses = %d, %q, Cache-Control %q, ETag %q", res.StatusCode, body,
			res.Header.Get("Cache-Control"), res.Header.Get("ETag"))
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodHead, w.url+filePath(w.older.Digest, "LICENSE"), nil)
	if err != nil {
		t.Fatal(err)
	}
	head, err := w.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = head.Body.Close()
	if head.StatusCode != http.StatusOK || head.ContentLength != int64(len(w.older.Files["device/LICENSE"])) {
		t.Errorf("HEAD = %d, %d bytes", head.StatusCode, head.ContentLength)
	}
	if !strings.Contains(w.log.String(), `"message":"package_file","status":304`) {
		t.Errorf("no line of the log of a file answered 304: %s", w.log.String())
	}
}

// A path the device part does not have, a digest that is no digest, a version
// the image no longer gives and a digest it does not know are refused, with no
// body, kept by nobody, and with CORS; the log says nothing of the path.
func TestTheFilesAreRefusedForWhatTheImageDoesNotGive(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	for _, tc := range []struct {
		name, path string
		status     int
	}{
		{"no such path", filePath(w.media.Digest, "media/pictures/none.png"), http.StatusNotFound},
		{"a path of no device part", filePath(w.media.Digest, "inventory.json"), http.StatusNotFound},
		{"no NOTICE", filePath(w.older.Digest, "NOTICE"), http.StatusNotFound},
		{"no digest", "/packages/sha256-x/device/LICENSE", http.StatusNotFound},
		{"no prefix", "/packages/" + strings.Repeat("a", 64) + "/device/LICENSE", http.StatusNotFound},
		{"withdrawn", filePath(w.newer.Digest, "LICENSE"), http.StatusGone},
		{"out of the window", filePath(outOfWindow, "LICENSE"), http.StatusGone},
		{"unknown", filePath(unknown, "LICENSE"), http.StatusServiceUnavailable},
		{"not clean", "/packages/" + packages.Name(w.media.Digest) + "/device/media/../LICENSE", http.StatusNotFound},
	} {
		res, body := w.get(t, tc.path)
		if res.StatusCode != tc.status || len(body) != 0 {
			t.Errorf("%s: GET = %d, %q; want %d and no body", tc.name, res.StatusCode, body, tc.status)
		}
		if tc.status != http.StatusNotFound || tc.name == "no such path" {
			if got := res.Header.Get("Cache-Control"); got != "no-store" {
				t.Errorf("%s: Cache-Control = %q, want no-store", tc.name, got)
			}
		}
		if got := res.Header.Get("Access-Control-Allow-Origin"); got != appOrigin {
			t.Errorf("%s: CORS to %q", tc.name, got)
		}
		retry := res.Header.Get("Retry-After")
		if (tc.status == http.StatusServiceUnavailable) != (retry == "30") {
			t.Errorf("%s: Retry-After %q", tc.name, retry)
		}
	}
	log := w.log.String()
	for _, said := range []string{"LICENSE", "sha256", "media/"} {
		if strings.Contains(log, said) {
			t.Errorf("the log says %q: %s", said, log)
		}
	}
	for _, status := range []string{`"status":404`, `"status":410`, `"status":503`} {
		if !strings.Contains(log, `"message":"package_file",`+status) {
			t.Errorf("no line of the log with %s: %s", status, log)
		}
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, w.url+filePath(w.media.Digest, "LICENSE"), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("POST of a file = %d, want 404, the answer of a route that is not there", res.StatusCode)
	}
}

// A file gone from the folder under the running server is a failure, answered
// 500 and kept by nobody, and its line of the log is an error.
func TestAFileGoneFromTheFolderIsAFailure(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	license := filepath.Join(w.dir, "files", packages.Name(w.solver.Listing.License.Digest))
	if err := os.Remove(license); err != nil {
		t.Fatal(err)
	}
	res, body := w.get(t, filePath(w.solver.Digest, "LICENSE"))
	if res.StatusCode != http.StatusInternalServerError || len(body) != 0 || res.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("GET of a file gone = %d, %q, Cache-Control %q", res.StatusCode, body, res.Header.Get("Cache-Control"))
	}
	if !strings.Contains(w.log.String(), `"severity":"ERROR","message":"package_file","status":500`) {
		t.Errorf("no error line of the file gone: %s", w.log.String())
	}
}
