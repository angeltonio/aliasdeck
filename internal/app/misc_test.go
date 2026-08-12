package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
)

func TestPromptYesNo(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"lowercase y", "y\n", true},
		{"yes", "yes\n", true},
		{"uppercase Y", "Y\n", true},
		{"n", "n\n", false},
		{"empty defaults to no", "\n", false},
		{"EOF defaults to no", "", false},
		{"garbage defaults to no", "sure\n", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			te := newTestEnv(t)
			te.setStdin(tt.input)

			got, err := promptYesNo(te.Env, "Proceed?")
			if err != nil {
				t.Fatalf("promptYesNo() returned an error: %v", err)
			}
			if got != tt.want {
				t.Errorf("promptYesNo(%q) = %v, want %v", tt.input, got, tt.want)
			}
			if te.stdout.Len() == 0 {
				t.Error("promptYesNo() did not print the question")
			}
		})
	}
}

func TestResolveBackend(t *testing.T) {
	base := "/config"

	t.Run("native", func(t *testing.T) {
		b, err := resolveBackend(config.DeviceFileConfig{Backend: config.BackendNative}, base)
		if err != nil {
			t.Fatalf("resolveBackend() returned an error: %v", err)
		}
		if b.Name() != "native" {
			t.Errorf("Name() = %q, want %q", b.Name(), "native")
		}
	})

	t.Run("empty defaults to native", func(t *testing.T) {
		b, err := resolveBackend(config.DeviceFileConfig{}, base)
		if err != nil {
			t.Fatalf("resolveBackend() returned an error: %v", err)
		}
		if b.Name() != "native" {
			t.Errorf("Name() = %q, want %q", b.Name(), "native")
		}
	})

	t.Run("chezmoi is valid to select", func(t *testing.T) {
		b, err := resolveBackend(config.DeviceFileConfig{Backend: config.BackendChezmoi}, base)
		if err != nil {
			t.Fatalf("resolveBackend() returned an error: %v", err)
		}
		if b.Name() != "chezmoi" {
			t.Errorf("Name() = %q, want %q", b.Name(), "chezmoi")
		}
	})

	t.Run("unsupported backend is an error", func(t *testing.T) {
		if _, err := resolveBackend(config.DeviceFileConfig{Backend: "bogus"}, base); err == nil {
			t.Error("resolveBackend() must fail for an unsupported backend")
		}
	})
}

func TestResolveRCPath(t *testing.T) {
	t.Run("override wins", func(t *testing.T) {
		te := newTestEnv(t)
		got, err := resolveRCPath(te.Env, domain.ShellZsh, domain.PlatformMacOS, "~/custom.rc")
		if err != nil {
			t.Fatalf("resolveRCPath() returned an error: %v", err)
		}
		want := filepath.Join(te.Home, "custom.rc")
		if got != want {
			t.Errorf("resolveRCPath() = %q, want %q", got, want)
		}
	})

	t.Run("zsh honors ZDOTDIR", func(t *testing.T) {
		te := newTestEnv(t)
		zdotdir := filepath.Join(te.Home, "zdotdir")
		te.setenv("ZDOTDIR", zdotdir)

		got, err := resolveRCPath(te.Env, domain.ShellZsh, domain.PlatformMacOS, "")
		if err != nil {
			t.Fatalf("resolveRCPath() returned an error: %v", err)
		}
		want := filepath.Join(zdotdir, ".zshrc")
		if got != want {
			t.Errorf("resolveRCPath() = %q, want %q", got, want)
		}
	})

	t.Run("zsh falls back to home", func(t *testing.T) {
		te := newTestEnv(t)
		got, err := resolveRCPath(te.Env, domain.ShellZsh, domain.PlatformMacOS, "")
		if err != nil {
			t.Fatalf("resolveRCPath() returned an error: %v", err)
		}
		want := filepath.Join(te.Home, ".zshrc")
		if got != want {
			t.Errorf("resolveRCPath() = %q, want %q", got, want)
		}
	})

	t.Run("bash macOS prefers an existing .bash_profile", func(t *testing.T) {
		te := newTestEnv(t)
		bashrc := filepath.Join(te.Home, ".bashrc")
		if err := os.WriteFile(bashrc, []byte(""), 0o644); err != nil {
			t.Fatalf("seeding .bashrc: %v", err)
		}
		profile := filepath.Join(te.Home, ".bash_profile")
		if err := os.WriteFile(profile, []byte(""), 0o644); err != nil {
			t.Fatalf("seeding .bash_profile: %v", err)
		}

		got, err := resolveRCPath(te.Env, domain.ShellBash, domain.PlatformMacOS, "")
		if err != nil {
			t.Fatalf("resolveRCPath() returned an error: %v", err)
		}
		if got != profile {
			t.Errorf("resolveRCPath() = %q, want %q (macOS prefers .bash_profile)", got, profile)
		}
	})

	t.Run("bash Linux prefers an existing .bashrc", func(t *testing.T) {
		te := newTestEnv(t)
		bashrc := filepath.Join(te.Home, ".bashrc")
		if err := os.WriteFile(bashrc, []byte(""), 0o644); err != nil {
			t.Fatalf("seeding .bashrc: %v", err)
		}

		got, err := resolveRCPath(te.Env, domain.ShellBash, domain.PlatformLinux, "")
		if err != nil {
			t.Fatalf("resolveRCPath() returned an error: %v", err)
		}
		if got != bashrc {
			t.Errorf("resolveRCPath() = %q, want %q (Linux prefers .bashrc)", got, bashrc)
		}
	})

	t.Run("bash with neither candidate returns the platform default to create", func(t *testing.T) {
		te := newTestEnv(t)
		got, err := resolveRCPath(te.Env, domain.ShellBash, domain.PlatformLinux, "")
		if err != nil {
			t.Fatalf("resolveRCPath() returned an error: %v", err)
		}
		want := filepath.Join(te.Home, ".bashrc")
		if got != want {
			t.Errorf("resolveRCPath() = %q, want %q", got, want)
		}
	})

	t.Run("unsupported shell is an error", func(t *testing.T) {
		te := newTestEnv(t)
		if _, err := resolveRCPath(te.Env, domain.ShellPowerShell, domain.PlatformMacOS, ""); err == nil {
			t.Error("resolveRCPath() must fail for a shell with no rc-file convention")
		}
	})
}

func TestSkipReasonCoversEveryTargetingDimension(t *testing.T) {
	dev := domain.Device{ID: "this-device", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh, ProfileIDs: []string{"development"}}

	tests := []struct {
		name  string
		alias domain.Alias
		want  string
	}{
		{"active", domain.Alias{Enabled: true, Name: "a"}, ""},
		{"disabled", domain.Alias{Enabled: false, Name: "b"}, "disabled"},
		{"wrong platform", domain.Alias{Enabled: true, Platforms: []domain.Platform{domain.PlatformLinux}, Name: "c"}, `not targeted at platform "macos"`},
		{"wrong shell", domain.Alias{Enabled: true, Shells: []domain.Shell{domain.ShellBash}, Name: "d"}, `not targeted at shell "zsh"`},
		{"no matching profile", domain.Alias{Enabled: true, ProfileIDs: []string{"homelab"}, Name: "e"}, "no matching profile"},
		{"not this device", domain.Alias{Enabled: true, DeviceIDs: []string{"other-device"}, Name: "f"}, "not targeted at this device"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := skipReason(tt.alias, dev)
			if got != tt.want {
				t.Errorf("skipReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigErrorUnwraps(t *testing.T) {
	inner := errors.New("boom")
	ce := ConfigError{Err: inner}

	if ce.Error() != "boom" {
		t.Errorf("Error() = %q, want %q", ce.Error(), "boom")
	}
	if !errors.Is(ce, inner) {
		t.Error("errors.Is(ce, inner) = false, want true")
	}
}

func TestOSEnvIsWiredToTheRealProcess(t *testing.T) {
	env := OSEnv()

	if env.Stdin == nil || env.Stdout == nil || env.Stderr == nil {
		t.Fatal("OSEnv() left an I/O stream nil")
	}
	if env.Getenv == nil || env.HomeDir == nil || env.Now == nil || env.LookPath == nil {
		t.Fatal("OSEnv() left a function field nil")
	}

	t.Setenv("ALIASDECK_OSENV_PROBE", "probe-value")
	if got := env.Getenv("ALIASDECK_OSENV_PROBE"); got != "probe-value" {
		t.Errorf("Getenv() = %q, want %q", got, "probe-value")
	}
	if env.Now().IsZero() {
		t.Error("Now() returned the zero time")
	}
}
