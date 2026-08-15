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

func TestInitCreatesBothConfigFiles(t *testing.T) {
	te := newTestEnv(t)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	report, err := Init(context.Background(), te.Env, InitOptions{NoBootstrap: true})
	if err != nil {
		t.Fatalf("Init() returned an error: %v", err)
	}

	if !report.ConfigCreated {
		t.Error("report.ConfigCreated = false, want true")
	}
	if !report.AliasesCreated {
		t.Error("report.AliasesCreated = false, want true")
	}

	if _, err := os.Stat(config.ConfigFile(te.Base)); err != nil {
		t.Errorf("config.yaml was not created: %v", err)
	}
	if _, err := os.Stat(config.AliasesFile(te.Base)); err != nil {
		t.Errorf("aliases.yaml was not created: %v", err)
	}

	// init also runs the initial sync, per the standalone user flow
	// (PROJECT.md §15.1: init creates files, sync applies them).
	if report.Sync.AliasCount != 0 {
		t.Errorf("Sync.AliasCount = %d, want 0 for a freshly created empty aliases.yaml", report.Sync.AliasCount)
	}
	if _, err := os.Stat(report.Sync.OutputPath); err != nil {
		t.Errorf("generated file was not created by the initial sync: %v", err)
	}
}

func TestInitNoBootstrapSkipsPromptAndRCFile(t *testing.T) {
	te := newTestEnv(t)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	confirmCalled := false
	report, err := Init(context.Background(), te.Env, InitOptions{
		NoBootstrap: true,
		Confirm: func(string) (bool, error) {
			confirmCalled = true
			return true, nil
		},
	})
	if err != nil {
		t.Fatalf("Init() returned an error: %v", err)
	}

	if confirmCalled {
		t.Error("--no-bootstrap must never prompt for consent")
	}
	if report.BootstrapSkippedReason != "--no-bootstrap" {
		t.Errorf("BootstrapSkippedReason = %q, want %q", report.BootstrapSkippedReason, "--no-bootstrap")
	}
	if report.ManualBootstrapLine == "" {
		t.Error("Init must print how to add the bootstrap line manually when it is skipped")
	}

	rcPath := te.Home + "/.zshrc"
	if _, err := os.Stat(rcPath); err == nil {
		t.Errorf("%s must not be touched when --no-bootstrap is set", rcPath)
	}
}

func TestInitCanSkipInitialSync(t *testing.T) {
	te := newTestEnv(t)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	report, err := Init(context.Background(), te.Env, InitOptions{
		NoBootstrap:     true,
		SkipInitialSync: true,
	})
	if err != nil {
		t.Fatalf("Init() returned an error: %v", err)
	}
	if report.Sync.AliasCount != 0 || report.Sync.Revision != "" {
		t.Fatalf("skipped sync report = %+v, want no sync result", report.Sync)
	}
	if _, err := os.Stat(report.Sync.OutputPath); !os.IsNotExist(err) {
		t.Fatalf("generated file exists after skipped initial sync, stat error = %v", err)
	}
}

func TestInitPromptsBeforeBootstrapAndAddsOnConsent(t *testing.T) {
	te := newTestEnv(t)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	var askedQuestion string
	report, err := Init(context.Background(), te.Env, InitOptions{
		Confirm: func(q string) (bool, error) {
			askedQuestion = q
			return true, nil
		},
	})
	if err != nil {
		t.Fatalf("Init() returned an error: %v", err)
	}

	if askedQuestion == "" {
		t.Fatal("Init must prompt for consent before editing the rc file")
	}
	if !report.BootstrapAdded {
		t.Error("BootstrapAdded = false, want true after consent")
	}

	// filepath.Join, not a literal "/" concatenation: state.Bootstrap.RCPath
	// is built by production code with filepath.Join, which uses the native
	// separator, so a literal "/" here would only match by accident on a
	// host whose native separator is "/".
	rcPath := filepath.Join(te.Home, ".zshrc")
	data, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file: %v", err)
	}
	if !strings.Contains(string(data), "aliasdeck") {
		t.Errorf("rc file %q does not contain the AliasDeck bootstrap block: %q", rcPath, data)
	}

	st, err := state.Load(config.StateFile(te.Base))
	if err != nil {
		t.Fatalf("loading state: %v", err)
	}
	if st.Bootstrap == nil {
		t.Fatal("state.Bootstrap was not recorded after a successful bootstrap add")
	}
	if st.Bootstrap.RCPath != rcPath {
		t.Errorf("state.Bootstrap.RCPath = %q, want %q", st.Bootstrap.RCPath, rcPath)
	}
}

func TestInitPromptDeclinedLeavesRCFileUntouched(t *testing.T) {
	te := newTestEnv(t)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	report, err := Init(context.Background(), te.Env, InitOptions{
		Confirm: func(string) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatalf("Init() returned an error: %v", err)
	}

	if report.BootstrapAdded {
		t.Error("BootstrapAdded = true, want false after declining consent")
	}
	if report.BootstrapSkippedReason != "declined" {
		t.Errorf("BootstrapSkippedReason = %q, want %q", report.BootstrapSkippedReason, "declined")
	}

	rcPath := te.Home + "/.zshrc"
	if _, err := os.Stat(rcPath); err == nil {
		t.Errorf("%s must not be created when the user declines the bootstrap prompt", rcPath)
	}
}

func TestInitIsIdempotentForExistingFiles(t *testing.T) {
	te := newTestEnv(t)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	if _, err := Init(context.Background(), te.Env, InitOptions{NoBootstrap: true}); err != nil {
		t.Fatalf("first Init() returned an error: %v", err)
	}

	report, err := Init(context.Background(), te.Env, InitOptions{NoBootstrap: true})
	if err != nil {
		t.Fatalf("second Init() returned an error: %v", err)
	}
	if report.ConfigCreated {
		t.Error("second Init() must not report ConfigCreated when config.yaml already exists")
	}
	if report.AliasesCreated {
		t.Error("second Init() must not report AliasesCreated when aliases.yaml already exists")
	}
}

// TestInitAssumeYesConsentsWithoutPrompting covers the flag that makes an
// unattended install possible.
//
// Prompts are skipped when stdin is not a terminal, because reading a pipe
// that never delivers a line blocks forever. Without an explicit way to
// consent, that safety measure would leave an install script or a container
// build permanently unable to add the bootstrap — the one step that makes
// aliases actually load.
func TestInitAssumeYesConsentsWithoutPrompting(t *testing.T) {
	te := newTestEnv(t)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	rcPath := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(rcPath, []byte("alias ll='ls -la'\n"), 0o644); err != nil {
		t.Fatalf("seeding rc file: %v", err)
	}

	confirmCalled := false
	report, err := Init(context.Background(), te.Env, InitOptions{
		AssumeYes: true,
		RCFile:    rcPath,
		Confirm: func(string) (bool, error) {
			confirmCalled = true
			return false, nil
		},
	})
	if err != nil {
		t.Fatalf("Init() returned an error: %v", err)
	}

	if confirmCalled {
		t.Error("--yes must not ask a question it already has the answer to")
	}
	if !report.BootstrapAdded {
		t.Fatalf("--yes did not add the bootstrap line (skipped: %q)", report.BootstrapSkippedReason)
	}

	rc, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file: %v", err)
	}
	if !strings.Contains(string(rc), "aliasdeck") {
		t.Errorf("rc file has no bootstrap block after --yes:\n%s", rc)
	}
	if !strings.Contains(string(rc), "alias ll='ls -la'") {
		t.Error("--yes destroyed the user's existing rc content")
	}
}

// TestInitAssumeYesAndNoBootstrapPrefersNotTouchingTheFile pins the safe
// resolution when a caller passes both flags: the one that declines wins.
func TestInitAssumeYesAndNoBootstrapPrefersNotTouchingTheFile(t *testing.T) {
	te := newTestEnv(t)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	rcPath := filepath.Join(t.TempDir(), ".zshrc")
	original := "alias ll='ls -la'\n"
	if err := os.WriteFile(rcPath, []byte(original), 0o644); err != nil {
		t.Fatalf("seeding rc file: %v", err)
	}

	report, err := Init(context.Background(), te.Env, InitOptions{
		AssumeYes:   true,
		NoBootstrap: true,
		RCFile:      rcPath,
	})
	if err != nil {
		t.Fatalf("Init() returned an error: %v", err)
	}
	if report.BootstrapAdded {
		t.Error("--no-bootstrap must win over --yes; the safe outcome is not editing the file")
	}

	rc, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file: %v", err)
	}
	if string(rc) != original {
		t.Errorf("rc file was modified despite --no-bootstrap:\n%s", rc)
	}
}
