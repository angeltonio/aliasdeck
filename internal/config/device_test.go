package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const validConfigYAML = `
version: 1

device:
  name: macbook
  profiles: [development, homelab]

source:
  type: file
  path: ~/dotfiles/aliases.yaml

backend: native
`

func TestParseDeviceConfigValidFile(t *testing.T) {
	cfg, err := ParseDeviceConfig([]byte(validConfigYAML))
	if err != nil {
		t.Fatalf("ParseDeviceConfig() returned an error for a well-formed file: %v", err)
	}

	if cfg.Device.Name != "macbook" {
		t.Errorf("Device.Name = %q, want %q", cfg.Device.Name, "macbook")
	}
	if cfg.Device.ID != "macbook" {
		t.Errorf("Device.ID = %q, want it derived from Name (%q)", cfg.Device.ID, "macbook")
	}
	if want := []string{"development", "homelab"}; !equalStrings(cfg.Device.ProfileIDs, want) {
		t.Errorf("Device.ProfileIDs = %v, want %v", cfg.Device.ProfileIDs, want)
	}
	if cfg.Source.Type != SourceTypeFile {
		t.Errorf("Source.Type = %q, want %q", cfg.Source.Type, SourceTypeFile)
	}
	if cfg.Source.Path != "~/dotfiles/aliases.yaml" {
		t.Errorf("Source.Path = %q, want %q", cfg.Source.Path, "~/dotfiles/aliases.yaml")
	}
	if cfg.Backend != BackendNative {
		t.Errorf("Backend = %q, want %q", cfg.Backend, BackendNative)
	}
}

func TestParseDeviceConfigUnknownBackendRejected(t *testing.T) {
	yaml := "version: 1\ndevice:\n  name: macbook\nsource:\n  type: file\n  path: aliases.yaml\nbackend: invalid-value\n"

	_, err := ParseDeviceConfig([]byte(yaml))
	if err == nil {
		t.Fatal("ParseDeviceConfig() must reject an unknown backend value")
	}
}

func TestParseDeviceConfigGitSource(t *testing.T) {
	body := `
version: 1

device:
  name: macbook

source:
  type: git
  git:
    url: https://example.com/dotfiles.git
    ref: main
    path: config/aliases.yaml

backend: native
`

	cfg, err := ParseDeviceConfig([]byte(body))
	if err != nil {
		t.Fatalf("ParseDeviceConfig() returned an error for a well-formed git source: %v", err)
	}

	if cfg.Source.Type != SourceTypeGit {
		t.Errorf("Source.Type = %q, want %q", cfg.Source.Type, SourceTypeGit)
	}
	if cfg.Source.Git.URL != "https://example.com/dotfiles.git" {
		t.Errorf("Source.Git.URL = %q, want %q", cfg.Source.Git.URL, "https://example.com/dotfiles.git")
	}
	if cfg.Source.Git.Ref != "main" {
		t.Errorf("Source.Git.Ref = %q, want %q", cfg.Source.Git.Ref, "main")
	}
	if cfg.Source.Git.Path != "config/aliases.yaml" {
		t.Errorf("Source.Git.Path = %q, want %q", cfg.Source.Git.Path, "config/aliases.yaml")
	}
}

// TestParseDeviceConfigGitSourceRefAndPathOptional pins design decision 16:
// source.git.path is optional (omitted means aliases.yaml at the checkout
// root), mirroring FileSource's existing path-omitted default. source.git.ref
// is optional too (omitted means the remote's default branch).
func TestParseDeviceConfigGitSourceRefAndPathOptional(t *testing.T) {
	body := "version: 1\ndevice:\n  name: macbook\nsource:\n  type: git\n  git:\n    url: https://example.com/dotfiles.git\nbackend: native\n"

	cfg, err := ParseDeviceConfig([]byte(body))
	if err != nil {
		t.Fatalf("ParseDeviceConfig() returned an error when source.git.ref/path are omitted: %v", err)
	}
	if cfg.Source.Git.Ref != "" {
		t.Errorf("Source.Git.Ref = %q, want empty when omitted", cfg.Source.Git.Ref)
	}
	if cfg.Source.Git.Path != "" {
		t.Errorf("Source.Git.Path = %q, want empty when omitted", cfg.Source.Git.Path)
	}
}

// TestParseDeviceConfigServerSource pins the server source.type parse path,
// including source.allowInsecureHTTP (design decision 13's opt-out), needed
// by internal/app's resolveSource server arm (task 8.1).
func TestParseDeviceConfigServerSource(t *testing.T) {
	body := `
version: 1

device:
  name: macbook

source:
  type: server
  url: https://aliases.example.com
  allowInsecureHTTP: true

backend: native
`

	cfg, err := ParseDeviceConfig([]byte(body))
	if err != nil {
		t.Fatalf("ParseDeviceConfig() returned an error for a well-formed server source: %v", err)
	}

	if cfg.Source.Type != SourceTypeServer {
		t.Errorf("Source.Type = %q, want %q", cfg.Source.Type, SourceTypeServer)
	}
	if cfg.Source.URL != "https://aliases.example.com" {
		t.Errorf("Source.URL = %q, want %q", cfg.Source.URL, "https://aliases.example.com")
	}
	if !cfg.Source.AllowInsecureHTTP {
		t.Error("Source.AllowInsecureHTTP = false, want true")
	}
}

// TestParseDeviceConfigServerSourceAllowInsecureHTTPDefaultsFalse proves the
// opt-out is never silently on: an omitted field must parse as false, not as
// whatever zero-value ambiguity a looser type might allow.
func TestParseDeviceConfigServerSourceAllowInsecureHTTPDefaultsFalse(t *testing.T) {
	body := "version: 1\ndevice:\n  name: macbook\nsource:\n  type: server\n  url: https://aliases.example.com\nbackend: native\n"

	cfg, err := ParseDeviceConfig([]byte(body))
	if err != nil {
		t.Fatalf("ParseDeviceConfig() returned an error: %v", err)
	}
	if cfg.Source.AllowInsecureHTTP {
		t.Error("Source.AllowInsecureHTTP = true, want false when the field is omitted")
	}
}

func TestParseDeviceConfigUnknownGitFieldRejected(t *testing.T) {
	body := "version: 1\ndevice:\n  name: macbook\nsource:\n  type: git\n  git:\n    url: https://example.com/dotfiles.git\n    branch: main\nbackend: native\n"

	if _, err := ParseDeviceConfig([]byte(body)); err == nil {
		t.Fatal("ParseDeviceConfig() must reject an unknown field under source.git")
	}
}

func TestParseDeviceConfigUnknownFieldRejected(t *testing.T) {
	yaml := "version: 1\ndevice:\n  name: macbook\n  nickname: mac\nsource:\n  type: file\n  path: aliases.yaml\nbackend: native\n"

	_, err := ParseDeviceConfig([]byte(yaml))
	if err == nil {
		t.Fatal("ParseDeviceConfig() must reject an unknown field")
	}
}

func TestLoadGeneratesStableFallbackIdentityWhenNameOmitted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := "version: 1\ndevice: {}\nsource:\n  type: file\n  path: aliases.yaml\nbackend: native\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("failed to seed config.yaml fixture: %v", err)
	}

	first, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned an error: %v", err)
	}
	if first.Device.ID == "" {
		t.Fatal("Load() must generate a fallback device identity when device.name is omitted")
	}

	second, err := Load(path)
	if err != nil {
		t.Fatalf("second Load() returned an error: %v", err)
	}
	if second.Device.ID != first.Device.ID {
		t.Errorf("device identity is not stable across reloads: %q then %q", first.Device.ID, second.Device.ID)
	}
}

func TestWriteThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := DeviceFileConfig{
		Version: 1,
		Device: DeviceConfig{
			ID:         "macbook",
			Name:       "macbook",
			ProfileIDs: []string{"development"},
		},
		Source:  Source{Type: SourceTypeFile, Path: "aliases.yaml"},
		Backend: BackendNative,
	}

	if err := Write(path, cfg); err != nil {
		t.Fatalf("Write() returned an error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned an error: %v", err)
	}
	if loaded.Device.Name != cfg.Device.Name {
		t.Errorf("Device.Name = %q, want %q", loaded.Device.Name, cfg.Device.Name)
	}
	if loaded.Backend != cfg.Backend {
		t.Errorf("Backend = %q, want %q", loaded.Backend, cfg.Backend)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat() returned an error: %v", err)
	}
	// config.yaml is written at 0600 specifically to keep a file that may
	// embed a source URL (potentially credential-bearing, e.g. a git remote)
	// out of other local users' reach. Windows has no Unix permission bits:
	// Go reports 0666 for any writable file regardless of the mode passed to
	// Chmod, so that protection genuinely does not exist there via this
	// mechanism — this is not a test artifact to paper over, it is a real
	// gap, see the apply-progress report's "Issues Found" section. The
	// assertion below therefore only enforces the POSIX guarantee on
	// platforms that can provide it.
	if runtime.GOOS == "windows" {
		if perm := info.Mode().Perm(); perm&0o200 == 0 {
			t.Errorf("config.yaml mode = %o, want a writable file", perm)
		}
	} else if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config.yaml mode = %o, want %o", perm, 0o600)
	}
}

// TestWriteThenLoadRoundTripsServerSource pins that Write/Load round-trip
// source.type: server, source.url, and source.allowInsecureHTTP together —
// the exact shape `register` (task 8.5) writes back to config.yaml after a
// successful enrollment.
func TestWriteThenLoadRoundTripsServerSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := DeviceFileConfig{
		Version: 1,
		Device: DeviceConfig{
			ID:   "macbook",
			Name: "macbook",
		},
		Source: Source{
			Type:              SourceTypeServer,
			URL:               "https://aliases.example.com",
			AllowInsecureHTTP: true,
		},
		Backend: BackendNative,
	}

	if err := Write(path, cfg); err != nil {
		t.Fatalf("Write() returned an error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned an error: %v", err)
	}
	if loaded.Source.Type != SourceTypeServer {
		t.Errorf("Source.Type = %q, want %q", loaded.Source.Type, SourceTypeServer)
	}
	if loaded.Source.URL != cfg.Source.URL {
		t.Errorf("Source.URL = %q, want %q", loaded.Source.URL, cfg.Source.URL)
	}
	if !loaded.Source.AllowInsecureHTTP {
		t.Error("Source.AllowInsecureHTTP = false after round trip, want true")
	}
}

func TestSanitizeHostname(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain hostname", in: "macbook", want: "macbook"},
		{name: "macOS local suffix is dropped", in: "macbook.local", want: "macbook"},
		{name: "dns suffix is dropped", in: "nas.home.example.com", want: "nas"},
		{name: "uppercase is folded", in: "MacBook-Pro", want: "macbook-pro"},
		{name: "spaces and punctuation collapse", in: "Angel's MacBook Pro", want: "angel-s-macbook-pro"},
		{name: "digits survive", in: "node02", want: "node02"},
		{name: "underscores survive", in: "build_box", want: "build_box"},
		{name: "leading and trailing separators are trimmed", in: "--host--", want: "host"},
		{name: "unusable hostname yields empty so the caller falls back", in: "...", want: ""},
		{name: "empty input", in: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeHostname(tt.in); got != tt.want {
				t.Errorf("sanitizeHostname(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestGenerateDeviceNamePrefersHostname pins the choice that the fallback
// identity is a label a human can recognize. `aliasdeck status` prints this
// name, so "macbook" is useful where "device-a3f9c2b1" is noise.
func TestGenerateDeviceNamePrefersHostname(t *testing.T) {
	host, err := os.Hostname()
	if err != nil {
		t.Skip("hostname unavailable on this machine")
	}
	want := sanitizeHostname(host)
	if want == "" {
		t.Skip("this machine's hostname does not survive sanitization")
	}

	got, err := generateDeviceName()
	if err != nil {
		t.Fatalf("generateDeviceName: %v", err)
	}
	if got != want {
		t.Errorf("generateDeviceName() = %q, want the sanitized hostname %q", got, want)
	}
	if strings.HasPrefix(got, "device-") {
		t.Error("fell back to a random identity while a usable hostname was available")
	}
}

// TestLoadNeverWritesTheFileBack pins loading as a pure read.
//
// Load previously persisted a generated identity, which made every command
// that merely reads configuration — doctor, status, list — rewrite a
// hand-authored config.yaml, reformatting it and inserting empty keys. The
// fixture is written the way a person writes it, not the way Write emits it,
// so any round-trip through the serializer is visible as a byte difference.
func TestLoadNeverWritesTheFileBack(t *testing.T) {
	fixtures := []struct {
		name string
		body string
	}{
		{
			name: "no device block at all",
			body: "version: 1\nsource:\n  type: file\nbackend: native\n",
		},
		{
			name: "empty device block",
			body: "version: 1\ndevice: {}\nsource:\n  type: file\nbackend: native\n",
		},
		{
			name: "comments and spacing a serializer would not produce",
			body: "version: 1\n\n# my laptop\ndevice:\n  name: work-laptop\n\nsource:\n  type: file\nbackend: native\n",
		},
	}

	for _, tt := range fixtures {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatalf("seeding config.yaml: %v", err)
			}

			if _, err := Load(path); err != nil {
				t.Fatalf("Load: %v", err)
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("re-reading config.yaml: %v", err)
			}
			if string(got) != tt.body {
				t.Errorf("Load modified the file on disk\n before: %q\n  after: %q", tt.body, got)
			}
		})
	}
}
