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
	"os"

	"github.com/angeltonio/aliasdeck/internal/store"
	_ "modernc.org/sqlite"
)

// dbFileMode is the permission the SQLite database file (and its WAL/SHM
// sidecars) is created and kept at: the file holds operator password
// hashes and token secret hashes, the same class of secret
// internal/config/credentials.json is protected for (design decision 14).
// See design.md's "Windows 0600 Gap" — the same documented gap and
// compensating controls apply here verbatim; this package does not invent
// a second Windows convention.
const dbFileMode = 0o600

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
	if err := ensureFileMode(path, dbFileMode); err != nil {
		return nil, err
	}

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

	// WAL mode (decision 7) creates path-wal and path-shm sidecar files
	// lazily, on the first write — which Migrate above just performed on
	// an empty database. Those sidecars can hold the same class of
	// in-flight secret data as the main file, so they get the same mode.
	// Best-effort: a sidecar that does not exist yet (e.g. re-opening an
	// already-migrated, otherwise-idle database) is not an error.
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Chmod(path+suffix, dbFileMode); err != nil && !os.IsNotExist(err) {
			db.Close()
			return nil, fmt.Errorf("store: setting permissions on %s: %w", path+suffix, err)
		}
	}

	return &SQLiteStore{db: db, q: New(db)}, nil
}

// ensureFileMode makes sure the database file at path exists and is at
// mode perm before sqlite ever opens it, mirroring the atomic-write
// discipline internal/config uses for credentials.json (design decision
// 14): a file created at 0600 never has a window where a more permissive
// default (process umask) applies. If path already exists, its mode is
// tightened to perm rather than left as whatever created it — this
// package is the only writer of its own database file. Windows has no
// POSIX mode bits; os.Chmod there only toggles the read-only attribute, so
// this call is a no-op-ish best effort there, exactly as design.md's
// "Windows 0600 Gap" already documents for credentials.json.
func ensureFileMode(path string, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, perm)
	if err != nil {
		return fmt.Errorf("store: opening database file %s: %w", path, err)
	}
	defer f.Close()
	if err := f.Chmod(perm); err != nil {
		return fmt.Errorf("store: setting permissions on %s: %w", path, err)
	}
	return nil
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error { return s.db.Close() }

func (s *SQLiteStore) Aliases() store.AliasRepo      { return aliasRepo{db: s.db, q: s.q} }
func (s *SQLiteStore) Devices() store.DeviceRepo     { return deviceRepo{db: s.db, q: s.q} }
func (s *SQLiteStore) Profiles() store.ProfileRepo   { return profileRepo{q: s.q} }
func (s *SQLiteStore) Tokens() store.TokenRepo       { return tokenRepo{db: s.db, q: s.q} }
func (s *SQLiteStore) Operators() store.OperatorRepo { return operatorRepo{q: s.q} }
func (s *SQLiteStore) Audit() store.AuditRepo        { return auditRepo{q: s.q} }
