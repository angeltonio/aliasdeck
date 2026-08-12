// Package apply writes rendered output to disk and manages the shell rc
// bootstrap line that sources it.
package apply

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// osRename is a package-level indirection over os.Rename so a test can force
// the final commit step to fail and prove the temp file is still cleaned up.
// It is never reassigned outside tests.
var osRename = os.Rename

// writeFileAtomic writes data to path without ever exposing a partial file to
// a reader.
//
// It creates a temp file in the same directory as path (guaranteeing the
// final rename stays on one filesystem, so it is atomic), writes and syncs
// the data, then renames it into place. A deferred Remove of the temp file
// covers every failure before the rename, so a partial write is never
// visible and never sourceable (design, "Atomic write").
//
// It refuses to write when path already exists as a symlink or a directory
// (threat matrix "Output path"): a symlinked destination could point
// anywhere, and renaming over a directory would destroy its contents rather
// than replace a single file.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	if err := refuseUnsafeDestination(path); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below has succeeded

	if err := writeSyncClose(tmp, data, mode); err != nil {
		return err
	}

	if err := osRename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", tmpPath, path, err)
	}

	return nil
}

// refuseUnsafeDestination reports an error if path already exists as a
// symlink or a directory. A missing path is not an error: that is the normal
// case for a first write.
func refuseUnsafeDestination(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspecting %s: %w", path, err)
	}

	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("refusing to write %s: destination is a symlink", path)
	case info.IsDir():
		return fmt.Errorf("refusing to write %s: destination is a directory", path)
	}
	return nil
}

// writeSyncClose sets mode, writes data, flushes it to stable storage, and
// closes f, returning the first error encountered. f is always closed before
// returning, even on error.
func writeSyncClose(f *os.File, data []byte, mode os.FileMode) error {
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return fmt.Errorf("setting mode on %s: %w", f.Name(), err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("writing %s: %w", f.Name(), err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("syncing %s: %w", f.Name(), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", f.Name(), err)
	}
	return nil
}
