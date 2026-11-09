package catalog

import (
	"log/slog"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/packages"
)

// FilesPattern is the route of the files of the device parts (contract,
// section 15): sha256-<hex> of the package, then the path of the file in its
// device part — LICENSE, NOTICE or media/<path>.
const FilesPattern = "GET /packages/{package}/device/{path...}"

// retryAfter is how long, in seconds, app waits before it asks again for a file
// of a digest the index of the image does not know: a deployment is going on,
// and a repeat reaches the new image (R07).
const retryAfter = 30

// mediaTypes are the media types of the four formats of media/, by their one
// extension each (format/README.md, section 10).
var mediaTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".webp": "image/webp",
	".mp3":  "audio/mpeg",
}

// Files is the handler of FilesPattern (R18, R42). It goes with no token, so
// that fetch sends it with no preflight and through the HTTP cache:
//   - 200, the file, by http.ServeContent — HEAD, ranges, and 304 by its ETag,
//     the file's own digest — kept for a day;
//   - 404, a digest that is not one, or a path the device part does not have;
//   - 410, a package the image no longer gives: withdrawn, or out of the window;
//   - 503 with Retry-After, a digest the index of the image does not know.
//
// No refusal has a body or is kept. Each answer writes its line of the log,
// with its status and time and nothing of its path.
func (c *Catalog) Files(log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		answer := &answered{ResponseWriter: w, status: http.StatusOK}
		err := c.serveFile(answer, r)
		attrs := []any{logging.FieldStatus, answer.status, logging.FieldDurationMS, time.Since(start).Milliseconds()}
		level := slog.LevelInfo
		if err != nil {
			attrs = append(attrs, logging.FieldErrorKind, logging.ErrorKind(err))
		}
		// A 500 of http.ServeContent — a file of the folder that cannot be read —
		// is a failure too, though it returns no error.
		if answer.status >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		log.Log(r.Context(), level, logging.EventPackageFile, attrs...)
	})
}

// serveFile answers the request; its error is a file of the folder that could
// not be opened, answered 500.
func (c *Catalog) serveFile(w http.ResponseWriter, r *http.Request) error {
	digest, ok := packages.FromName(r.PathValue("package"))
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}
	switch c.set.Status(digest) {
	case packages.Unknown:
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		w.WriteHeader(http.StatusServiceUnavailable)
		return nil
	case packages.NotGiven:
		w.WriteHeader(http.StatusGone)
		return nil
	}
	name, fileDigest, ok := c.set.DeviceFile(digest, r.PathValue("path"))
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}
	file, err := os.Open(name)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return err
	}
	defer func() { _ = file.Close() }()
	header := w.Header()
	header.Set("Content-Type", mediaType(r.PathValue("path")))
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Cache-Control", cacheFor)
	header.Set("ETag", `"`+string(fileDigest)+`"`)
	http.ServeContent(w, r, "", time.Time{}, file)
	return nil
}

// mediaType is the media type of the file at p of a device part: the text of a
// license or a notice, or a medium by its extension, which the format's check
// held to its signature.
func mediaType(p string) string {
	if t, ok := mediaTypes[path.Ext(p)]; ok && strings.HasPrefix(p, "media/") {
		return t
	}
	if p == "LICENSE" || p == "NOTICE" {
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

// answered is a ResponseWriter that knows its status, and keeps no answer but
// a file, or the 206 and 304 of one, in a cache. Any other answer — a refusal,
// and whatever error http.ServeContent writes, a range that misses the file or
// a file that cannot be read — is no-store and has no body: the text of such an
// error is the server's, not the contract's.
type answered struct {
	http.ResponseWriter
	status  int
	refused bool
}

func (a *answered) WriteHeader(status int) {
	a.status = status
	switch status {
	case http.StatusOK, http.StatusPartialContent, http.StatusNotModified:
	default:
		a.refused = true
		header := a.Header()
		header.Set("Cache-Control", noStore)
		for _, name := range []string{"ETag", "Content-Type", "Content-Length", "X-Content-Type-Options"} {
			header.Del(name)
		}
	}
	a.ResponseWriter.WriteHeader(status)
}

// Write writes the body of a file, and drops that of any other answer.
func (a *answered) Write(b []byte) (int, error) {
	if a.refused {
		return len(b), nil
	}
	return a.ResponseWriter.Write(b)
}
