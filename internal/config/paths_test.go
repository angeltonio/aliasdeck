package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeEnv builds an Env backed by an in-memory map instead of the real
// process environment, so path-resolution tests never depend on (or leak
// into) the machine actually running them.
func fakeEnv(vars map[string]string, home string) Env {
	return Env{
		Getenv: func(key string) string { return vars[key] },
		HomeDir: func() (string, error) {
			if home == "" {
				return "", fmt.Errorf("no home directory configured")
			}
			return home, nil
		},
	}
}

func TestOSEnvForwardsToRealProcessState(t *testing.T) {
	t.Setenv("ALIASDECK_HOME_TEST_PROBE", "probe-value")

	env := OSEnv()

	if got := env.Getenv("ALIASDECK_HOME_TEST_PROBE"); got != "probe-value" {
		t.Errorf("OSEnv().Getenv() = %q, want %q", got, "probe-value")
	}

	wantHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() returned an error: %v", err)
	}
	gotHome, err := env.HomeDir()
	if err != nil {
		t.Fatalf("OSEnv().HomeDir() returned an error: %v", err)
	}
	if gotHome != wantHome {
		t.Errorf("OSEnv().HomeDir() = %q, want %q (must be os.UserHomeDir, never os.UserConfigDir)", gotHome, wantHome)
	}
}

func TestBasePrecedence(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "user")

	tests := []struct {
		name string
		vars map[string]string
		home string
		want string
	}{
		{
			name: "ALIASDECK_HOME wins over everything",
			vars: map[string]string{
				"ALIASDECK_HOME":  filepath.Join(home, "custom-aliasdeck"),
				"XDG_CONFIG_HOME": filepath.Join(home, "xdg-config"),
			},
			home: home,
			want: filepath.Join(home, "custom-aliasdeck"),
		},
		{
			name: "XDG_CONFIG_HOME wins when ALIASDECK_HOME is unset",
			vars: map[string]string{
				"XDG_CONFIG_HOME": filepath.Join(home, "xdg-config"),
			},
			home: home,
			want: filepath.Join(home, "xdg-config", "aliasdeck"),
		},
		{
			name: "falls back to ~/.config/aliasdeck when nothing is set",
			vars: map[string]string{},
			home: home,
			want: filepath.Join(home, ".config", "aliasdeck"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Base(fakeEnv(tt.vars, tt.home))
			if err != nil {
				t.Fatalf("Base() returned an error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Base() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBaseExpandsTildeInAliasdeckHome(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "user")
	env := fakeEnv(map[string]string{"ALIASDECK_HOME": "~/custom-base"}, home)

	got, err := Base(env)
	if err != nil {
		t.Fatalf("Base() returned an error: %v", err)
	}
	want := filepath.Join(home, "custom-base")
	if got != want {
		t.Errorf("Base() = %q, want %q (tilde must expand against HomeDir)", got, want)
	}
}

// TestBaseDoesNotUseUserConfigDir guards the project's non-negotiable path
// constraint: os.UserConfigDir() returns ~/Library/Application Support on
// macOS, which contradicts the ~/.config/aliasdeck path PROJECT.md §3.4
// documents and promises.
//
// The forbidden-path half of the assertion only means anything where the two
// actually diverge. On Linux os.UserConfigDir() resolves to $XDG_CONFIG_HOME
// or ~/.config — the same place AliasDeck correctly resolves to — so demanding
// they differ there fails a correct implementation. That is not hypothetical:
// it is what this test did the first time it ran on Linux.
func TestBaseDoesNotUseUserConfigDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("ALIASDECK_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	env := Env{
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return tmp, nil },
	}

	got, err := Base(env)
	if err != nil {
		t.Fatalf("Base() returned an error: %v", err)
	}

	want := filepath.Join(tmp, ".config", "aliasdeck")
	if got != want {
		t.Fatalf("Base() = %q, want %q", got, want)
	}

	// The path must never land under macOS's Application Support, whichever
	// platform the test happens to run on.
	if strings.Contains(got, filepath.Join("Library", "Application Support")) {
		t.Fatalf("Base() = %q resolves under macOS's Application Support directory", got)
	}

	// Only assert divergence from os.UserConfigDir() where the two genuinely
	// differ. On Linux they coincide by design, and requiring them to differ
	// would reject the correct answer.
	if runtime.GOOS != "darwin" {
		return
	}
	if userConfigDir, err := os.UserConfigDir(); err == nil {
		forbidden := filepath.Join(userConfigDir, "aliasdeck")
		if got == forbidden {
			t.Fatalf("Base() = %q matches the os.UserConfigDir()-derived path; "+
				"os.UserConfigDir must never be used to resolve AliasDeck's base directory", got)
		}
	}
}

func TestBaseHomeDirError(t *testing.T) {
	env := fakeEnv(map[string]string{}, "")

	if _, err := Base(env); err == nil {
		t.Fatal("Base() must propagate a HomeDir() failure when no override is set")
	}
}

func TestPerFilePaths(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "home", "user", ".config", "aliasdeck")

	tests := []struct {
		name string
		fn   func(string) string
		want string
	}{
		{"ConfigFile", ConfigFile, filepath.Join(base, "config.yaml")},
		{"AliasesFile", AliasesFile, filepath.Join(base, "aliases.yaml")},
		{"StateFile", StateFile, filepath.Join(base, "state.json")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn(base); got != tt.want {
				t.Errorf("%s(%q) = %q, want %q", tt.name, base, got, tt.want)
			}
		})
	}
}

func TestExpandPath(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "user")
	env := fakeEnv(nil, home)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bare tilde", "~", home},
		{"tilde slash prefix", "~/dotfiles/aliases.yaml", filepath.Join(home, "dotfiles", "aliases.yaml")},
		{"tilde backslash prefix (Windows-shaped path)", `~\dotfiles\aliases.yaml`, filepath.Join(home, "dotfiles", "aliases.yaml")},
		{"embedded $HOME", "$HOME/dotfiles/aliases.yaml", filepath.Join(home, "dotfiles", "aliases.yaml")},
		{"no expansion needed", "/etc/aliasdeck/aliases.yaml", "/etc/aliasdeck/aliases.yaml"},
		{"empty path", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandPath(tt.in, env)
			if err != nil {
				t.Fatalf("ExpandPath(%q) returned an error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ExpandPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
