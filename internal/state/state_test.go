package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

func sampleState() State {
	return State{
		Version:       1,
		Revision:      "abc123def456",
		OutputPath:    "/home/user/.config/aliasdeck/aliases.zsh",
		OutputHash:    "deadbeef",
		AliasCount:    3,
		Platform:      domain.PlatformLinux,
		Shell:         domain.ShellZsh,
		SourceType:    "file",
		SourceRef:     "/home/user/dotfiles/aliases.yaml",
		LastSyncAt:    time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
		ClientVersion: "0.1.0",
		Bootstrap: &Bootstrap{
			RCPath:  "/home/user/.zshrc",
			Block:   "\n# >>> aliasdeck >>>\nline\n# <<< aliasdeck <<<\n",
			RCHash:  "cafebabe",
			AddedAt: time.Date(2026, 1, 15, 10, 29, 0, 0, time.UTC),
		},
	}
}

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	want := sampleState()

	if err := Save(path, want); err != nil {
		t.Fatalf("Save() returned an error: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned an error: %v", err)
	}

	if got.Version != want.Version ||
		got.Revision != want.Revision ||
		got.OutputPath != want.OutputPath ||
		got.OutputHash != want.OutputHash ||
		got.AliasCount != want.AliasCount ||
		got.Platform != want.Platform ||
		got.Shell != want.Shell ||
		got.SourceType != want.SourceType ||
		got.SourceRef != want.SourceRef ||
		got.ClientVersion != want.ClientVersion ||
		!got.LastSyncAt.Equal(want.LastSyncAt) {
		t.Errorf("round trip mismatch:\ngot:  %+v\nwant: %+v", got, want)
	}

	if got.Bootstrap == nil {
		t.Fatal("round trip lost the Bootstrap field")
	}
	if got.Bootstrap.RCPath != want.Bootstrap.RCPath ||
		got.Bootstrap.Block != want.Bootstrap.Block ||
		got.Bootstrap.RCHash != want.Bootstrap.RCHash ||
		!got.Bootstrap.AddedAt.Equal(want.Bootstrap.AddedAt) {
		t.Errorf("Bootstrap round trip mismatch:\ngot:  %+v\nwant: %+v", got.Bootstrap, want.Bootstrap)
	}
}

// TestStateRoundTripWithGitStaleness pins design decision 14: state.State
// gains SourceStale and SourceFetchedAt so a GitSource's offline fallback
// survives a save/load cycle instead of being reported as current.
func TestStateRoundTripWithGitStaleness(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	want := sampleState()
	want.SourceType = "git"
	want.SourceRef = "https://example.com/dotfiles.git#main@0123456789ab"
	want.SourceStale = true
	want.SourceFetchedAt = time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC)

	if err := Save(path, want); err != nil {
		t.Fatalf("Save() returned an error: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned an error: %v", err)
	}

	if got.SourceStale != want.SourceStale {
		t.Errorf("SourceStale = %v, want %v", got.SourceStale, want.SourceStale)
	}
	if !got.SourceFetchedAt.Equal(want.SourceFetchedAt) {
		t.Errorf("SourceFetchedAt = %v, want %v", got.SourceFetchedAt, want.SourceFetchedAt)
	}
	if got.SourceRef != want.SourceRef {
		t.Errorf("SourceRef = %q, want %q", got.SourceRef, want.SourceRef)
	}
}

// TestStateOmitsSourceStaleWhenFalse pins the "omitempty" half of design
// decision 14 for the field it actually applies to: encoding/json's
// omitempty never omits a zero-value time.Time (it is a struct, not one of
// the basic types isEmptyValue recognizes), so only SourceStale's `false`
// is genuinely omitted from a v0.1-shaped file source's state.json.
func TestStateOmitsSourceStaleWhenFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	want := sampleState() // file source: SourceStale/SourceFetchedAt left at zero value

	if err := Save(path, want); err != nil {
		t.Fatalf("Save() returned an error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading state.json: %v", err)
	}
	if strings.Contains(string(data), "sourceStale") {
		t.Errorf("state.json contains sourceStale for a non-stale file source:\n%s", data)
	}
}

func TestStateRoundTripWithoutBootstrap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	want := sampleState()
	want.Bootstrap = nil

	if err := Save(path, want); err != nil {
		t.Fatalf("Save() returned an error: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned an error: %v", err)
	}
	if got.Bootstrap != nil {
		t.Errorf("Bootstrap = %+v, want nil", got.Bootstrap)
	}
}

func TestStateSaveSetsFileMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := Save(path, sampleState()); err != nil {
		t.Fatalf("Save() returned an error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("state.json mode = %o, want %o", info.Mode().Perm(), 0o600)
	}
}

func TestStateLoadMissingFileIsToleratedAsEmptyState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() must not error on a missing file, got: %v", err)
	}
	if got != (State{}) {
		t.Errorf("Load() of a missing file = %+v, want zero-value State", got)
	}
}

func TestStateLoadCorruptJSONIsToleratedAsEmptyState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("seeding corrupt state.json: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() must not error on corrupt JSON, got: %v", err)
	}
	if got != (State{}) {
		t.Errorf("Load() of a corrupt file = %+v, want zero-value State", got)
	}
}

func TestStateSaveOverwritesWithoutLeftoverTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	first := sampleState()
	if err := Save(path, first); err != nil {
		t.Fatalf("first Save() returned an error: %v", err)
	}

	second := sampleState()
	second.Revision = "newrevision"
	if err := Save(path, second); err != nil {
		t.Fatalf("second Save() returned an error: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned an error: %v", err)
	}
	if got.Revision != "newrevision" {
		t.Errorf("Revision after second save = %q, want %q", got.Revision, "newrevision")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("dir entries = %d, want exactly 1 (state.json, no leftover temp files): %v", len(entries), entries)
	}
}

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permission checks are bypassed when running as root")
	}
}

func TestStateSaveFailsWhenDirectoryCannotBeCreated(t *testing.T) {
	dir := t.TempDir()
	// blocker is a regular file; MkdirAll must fail trying to create a
	// directory through it.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("seeding blocker file: %v", err)
	}

	path := filepath.Join(blocker, "sub", "state.json")
	if err := Save(path, sampleState()); err == nil {
		t.Fatal("Save() must return an error when its directory cannot be created")
	}
}

func TestStateSaveFailsWhenTempFileCannotBeCreated(t *testing.T) {
	skipIfRoot(t)

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	defer os.Chmod(dir, 0o700)

	path := filepath.Join(dir, "state.json")
	if err := Save(path, sampleState()); err == nil {
		t.Fatal("Save() must return an error when the temp file cannot be created in a read-only directory")
	}
}

func TestStateLoadPropagatesNonNotExistReadErrors(t *testing.T) {
	skipIfRoot(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("seeding state.json: %v", err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod path: %v", err)
	}
	defer os.Chmod(path, 0o600)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() must propagate a permission-denied read error rather than tolerate it")
	}
}
