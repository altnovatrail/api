package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altnovatrail/spec"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/forms"
)

// copyOfTexts is a folder of the texts of spec's tag, as a copy's operator lays
// one.
func copyOfTexts(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	err := fs.WalkDir(spec.Legal, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == "." {
			return err
		}
		if path != "VERSION" && path != "legal.yaml" && path != "texts" && !strings.HasPrefix(path, "texts/") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dir, path), 0o755)
		}
		data, err := fs.ReadFile(spec.Legal, path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, path), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// The texts are the tag's, or a copy's folder read by the rules of the part;
// the store of the forms is a folder, a bucket by the service's own account, or
// none.
func TestTheTextsAndTheStoreOfTheForms(t *testing.T) {
	t.Parallel()

	if texts, err := legalTexts(config.Agreements{}); err != nil || texts == nil {
		t.Errorf("the tag's texts = %v, %v", texts, err)
	}
	dir := copyOfTexts(t)
	if _, err := legalTexts(config.Agreements{LegalDir: dir}); err != nil {
		t.Errorf("a copy's folder = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := legalTexts(config.Agreements{LegalDir: dir}); err == nil {
		t.Error("a folder that breaks the rules: nil error")
	}
	if store, err := formsStore(context.Background(), config.Agreements{}); err != nil || store != nil {
		t.Errorf("no store = %v, %v", store, err)
	}
	folder := t.TempDir()
	if store, err := formsStore(context.Background(), config.Agreements{FormsDir: folder}); err != nil || store != forms.Store(forms.Dir(folder)) {
		t.Errorf("a folder = %v, %v", store, err)
	}
}

// A copy's folder that breaks the rules stops api check before the
// migrations, and api serve at its start.
func TestABrokenFolderOfTextsStopsTheCheck(t *testing.T) {
	t.Parallel()

	dir := copyOfTexts(t)
	if code, stdout, stderr := runCommand(t.Context(), []string{"API_LEGAL_DIR=" + dir, "API_CONSENT_FORMS_DIR=" + t.TempDir()}, "check"); code != exitOK ||
		!strings.Contains(stdout, `"legal_dir":true`) || !strings.Contains(stdout, `"consent_forms":"folder"`) {
		t.Errorf("check with a copy's folder = %d, %q, %q", code, stdout, stderr)
	}
	if err := os.Remove(filepath.Join(dir, "legal.yaml")); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runCommand(t.Context(), []string{"API_LEGAL_DIR=" + dir}, "check"); code != exitFailure || !strings.Contains(stderr, "legal.yaml") {
		t.Errorf("check with a broken folder = %d, %q; want the failure", code, stderr)
	}
	if code, _, stderr := runCommand(t.Context(), []string{"API_LEGAL_DIR=texts"}, "check"); code != exitFailure || !strings.Contains(stderr, "API_LEGAL_DIR") {
		t.Errorf("check with a relative folder = %d, %q; want the refusal", code, stderr)
	}
	var stdout, stderr syncBuffer
	code := run(t.Context(), []string{"serve"}, serveEnviron(t, "API_LEGAL_DIR="+dir), &stdout, &stderr)
	if code != exitFailure || !strings.Contains(stdout.String(), `"moment":"startup"`) {
		t.Errorf("serve with a broken folder = %d, %q; want a failed start", code, stdout.String())
	}
}

// The bucket of the forms takes the client of the service's own account, which
// a deployment alone has.
func TestTheBucketOfTheForms(t *testing.T) {
	t.Parallel()

	store, err := bucketOf(context.Background(), config.Agreements{FormsBucket: "b", FormsPrefix: "api/forms/"},
		func(context.Context) (forms.Bucket, error) { return forms.Bucket{}, nil })
	if err != nil || store != forms.Store(forms.Bucket{Name: "b", Prefix: "api/forms/"}) {
		t.Errorf("the bucket = %#v, %v", store, err)
	}
	failed := errors.New("no credentials")
	if _, err := bucketOf(context.Background(), config.Agreements{FormsBucket: "b", FormsPrefix: "p/"},
		func(context.Context) (forms.Bucket, error) { return forms.Bucket{}, failed }); !errors.Is(err, failed) {
		t.Errorf("no client = %v, want its failure", err)
	}
}
