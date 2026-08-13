package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrationsDir mirrors the directory go:embed reads from. Go's embed
// cannot reach outside its own package (design decision 5's correction to
// docs/PROJECT.md §10), so this file and the migrations it applies live in
// the same package on purpose.
const migrationsDir = "migrations"

// migrationTableName names the goose version table. It matches the
// `schema_migrations` name used in design.md's Storage Schema section;
// goose's own default is "goose_db_version".
const migrationTableName = "schema_migrations"

// migrationTimeout bounds the whole migration run (design's Bounded
// Operations table): a stuck migration must fail loudly, not hang startup
// forever.
const migrationTimeout = 30 * time.Second

// ErrSchemaNewer is returned when the database's already-applied schema
// version is ahead of the highest migration this binary embeds. The server
// refuses to start rather than attempt a downgrade migration, which this
// package never implements (design decision 5, threat matrix "migration
// execution").
var ErrSchemaNewer = errors.New("store: database schema is newer than this binary knows how to run")

// Migrate applies every pending embedded migration to db, forward only,
// each file in its own transaction (goose's default transaction mode). It
// is idempotent: a database already at the latest version is left
// untouched. SQLite is the only backend this milestone ships (server
// persistence spec), so the dialect is fixed here.
func Migrate(ctx context.Context, db *sql.DB) error {
	sub, err := fs.Sub(migrationFiles, migrationsDir)
	if err != nil {
		return fmt.Errorf("store: reading embedded migrations: %w", err)
	}
	return migrateFS(ctx, db, sub)
}

// migrateFS is Migrate's implementation, parameterized over the migration
// source so tests can substitute a small in-memory fixture (testing/fstest)
// to exercise the newer-schema refusal without mutating the production
// migration file.
func migrateFS(ctx context.Context, db *sql.DB, fsys fs.FS) error {
	ctx, cancel := context.WithTimeout(ctx, migrationTimeout)
	defer cancel()

	provider, err := goose.NewProvider(goose.DialectSQLite3, db, fsys,
		goose.WithTableName(migrationTableName))
	if err != nil {
		return fmt.Errorf("store: creating migration provider: %w", err)
	}

	current, target, err := provider.GetVersions(ctx)
	if err != nil {
		return fmt.Errorf("store: checking schema version: %w", err)
	}
	if current > target {
		return fmt.Errorf("%w: database is at version %d, this binary knows up to version %d",
			ErrSchemaNewer, current, target)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("store: applying migrations: %w", err)
	}
	return nil
}
