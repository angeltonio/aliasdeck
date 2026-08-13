package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"testing/fstest"

	_ "modernc.org/sqlite"
)

// openTestDB opens a fresh SQLite database backed by a file under
// t.TempDir(), never a shared or on-disk fixture (per the standing
// constraint: t.TempDir() database files, no time.Sleep anywhere).
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := filepath.Join(t.TempDir(), "store_test.db")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("opening sqlite database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestMigrateAppliesEmbeddedSchemaToEmptyDatabase proves Migrate actually
// runs the embedded DDL against an empty database, not merely returns nil:
// every table named in the Storage Schema section of design.md must exist
// afterward.
func TestMigrateAppliesEmbeddedSchemaToEmptyDatabase(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate() on an empty database: %v", err)
	}

	wantTables := []string{
		"operators", "profiles", "aliases", "alias_profiles",
		"alias_devices", "devices", "device_profiles", "tokens",
	}
	for _, table := range wantTables {
		var name string
		err := db.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("table %q missing after Migrate(): %v", table, err)
		}
	}
}

// TestMigrateSecondRunIsANoOp proves a repeated startup against an
// already-migrated database applies nothing: rows written between the two
// runs must survive untouched.
func TestMigrateSecondRunIsANoOp(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("first Migrate(): %v", err)
	}

	const insertOperator = `INSERT INTO operators (id, username, password_hash, created_at, updated_at)
		VALUES ('op-1', 'root', 'hash', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`
	if _, err := db.ExecContext(ctx, insertOperator); err != nil {
		t.Fatalf("seeding an operator row: %v", err)
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second Migrate(): %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM operators").Scan(&count); err != nil {
		t.Fatalf("counting operators: %v", err)
	}
	if count != 1 {
		t.Fatalf("operators count = %d after a re-run that must apply nothing, want 1", count)
	}
}

// TestMigrateRefusesNewerSchema is the threat-matrix (migration execution)
// case: a database already migrated ahead of what this binary knows must
// refuse to start, never migrate down. It uses two in-memory fixtures
// (fstest.MapFS) rather than the real embedded migrations so it can
// simulate "the binary is older than the database" without touching the
// production schema file.
func TestMigrateRefusesNewerSchema(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	ahead := fstest.MapFS{
		"0001_init.sql":  &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE TABLE a (id TEXT);\n-- +goose Down\nDROP TABLE a;\n")},
		"0002_extra.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE TABLE b (id TEXT);\n-- +goose Down\nDROP TABLE b;\n")},
	}
	if err := migrateFS(ctx, db, ahead); err != nil {
		t.Fatalf("migrating the ahead fixture to seed a newer schema: %v", err)
	}

	behind := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE TABLE a (id TEXT);\n-- +goose Down\nDROP TABLE a;\n")},
	}
	err := migrateFS(ctx, db, behind)
	if !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("migrateFS() against a database ahead of the binary = %v, want ErrSchemaNewer", err)
	}
}

// TestMigrateFailureRollsBackTransactionally is the spec's "migration
// failure aborts startup transactionally" scenario: a migration file whose
// second statement fails must leave the first statement's DDL rolled back,
// not partially applied.
func TestMigrateFailureRollsBackTransactionally(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	broken := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{Data: []byte(
			"-- +goose Up\n" +
				"CREATE TABLE ok_table (id TEXT);\n" +
				"SELECT this_identifier_does_not_exist_and_must_fail;\n" +
				"-- +goose Down\n" +
				"DROP TABLE ok_table;\n",
		)},
	}

	if err := migrateFS(ctx, db, broken); err == nil {
		t.Fatal("migrateFS() with a failing statement returned nil error, want an error")
	}

	err := db.QueryRowContext(ctx,
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'ok_table'").Scan(new(string))
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ok_table exists after a failed migration transaction that must have rolled back; lookup err = %v", err)
	}
}
