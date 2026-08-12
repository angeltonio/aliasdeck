package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// maxConfigFileSize caps config.yaml at 1 MiB, matching the aliases.yaml
// limit: every on-disk input is treated as hostile (PROJECT.md §12.1).
const maxConfigFileSize = 1 << 20

// Backend enumerates the apply-time targets a device can use (PROJECT.md
// §11, design D9). Only BackendNative ships a real implementation in this
// milestone; BackendChezmoi is accepted here so the schema is stable, but is
// a hard error at apply time.
type Backend string

const (
	BackendNative  Backend = "native"
	BackendChezmoi Backend = "chezmoi"
)

// Valid reports whether b is a backend AliasDeck's config schema knows about.
func (b Backend) Valid() bool {
	switch b {
	case BackendNative, BackendChezmoi:
		return true
	}
	return false
}

// SourceType enumerates where a device's aliases come from (PROJECT.md §7).
type SourceType string

const (
	SourceTypeFile   SourceType = "file"
	SourceTypeGit    SourceType = "git"
	SourceTypeServer SourceType = "server"
)

// Source is config.yaml's source: block. Which fields are populated depends
// on Type; semantic validation of Type belongs to internal/source, which
// actually has to act on it.
type Source struct {
	Type SourceType
	Path string // file, git
	URL  string // server
}

// DeviceConfig is the device: block of config.yaml.
//
// Platform and Shell carry only the raw override strings a user declared
// directly; resolving them against detection and validating them against
// domain.Platform/domain.Shell is detect.go's job, not this parser's.
type DeviceConfig struct {
	ID         string
	Name       string
	ProfileIDs []string
	Platform   string
	Shell      string
}

// DeviceFileConfig is the parsed, strictly-typed content of config.yaml
// (PROJECT.md §7.3).
type DeviceFileConfig struct {
	Version int
	Device  DeviceConfig
	Source  Source
	Backend Backend
}

// configFileDTO mirrors config.yaml's on-disk shape exactly, so
// yaml.Decoder's KnownFields(true) can reject an unknown field before it
// reaches DeviceFileConfig.
type configFileDTO struct {
	Version int       `yaml:"version"`
	Device  deviceDTO `yaml:"device"`
	Source  sourceDTO `yaml:"source"`
	Backend string    `yaml:"backend"`
}

type deviceDTO struct {
	Name     string   `yaml:"name"`
	Profiles []string `yaml:"profiles"`
	Platform string   `yaml:"platform"`
	Shell    string   `yaml:"shell"`
}

type sourceDTO struct {
	Type string `yaml:"type"`
	Path string `yaml:"path"`
	URL  string `yaml:"url"`
}

// ParseDeviceConfig decodes and strictly validates the bytes of a
// config.yaml file: an unknown field, a version other than 1, an unknown
// backend, and oversized input are all parse errors.
func ParseDeviceConfig(data []byte) (DeviceFileConfig, error) {
	if len(data) > maxConfigFileSize {
		return DeviceFileConfig{}, fmt.Errorf(
			"config.yaml is %d bytes, exceeds the %d byte limit", len(data), maxConfigFileSize)
	}

	var dto configFileDTO
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&dto); err != nil {
		return DeviceFileConfig{}, fmt.Errorf("parsing config.yaml: %w", err)
	}

	if dto.Version != 1 {
		return DeviceFileConfig{}, fmt.Errorf(
			"config.yaml version %d is not supported, expected 1", dto.Version)
	}

	backend := Backend(dto.Backend)
	if !backend.Valid() {
		return DeviceFileConfig{}, fmt.Errorf(
			"config.yaml: unknown backend %q, must be %q or %q", dto.Backend, BackendNative, BackendChezmoi)
	}

	id := dto.Device.Name

	return DeviceFileConfig{
		Version: dto.Version,
		Device: DeviceConfig{
			ID:         id,
			Name:       dto.Device.Name,
			ProfileIDs: dto.Device.Profiles,
			Platform:   dto.Device.Platform,
			Shell:      dto.Device.Shell,
		},
		Source: Source{
			Type: SourceType(dto.Source.Type),
			Path: dto.Source.Path,
			URL:  dto.Source.URL,
		},
		Backend: backend,
	}, nil
}

// Load reads path, strictly parses it, and ensures the device has a stable
// identity.
//
// If device.name is empty, Load fills in a fallback identity **in memory only**
// and never writes the file back.
//
// Loading is a read. Persisting here would mean that `doctor`, `status` and
// `list` — commands documented as writing nothing — silently rewrote a
// hand-authored config.yaml, reformatting it and adding empty keys the user
// never typed. A diagnostic that edits the thing it is diagnosing is not a
// diagnostic.
//
// The identity stays stable across runs without being stored because
// generateDeviceName derives it from the hostname. The random last-resort
// branch is the exception: on a machine whose hostname yields nothing usable,
// the generated name differs on every run. Setting device.name in config.yaml
// is the fix, and `aliasdeck status` prints the name so the instability is
// visible rather than mysterious.
func Load(path string) (DeviceFileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DeviceFileConfig{}, fmt.Errorf("reading config.yaml: %w", err)
	}

	cfg, err := ParseDeviceConfig(data)
	if err != nil {
		return DeviceFileConfig{}, err
	}

	if cfg.Device.Name == "" {
		name, err := generateDeviceName()
		if err != nil {
			return DeviceFileConfig{}, fmt.Errorf("generating fallback device identity: %w", err)
		}
		cfg.Device.Name = name
		cfg.Device.ID = name
	}

	return cfg, nil
}

// Write serializes cfg back to path as config.yaml, at mode 0600 per the
// project's file table (design, "Paths, Detection, Exit Codes").
func Write(path string, cfg DeviceFileConfig) error {
	dto := configFileDTO{
		Version: cfg.Version,
		Device: deviceDTO{
			Name:     cfg.Device.Name,
			Profiles: cfg.Device.ProfileIDs,
			Platform: cfg.Device.Platform,
			Shell:    cfg.Device.Shell,
		},
		Source: sourceDTO{
			Type: string(cfg.Source.Type),
			Path: cfg.Source.Path,
			URL:  cfg.Source.URL,
		},
		Backend: string(cfg.Backend),
	}

	data, err := yaml.Marshal(dto)
	if err != nil {
		return fmt.Errorf("marshaling config.yaml: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing config.yaml: %w", err)
	}
	return nil
}

// generateDeviceName produces a fallback identity for a device whose
// config.yaml omits device.name.
//
// The hostname comes first because this name is a label a human reads: it is
// what `aliasdeck status` prints and what PROJECT.md §7.3 shows in every
// example. "macbook" tells the user which machine they are looking at;
// "device-a3f9c2b1" tells them nothing.
//
// A random suffix is the fallback rather than the default. Two machines
// sharing a hostname cannot collide here anyway — in standalone mode there is
// one device per config file and nothing to collide with. Collision only
// becomes real once devices register against a server, and by then the server
// assigns Device.ID, which is a separate field from this display name.
func generateDeviceName() (string, error) {
	if host, err := os.Hostname(); err == nil {
		if name := sanitizeHostname(host); name != "" {
			return name, nil
		}
	}

	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "device-" + hex.EncodeToString(buf), nil
}

// sanitizeHostname reduces a hostname to a readable device label.
//
// It drops any DNS suffix and the ".local" macOS appends, then keeps only
// characters that are unambiguous in a config file and in terminal output.
func sanitizeHostname(host string) string {
	if i := strings.IndexByte(host, '.'); i > 0 {
		host = host[:i]
	}

	var b strings.Builder
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		default:
			// Spaces and punctuation appear in real hostnames such as
			// "Angel's MacBook Pro"; collapse them rather than emitting them.
			b.WriteByte('-')
		}
	}

	return strings.Trim(b.String(), "-")
}
