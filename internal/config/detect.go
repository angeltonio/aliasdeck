package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// PlatformDetection is the resolved platform plus a human-readable
// explanation of where it came from, so `status`/`doctor` can show a wrong
// guess instead of hiding it behind a silent default.
type PlatformDetection struct {
	Platform   domain.Platform
	Provenance string
}

// ShellDetection is the resolved shell plus its provenance, mirroring
// PlatformDetection.
type ShellDetection struct {
	Shell      domain.Shell
	Provenance string
}

// DetectPlatform resolves the active platform.
//
// Precedence: configOverride (config.yaml device.platform) →
// $ALIASDECK_PLATFORM (a test seam, since GOOS cannot otherwise be faked) →
// goos (runtime.GOOS), mapped darwin→macos, linux→linux. Anything else is an
// error: AliasDeck has no renderer story for it yet.
func DetectPlatform(configOverride string, getenv func(string) string, goos string) (PlatformDetection, error) {
	if configOverride != "" {
		p, err := domain.ParsePlatform(configOverride)
		if err != nil {
			return PlatformDetection{}, fmt.Errorf("config.yaml device.platform: %w", err)
		}
		return PlatformDetection{Platform: p, Provenance: "config.yaml device.platform"}, nil
	}

	if v := getenv("ALIASDECK_PLATFORM"); v != "" {
		p, err := domain.ParsePlatform(v)
		if err != nil {
			return PlatformDetection{}, fmt.Errorf("$ALIASDECK_PLATFORM: %w", err)
		}
		return PlatformDetection{Platform: p, Provenance: "$ALIASDECK_PLATFORM"}, nil
	}

	switch goos {
	case "darwin":
		return PlatformDetection{Platform: domain.PlatformMacOS, Provenance: "runtime.GOOS"}, nil
	case "linux":
		return PlatformDetection{Platform: domain.PlatformLinux, Provenance: "runtime.GOOS"}, nil
	case "windows":
		return PlatformDetection{Platform: domain.PlatformWindows, Provenance: "runtime.GOOS"}, nil
	default:
		return PlatformDetection{}, fmt.Errorf("unsupported operating system %q", goos)
	}
}

// DetectShell resolves the active shell.
//
// Precedence: flagOverride (--shell) → configOverride (config.yaml
// device.shell) → $ALIASDECK_SHELL (test seam) → $SHELL basename (a
// login-shell leading "-" stripped) → domain.DefaultShellFor(platform).
//
// A value nothing in domain.AllShells recognizes is an error: per the spec,
// an unsupported shell must be reported via doctor/status, never silently
// swapped for a guess.
func DetectShell(flagOverride, configOverride string, getenv func(string) string, platform domain.Platform) (ShellDetection, error) {
	if flagOverride != "" {
		return parseShellDetection(flagOverride, "--shell flag")
	}
	if configOverride != "" {
		return parseShellDetection(configOverride, "config.yaml device.shell")
	}
	if v := getenv("ALIASDECK_SHELL"); v != "" {
		return parseShellDetection(v, "$ALIASDECK_SHELL")
	}
	if v := getenv("SHELL"); v != "" {
		return parseShellDetection(shellBasename(v), "$SHELL")
	}

	def := domain.DefaultShellFor(platform)
	return ShellDetection{
		Shell:      def,
		Provenance: "default for platform " + platform.String(),
	}, nil
}

func parseShellDetection(value, provenance string) (ShellDetection, error) {
	sh, err := domain.ParseShell(value)
	if err != nil {
		return ShellDetection{}, fmt.Errorf("%s: %w", provenance, err)
	}
	return ShellDetection{Shell: sh, Provenance: provenance}, nil
}

// shellBasename extracts the shell name from a $SHELL path, stripping a
// login-shell leading "-" (e.g. "-zsh", as some terminal emulators set it).
func shellBasename(shellPath string) string {
	return strings.TrimPrefix(filepath.Base(shellPath), "-")
}
