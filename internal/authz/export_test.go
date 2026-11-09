package authz

import (
	"context"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/server"
)

// NewServer opens OpenFGA on the database at url for the tests, which reach into
// the store as no code of api does: they drop it, or write it another model.
func NewServer(ctx context.Context, url string) (*server.Server, error) {
	srv, _, err := newServer(ctx, url)
	return srv, err
}

// WriteTuple writes a tuple of any kind the model has, as no code of api does
// yet: a teacher of a classroom, a learner in it.
func (r *Rights) WriteTuple(ctx context.Context, user, relation, object string) error {
	_, err := r.srv.Write(ctx, &openfgav1.WriteRequest{
		StoreId:              r.store,
		AuthorizationModelId: r.model,
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys: []*openfgav1.TupleKey{{User: user, Relation: relation, Object: object}},
		},
	})
	return err
}
