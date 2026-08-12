package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/state"
)

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
