package config

import (
	"os"
	"path/filepath"
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
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config.yaml mode = %o, want %o", perm, 0o600)
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
