package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// lookPathFake returns an Env.LookPath that reports the given names as
// found (returning a fake absolute path) and everything else as not found,
// mirroring exec.LookPath's error shape without ever spawning a process
// (non-negotiable constraint 3).
func lookPathFake(found ...string) func(string) (string, error) {
	set := make(map[string]bool, len(found))
	for _, f := range found {
		set[f] = true
	}
	return func(file string) (string, error) {
		if set[file] {
			return filepath.Join("/fake/bin", file), nil
		}
		return "", &os.PathError{Op: "lookpath", Path: file, Err: os.ErrNotExist}
	}
}

// TestResolvePowerShellProfile covers design decision 8's precedence table
// on Windows: $ALIASDECK_PWSH_PROFILE -> LookPath("pwsh") => Core ->
// LookPath("powershell") => Desktop -> Core default. resolveRCPath's own
// --rc-file short-circuit is covered separately in TestResolveRCPath,
// since it never reaches this function (design decision 8).
func TestResolvePowerShellProfile(t *testing.T) {
	t.Run("both editions present on PATH prefers Core, never both", func(t *testing.T) {
		te := newTestEnv(t)
		te.Env.LookPath = lookPathFake("pwsh", "powershell")
		// Seed the Desktop profile so OtherExists is provably true, not a
		// default zero value.
		desktop := filepath.Join(te.Home, "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1")
		mustWriteFile(t, desktop, "")

		got, err := resolvePowerShellProfile(te.Env, domain.PlatformWindows)
		if err != nil {
			t.Fatalf("resolvePowerShellProfile() returned an error: %v", err)
		}
		wantPath := filepath.Join(te.Home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
		if got.Path != wantPath {
			t.Errorf("Path = %q, want %q", got.Path, wantPath)
		}
		if got.Edition != pwshEditionCore {
			t.Errorf("Edition = %q, want %q", got.Edition, pwshEditionCore)
		}
		if got.OtherPath != desktop {
			t.Errorf("OtherPath = %q, want %q", got.OtherPath, desktop)
		}
		if !got.OtherExists {
			t.Error("OtherExists = false, want true (the Desktop profile was seeded)")
		}
		if got.Provenance == "" {
			t.Error("Provenance is empty; a heuristic that cannot be inspected is worse than no heuristic")
		}
	})

	t.Run("only Desktop on PATH", func(t *testing.T) {
		te := newTestEnv(t)
		te.Env.LookPath = lookPathFake("powershell")

		got, err := resolvePowerShellProfile(te.Env, domain.PlatformWindows)
		if err != nil {
			t.Fatalf("resolvePowerShellProfile() returned an error: %v", err)
		}
		want := filepath.Join(te.Home, "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1")
		if got.Path != want {
			t.Errorf("Path = %q, want %q", got.Path, want)
		}
		if got.Edition != pwshEditionDesktop {
			t.Errorf("Edition = %q, want %q", got.Edition, pwshEditionDesktop)
		}
		otherWant := filepath.Join(te.Home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
		if got.OtherPath != otherWant {
			t.Errorf("OtherPath = %q, want %q", got.OtherPath, otherWant)
		}
		if got.OtherExists {
			t.Error("OtherExists = true, want false (Core profile was never created)")
		}
	})

	t.Run("only Core on PATH", func(t *testing.T) {
		te := newTestEnv(t)
		te.Env.LookPath = lookPathFake("pwsh")

		got, err := resolvePowerShellProfile(te.Env, domain.PlatformWindows)
		if err != nil {
			t.Fatalf("resolvePowerShellProfile() returned an error: %v", err)
		}
		want := filepath.Join(te.Home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
		if got.Path != want {
			t.Errorf("Path = %q, want %q", got.Path, want)
		}
		if got.Edition != pwshEditionCore {
			t.Errorf("Edition = %q, want %q", got.Edition, pwshEditionCore)
		}
	})

	t.Run("neither on PATH defaults to Core", func(t *testing.T) {
		te := newTestEnv(t)
		// newTestEnv's default LookPath already reports everything as not
		// found; asserting that here documents the precondition.

		got, err := resolvePowerShellProfile(te.Env, domain.PlatformWindows)
		if err != nil {
			t.Fatalf("resolvePowerShellProfile() returned an error: %v", err)
		}
		want := filepath.Join(te.Home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
		if got.Path != want {
			t.Errorf("Path = %q, want %q", got.Path, want)
		}
		if got.Edition != pwshEditionCore {
			t.Errorf("Edition = %q, want %q", got.Edition, pwshEditionCore)
		}
	})

	t.Run("ALIASDECK_PWSH_PROFILE overrides detection entirely", func(t *testing.T) {
		te := newTestEnv(t)
		te.Env.LookPath = lookPathFake("powershell") // would otherwise pick Desktop
		custom := filepath.Join(te.Home, "custom-profile.ps1")
		te.setenv("ALIASDECK_PWSH_PROFILE", custom)

		got, err := resolvePowerShellProfile(te.Env, domain.PlatformWindows)
		if err != nil {
			t.Fatalf("resolvePowerShellProfile() returned an error: %v", err)
		}
		if got.Path != custom {
			t.Errorf("Path = %q, want %q", got.Path, custom)
		}
		if got.Provenance == "" {
			t.Error("Provenance is empty for the $ALIASDECK_PWSH_PROFILE seam")
		}
	})

	t.Run("OneDrive redirection present: $HOME\\Documents absent, $OneDrive names an existing Documents", func(t *testing.T) {
		te := newTestEnv(t)
		te.Env.LookPath = lookPathFake("pwsh")
		oneDriveRoot := filepath.Join(filepath.Dir(te.Home), "onedrive-root")
		oneDriveDocs := filepath.Join(oneDriveRoot, "Documents")
		if err := os.MkdirAll(oneDriveDocs, 0o755); err != nil {
			t.Fatalf("seeding OneDrive Documents: %v", err)
		}
		te.setenv("OneDrive", oneDriveRoot)

		got, err := resolvePowerShellProfile(te.Env, domain.PlatformWindows)
		if err != nil {
			t.Fatalf("resolvePowerShellProfile() returned an error: %v", err)
		}
		want := filepath.Join(oneDriveDocs, "PowerShell", "Microsoft.PowerShell_profile.ps1")
		if got.Path != want {
			t.Errorf("Path = %q, want %q (OneDrive-redirected Documents)", got.Path, want)
		}
		if got.Provenance == "" {
			t.Error("Provenance is empty; the OneDrive redirection must be reported, not silent")
		}
	})

	t.Run("OneDrive redirection absent: $OneDrive set but names no existing Documents, falls back to $HOME\\Documents", func(t *testing.T) {
		te := newTestEnv(t)
		te.Env.LookPath = lookPathFake("pwsh")
		te.setenv("OneDrive", filepath.Join(te.Home, "nonexistent-onedrive"))

		got, err := resolvePowerShellProfile(te.Env, domain.PlatformWindows)
		if err != nil {
			t.Fatalf("resolvePowerShellProfile() returned an error: %v", err)
		}
		want := filepath.Join(te.Home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
		if got.Path != want {
			t.Errorf("Path = %q, want %q (fall back to $HOME\\Documents)", got.Path, want)
		}
	})

	t.Run("non-Windows uses the Core XDG-style config path, never Documents", func(t *testing.T) {
		te := newTestEnv(t)
		// LookPath left at its default (nothing found); the Core default
		// still applies on macOS/Linux, per design decision 10.

		for _, platform := range []domain.Platform{domain.PlatformMacOS, domain.PlatformLinux} {
			got, err := resolvePowerShellProfile(te.Env, platform)
			if err != nil {
				t.Fatalf("resolvePowerShellProfile(%s) returned an error: %v", platform, err)
			}
			want := filepath.Join(te.Home, ".config", "powershell", "Microsoft.PowerShell_profile.ps1")
			if got.Path != want {
				t.Errorf("Path(%s) = %q, want %q", platform, got.Path, want)
			}
			if got.Edition != pwshEditionCore {
				t.Errorf("Edition(%s) = %q, want %q", platform, got.Edition, pwshEditionCore)
			}
			if got.OtherExists {
				t.Errorf("OtherExists(%s) = true, want false: Desktop does not exist off Windows", platform)
			}
		}
	})
}

// TestWindowsDocumentsDir isolates design decision 9 (Documents
// redirection) from LookPath/edition precedence, and is a pure function of
// home and getenv, so it is directly testable on any host OS regardless of
// the real runtime.GOOS.
func TestWindowsDocumentsDir(t *testing.T) {
	t.Run("default $HOME\\Documents exists", func(t *testing.T) {
		home := t.TempDir()
		docs := filepath.Join(home, "Documents")
		if err := os.MkdirAll(docs, 0o755); err != nil {
			t.Fatalf("seeding Documents: %v", err)
		}

		got, provenance := windowsDocumentsDir(home, func(string) string { return "" })
		if got != docs {
			t.Errorf("windowsDocumentsDir() = %q, want %q", got, docs)
		}
		if provenance == "" {
			t.Error("provenance is empty")
		}
	})

	t.Run("absent, no OneDrive vars set, falls back to the default location to create", func(t *testing.T) {
		home := t.TempDir()

		got, provenance := windowsDocumentsDir(home, func(string) string { return "" })
		want := filepath.Join(home, "Documents")
		if got != want {
			t.Errorf("windowsDocumentsDir() = %q, want %q", got, want)
		}
		if provenance == "" {
			t.Error("provenance is empty")
		}
	})

	t.Run("absent, $OneDriveCommercial names an existing Documents", func(t *testing.T) {
		home := t.TempDir()
		odRoot := t.TempDir()
		odDocs := filepath.Join(odRoot, "Documents")
		if err := os.MkdirAll(odDocs, 0o755); err != nil {
			t.Fatalf("seeding OneDriveCommercial Documents: %v", err)
		}

		got, provenance := windowsDocumentsDir(home, func(key string) string {
			if key == "OneDriveCommercial" {
				return odRoot
			}
			return ""
		})
		if got != odDocs {
			t.Errorf("windowsDocumentsDir() = %q, want %q", got, odDocs)
		}
		if provenance == "" {
			t.Error("provenance is empty")
		}
	})
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
