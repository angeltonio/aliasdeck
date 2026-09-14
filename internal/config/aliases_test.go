package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

const validAliasesYAML = `
version: 1

profiles:
  - development
  - homelab

aliases:
  - name: dcu
    command: docker compose up -d
    description: Start Docker Compose stack
    platforms: [macos, linux]
    shells: [zsh, bash]
    tags: [docker]
    profiles: [development]

  - name: dps
    command: docker ps
    shells: [zsh, bash, powershell]
    profiles: [development, homelab]

  - name: pve
    command: ssh root@proxmox.local
    platforms: [macos, linux]
    shells: [zsh]
    profiles: [homelab]
`

func TestParseAliasesValidFile(t *testing.T) {
	doc, err := ParseAliases([]byte(validAliasesYAML))
	if err != nil {
		t.Fatalf("ParseAliases() returned an error for a well-formed file: %v", err)
	}

	if want := []string{"development", "homelab"}; !equalStrings(doc.Profiles, want) {
		t.Errorf("doc.Profiles = %v, want %v", doc.Profiles, want)
	}

	if len(doc.Aliases) != 3 {
		t.Fatalf("got %d aliases, want 3: %+v", len(doc.Aliases), doc.Aliases)
	}

	dcu := doc.Aliases[0]
	if dcu.Name != "dcu" || dcu.Command != "docker compose up -d" {
		t.Errorf("dcu alias = %+v, want name=dcu command=%q", dcu, "docker compose up -d")
	}
	if dcu.ID != dcu.Name {
		t.Errorf("dcu.ID = %q, want it derived from Name (%q)", dcu.ID, dcu.Name)
	}
	if !dcu.Enabled {
		t.Error("dcu omits `enabled`; it must default to true")
	}
	if want := []domain.Platform{domain.PlatformMacOS, domain.PlatformLinux}; !equalPlatforms(dcu.Platforms, want) {
		t.Errorf("dcu.Platforms = %v, want %v", dcu.Platforms, want)
	}
	if want := []domain.Shell{domain.ShellZsh, domain.ShellBash}; !equalShells(dcu.Shells, want) {
		t.Errorf("dcu.Shells = %v, want %v", dcu.Shells, want)
	}
	if want := []string{"development"}; !equalStrings(dcu.ProfileIDs, want) {
		t.Errorf("dcu.ProfileIDs = %v, want %v (profiles: must map to ProfileIDs)", dcu.ProfileIDs, want)
	}

	pve := doc.Aliases[2]
	if pve.Name != "pve" || pve.ID != "pve" {
		t.Errorf("pve alias = %+v, want name=id=pve", pve)
	}
}

func TestParseAliasesEnabledDefault(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want bool
	}{
		{
			name: "omitted enabled defaults to true",
			yaml: "version: 1\naliases:\n  - name: x\n    command: echo x\n",
			want: true,
		},
		{
			name: "explicit enabled: true stays true",
			yaml: "version: 1\naliases:\n  - name: x\n    command: echo x\n    enabled: true\n",
			want: true,
		},
		{
			name: "explicit enabled: false is honored",
			yaml: "version: 1\naliases:\n  - name: x\n    command: echo x\n    enabled: false\n",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseAliases([]byte(tt.yaml))
			if err != nil {
				t.Fatalf("ParseAliases() returned an error: %v", err)
			}
			if len(doc.Aliases) != 1 {
				t.Fatalf("got %d aliases, want 1", len(doc.Aliases))
			}
			if got := doc.Aliases[0].Enabled; got != tt.want {
				t.Errorf("Enabled = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseAliasesUnknownFieldRejected(t *testing.T) {
	tests := []struct {
		name          string
		yaml          string
		wantSubstring string
	}{
		{
			name:          "unknown alias-level field",
			yaml:          "version: 1\naliases:\n  - name: dcu\n    commnad: docker compose up -d\n",
			wantSubstring: "commnad",
		},
		{
			name:          "unknown top-level field",
			yaml:          "version: 1\nsourceRepo: https://example.com\naliases: []\n",
			wantSubstring: "sourceRepo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAliases([]byte(tt.yaml))
			if err == nil {
				t.Fatal("ParseAliases() must reject an unknown field, got nil error")
			}
			if !strings.Contains(err.Error(), tt.wantSubstring) {
				t.Errorf("error %q does not name the offending field %q", err.Error(), tt.wantSubstring)
			}
		})
	}
}

func TestParseAliasesWrongVersionRejected(t *testing.T) {
	_, err := ParseAliases([]byte("version: 2\naliases: []\n"))
	if err == nil {
		t.Fatal("ParseAliases() must reject a version other than 1")
	}
}

func TestParseAliasesOversizeRejected(t *testing.T) {
	oversized := make([]byte, (1<<20)+1)
	for i := range oversized {
		oversized[i] = ' '
	}

	_, err := ParseAliases(oversized)
	if err == nil {
		t.Fatal("ParseAliases() must reject input larger than 1 MiB")
	}
}

func TestProfileWarningsUndeclaredProfile(t *testing.T) {
	declared := []string{"development", "homelab"}
	aliases := []domain.Alias{
		{Name: "dcu", ProfileIDs: []string{"development"}},
		{Name: "pve", ProfileIDs: []string{"typo-profile"}},
	}

	warnings := ProfileWarnings(declared, aliases)

	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "pve") || !strings.Contains(warnings[0], "typo-profile") {
		t.Errorf("warning %q must name both the alias and the undeclared profile", warnings[0])
	}
}

func TestProfileWarningsNoUndeclaredReferences(t *testing.T) {
	declared := []string{"development"}
	aliases := []domain.Alias{{Name: "dcu", ProfileIDs: []string{"development"}}}

	if warnings := ProfileWarnings(declared, aliases); len(warnings) != 0 {
		t.Errorf("got %d warnings, want 0: %v", len(warnings), warnings)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func equalPlatforms(got, want []domain.Platform) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func equalShells(got, want []domain.Shell) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestMarshalAliasesRoundTripsThroughParseAliases is what lets MarshalAliases
// share a DTO with the parser and call that a guarantee. Sharing the struct
// makes the two spellings identical; only this proves the two *meanings* are.
func TestMarshalAliasesRoundTripsThroughParseAliases(t *testing.T) {
	want := AliasesDocument{
		Profiles: []string{"work", "home"},
		Aliases: []domain.Alias{
			{ID: "gs", Name: "gs", Command: "git status", Enabled: true},
			{
				ID:          "deploy",
				Name:        "deploy",
				Command:     "make deploy",
				Description: "ship it",
				Enabled:     false,
				Tags:        []string{"risky"},
				Platforms:   []domain.Platform{domain.PlatformMacOS},
				Shells:      []domain.Shell{domain.ShellZsh},
				ProfileIDs:  []string{"work"},
			},
		},
	}

	data, err := MarshalAliases(want)
	if err != nil {
		t.Fatalf("MarshalAliases(): %v", err)
	}

	got, err := ParseAliases(data)
	if err != nil {
		t.Fatalf("ParseAliases() on our own output: %v\n%s", err, data)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip lost something:\n got %+v\nwant %+v\n---\n%s", got, want, data)
	}
}

// TestMarshalAliasesOmitsFieldsNobodySet keeps the file hand-editable, which
// is the whole premise of aliases.yaml. Forty entries each carrying
// `enabled: null`, `tags: []` and `shells: []` is a file people stop reading.
func TestMarshalAliasesOmitsFieldsNobodySet(t *testing.T) {
	data, err := MarshalAliases(AliasesDocument{
		Aliases: []domain.Alias{{ID: "gs", Name: "gs", Command: "git status", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("MarshalAliases(): %v", err)
	}

	for _, noise := range []string{"description:", "enabled:", "tags:", "platforms:", "shells:", "profiles:"} {
		if strings.Contains(string(data), noise) {
			t.Errorf("output carries an unset %s\n%s", noise, data)
		}
	}
	// enabled:false is the one that must survive, because omitting it means
	// true.
	off, err := MarshalAliases(AliasesDocument{
		Aliases: []domain.Alias{{ID: "gs", Name: "gs", Command: "git status", Enabled: false}},
	})
	if err != nil {
		t.Fatalf("MarshalAliases(): %v", err)
	}
	if !strings.Contains(string(off), "enabled: false") {
		t.Errorf("a disabled alias round-trips as enabled:\n%s", off)
	}
}

// TestWriteAliasesReplacesAtomicallyAndKeepsPermissions pins both halves of
// the write: the temp-and-rename discipline the rest of this package uses,
// and leaving a file the user deliberately made readable alone.
func TestWriteAliasesReplacesAtomicallyAndKeepsPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aliases.yaml")
	if err := os.WriteFile(path, []byte("version: 1\naliases: []\n"), 0o644); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	err := WriteAliases(path, AliasesDocument{
		Aliases: []domain.Alias{{ID: "gs", Name: "gs", Command: "git status", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("WriteAliases(): %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want the 0644 the user had", info.Mode().Perm())
	}

	// No temp file may survive a successful write.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("a temp file was left behind: %s", e.Name())
		}
	}

	doc, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if !strings.Contains(string(doc), "git status") {
		t.Errorf("content was not replaced:\n%s", doc)
	}
}
