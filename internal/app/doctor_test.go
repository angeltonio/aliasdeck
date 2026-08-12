package app

import (
	"context"
	"os"
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

func dirEntryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
