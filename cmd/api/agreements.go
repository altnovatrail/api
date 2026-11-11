package main

import (
	"context"
	"fmt"

	"golang.org/x/oauth2/google"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/forms"
	"github.com/altnovatrail/api/internal/legal"
)

// storageScope is the scope of the token api writes to the bucket of the forms
// with: its role there creates and erases objects and reads none (R116 spec).
const storageScope = "https://www.googleapis.com/auth/devstorage.read_write"

// legalTexts is the legal texts api serves: a self-hosted copy's folder, read
// by the rules of the part, or the texts of its tag of spec (R52).
func legalTexts(a config.Agreements) (*legal.Texts, error) {
	if a.LegalDir == "" {
		return legal.Spec(), nil
	}
	return legal.LoadDir(a.LegalDir)
}

// newBucketClient is the client of the bucket of the forms, with the token of
// the service's own account, which a deployment alone has.
func newBucketClient(ctx context.Context) (forms.Bucket, error) {
	client, err := google.DefaultClient(ctx, storageScope)
	if err != nil {
		return forms.Bucket{}, fmt.Errorf("serve: the client of the bucket of the forms: %w", err)
	}
	return forms.Bucket{Client: client}, nil
}

// formsStore is the store of the scans of the forms the deployment names, or
// none.
func formsStore(ctx context.Context, a config.Agreements) (forms.Store, error) {
	return bucketOf(ctx, a, newBucketClient)
}

// bucketOf is the store a names, with the client of a bucket of newClient.
func bucketOf(ctx context.Context, a config.Agreements, newClient func(context.Context) (forms.Bucket, error)) (forms.Store, error) {
	switch {
	case a.FormsDir != "":
		return forms.Dir(a.FormsDir), nil
	case a.FormsBucket != "":
		bucket, err := newClient(ctx)
		if err != nil {
			return nil, err
		}
		bucket.Name, bucket.Prefix = a.FormsBucket, a.FormsPrefix
		return bucket, nil
	}
	return nil, nil
}
