package catalog

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/spec/types/formatv0"

	"github.com/altnovatrail/api/internal/packages"
	"github.com/altnovatrail/api/internal/store"
)

// A file is of its media type by its extension under media/, a license or a
// notice is text, and anything else is bytes, which nosniff keeps so.
func TestTheMediaTypeIsByTheExtension(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]string{
		"media/a/b.jpg": "image/jpeg",
		"media/b.webp":  "image/webp",
		"media/c.mp3":   "audio/mpeg",
		"media/d.png":   "image/png",
		"media/e.gif":   "application/octet-stream",
		"f.png":         "application/octet-stream",
		"LICENSE":       "text/plain; charset=utf-8",
		"NOTICE":        "text/plain; charset=utf-8",
		"media/LICENSE": "application/octet-stream",
	} {
		if got := mediaType(path); got != want {
			t.Errorf("mediaType(%s) = %s, want %s", path, got, want)
		}
	}
}

// A profile or an account gone after the checks is refused as gone, and
// anything else is a failure.
func TestWhatARequestMeetsAfterTheChecks(t *testing.T) {
	t.Parallel()

	for err, want := range map[error]connect.Code{
		store.ErrNoProfile:       connect.CodeNotFound,
		store.ErrAccountDeleting: connect.CodeUnauthenticated,
		store.ErrNoAccount:       connect.CodeUnauthenticated,
		errors.New("down"):       connect.CodeUnavailable,
	} {
		if got := connect.CodeOf(refused(context.Background(), err)); got != want {
			t.Errorf("refused(%v) = %v, want %v", err, got, want)
		}
	}
}

// A package whose open views do not build is a failed start, not a refusal of
// a request.
func TestAPackageWhoseViewsDoNotBuildFailsTheCatalog(t *testing.T) {
	t.Parallel()

	valid := formatv0.Manifest{
		ModuleFormatVersion: "0.4", ID: "example/x", Version: "0.1.0", Title: "X", Subject: "mathematics",
		Audience:  formatv0.Audience{AgeCategories: []formatv0.AgeCategory{formatv0.AgeCategoryAdult}},
		Languages: formatv0.Languages{Explanation: "en"}, Authors: []formatv0.Author{{Name: "A"}}, License: "Apache-2.0",
	}
	if _, err := viewsOf(formatv0.CanonicalView{Manifest: valid}); err != nil {
		t.Fatalf("the views of a valid manifest: %v", err)
	}
	for name, view := range map[string]formatv0.CanonicalView{
		"manifest":      {},
		"skill graph":   {Manifest: valid, SkillGraph: &formatv0.SkillGraph{}},
		"techniques":    {Manifest: valid, Techniques: &formatv0.Techniques{}},
		"solving steps": {Manifest: valid, SolvingSteps: &formatv0.SolvingSteps{}},
	} {
		set := &packages.Set{Packages: map[formatv0.Digest]*modulepackage.Package{"sha256:x": {View: view}}}
		if _, err := New(set); err == nil {
			t.Errorf("New of a package with a broken %s: no error", name)
		}
	}
}
