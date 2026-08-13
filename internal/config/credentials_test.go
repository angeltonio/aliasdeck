package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func sampleCredentials() Credentials {
	return Credentials{
		Version:     1,
		ServerURL:   "https://aliases.example.com",
		DeviceID:    "device-abc123",
		DeviceToken: "add_lookup123.secret456",
		ObtainedAt:  time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
	}
}

func TestCredentialsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	want := sampleCredentials()

	if err := SaveCredentials(path, want); err != nil {
		t.Fatalf("SaveCredentials() returned an error: %v", err)
	}

	got, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials() returned an error: %v", err)
	}

	if got.Version != want.Version ||
		got.ServerURL != want.ServerURL ||
		got.DeviceID != want.DeviceID ||
		got.DeviceToken != want.DeviceToken ||
		!got.ObtainedAt.Equal(want.ObtainedAt) {
		t.Errorf("round trip mismatch:\ngot:  %+v\nwant: %+v", got, want)
	}
}

// TestCredentialsFileMode0600 pins the threat-matrix "credential file" case:
// a device token must never be left at whatever mode the process umask
// happens to allow.
func TestCredentialsFileMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")

	if err := SaveCredentials(path, sampleCredentials()); err != nil {
		t.Fatalf("SaveCredentials() returned an error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	// Windows has no POSIX mode bits — Go's Chmod there toggles only the
	// read-only bit, so a file this test just wrote reports 0666. This
	// mirrors sqlitestore.TestOpenCreatesDatabaseAtRestrictedMode and
	// auth.TestBootstrapWritesGeneratedPasswordToFileWhenPathGiven exactly,
	// rather than inventing a fourth way to say the same thing (design.md's
	// "Windows 0600 Gap").
	if runtime.GOOS == "windows" {
		if perm := info.Mode().Perm(); perm&0o200 == 0 {
			t.Errorf("credentials.json mode = %o, want a writable file", perm)
		}
		return
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("credentials.json mode = %o, want %o", perm, 0o600)
	}
}

func TestCredentialsLoadMissingFileIsToleratedAsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")

	got, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials() must not error on a missing file, got: %v", err)
	}
	if got != (Credentials{}) {
		t.Errorf("LoadCredentials() of a missing file = %+v, want zero-value Credentials", got)
	}
}

// TestCredentialsLoadCorruptJSONIsAnError is the deliberate divergence from
// state.Load's tolerance: a credentials file holds a live, server-issued
// token this process cannot safely regenerate, so corruption must surface
// rather than silently degrade to an empty credential.
func TestCredentialsLoadCorruptJSONIsAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("seeding corrupt credentials.json: %v", err)
	}

	if _, err := LoadCredentials(path); err == nil {
		t.Fatal("LoadCredentials() must return an error for corrupt JSON, not silently tolerate it")
	}
}

// TestCredentialsSaveOverwritesWithoutLeftoverTempFiles proves the atomic
// write cleans up its temp file on the success path.
func TestCredentialsSaveOverwritesWithoutLeftoverTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")

	first := sampleCredentials()
	if err := SaveCredentials(path, first); err != nil {
		t.Fatalf("first SaveCredentials() returned an error: %v", err)
	}

	second := sampleCredentials()
	second.DeviceToken = "add_newlookup.newsecret"
	if err := SaveCredentials(path, second); err != nil {
		t.Fatalf("second SaveCredentials() returned an error: %v", err)
	}

	got, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials() returned an error: %v", err)
	}
	if got.DeviceToken != "add_newlookup.newsecret" {
		t.Errorf("DeviceToken after second save = %q, want %q", got.DeviceToken, "add_newlookup.newsecret")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("dir entries = %d, want exactly 1 (credentials.json, no leftover temp files): %v", len(entries), entries)
	}
}

// TestCredentialsSaveFailsWhenTempFileCannotBeCreated checks that a failure
// to create the temp file is reported rather than swallowed.
//
// The failure is induced by giving the credentials path a parent that is a
// regular file rather than a directory — the same technique
// state.TestStateSaveFailsWhenTempFileCannotBeCreated uses, chosen because a
// read-only directory is not reliably enforced on Windows.
func TestCredentialsSaveFailsWhenTempFileCannotBeCreated(t *testing.T) {
	dir := t.TempDir()

	notADir := filepath.Join(dir, "blocking-file")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatalf("seeding the blocking file: %v", err)
	}

	path := filepath.Join(notADir, "credentials.json")
	if err := SaveCredentials(path, sampleCredentials()); err == nil {
		t.Fatal("SaveCredentials() must return an error when its parent directory cannot be created or written")
	}
}

// TestCredentialsSaveCleansUpTempFileOnRenameFailure is the atomic-write
// cleanup-on-failure case task 7.3 names explicitly: the temp file is
// created successfully, but the final rename fails (here, because the
// destination is already a directory, which os.Rename refuses to replace
// with a file on every supported OS) — SaveCredentials must still remove
// its own temp file rather than leaving it behind in the target directory.
func TestCredentialsSaveCleansUpTempFileOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("seeding a directory where credentials.json belongs: %v", err)
	}

	if err := SaveCredentials(path, sampleCredentials()); err == nil {
		t.Fatal("SaveCredentials() must return an error when the destination is a directory")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "credentials.json" {
			t.Errorf("leftover temp file after a failed SaveCredentials(): %s", e.Name())
		}
	}
}
