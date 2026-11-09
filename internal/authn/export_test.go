package authn

import (
	"context"
	"net/http"

	"google.golang.org/api/option"
)

// NewWithClient builds the adapter on the tests' client of HTTP, whose calls of
// the Identity Toolkit reach the tests' own Google.
func NewWithClient(ctx context.Context, o Options, client *http.Client) (*Firebase, error) {
	return newFirebase(ctx, o, option.WithHTTPClient(client))
}
