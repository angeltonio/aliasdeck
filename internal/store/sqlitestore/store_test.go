package sqlitestore

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestOpenCreatesDatabaseAtRestrictedMode is the threat-matrix "credential
// file" case extended to the server's database (design.md's "Windows 0600
// Gap" note): the file holds operator password hashes and token secret
// hashes, so it must not be left at whatever the process umask happens to
// allow. Windows has no POSIX mode bits — the assertion below only
// enforces the POSIX guarantee on platforms that can provide it, mirroring
// internal/config's identical carve-out for credentials.json.
func TestOpenCreatesDatabaseAtRestrictedMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mode_test.db")

	st, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() { st.Close() })

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat(%q): %v", path, err)
	}

	if runtime.GOOS == "windows" {
		if perm := info.Mode().Perm(); perm&0o200 == 0 {
			t.Errorf("database file mode = %o, want a writable file", perm)
		}
		return
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("database file mode = %o, want %o", perm, 0o600)
	}
}

// TestOpenTightensPermissionsOnExistingFile proves ensureFileMode does not
// merely rely on O_CREATE's mode argument (which the OS ignores for a file
// that already exists): a pre-existing database file created at a wider
// mode must still end up at 0600 after Open.
func TestOpenTightensPermissionsOnExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits do not apply on windows")
	}

	path := filepath.Join(t.TempDir(), "loose_mode_test.db")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("seeding a pre-existing file: %v", err)
	}

	st, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() { st.Close() })

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat(%q): %v", path, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("database file mode = %o after Open() on a pre-existing 0644 file, want %o", perm, 0o600)
	}
}
