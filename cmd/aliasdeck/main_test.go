package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCmd runs the real Cobra tree with real env vars isolated to a fresh
// t.TempDir(), so these tests exercise the actual command wiring and exit
// code mapping without ever touching a developer's real machine state.
func runCmd(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestRunNotInitializedExitsFour(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ALIASDECK_HOME", filepath.Join(home, ".config", "aliasdeck"))

	_, stderr, code := runCmd(t, "status")
	if code != exitNotInitialized {
		t.Errorf("exit code = %d, want %d", code, exitNotInitialized)
	}
	if !strings.Contains(stderr, "aliasdeck init") {
		t.Errorf("stderr = %q, want it to name `aliasdeck init`", stderr)
	}
}

func TestRunInitThenSyncSucceeds(t *testing.T) {
	home := t.TempDir()
	base := filepath.Join(home, ".config", "aliasdeck")
	t.Setenv("ALIASDECK_HOME", base)
	t.Setenv("ALIASDECK_PLATFORM", "macos")
	t.Setenv("ALIASDECK_SHELL", "zsh")

	_, stderr, code := runCmd(t, "init", "--no-bootstrap")
	if code != exitOK {
		t.Fatalf("init exit code = %d, want %d (stderr: %s)", code, exitOK, stderr)
	}

	stdout, stderr, code := runCmd(t, "sync")
	if code != exitOK {
		t.Fatalf("sync exit code = %d, want %d (stderr: %s)", code, exitOK, stderr)
	}
	if !strings.Contains(stdout, "alias(es)") {
		t.Errorf("sync stdout = %q, want it to mention the alias count", stdout)
	}
}

func TestRunDoctorFindsErrorExitsThree(t *testing.T) {
	home := t.TempDir()
	base := filepath.Join(home, ".config", "aliasdeck")
	t.Setenv("ALIASDECK_HOME", base)
	t.Setenv("ALIASDECK_PLATFORM", "macos")
	t.Setenv("ALIASDECK_SHELL", "zsh")

	if _, _, code := runCmd(t, "init", "--no-bootstrap"); code != exitOK {
		t.Fatalf("init exit code = %d, want %d", code, exitOK)
	}

	hostileAliases := "version: 1\naliases:\n  - name: \"bad name!\"\n    command: echo hi\n"
	if err := os.WriteFile(filepath.Join(base, "aliases.yaml"), []byte(hostileAliases), 0o600); err != nil {
		t.Fatalf("writing hostile aliases.yaml: %v", err)
	}

	stdout, _, code := runCmd(t, "doctor")
	if code != exitInvalidConfig {
		t.Errorf("doctor exit code = %d, want %d", code, exitInvalidConfig)
	}
	if !strings.Contains(stdout, "bad name!") {
		t.Errorf("doctor stdout = %q, want it to name the hostile entry", stdout)
	}
}

func TestRunUnknownCommandExitsTwo(t *testing.T) {
	_, _, code := runCmd(t, "not-a-real-command")
	if code != exitUsageError {
		t.Errorf("exit code = %d, want %d", code, exitUsageError)
	}
}

func TestRunEditWithoutEditorExitsOne(t *testing.T) {
	home := t.TempDir()
	base := filepath.Join(home, ".config", "aliasdeck")
	t.Setenv("ALIASDECK_HOME", base)
	t.Setenv("ALIASDECK_PLATFORM", "macos")
	t.Setenv("ALIASDECK_SHELL", "zsh")
	t.Setenv("EDITOR", "")

	if _, _, code := runCmd(t, "init", "--no-bootstrap"); code != exitOK {
		t.Fatalf("init exit code = %d, want %d", code, exitOK)
	}

	_, stderr, code := runCmd(t, "edit")
	if code != exitRuntimeError {
		t.Errorf("edit exit code = %d, want %d", code, exitRuntimeError)
	}
	if !strings.Contains(stderr, "EDITOR") {
		t.Errorf("stderr = %q, want it to mention $EDITOR", stderr)
	}
}
