package sqlitestore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/angeltonio/aliasdeck/internal/store/storetest"
)

// newTestStore opens a freshly migrated SQLiteStore backed by a file under
// t.TempDir(), per the standing constraint: no shared or on-disk fixtures,
// no time.Sleep anywhere.
func newTestStore(t *testing.T) store.Store {
	t.Helper()

	path := filepath.Join(t.TempDir(), "sqlitestore_test.db")
	st, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// TestSQLiteStoreConformance wires the backend-agnostic conformance suite
// (server-persistence spec, "SQLite backend passes conformance suite")
// against the real SQLite implementation.
func TestSQLiteStoreConformance(t *testing.T) {
	storetest.Run(t, newTestStore)
}
