// Package sqlitestore is the only backend internal/store ships in this
// milestone (server-persistence spec, "SQLite Is the Only Implemented
// Backend"). It wires modernc.org/sqlite — a pure-Go driver, no cgo — behind
// the store.Store seam; nothing outside this package ever imports
// modernc.org/sqlite or sees a *sql.DB.
package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/angeltonio/aliasdeck/internal/store"
	_ "modernc.org/sqlite"
)

// SQLiteStore implements store.Store over one *sql.DB, deliberately
// serialized to a single connection (design decision 7): SetMaxOpenConns(1)
// plus busy_timeout removes SQLITE_BUSY as a failure class outright. The
// throughput a personal control plane needs does not notice; an unbounded
// lock wait is a trap this project has shipped three times already.
type SQLiteStore struct {
	db *sql.DB
	q  *Queries
}

// Open opens the SQLite database at path, applies the connection pragmas
// design decision 7 requires (journal_mode=WAL, busy_timeout=5000,
// foreign_keys=on), migrates it to the latest schema, and returns a ready
// Store. Callers never need to know modernc.org/sqlite's pragma
// query-string syntax.
func Open(ctx context.Context, path string) (*SQLiteStore, error) {
	dsn := fmt.Sprintf("%s?_busy_timeout=5000&_foreign_keys=on&_journal_mode=WAL", path)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: opening sqlite database: %w", err)
	}
	db.SetMaxOpenConns(1)

	if err := store.Migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	return &SQLiteStore{db: db, q: New(db)}, nil
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error { return s.db.Close() }

func (s *SQLiteStore) Aliases() store.AliasRepo      { return aliasRepo{db: s.db, q: s.q} }
func (s *SQLiteStore) Devices() store.DeviceRepo     { return deviceRepo{db: s.db, q: s.q} }
func (s *SQLiteStore) Profiles() store.ProfileRepo   { return profileRepo{q: s.q} }
func (s *SQLiteStore) Tokens() store.TokenRepo       { return tokenRepo{db: s.db, q: s.q} }
func (s *SQLiteStore) Operators() store.OperatorRepo { return operatorRepo{q: s.q} }
