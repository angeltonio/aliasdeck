package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/source"
)

// TestEditNeverInvokesAShell is the required RED test for the "Editor
// subprocess" threat-matrix case: $EDITOR="x; rm -rf ." MUST NOT execute
// `rm -rf .`. If Edit ever handed $EDITOR to `sh -c`, this test's marker
// file would be deleted; if it splits argv correctly, "x;" is looked up as
// a literal (nonexistent) executable name and Edit fails before anything
// resembling a shell ever runs.
func TestEditNeverInvokesAShell(t *testing.T) {
	te := newTestEnv(t)
	seedSyncableDevice(t, te)

	// A file `rm -rf .` would delete if the shell ever ran in the working
	// directory Edit could plausibly execute from.
	marker := filepath.Join(te.Base, "marker")
	if err := os.WriteFile(marker, []byte("still here"), 0o644); err != nil {
		t.Fatalf("seeding marker file: %v", err)
	}

	te.setenv("EDITOR", `x; rm -rf .`)
	// LookPath must be asked for the literal first token, never for a
	// shell. It reports "not found", exactly as a real PATH lookup would
	// for a program named "x;" — Edit must surface that as an error and
	// never fall back to any shell-based execution.
	lookedUp := ""
	te.Env.LookPath = func(file string) (string, error) {
		lookedUp = file
		return "", errors.New("executable file not found in $PATH")
	}

	_, err := Edit(context.Background(), te.Env, EditOptions{})
	if err == nil {
		t.Fatal("Edit() must fail when $EDITOR's binary cannot be resolved")
	}
	if lookedUp != "x;" {
		t.Errorf("LookPath was asked to resolve %q, want the literal first token %q", lookedUp, "x;")
	}

	// The marker file must be untouched: rm -rf . never ran.
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("marker file was removed: %v", err)
	}
	if string(data) != "still here" {
		t.Errorf("marker file content changed: %q", data)
	}
}

// TestEditMultiWordEditorPassesThrough proves the documented limitation is
// exactly that — a limitation, not a broken feature: a common multi-word
// editor invocation like `code -w` must still work end to end.
func TestEditMultiWordEditorPassesThrough(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture requires a POSIX shell")
	}

	te := newTestEnv(t)
	seedSyncableDevice(t, te)

	captureFile := filepath.Join(t.TempDir(), "captured-args")
	scriptPath := filepath.Join(t.TempDir(), "code")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + captureFile + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake editor script: %v", err)
	}

	te.setenv("EDITOR", "code -w")
	te.Env.LookPath = func(file string) (string, error) {
		if file == "code" {
			return scriptPath, nil
		}
		return "", errors.New("not found")
	}

	report, err := Edit(context.Background(), te.Env, EditOptions{})
	if err != nil {
		t.Fatalf("Edit() returned an error: %v", err)
	}
	if report.Editor != "code" {
		t.Errorf("report.Editor = %q, want %q", report.Editor, "code")
	}

	captured, err := os.ReadFile(captureFile)
	if err != nil {
		t.Fatalf("reading captured args: %v", err)
	}
	wantLine1 := "-w"
	got := string(captured)
	if got == "" {
		t.Fatal("fake editor script received no arguments")
	}
	if got[:len(wantLine1)] != wantLine1 {
		t.Errorf("captured args = %q, want to start with %q", got, wantLine1)
	}
}

func TestEditHasNoSyncSideEffect(t *testing.T) {
	te := newTestEnv(t)
	seedSyncableDevice(t, te)

	scriptPath := filepath.Join(t.TempDir(), "true-editor")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing fake editor script: %v", err)
	}
	te.setenv("EDITOR", "true-editor")
	te.Env.LookPath = func(string) (string, error) { return scriptPath, nil }

	if _, err := Edit(context.Background(), te.Env, EditOptions{}); err != nil {
		t.Fatalf("Edit() returned an error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(te.Base, "aliases.zsh")); err == nil {
		t.Error("Edit() must not sync/render/apply as a side effect")
	}
}

// TestEditGitSourcePerformsNoGitWrite is the RED test for config-source
// spec's "GitSource Is Read-Only in v0.2": opening a git-sourced
// aliases.yaml in $EDITOR must never clone, fetch, commit, or push — Edit
// never calls dc.Source.Resolve at all, so proving the cache directory was
// never even created is the strongest available proof no git process ran.
func TestEditGitSourcePerformsNoGitWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture requires a POSIX shell")
	}

	te := newTestEnv(t)
	url := "https://example.com/dotfiles.git"
	cfg := nativeDeviceConfig("test-device")
	cfg.Source = config.Source{Type: config.SourceTypeGit, Git: config.GitSourceConfig{URL: url}}
	writeConfigYAML(t, te.Base, cfg)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	captureFile := filepath.Join(t.TempDir(), "captured-args")
	scriptPath := filepath.Join(t.TempDir(), "fakeeditor")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + captureFile + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake editor script: %v", err)
	}
	te.setenv("EDITOR", "fakeeditor")
	te.Env.LookPath = func(file string) (string, error) {
		if file == "fakeeditor" {
			return scriptPath, nil
		}
		return "", errors.New("not found")
	}

	report, err := Edit(context.Background(), te.Env, EditOptions{})
	if err != nil {
		t.Fatalf("Edit() returned an error: %v", err)
	}

	wantCacheDir := source.GitCacheDir(te.Base, url)
	wantPath := filepath.Join(wantCacheDir, "aliases.yaml")
	if report.Path != wantPath {
		t.Errorf("report.Path = %q, want %q", report.Path, wantPath)
	}

	if _, err := os.Stat(wantCacheDir); !os.IsNotExist(err) {
		t.Errorf("Edit() must not create a git checkout as a side effect; stat(%q) err = %v", wantCacheDir, err)
	}
}

func TestEditReturnsErrorWhenEditorNotSet(t *testing.T) {
	te := newTestEnv(t)
	seedSyncableDevice(t, te)

	if _, err := Edit(context.Background(), te.Env, EditOptions{}); err != ErrEditorNotSet {
		t.Errorf("Edit() error = %v, want ErrEditorNotSet", err)
	}
}

// TestEditTargetSelectsTheRightFile covers the --config branch, which the
// cli-commands spec requires and which had no test.
//
// Both targets resolve to files in the same directory with similar names, so a
// transposition would be easy to introduce and easy to miss: a user reaching
// for their aliases would silently be handed their device configuration
// instead, and would edit the wrong file believing it was the right one.
func TestEditTargetSelectsTheRightFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture requires a POSIX shell")
	}

	tests := []struct {
		name   string
		target EditTarget
		want   func(dir string) string
	}{
		{
			name:   "default target is aliases.yaml",
			target: "",
			want:   func(dir string) string { return config.AliasesFile(dir) },
		},
		{
			name:   "explicit aliases target",
			target: EditTargetAliases,
			want:   func(dir string) string { return config.AliasesFile(dir) },
		},
		{
			name:   "config target opens config.yaml",
			target: EditTargetConfig,
			want:   func(dir string) string { return config.ConfigFile(dir) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			te := newTestEnv(t)
			seedSyncableDevice(t, te)

			captureFile := filepath.Join(t.TempDir(), "captured-args")
			scriptPath := filepath.Join(t.TempDir(), "fakeeditor")
			script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + captureFile + "\n"
			if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
				t.Fatalf("writing fake editor script: %v", err)
			}

			te.setenv("EDITOR", "fakeeditor")
			te.Env.LookPath = func(file string) (string, error) {
				if file == "fakeeditor" {
					return scriptPath, nil
				}
				return "", errors.New("not found")
			}

			report, err := Edit(context.Background(), te.Env, EditOptions{Target: tt.target})
			if err != nil {
				t.Fatalf("Edit() returned an error: %v", err)
			}

			want := tt.want(te.Base)
			if report.Path != want {
				t.Errorf("report.Path = %q, want %q", report.Path, want)
			}

			captured, err := os.ReadFile(captureFile)
			if err != nil {
				t.Fatalf("reading captured args: %v", err)
			}
			if strings.TrimSpace(string(captured)) != want {
				t.Errorf("editor received %q, want %q", strings.TrimSpace(string(captured)), want)
			}
		})
	}
}
