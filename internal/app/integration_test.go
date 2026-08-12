package app

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/config"
)

// TestFullLifecycleInitSyncSyncUninstall is the Phase 3 integration test:
// init -> sync -> a second sync (no write) -> uninstall (byte-identical
// rc), all against a t.TempDir() HOME (design's Testing Strategy table,
// "init -> sync -> second sync (no-op) -> uninstall").
func TestFullLifecycleInitSyncSyncUninstall(t *testing.T) {
	te := newTestEnv(t)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	priorRC := "# pre-existing dotfiles content\nexport EDITOR=vim\n"
	rcPath := te.Home + "/.zshrc"
	if err := os.WriteFile(rcPath, []byte(priorRC), 0o644); err != nil {
		t.Fatalf("seeding rc file: %v", err)
	}

	ctx := context.Background()

	// 1. init: creates both config files and bootstraps the rc file with
	// consent.
	initReport, err := Init(ctx, te.Env, InitOptions{
		Confirm: func(string) (bool, error) { return true, nil },
	})
	if err != nil {
		t.Fatalf("Init() returned an error: %v", err)
	}
	if !initReport.ConfigCreated || !initReport.AliasesCreated {
		t.Fatalf("init did not create both config files: %+v", initReport)
	}
	if !initReport.BootstrapAdded {
		t.Fatal("init did not add the bootstrap line")
	}

	afterInitRC, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file after init: %v", err)
	}
	if !strings.Contains(string(afterInitRC), "aliasdeck") {
		t.Fatalf("rc file after init does not contain AliasDeck's block: %q", afterInitRC)
	}
	if !strings.HasPrefix(string(afterInitRC), priorRC) {
		t.Fatalf("rc file after init lost the user's pre-existing content: %q", afterInitRC)
	}

	// Give the device some real aliases to sync, then sync explicitly
	// (init already performed one sync of the still-empty file; write
	// content now and prove a second, content-bearing sync also behaves).
	writeAliasesYAML(t, te.Base, testAliasesYAML)

	firstSync, err := Sync(ctx, te.Env, Options{})
	if err != nil {
		t.Fatalf("first explicit Sync() returned an error: %v", err)
	}
	if firstSync.Skipped {
		t.Fatal("sync after a content change must not be skipped")
	}
	if firstSync.AliasCount != 2 {
		t.Errorf("AliasCount = %d, want 2", firstSync.AliasCount)
	}

	generatedAfterFirstSync, err := os.ReadFile(firstSync.OutputPath)
	if err != nil {
		t.Fatalf("reading generated file: %v", err)
	}

	// 2. second sync: no upstream change, so it must be a true no-op. A
	// read-only base dir proves no write is even attempted.
	if err := os.Chmod(te.Base, 0o500); err != nil {
		t.Fatalf("making base dir read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(te.Base, 0o755) })

	secondSync, err := Sync(ctx, te.Env, Options{})
	if err != nil {
		t.Fatalf("second Sync() returned an error: %v", err)
	}
	if !secondSync.Skipped {
		t.Fatal("second sync with no upstream change must be skipped (no-op)")
	}

	if err := os.Chmod(te.Base, 0o755); err != nil {
		t.Fatalf("restoring base dir permissions: %v", err)
	}

	generatedAfterSecondSync, err := os.ReadFile(firstSync.OutputPath)
	if err != nil {
		t.Fatalf("reading generated file after second sync: %v", err)
	}
	if string(generatedAfterFirstSync) != string(generatedAfterSecondSync) {
		t.Error("generated file changed across a no-op sync")
	}

	// 3. uninstall: removes the generated file and restores the rc file
	// byte-identically to what it was before init ever touched it.
	uninstallReport, err := Uninstall(ctx, te.Env, UninstallOptions{Yes: true})
	if err != nil {
		t.Fatalf("Uninstall() returned an error: %v", err)
	}
	if !uninstallReport.OutputRemoved {
		t.Error("uninstall did not remove the generated file")
	}
	if !uninstallReport.BootstrapRemoved {
		t.Error("uninstall did not remove the bootstrap line")
	}
	if !uninstallReport.BootstrapExact {
		t.Error("uninstall's rc restoration was not byte-exact")
	}

	finalRC, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file after uninstall: %v", err)
	}
	if string(finalRC) != priorRC {
		t.Errorf("rc file after uninstall = %q, want byte-identical to the original %q", finalRC, priorRC)
	}

	if _, err := os.Stat(firstSync.OutputPath); err == nil {
		t.Error("generated file still exists after uninstall")
	}
	if _, err := os.Stat(config.StateFile(te.Base)); err == nil {
		t.Error("state.json still exists after uninstall")
	}

	// state.json is gone, but config.yaml and aliases.yaml — the user's own
	// files — are untouched by uninstall.
	if _, err := os.Stat(config.ConfigFile(te.Base)); err != nil {
		t.Error("uninstall must not remove config.yaml")
	}
	if _, err := os.Stat(config.AliasesFile(te.Base)); err != nil {
		t.Error("uninstall must not remove aliases.yaml")
	}
}
