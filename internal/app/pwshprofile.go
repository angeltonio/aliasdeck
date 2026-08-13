package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// pwshEdition identifies which PowerShell edition's $PROFILE
// resolvePowerShellProfile chose to bootstrap (design decision 8).
//
// A machine can have both editions installed; AliasDeck never bootstraps
// both (non-negotiable constraint 1), so exactly one edition is named here.
type pwshEdition string

const (
	// pwshEditionCore is PowerShell 7+, the "pwsh" binary.
	pwshEditionCore pwshEdition = "Core"
	// pwshEditionDesktop is Windows PowerShell 5.1, the "powershell" binary.
	// It exists only on Windows.
	pwshEditionDesktop pwshEdition = "Desktop"
)

// pwshProfile is the resolved PowerShell $PROFILE target, plus enough
// context for `status`/`doctor` to make the choice inspectable instead of
// a silent guess (non-negotiable constraint 2).
type pwshProfile struct {
	// Path is the $PROFILE this device's chosen edition will load.
	Path string
	// Edition is the edition Path belongs to.
	Edition pwshEdition
	// Provenance explains how Path and Edition were chosen.
	Provenance string
	// OtherPath is the *other* edition's $PROFILE path, computed
	// unconditionally so `doctor` can warn about it without re-deriving it.
	// Empty when the other edition cannot exist on this platform (Desktop
	// off Windows).
	OtherPath string
	// OtherExists reports whether OtherPath exists on disk right now.
	OtherExists bool
}

// resolvePowerShellProfile resolves the $PROFILE AliasDeck should
// bootstrap for PowerShell, on every platform (design decisions 8, 9, 10).
//
// Precedence: $ALIASDECK_PWSH_PROFILE (test seam) -> LookPath("pwsh") =>
// Core -> LookPath("powershell") => Desktop -> Core default. --rc-file is
// handled entirely by the caller (resolveRCPath) before this is ever
// reached, since it is a full override, not part of PowerShell detection.
//
// LookPath never spawns a process (non-negotiable constraint 3): it is
// Env.LookPath, which is exec.LookPath in production and a fake in tests.
func resolvePowerShellProfile(env Env, platform domain.Platform) (pwshProfile, error) {
	home, err := env.HomeDir()
	if err != nil {
		return pwshProfile{}, fmt.Errorf("resolving home directory: %w", err)
	}

	corePath, desktopPath, docsProvenance := pwshProfilePaths(home, platform, env.Getenv)

	if v := env.Getenv("ALIASDECK_PWSH_PROFILE"); v != "" {
		return pwshProfile{
			Path:        v,
			Edition:     pwshEditionCore,
			Provenance:  "$ALIASDECK_PWSH_PROFILE (test seam)",
			OtherPath:   desktopPath,
			OtherExists: pathExists(desktopPath),
		}, nil
	}

	if _, err := env.LookPath("pwsh"); err == nil {
		return pwshProfile{
			Path:        corePath,
			Edition:     pwshEditionCore,
			Provenance:  withDocsProvenance(`LookPath("pwsh") found PowerShell 7 (Core)`, docsProvenance),
			OtherPath:   desktopPath,
			OtherExists: pathExists(desktopPath),
		}, nil
	}

	if _, err := env.LookPath("powershell"); err == nil {
		return pwshProfile{
			Path:        desktopPath,
			Edition:     pwshEditionDesktop,
			Provenance:  withDocsProvenance(`LookPath("powershell") found Windows PowerShell 5.1 (Desktop)`, docsProvenance),
			OtherPath:   corePath,
			OtherExists: pathExists(corePath),
		}, nil
	}

	return pwshProfile{
		Path:        corePath,
		Edition:     pwshEditionCore,
		Provenance:  withDocsProvenance(`neither "pwsh" nor "powershell" found on PATH; defaulting to Core`, docsProvenance),
		OtherPath:   desktopPath,
		OtherExists: pathExists(desktopPath),
	}, nil
}

// pwshProfilePaths computes both editions' $PROFILE paths for platform.
//
// On Windows, both live under the resolved Documents folder (decision 9).
// Off Windows, only Core exists, at the Unix XDG-style config path
// (decision 10); desktopPath is empty since Desktop cannot exist there.
func pwshProfilePaths(home string, platform domain.Platform, getenv func(string) string) (corePath, desktopPath, docsProvenance string) {
	if platform != domain.PlatformWindows {
		return filepath.Join(home, ".config", "powershell", "Microsoft.PowerShell_profile.ps1"), "", ""
	}

	docs, provenance := windowsDocumentsDir(home, getenv)
	core := filepath.Join(docs, "PowerShell", "Microsoft.PowerShell_profile.ps1")
	desktop := filepath.Join(docs, "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1")
	return core, desktop, provenance
}

// windowsDocumentsDir resolves the Windows "Documents" folder AliasDeck
// bootstraps PowerShell's $PROFILE under (design decision 9). It is a
// pure function of home and getenv, so it is unit-testable on any host OS
// independent of the real runtime.GOOS.
//
// Base is home's "Documents" subdirectory. OneDrive Known Folder Move is
// the default on many managed Windows devices, silently redirecting
// Documents elsewhere; when the base is absent and $OneDrive or
// $OneDriveCommercial names an existing Documents folder, that is used
// instead, and the choice is reported so it is never a silent guess.
func windowsDocumentsDir(home string, getenv func(string) string) (path, provenance string) {
	base := filepath.Join(home, "Documents")
	if isDir(base) {
		return base, "Documents under $HOME"
	}

	for _, envVar := range []string{"OneDrive", "OneDriveCommercial"} {
		v := getenv(envVar)
		if v == "" {
			continue
		}
		candidate := filepath.Join(v, "Documents")
		if isDir(candidate) {
			return candidate, fmt.Sprintf("$HOME\\Documents absent; Documents redirected via $%s (OneDrive Known Folder Move)", envVar)
		}
	}

	// Nothing exists yet (e.g. a fresh profile): fall back to the
	// conventional, uncontested default so init has somewhere to create
	// it, the same posture as resolveRCPath's bash "neither candidate
	// exists" branch.
	return base, "Documents under $HOME (default; not yet created)"
}

// pathExists reports whether path exists on disk. It never errors: any
// os.Stat failure (including "not found") reports false, which is exactly
// what OtherExists means. An empty path (the other edition cannot exist
// on this platform) always reports false.
func pathExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// isDir reports whether path exists and is a directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// withDocsProvenance appends docsProvenance to base when non-empty, so the
// combined string explains both edition selection and the Documents
// location in one reportable line (non-negotiable constraint 2).
func withDocsProvenance(base, docsProvenance string) string {
	if docsProvenance == "" {
		return base
	}
	return base + "; " + docsProvenance
}
