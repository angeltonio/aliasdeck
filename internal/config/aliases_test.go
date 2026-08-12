package config

import (
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
