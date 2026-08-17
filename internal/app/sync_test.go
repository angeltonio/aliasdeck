package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/source"
	"github.com/angeltonio/aliasdeck/internal/state"
)

var errFakeUnreachable = errors.New("could not resolve host")

const testAliasesYAML = `version: 1

profiles:
  - development

aliases:
  - name: dcu
    command: docker compose up -d
    description: Start Docker Compose stack
  - name: dps
    command: docker ps
`

func seedSyncableDevice(t *testing.T, te *testEnv) {
	t.Helper()
	writeConfigYAML(t, te.Base, nativeDeviceConfig("test-device"))
	writeAliasesYAML(t, te.Base, testAliasesYAML)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")
}

func TestSyncFullPipelineOrder(t *testing.T) {
	te := newTestEnv(t)
	seedSyncableDevice(t, te)

	report, err := Sync(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("Sync() returned an error: %v", err)
	}

	if report.Skipped {
		t.Fatal("first sync must not be skipped")
	}
	if report.AliasCount != 2 {
		t.Errorf("AliasCount = %d, want 2", report.AliasCount)
	}
	wantOutput := filepath.Join(te.Base, "aliases.zsh")
	if report.OutputPath != wantOutput {
		t.Errorf("OutputPath = %q, want %q", report.OutputPath, wantOutput)
	}

	// resolve -> validate -> render -> apply -> state, in that order: the
	// generated file and the state record must both exist and agree.
	generated, err := os.ReadFile(wantOutput)
	if err != nil {
		t.Fatalf("reading generated file: %v", err)
	}
	if len(generated) == 0 {
		t.Fatal("generated file is empty")
	}

	st, err := state.Load(config.StateFile(te.Base))
	if err != nil {
		t.Fatalf("loading state: %v", err)
	}
	if st.Revision != report.Revision {
		t.Errorf("state.Revision = %q, want %q", st.Revision, report.Revision)
	}
	if st.AliasCount != 2 {
		t.Errorf("state.AliasCount = %d, want 2", st.AliasCount)
	}
	if st.OutputPath != wantOutput {
		t.Errorf("state.OutputPath = %q, want %q", st.OutputPath, wantOutput)
	}
}

func TestSyncNoOpSkipWhenUnchanged(t *testing.T) {
	te := newTestEnv(t)
	seedSyncableDevice(t, te)

	if _, err := Sync(context.Background(), te.Env, Options{}); err != nil {
		t.Fatalf("first Sync() returned an error: %v", err)
	}

	// Make the base directory read-only so any write attempt (temp file
	// creation) during the second sync fails loudly instead of silently
	// succeeding — the strongest available proof that no write occurs.
	if err := os.Chmod(te.Base, 0o500); err != nil {
		t.Fatalf("making base dir read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(te.Base, 0o755) })

	report, err := Sync(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("second Sync() returned an error: %v", err)
	}
	if !report.Skipped {
		t.Error("second sync with no upstream change must be skipped (no-op)")
	}
}

func TestSyncForcedRewriteOnDiskHashMismatch(t *testing.T) {
	te := newTestEnv(t)
	seedSyncableDevice(t, te)

	first, err := Sync(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("first Sync() returned an error: %v", err)
	}

	// Hand-edit the generated file after the sync. The revision on disk in
	// state.json is unchanged (aliases.yaml did not change), but the file's
	// hash no longer matches, so the next sync must rewrite it anyway.
	tampered := "# hand-edited, should be overwritten\n"
	if err := os.WriteFile(first.OutputPath, []byte(tampered), 0o644); err != nil {
		t.Fatalf("tampering with generated file: %v", err)
	}

	second, err := Sync(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("second Sync() returned an error: %v", err)
	}
	if second.Skipped {
		t.Fatal("sync must force a rewrite when the on-disk hash no longer matches recorded state")
	}

	got, err := os.ReadFile(first.OutputPath)
	if err != nil {
		t.Fatalf("reading generated file: %v", err)
	}
	if string(got) == tampered {
		t.Error("generated file was not rewritten after a disk-hash mismatch")
	}
}

func TestSyncMigratesV053ManagedAliasesBeforeAtomicApply(t *testing.T) {
	te := newTestEnv(t)
	writeConfigYAML(t, te.Base, nativeDeviceConfig("test-device"))
	writeAliasesYAML(t, te.Base, "version: 1\naliases: []\n")
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")
	legacy, err := os.ReadFile(filepath.Join("..", "renderers", "testdata", "v053_zsh_legacy.golden"))
	if err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(te.Base, "aliases.zsh")
	if err := os.MkdirAll(te.Base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outputPath, legacy, 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := Sync(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("Sync() returned an error: %v", err)
	}
	if first.Skipped {
		t.Fatal("legacy output migration must write the current format")
	}
	generated, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`if [ -z "${ALIASDECK_MANAGED_ALIAS_NAMES+x}" ]; then`,
		`ALIASDECK_MANAGED_ALIAS_NAMES='_under dot.name foo-bar'`,
	} {
		if !strings.Contains(string(generated), want) {
			t.Fatalf("migrated output missing %q:\n%s", want, generated)
		}
	}
	st, err := state.Load(config.StateFile(te.Base))
	if err != nil {
		t.Fatal(err)
	}
	if st.OutputHash != hashBytes(generated) {
		t.Fatalf("state hash %q does not bind the atomically applied migrated bytes", st.OutputHash)
	}

	second, err := Sync(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("second Sync() returned an error: %v", err)
	}
	if !second.Skipped {
		t.Fatal("unchanged migrated output must be an idempotent no-op")
	}

	writeAliasesYAML(t, te.Base, "version: 1\naliases:\n  - name: current\n    command: printf current\n")
	third, err := Sync(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("third Sync() returned an error: %v", err)
	}
	if third.Skipped {
		t.Fatal("changed aliases must write a successor revision")
	}
	carried, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(carried), `ALIASDECK_MANAGED_ALIAS_NAMES='_under dot.name foo-bar'`) {
		t.Fatalf("successor revision dropped the pending legacy migration:\n%s", carried)
	}
}

func TestSyncMigratesV053OutputWhenAliasRevisionIsUnchanged(t *testing.T) {
	te := newTestEnv(t)
	writeConfigYAML(t, te.Base, nativeDeviceConfig("legacy-mac"))
	writeAliasesYAML(t, te.Base, `version: 1
aliases:
  - name: _under
    command: printf under
    description: Leading underscore
  - name: dot.name
    command: printf dot
    description: Dot in a valid POSIX alias name
  - name: foo-bar
    command: printf 'quoted value'
    description: Hyphen and embedded quote
`)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	// Establish the exact revision for this config, then replace only the
	// generated bytes/state hash with the corresponding v0.5.3 format. This
	// models upgrading the client without changing aliases.
	if _, err := Sync(context.Background(), te.Env, Options{}); err != nil {
		t.Fatalf("seeding current revision: %v", err)
	}
	statePath := config.StateFile(te.Base)
	st, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := os.ReadFile(filepath.Join("..", "renderers", "testdata", "v053_zsh_legacy.golden"))
	if err != nil {
		t.Fatal(err)
	}
	legacy = []byte(strings.Replace(string(legacy), "# Revision: 0123456789ab", "# Revision: "+st.Revision, 1))
	if err := os.WriteFile(st.OutputPath, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	st.OutputHash = hashBytes(legacy)
	st.ClientVersion = "0.5.3"
	if err := state.Save(statePath, st); err != nil {
		t.Fatal(err)
	}

	report, err := Sync(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("Sync() returned an error: %v", err)
	}
	if report.Skipped {
		t.Fatal("client upgrade must rewrite a legacy renderer even when the alias revision is unchanged")
	}
	generated, err := os.ReadFile(st.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), `if [ -z "${ALIASDECK_MANAGED_ALIAS_NAMES+x}" ]; then`) {
		t.Fatalf("unchanged-revision upgrade omitted legacy cleanup marker:\n%s", generated)
	}

	second, err := Sync(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("second Sync() returned an error: %v", err)
	}
	if !second.Skipped {
		t.Fatal("completed renderer migration must be idempotent")
	}
}

func TestSyncRenderedOutputIsDeterministic(t *testing.T) {
	te := newTestEnv(t)
	seedSyncableDevice(t, te)

	first, err := Sync(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("first Sync() returned an error: %v", err)
	}
	firstState, err := state.Load(config.StateFile(te.Base))
	if err != nil {
		t.Fatalf("loading state: %v", err)
	}

	// Delete state so the second sync cannot take the no-op skip path, then
	// resolve and render from scratch: the output hash must be identical,
	// proving rendered output never embeds a timestamp or other
	// non-deterministic content (sync-state spec, "Rendered Output Is
	// Deterministic").
	if err := os.Remove(config.StateFile(te.Base)); err != nil {
		t.Fatalf("removing state.json: %v", err)
	}

	if _, err := Sync(context.Background(), te.Env, Options{}); err != nil {
		t.Fatalf("second Sync() returned an error: %v", err)
	}
	secondState, err := state.Load(config.StateFile(te.Base))
	if err != nil {
		t.Fatalf("loading state: %v", err)
	}

	if firstState.OutputHash != secondState.OutputHash {
		t.Errorf("OutputHash changed across identical resolutions: %q != %q",
			firstState.OutputHash, secondState.OutputHash)
	}
	if first.Revision != secondState.Revision {
		t.Errorf("Revision changed across identical resolutions: %q != %q",
			first.Revision, secondState.Revision)
	}
}

// fakeGitSource is a minimal source.ConfigSource + source.ResolveReporter
// double, so syncWithContext's staleness wiring can be tested without a
// real git subprocess or filesystem checkout.
type fakeGitSource struct {
	resolved domain.ResolvedConfig
	err      error
	info     source.ResolveInfo
}

func (f *fakeGitSource) Resolve(context.Context, domain.Device) (domain.ResolvedConfig, error) {
	return f.resolved, f.err
}

func (f *fakeGitSource) LastResolve() source.ResolveInfo { return f.info }

func gitDeviceContext(t *testing.T, te *testEnv, src source.ConfigSource, desc source.Descriptor) deviceContext {
	t.Helper()
	dev := domain.Device{Platform: domain.PlatformLinux, Shell: domain.ShellBash, ClientVersion: Version}
	backend, err := resolveBackend(config.DeviceFileConfig{Backend: config.BackendNative}, te.Base)
	if err != nil {
		t.Fatalf("resolveBackend() returned an error: %v", err)
	}
	return deviceContext{
		Base:       te.Base,
		Device:     dev,
		Source:     src,
		SourceDesc: desc,
		Backend:    backend,
	}
}

// TestSyncRecordsGitSourceStaleness pins design decision 14's wiring:
// syncWithContext type-asserts source.ResolveReporter and records what it
// reports into state.State, including the resolved-commit-augmented
// SourceRef (<url>#<ref>@<short-sha>).
func TestSyncRecordsGitSourceStaleness(t *testing.T) {
	te := newTestEnv(t)
	dev := domain.Device{Platform: domain.PlatformLinux, Shell: domain.ShellBash, ClientVersion: Version}
	resolved := domain.Resolve(dev, []domain.Alias{{Name: "gs", Command: "git status", Enabled: true}})

	fetchedAt := time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC)
	src := &fakeGitSource{
		resolved: resolved,
		info: source.ResolveInfo{
			Ref:       "HEAD",
			Commit:    "0123456789abcdef0123456789abcdef01234567",
			FetchedAt: fetchedAt,
			Stale:     true,
		},
	}
	desc := source.Descriptor{Type: "git", Ref: "https://example.com/dotfiles.git#HEAD"}
	dc := gitDeviceContext(t, te, src, desc)

	if _, err := syncWithContext(context.Background(), te.Env, dc); err != nil {
		t.Fatalf("syncWithContext() returned an error: %v", err)
	}

	st, err := state.Load(config.StateFile(te.Base))
	if err != nil {
		t.Fatalf("loading state: %v", err)
	}
	if !st.SourceStale {
		t.Error("state.SourceStale = false, want true")
	}
	if !st.SourceFetchedAt.Equal(fetchedAt) {
		t.Errorf("state.SourceFetchedAt = %v, want %v", st.SourceFetchedAt, fetchedAt)
	}
	wantRef := "https://example.com/dotfiles.git#HEAD@0123456789ab"
	if st.SourceRef != wantRef {
		t.Errorf("state.SourceRef = %q, want %q", st.SourceRef, wantRef)
	}
	if st.SourceType != "git" {
		t.Errorf("state.SourceType = %q, want %q", st.SourceType, "git")
	}
}

// TestSyncFileSourceLeavesStalenessUnset pins that FileSource — which does
// not implement source.ResolveReporter — never sets SourceStale, matching
// the migration note that a v0.1 state file (or a v0.2 file-source sync)
// yields SourceStale=false.
func TestSyncFileSourceLeavesStalenessUnset(t *testing.T) {
	te := newTestEnv(t)
	seedSyncableDevice(t, te)

	if _, err := Sync(context.Background(), te.Env, Options{}); err != nil {
		t.Fatalf("Sync() returned an error: %v", err)
	}

	st, err := state.Load(config.StateFile(te.Base))
	if err != nil {
		t.Fatalf("loading state: %v", err)
	}
	if st.SourceStale {
		t.Error("state.SourceStale = true for a file source, want false")
	}
	if !st.SourceFetchedAt.IsZero() {
		t.Errorf("state.SourceFetchedAt = %v, want zero for a file source", st.SourceFetchedAt)
	}
}

// TestSyncUnreachableGitSourceWithoutCacheFailsAndNamesSource pins the
// "Unreachable remote with no prior checkout" scenario at the syncWithContext
// boundary: sync must fail, naming the source, and must not write state.
func TestSyncUnreachableGitSourceWithoutCacheFailsAndNamesSource(t *testing.T) {
	te := newTestEnv(t)
	url := "https://example.com/dotfiles.git"
	src := &fakeGitSource{err: errFakeUnreachable}
	desc := source.Descriptor{Type: "git", Ref: url + "#HEAD"}
	dc := gitDeviceContext(t, te, src, desc)

	if _, err := syncWithContext(context.Background(), te.Env, dc); err == nil {
		t.Fatal("syncWithContext() must fail when the git source cannot be resolved")
	} else if !strings.Contains(err.Error(), url) {
		t.Errorf("error %q does not name the unresolvable source %q", err, url)
	}

	if _, err := os.Stat(config.StateFile(te.Base)); !os.IsNotExist(err) {
		t.Errorf("state.json must not be written when resolve fails outright: stat err = %v", err)
	}
}

func TestSyncUnresolvableSourceNamesTheSource(t *testing.T) {
	te := newTestEnv(t)
	writeConfigYAML(t, te.Base, nativeDeviceConfig("test-device"))
	// No aliases.yaml written: the source cannot be resolved.
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	_, err := Sync(context.Background(), te.Env, Options{})
	if err == nil {
		t.Fatal("Sync() must fail when the source cannot be resolved")
	}
	if want := config.AliasesFile(te.Base); !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name the unresolvable source %q", err, want)
	}
}

// TestSyncReportsStalenessEvenWhenSkipping covers the case an offline sync
// almost always takes.
//
// A machine whose remote is unreachable usually has an unchanged configuration
// too, so the sync skips. Reporting staleness only alongside a write would stay
// silent exactly there: the user sees "up to date" and believes their aliases
// describe the repository as it is now, when they describe it as it was.
func TestSyncReportsStalenessEvenWhenSkipping(t *testing.T) {
	te := newTestEnv(t)
	seedSyncableDevice(t, te)

	// First sync writes and records state.
	first, err := Sync(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	if first.Skipped {
		t.Fatal("the first sync should have written")
	}

	// Swap in a source that reports the same content but flags it as served
	// from cache, then sync again. Nothing changed, so it skips.
	dc, err := loadDeviceContext(te.Env, Options{})
	if err != nil {
		t.Fatalf("loadDeviceContext: %v", err)
	}
	fetched := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	dc.Source = staleSource{inner: dc.Source, fetchedAt: fetched}

	second, err := syncWithContext(context.Background(), te.Env, dc)
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if !second.Skipped {
		t.Fatal("the second sync should have skipped; the fixture changed nothing")
	}
	if !second.SourceStale {
		t.Error("a skipped sync must still report that its source served cached content")
	}
	if !second.SourceFetchedAt.Equal(fetched) {
		t.Errorf("SourceFetchedAt = %v, want %v", second.SourceFetchedAt, fetched)
	}
}

// staleSource wraps a ConfigSource and reports every resolve as cache-served.
type staleSource struct {
	inner     source.ConfigSource
	fetchedAt time.Time
}

func (s staleSource) Resolve(ctx context.Context, dev domain.Device) (domain.ResolvedConfig, error) {
	return s.inner.Resolve(ctx, dev)
}

func (s staleSource) LastResolve() source.ResolveInfo {
	return source.ResolveInfo{Stale: true, FetchedAt: s.fetchedAt}
}
