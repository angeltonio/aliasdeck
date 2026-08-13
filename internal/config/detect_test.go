package config

import (
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

func envWith(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func TestDetectPlatformPrecedence(t *testing.T) {
	tests := []struct {
		name           string
		configOverride string
		vars           map[string]string
		goos           string
		wantPlatform   domain.Platform
		wantProvenance string
		wantErr        bool
	}{
		{
			name:           "config.yaml override wins over everything",
			configOverride: "linux",
			vars:           map[string]string{"ALIASDECK_PLATFORM": "macos"},
			goos:           "darwin",
			wantPlatform:   domain.PlatformLinux,
			wantProvenance: "config.yaml device.platform",
		},
		{
			name:           "ALIASDECK_PLATFORM test seam wins when no config override",
			vars:           map[string]string{"ALIASDECK_PLATFORM": "linux"},
			goos:           "darwin",
			wantPlatform:   domain.PlatformLinux,
			wantProvenance: "$ALIASDECK_PLATFORM",
		},
		{
			name:           "darwin maps to macos via runtime.GOOS",
			goos:           "darwin",
			wantPlatform:   domain.PlatformMacOS,
			wantProvenance: "runtime.GOOS",
		},
		{
			name:           "linux maps to linux via runtime.GOOS",
			goos:           "linux",
			wantPlatform:   domain.PlatformLinux,
			wantProvenance: "runtime.GOOS",
		},
		{
			name:           "windows maps to windows via runtime.GOOS",
			goos:           "windows",
			wantPlatform:   domain.PlatformWindows,
			wantProvenance: "runtime.GOOS",
		},
		{
			name:           "ALIASDECK_PLATFORM=windows is accepted as a test seam",
			vars:           map[string]string{"ALIASDECK_PLATFORM": "windows"},
			goos:           "darwin",
			wantPlatform:   domain.PlatformWindows,
			wantProvenance: "$ALIASDECK_PLATFORM",
		},
		{
			name:    "unrecognized GOOS is an error",
			goos:    "plan9",
			wantErr: true,
		},
		{
			name:           "invalid config override is an error",
			configOverride: "atari",
			goos:           "darwin",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectPlatform(tt.configOverride, envWith(tt.vars), tt.goos)
			if tt.wantErr {
				if err == nil {
					t.Fatal("DetectPlatform() must return an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("DetectPlatform() returned an error: %v", err)
			}
			if got.Platform != tt.wantPlatform {
				t.Errorf("Platform = %q, want %q", got.Platform, tt.wantPlatform)
			}
			if got.Provenance != tt.wantProvenance {
				t.Errorf("Provenance = %q, want %q", got.Provenance, tt.wantProvenance)
			}
		})
	}
}

func TestDetectShellPrecedence(t *testing.T) {
	tests := []struct {
		name           string
		flagOverride   string
		configOverride string
		vars           map[string]string
		platform       domain.Platform
		wantShell      domain.Shell
		wantProvenance string
		wantErr        bool
	}{
		{
			name:           "--shell flag wins over everything",
			flagOverride:   "bash",
			configOverride: "zsh",
			vars:           map[string]string{"ALIASDECK_SHELL": "zsh", "SHELL": "/bin/zsh"},
			platform:       domain.PlatformMacOS,
			wantShell:      domain.ShellBash,
			wantProvenance: "--shell flag",
		},
		{
			name:           "config.yaml override wins when no flag",
			configOverride: "bash",
			vars:           map[string]string{"ALIASDECK_SHELL": "zsh"},
			platform:       domain.PlatformMacOS,
			wantShell:      domain.ShellBash,
			wantProvenance: "config.yaml device.shell",
		},
		{
			name:           "ALIASDECK_SHELL test seam wins over $SHELL",
			vars:           map[string]string{"ALIASDECK_SHELL": "bash", "SHELL": "/bin/zsh"},
			platform:       domain.PlatformMacOS,
			wantShell:      domain.ShellBash,
			wantProvenance: "$ALIASDECK_SHELL",
		},
		{
			name:           "$SHELL basename resolves the shell",
			vars:           map[string]string{"SHELL": "/usr/bin/zsh"},
			platform:       domain.PlatformMacOS,
			wantShell:      domain.ShellZsh,
			wantProvenance: "$SHELL",
		},
		{
			name:           "$SHELL strips the login-shell leading dash",
			vars:           map[string]string{"SHELL": "-zsh"},
			platform:       domain.PlatformMacOS,
			wantShell:      domain.ShellZsh,
			wantProvenance: "$SHELL",
		},
		{
			name:           "falls back to the platform default when nothing is set",
			platform:       domain.PlatformLinux,
			wantShell:      domain.DefaultShellFor(domain.PlatformLinux),
			wantProvenance: "default for platform linux",
		},
		{
			name:     "unsupported $SHELL is reported, not silently replaced",
			vars:     map[string]string{"SHELL": "/usr/bin/fish"},
			platform: domain.PlatformMacOS,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectShell(tt.flagOverride, tt.configOverride, envWith(tt.vars), tt.platform)
			if tt.wantErr {
				if err == nil {
					t.Fatal("DetectShell() must return an error for an unsupported shell")
				}
				return
			}
			if err != nil {
				t.Fatalf("DetectShell() returned an error: %v", err)
			}
			if got.Shell != tt.wantShell {
				t.Errorf("Shell = %q, want %q", got.Shell, tt.wantShell)
			}
			if got.Provenance != tt.wantProvenance {
				t.Errorf("Provenance = %q, want %q", got.Provenance, tt.wantProvenance)
			}
		})
	}
}
