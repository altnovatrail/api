package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// migrations is the schema's migrations, in the binary, in the order of their
// numbers (R29).
//
//go:embed migrations/*.sql
var migrations embed.FS

// Migrate applies to the database at url every migration it has not had, and
// returns how many it applied and the version the schema is at. url is the
// direct connection: goose holds an advisory lock of the session while it
// migrates, so that two runs never migrate at once, and Neon's pooler keeps no
// session (R29). Each migration runs in a transaction of its own.
func Migrate(ctx context.Context, url string) (applied int, version int64, err error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return 0, 0, errConnectionString
	}
	db := stdlib.OpenDB(*cfg)
	defer func() { _ = db.Close() }()

	// Neither fails here: fs.Sub only on a name that is not valid, and the locker
	// only on options it is not given.
	files, _ := fs.Sub(migrations, "migrations")
	locker, _ := lock.NewPostgresSessionLocker()
	// Nothing of goose's global state: the migrations are this provider's alone.
	provider, err := goose.NewProvider(goose.DialectPostgres, db, files,
		goose.WithSessionLocker(locker), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return 0, 0, fmt.Errorf("store: the migrations' provider: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("store: migrate: %w", err)
	}
	if version, err = provider.GetDBVersion(ctx); err != nil {
		return len(results), 0, fmt.Errorf("store: the schema's version: %w", err)
	}
	return len(results), version, nil
}
