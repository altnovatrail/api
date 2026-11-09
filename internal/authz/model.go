package authz

import (
	_ "embed"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/language/pkg/go/transformer"
	"google.golang.org/protobuf/proto"
)

// modelDSL is the model of section 3.1 of docs/architecture.md, word for word: a
// test holds the two to each other.
//
//go:embed model.fga
var modelDSL string

// model is the embedded model, parsed once: a model that does not parse is a
// binary that cannot start, which the tests find before any release.
var model = transformer.MustTransformDSLToProto(modelDSL)

// embedded is the model api embeds.
func embedded() *openfgav1.AuthorizationModel { return model }

// sameModel says whether two models say the same: their schema, types and
// conditions, not the ID a store gave each.
func sameModel(a, b *openfgav1.AuthorizationModel) bool {
	strip := func(m *openfgav1.AuthorizationModel) *openfgav1.AuthorizationModel {
		return &openfgav1.AuthorizationModel{
			SchemaVersion: m.GetSchemaVersion(), TypeDefinitions: m.GetTypeDefinitions(), Conditions: m.GetConditions(),
		}
	}
	return proto.Equal(strip(a), strip(b))
}
