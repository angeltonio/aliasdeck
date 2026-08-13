package apply

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func hasLeftoverTempFiles(t *testing.T, dir string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			return true
		}
	}
	return false
}

func TestWriteFileAtomicSuccess(t *testing.T) {
	dir := t.TempDir()
	// Nested, non-existent subdirectory: the helper must MkdirAll it first.
	path := filepath.Join(dir, "nested", "aliases.zsh")
	want := []byte("alias ll='ls -la'\n")

	if err := writeFileAtomic(path, want, 0o644); err != nil {
		t.Fatalf("writeFileAtomic() returned an error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("file content = %q, want %q", got, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat result: %v", err)
	}
	// Windows has no Unix permission bits: Go's os package reports 0666 for
	// any writable file (0444 for a read-only one) regardless of the mode
	// passed to Chmod, because there is nothing else to report. The exact
	// requested-mode-is-honored guarantee this assertion protects is a POSIX
	// property; on Windows the file being present and writable is the
	// closest analogue, and that is what is asserted there instead.
	if runtime.GOOS == "windows" {
		if info.Mode().Perm()&0o200 == 0 {
			t.Errorf("file mode = %o, want a writable file", info.Mode().Perm())
		}
	} else if info.Mode().Perm() != 0o644 {
		t.Errorf("file mode = %o, want %o", info.Mode().Perm(), 0o644)
	}

	if hasLeftoverTempFiles(t, filepath.Dir(path)) {
		t.Error("a successful write must not leave any .tmp file behind")
	}
}

func TestWriteFileAtomicRefusesSymlinkDestination(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real-target")
	if err := os.WriteFile(real, []byte("original"), 0o644); err != nil {
		t.Fatalf("seeding real target: %v", err)
	}

	link := filepath.Join(dir, "aliases.zsh")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("creating symlink: %v", err)
	}

	err := writeFileAtomic(link, []byte("attacker-controlled"), 0o644)
	if err == nil {
		t.Fatal("writeFileAtomic() must refuse a symlinked destination")
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink itself must be left untouched on refusal")
	}

	content, err := os.ReadFile(real)
	if err != nil {
		t.Fatalf("reading real target: %v", err)
	}
	if string(content) != "original" {
		t.Errorf("symlink target content = %q, want %q (must not be overwritten)", content, "original")
	}

	if hasLeftoverTempFiles(t, dir) {
		t.Error("refusal must not leave any .tmp file behind")
	}
}

func TestWriteFileAtomicRefusesDirectoryDestination(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "aliases.zsh")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("seeding directory target: %v", err)
	}

	err := writeFileAtomic(target, []byte("data"), 0o644)
	if err == nil {
		t.Fatal("writeFileAtomic() must refuse a directory destination")
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if !info.IsDir() {
		t.Error("the directory must remain a directory on refusal")
	}

	if hasLeftoverTempFiles(t, dir) {
		t.Error("refusal must not leave any .tmp file behind")
	}
}

func TestWriteFileAtomicCleansUpTempFileOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aliases.zsh")

	original := osRename
	osRename = func(oldpath, newpath string) error {
		return errors.New("forced rename failure for test")
	}
	defer func() { osRename = original }()

	err := writeFileAtomic(path, []byte("data"), 0o644)
	if err == nil {
		t.Fatal("writeFileAtomic() must propagate a rename failure")
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the destination must not exist when rename fails")
	}

	if hasLeftoverTempFiles(t, dir) {
		t.Error("a rename failure must not leave the temp file behind (deferred Remove)")
	}
}
