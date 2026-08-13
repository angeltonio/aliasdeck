package app

import (
	"path/filepath"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/source"
)

// TestResolveSourceDispatchesGitSource pins design decision 12's wiring:
// source.type: git must build a *source.GitSource with a cache directory
// derived from source.git.url, not the "not supported" error Milestone 2
// returned for it.
func TestResolveSourceDispatchesGitSource(t *testing.T) {
	base := t.TempDir()
	cenv := config.Env{
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return "/home/user", nil },
	}
	devCfg := config.DeviceFileConfig{
		Source: config.Source{
			Type: config.SourceTypeGit,
			Git: config.GitSourceConfig{
				URL:  "https://example.com/dotfiles.git",
				Ref:  "main",
				Path: "config/aliases.yaml",
			},
		},
	}

	path, src, desc, err := resolveSource(devCfg, cenv, base)
	if err != nil {
		t.Fatalf("resolveSource() returned an error: %v", err)
	}

	gs, ok := src.(*source.GitSource)
	if !ok {
		t.Fatalf("resolveSource() Source = %T, want *source.GitSource", src)
	}
	if gs.URL != "https://example.com/dotfiles.git" {
		t.Errorf("GitSource.URL = %q, want the configured URL", gs.URL)
	}
	if gs.Ref != "main" {
		t.Errorf("GitSource.Ref = %q, want %q", gs.Ref, "main")
	}
	if gs.Run == nil {
		t.Error("GitSource.Run is nil, want RunGit wired in")
	}

	wantCacheDir := source.GitCacheDir(base, "https://example.com/dotfiles.git")
	if gs.CacheDir != wantCacheDir {
		t.Errorf("GitSource.CacheDir = %q, want %q", gs.CacheDir, wantCacheDir)
	}

	wantPath := filepath.Join(wantCacheDir, "config/aliases.yaml")
	if path != wantPath {
		t.Errorf("resolveSource() path = %q, want %q", path, wantPath)
	}
	if desc.Type != "git" {
		t.Errorf("Descriptor.Type = %q, want %q", desc.Type, "git")
	}
}

// TestResolveSourceGitRequiresURL pins that a git source without a URL
// fails fast, naming the problem, instead of constructing an unusable
// GitSource that would only fail once sync tries to use it.
func TestResolveSourceGitRequiresURL(t *testing.T) {
	base := t.TempDir()
	cenv := config.Env{
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return "/home/user", nil },
	}
	devCfg := config.DeviceFileConfig{
		Source: config.Source{Type: config.SourceTypeGit},
	}

	if _, _, _, err := resolveSource(devCfg, cenv, base); err == nil {
		t.Fatal("resolveSource() must fail when source.git.url is empty")
	}
}

// TestResolveSourceGitPathEscapingCheckoutRejected pins design decision 16
// at the dc-build boundary too: a ".."-bearing source.git.path fails before
// loadDeviceContext ever succeeds, not only inside GitSource.Resolve.
func TestResolveSourceGitPathEscapingCheckoutRejected(t *testing.T) {
	base := t.TempDir()
	cenv := config.Env{
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return "/home/user", nil },
	}
	devCfg := config.DeviceFileConfig{
		Source: config.Source{
			Type: config.SourceTypeGit,
			Git: config.GitSourceConfig{
				URL:  "https://example.com/dotfiles.git",
				Path: "../../etc/passwd",
			},
		},
	}

	if _, _, _, err := resolveSource(devCfg, cenv, base); err == nil {
		t.Fatal("resolveSource() must reject a source.git.path that escapes the checkout")
	}
}

// TestResolveSourceServerStillUnsupported pins that Milestone 4's
// ServerSource is still an explicit error, not a silent fallback, now that
// git is dispatched instead of falling into the same default branch.
func TestResolveSourceServerStillUnsupported(t *testing.T) {
	base := t.TempDir()
	cenv := config.Env{
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return "/home/user", nil },
	}
	devCfg := config.DeviceFileConfig{
		Source: config.Source{Type: config.SourceTypeServer, URL: "https://api.example.com"},
	}

	if _, _, _, err := resolveSource(devCfg, cenv, base); err == nil {
		t.Fatal("resolveSource() must still fail for source.type: server in this milestone")
	}
}
