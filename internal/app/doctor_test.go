package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/config"
)

const testHostileAliasesYAML = `version: 1

profiles:
  - development

aliases:
  - name: "bad name!"
    command: echo hostile
  - name: good
    command: echo fine
    profiles: [undeclared-profile]
`

func TestDoctorReportsHostileEntryAndUndeclaredProfile(t *testing.T) {
	te := newTestEnv(t)
	writeConfigYAML(t, te.Base, nativeDeviceConfig("test-device"))
	writeAliasesYAML(t, te.Base, testHostileAliasesYAML)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	report, err := Doctor(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("Doctor() returned an error: %v", err)
	}

	if !report.Issues.HasErrors() {
		t.Error("Issues.HasErrors() = false, want true for a hostile alias name")
	}

	found := false
	for _, issue := range report.Issues {
		if issue.AliasName == `bad name!` {
			found = true
		}
	}
	if !found {
		t.Errorf("Issues does not mention the hostile entry: %+v", report.Issues)
	}

	if len(report.ProfileWarnings) == 0 {
		t.Error("ProfileWarnings is empty, want a warning about \"undeclared-profile\"")
	}
}

func TestDoctorWritesNothing(t *testing.T) {
	te := newTestEnv(t)
	writeConfigYAML(t, te.Base, nativeDeviceConfig("test-device"))
	writeAliasesYAML(t, te.Base, testHostileAliasesYAML)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	before, err := os.ReadDir(te.Base)
	if err != nil {
		t.Fatalf("reading base dir: %v", err)
	}
	beforeNames := dirEntryNames(before)

	if _, err := Doctor(context.Background(), te.Env, Options{}); err != nil {
		t.Fatalf("Doctor() returned an error: %v", err)
	}

	if _, err := os.Stat(config.StateFile(te.Base)); err == nil {
		t.Error("doctor must not write state.json")
	}

	after, err := os.ReadDir(te.Base)
	if err != nil {
		t.Fatalf("reading base dir: %v", err)
	}
	afterNames := dirEntryNames(after)

	if len(beforeNames) != len(afterNames) {
		t.Errorf("doctor changed the number of files in the base dir: before=%v after=%v", beforeNames, afterNames)
	}
}

// TestDoctorLeavesAHandWrittenConfigUntouched covers the case the file-count
// assertion above cannot see.
//
// Counting files misses a rewrite in place, and seeding a config that already
// has device.name excludes the branch that used to do the rewriting: loading a
// config without one generated an identity and persisted it, so `doctor`
// reformatted a hand-authored file and added empty keys the user never typed.
//
// The fixture here is deliberately minimal and hand-shaped — no device block,
// no quoting AliasDeck would have produced — so any write at all shows up as a
// byte difference.
func TestDoctorLeavesAHandWrittenConfigUntouched(t *testing.T) {
	te := newTestEnv(t)

	handWritten := "version: 1\nsource:\n  type: file\nbackend: native\n"
	configPath := config.ConfigFile(te.Base)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("creating base dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(handWritten), 0o600); err != nil {
		t.Fatalf("seeding config.yaml: %v", err)
	}
	writeAliasesYAML(t, te.Base, testHostileAliasesYAML)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	if _, err := Doctor(context.Background(), te.Env, Options{}); err != nil {
		t.Fatalf("Doctor() returned an error: %v", err)
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("re-reading config.yaml: %v", err)
	}
	if string(got) != handWritten {
		t.Errorf("doctor rewrote the user's config.yaml\n before: %q\n  after: %q", handWritten, got)
	}
}

func dirEntryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
