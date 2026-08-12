package source

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing fixture %s: %v", path, err)
	}
}

func TestFileSourceResolveReadsConfiguredPathOnly(t *testing.T) {
	dir := t.TempDir()
	configured := filepath.Join(dir, "aliases.yaml")
	decoy := filepath.Join(dir, "decoy.yaml")

	writeFile(t, configured, `version: 1
aliases:
  - name: configured
    command: echo configured
`)
	// A file at a different path that FileSource must never read, let alone
	// merge with the configured one (config-source spec: "Exactly One Source
	// Per Device").
	writeFile(t, decoy, `version: 1
aliases:
  - name: decoy
    command: echo decoy
`)

	dev := domain.Device{Platform: domain.PlatformLinux, Shell: domain.ShellBash}
	src := FileSource{Path: configured}

	got, err := src.Resolve(context.Background(), dev)
	if err != nil {
		t.Fatalf("Resolve() returned an error: %v", err)
	}

	if len(got.Aliases) != 1 || got.Aliases[0].Name != "configured" {
		t.Fatalf("Resolve() aliases = %+v, want exactly the alias from the configured path", got.Aliases)
	}
}

func TestFileSourceResolveErrorNotPartiallyApplied(t *testing.T) {
	dev := domain.Device{Platform: domain.PlatformLinux, Shell: domain.ShellBash}

	tests := []struct {
		name    string
		path    string
		content string
	}{
		{
			name: "missing file",
			path: "does-not-exist.yaml",
		},
		{
			name:    "malformed YAML",
			path:    "malformed.yaml",
			content: "version: 1\naliases:\n  - name: broken\n    comand: typo\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, tt.path)
			if tt.content != "" {
				writeFile(t, path, tt.content)
			}

			src := FileSource{Path: path}
			got, err := src.Resolve(context.Background(), dev)
			if err == nil {
				t.Fatal("Resolve() must return an error")
			}
			if got.Revision != "" || len(got.Aliases) != 0 {
				t.Fatalf("Resolve() returned a partially applied config on error: %+v", got)
			}
		})
	}
}

func TestFileSourceResolveFiltersHostileInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aliases.yaml")

	oversizedCommand := strings.Repeat("a", 4200)
	writeFile(t, path, `version: 1
aliases:
  - name: safe
    command: echo safe
  - name: "evil rm"
    command: rm -rf /
  - name: oversized
    command: `+oversizedCommand+`
`)

	dev := domain.Device{Platform: domain.PlatformLinux, Shell: domain.ShellBash}
	src := FileSource{Path: path}

	got, err := src.Resolve(context.Background(), dev)
	if err != nil {
		t.Fatalf("Resolve() returned an error: %v", err)
	}

	if len(got.Aliases) != 1 || got.Aliases[0].Name != "safe" {
		t.Fatalf("Resolve() aliases = %+v, want only the safe alias; "+
			"hostile name and oversized command must be filtered by validate.FilterValid before "+
			"reaching a caller that might render them", got.Aliases)
	}
}

func TestFileSourceDescriptorNamesTheActiveSource(t *testing.T) {
	src := FileSource{Path: "/home/user/dotfiles/aliases.yaml"}

	got := src.Descriptor()
	if got.Type != "file" || got.Ref != "/home/user/dotfiles/aliases.yaml" {
		t.Errorf("Descriptor() = %+v, want Type=file Ref=configured path", got)
	}
}
