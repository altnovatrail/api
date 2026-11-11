// Package forms is the port of the store of the scans of the forms of a
// parent's consent (R52; R116 spec): the closed bucket of the operator in Cloud
// Storage, where api writes under a prefix of its own and reads nothing, and a
// folder outside every git working tree for the dev projects and the first app.
// The scan never enters the database api (R114 spec).
package forms

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Store is where the scans lie, by the name of each: api creates a scan, a new
// one in the place of the one before, and erases it, and never reads it back.
type Store interface {
	Put(ctx context.Context, object string, scan []byte, mediaType string) error
	Delete(ctx context.Context, object string) error
}

// ErrFailed is a store that did not do what it was asked.
var ErrFailed = errors.New("forms: the store failed")

// Dir is the store of a folder: a file a scan, under its name.
type Dir string

// Put writes the scan as the file of its name, in the place of one before.
func (d Dir) Put(_ context.Context, object string, scan []byte, _ string) error {
	if err := os.WriteFile(filepath.Join(string(d), filepath.Base(object)), scan, 0o600); err != nil {
		return fmt.Errorf("%w: %w", ErrFailed, err)
	}
	return nil
}

// Delete removes the file of the scan; one not there is erased already.
func (d Dir) Delete(_ context.Context, object string) error {
	err := os.Remove(filepath.Join(string(d), filepath.Base(object)))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %w", ErrFailed, err)
	}
	return nil
}

// Bucket is the store of a bucket of Cloud Storage, through its JSON API, with
// the service's own account: client carries its token — one of
// golang.org/x/oauth2/google's — and base is the API's root, its public one by
// default. Its objects are named by the prefix and the scan's name. The account
// may create and erase objects there and read none (R116 spec).
type Bucket struct {
	Client *http.Client
	Base   string
	Name   string
	Prefix string
}

// storageAPI is the root of the JSON API of Cloud Storage.
const storageAPI = "https://storage.googleapis.com"

func (b Bucket) base() string {
	if b.Base != "" {
		return strings.TrimSuffix(b.Base, "/")
	}
	return storageAPI
}

// Put uploads the scan as an object of the bucket, in one request, in the place
// of one before.
func (b Bucket) Put(ctx context.Context, object string, scan []byte, mediaType string) error {
	target := fmt.Sprintf("%s/upload/storage/v1/b/%s/o?uploadType=media&name=%s", b.base(), url.PathEscape(b.Name),
		url.QueryEscape(b.Prefix+object))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(scan))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFailed, err)
	}
	req.Header.Set("Content-Type", mediaType)
	return b.do(req, http.StatusOK)
}

// Delete erases the object of the scan; one not there is erased already.
func (b Bucket) Delete(ctx context.Context, object string) error {
	target := fmt.Sprintf("%s/storage/v1/b/%s/o/%s", b.base(), url.PathEscape(b.Name), url.PathEscape(b.Prefix+object))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, target, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFailed, err)
	}
	return b.do(req, http.StatusNoContent, http.StatusNotFound)
}

// do sends req and refuses an answer of a status other than ok. Nothing of the
// answer is kept: its body may name the object.
func (b Bucket) do(req *http.Request, ok ...int) error {
	client := b.Client
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFailed, err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	for _, status := range ok {
		if res.StatusCode == status {
			return nil
		}
	}
	return fmt.Errorf("%w: status %d", ErrFailed, res.StatusCode)
}
