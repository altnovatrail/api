package authz_test

import (
	"errors"
	"strings"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/language/pkg/go/transformer"
	"github.com/openfga/openfga/pkg/server"

	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/store/storetest"
)

// Migrate makes the schema, the store and the model on an empty database, and a
// second run changes nothing.
func TestMigrateMakesTheStoreAndTheModelOnce(t *testing.T) {
	t.Parallel()

	url := storetest.OpenFGAURL(t, false)
	first, err := authz.Migrate(t.Context(), url)
	if err != nil {
		t.Fatalf("Migrate error = %v", err)
	}
	if first.Applied == 0 || first.Version == 0 || !first.StoreCreated || !first.ModelWritten {
		t.Errorf("the first Migrate = %+v, want the schema, the store and the model", first)
	}
	second, err := authz.Migrate(t.Context(), url)
	if err != nil {
		t.Fatalf("the second Migrate error = %v", err)
	}
	if second.Applied != 0 || second.Version != first.Version || second.StoreCreated || second.ModelWritten {
		t.Errorf("the second Migrate = %+v, want nothing done at version %d", second, first.Version)
	}
	if _, err := authz.Open(t.Context(), url); err != nil {
		t.Errorf("Open after Migrate error = %v, want nil", err)
	}
}

func TestOpenRefusesADatabaseWithNoStore(t *testing.T) {
	t.Parallel()

	url := storetest.OpenFGAURL(t, false)
	// The schema, with no store: what migrate would make if it stopped after it.
	if _, err := authz.Migrate(t.Context(), url); err != nil {
		t.Fatal(err)
	}
	dropStores(t, url)
	if _, err := authz.Open(t.Context(), url); !errors.Is(err, authz.ErrNoStore) {
		t.Errorf("Open error = %v, want %v", err, authz.ErrNoStore)
	}
}

// A store that holds no model saying what the embedded one says is a migrate
// that did not run, and the server does not start on it; migrate writes the model.
func TestOpenRefusesAStoreWithoutItsModelAndMigrateWritesIt(t *testing.T) {
	t.Parallel()

	url := storetest.OpenFGAURL(t, true)
	dropStores(t, url)
	if _, err := openServer(t, url).CreateStore(t.Context(), &openfgav1.CreateStoreRequest{Name: "api"}); err != nil {
		t.Fatal(err)
	}
	writeOtherModel(t, url)
	if _, err := authz.Open(t.Context(), url); !errors.Is(err, authz.ErrModelDiffers) {
		t.Fatalf("Open error = %v, want %v", err, authz.ErrModelDiffers)
	}
	migrated, err := authz.Migrate(t.Context(), url)
	if err != nil || !migrated.ModelWritten || migrated.StoreCreated {
		t.Fatalf("Migrate = %+v, %v; want the model written to the store", migrated, err)
	}
	if _, err := authz.Open(t.Context(), url); err != nil {
		t.Errorf("Open after Migrate error = %v, want nil", err)
	}
}

// While a new revision rolls out, its migrate writes a newer model, and a
// revision still serving — or one rolled back to — starts on its own, which the
// store still holds; its migrate, run again, writes nothing (R31).
func TestARevisionStartsOnItsOwnModelWhenANewerIsLatest(t *testing.T) {
	t.Parallel()

	url := storetest.OpenFGAURL(t, true)
	writeOtherModel(t, url)
	if _, err := authz.Open(t.Context(), url); err != nil {
		t.Fatalf("Open with a newer model latest: error = %v, want nil", err)
	}
	migrated, err := authz.Migrate(t.Context(), url)
	if err != nil || migrated.ModelWritten || migrated.StoreCreated {
		t.Errorf("Migrate with its model in the store = %+v, %v; want nothing written", migrated, err)
	}
}

func TestMigrateRefusesAStringItCannotRead(t *testing.T) {
	t.Parallel()

	_, err := authz.Migrate(t.Context(), "postgres://u:not-a-password@host:notaport/openfga")
	if err == nil || strings.Contains(err.Error(), "not-a-password") {
		t.Errorf("Migrate error = %v, want a refusal that holds no password", err)
	}
}

func TestOpenFailsOnADatabaseThatIsNotThere(t *testing.T) {
	t.Parallel()

	url := strings.Replace(storetest.OpenFGAURL(t, false), "/test_", "/missing_", 1)
	if _, err := authz.Open(t.Context(), url); err == nil {
		t.Error("Open on a database that is not there: error = nil")
	}
}

// dropStores deletes every store, as OpenFGA does: softly.
func dropStores(t *testing.T, url string) {
	t.Helper()

	srv := openServer(t, url)
	listed, err := srv.ListStores(t.Context(), &openfgav1.ListStoresRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range listed.GetStores() {
		if _, err := srv.DeleteStore(t.Context(), &openfgav1.DeleteStoreRequest{StoreId: s.GetId()}); err != nil {
			t.Fatal(err)
		}
	}
}

// writeOtherModel writes into the store api a model that is not api's.
func writeOtherModel(t *testing.T, url string) {
	t.Helper()

	srv := openServer(t, url)
	listed, err := srv.ListStores(t.Context(), &openfgav1.ListStoresRequest{Name: "api"})
	if err != nil || len(listed.GetStores()) != 1 {
		t.Fatalf("the store api: %v, %v", listed, err)
	}
	other := transformer.MustTransformDSLToProto("model\n  schema 1.1\n\ntype user\n")
	if _, err := srv.WriteAuthorizationModel(t.Context(), &openfgav1.WriteAuthorizationModelRequest{
		StoreId: listed.GetStores()[0].GetId(), SchemaVersion: other.GetSchemaVersion(),
		TypeDefinitions: other.GetTypeDefinitions(),
	}); err != nil {
		t.Fatal(err)
	}
}

// openServer is OpenFGA on the database at url, closed when the test ends.
func openServer(t *testing.T, url string) *server.Server {
	t.Helper()

	srv, err := authz.NewServer(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv
}

// Two stores api are none that migrate makes: Open refuses them, and so does
// Migrate, rather than pick one.
func TestTwoStoresAreRefused(t *testing.T) {
	t.Parallel()

	url := storetest.OpenFGAURL(t, true)
	if _, err := openServer(t, url).CreateStore(t.Context(), &openfgav1.CreateStoreRequest{Name: "api"}); err != nil {
		t.Fatal(err)
	}
	if _, err := authz.Open(t.Context(), url); err == nil {
		t.Error("Open on two stores api: error = nil")
	}
	if _, err := authz.Migrate(t.Context(), url); err == nil {
		t.Error("Migrate on two stores api: error = nil")
	}
}

func TestMigrateFailsOnADatabaseThatIsNotThere(t *testing.T) {
	t.Parallel()

	url := strings.Replace(storetest.OpenFGAURL(t, false), "/test_", "/missing_", 1)
	if _, err := authz.Migrate(t.Context(), url); err == nil {
		t.Error("Migrate on a database that is not there: error = nil")
	}
}
