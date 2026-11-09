package authz

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/assets"
	"github.com/openfga/openfga/pkg/server"

	"github.com/altnovatrail/api/internal/store"
)

// storeLock is the advisory lock that migrate holds while it finds or makes the
// store and writes the model, so that two runs never make two stores (R31).
const storeLock = 0x617069 // "api"

// Migrated is what a run of Migrate did.
type Migrated struct {
	// Applied is how many of OpenFGA's migrations it applied, and Version the
	// version the schema is at.
	Applied int
	Version int64
	// StoreCreated and ModelWritten say whether it made the store api and wrote
	// the model, which it does only when the store has none, or no model that
	// says what the embedded one says.
	StoreCreated bool
	ModelWritten bool
}

// Migrate brings the database openfga at url — the direct connection — to what
// api needs: OpenFGA's schema, the store api, and in it the model api embeds
// (R31). The schema's migrations are OpenFGA's own files, which goose runs under
// its advisory lock of the session as it runs api's (R29), rather than by
// OpenFGA's RunMigrations: that one takes no lock, works on goose's global state,
// and its package brings a MySQL driver under MPL-2.0 into the binary.
func Migrate(ctx context.Context, url string) (Migrated, error) {
	// Never fails here: fs.Sub only on a name that is not valid.
	files, _ := fs.Sub(assets.EmbedMigrations, assets.PostgresMigrationDir)
	applied, version, err := store.MigrateFiles(ctx, url, files)
	migrated := Migrated{Applied: applied, Version: version}
	if err != nil {
		return migrated, fmt.Errorf("authz: %w", err)
	}

	// The lock is the session's, on a connection of the direct string, held until
	// the store and its model are as they must be.
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return migrated, fmt.Errorf("authz: a connection for the lock: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", storeLock); err != nil {
		return migrated, fmt.Errorf("authz: take the lock: %w", err)
	}
	defer func() { _, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", storeLock) }()

	srv, _, err := newServer(ctx, url)
	if err != nil {
		return migrated, err
	}
	defer srv.Close()
	err = ensure(ctx, srv, &migrated)
	return migrated, err
}

// ensure finds or makes the store api, and writes the embedded model when no
// model of the store says what it says. A model the store already holds is not
// written again, so that a revision rolled back to finds its own model and none
// becomes the latest by a second write.
func ensure(ctx context.Context, srv *server.Server, migrated *Migrated) error {
	storeID, err := findStore(ctx, srv)
	if errors.Is(err, ErrNoStore) {
		created, cerr := srv.CreateStore(ctx, &openfgav1.CreateStoreRequest{Name: storeName})
		if cerr != nil {
			return fmt.Errorf("authz: create the store: %w", cerr)
		}
		storeID, err = created.GetId(), nil
		migrated.StoreCreated = true
	}
	if err != nil {
		return err
	}

	found, err := findModel(ctx, srv, storeID)
	if err != nil || found != "" {
		return err
	}
	want := embedded()
	_, err = srv.WriteAuthorizationModel(ctx, &openfgav1.WriteAuthorizationModelRequest{
		StoreId:         storeID,
		SchemaVersion:   want.GetSchemaVersion(),
		TypeDefinitions: want.GetTypeDefinitions(),
		Conditions:      want.GetConditions(),
	})
	if err != nil {
		return fmt.Errorf("authz: write the model: %w", err)
	}
	migrated.ModelWritten = true
	return nil
}
