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

// TestResolveSourceDispatchesServerSource pins task 8.1: source.type: server
// must build a *source.ServerSource from source.url, the credentials file,
// and source.allowInsecureHTTP — not the "not supported" error Milestones
// 2-3 returned for it.
func TestResolveSourceDispatchesServerSource(t *testing.T) {
	base := t.TempDir()
	cenv := config.Env{
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return "/home/user", nil },
	}
	seedCredentials(t, base, config.Credentials{DeviceToken: "adt_lookup.secret"})

	devCfg := config.DeviceFileConfig{
		Source: config.Source{
			Type:              config.SourceTypeServer,
			URL:               "https://aliases.example.com",
			AllowInsecureHTTP: true,
		},
	}

	_, src, desc, err := resolveSource(devCfg, cenv, base)
	if err != nil {
		t.Fatalf("resolveSource() returned an error: %v", err)
	}

	ss, ok := src.(*source.ServerSource)
	if !ok {
		t.Fatalf("resolveSource() Source = %T, want *source.ServerSource", src)
	}
	if ss.URL != "https://aliases.example.com" {
		t.Errorf("ServerSource.URL = %q, want the configured URL", ss.URL)
	}
	if ss.Token != "adt_lookup.secret" {
		t.Errorf("ServerSource.Token = %q, want the credentials file's device token", ss.Token)
	}
	if !ss.AllowHTTP {
		t.Error("ServerSource.AllowHTTP = false, want true (source.allowInsecureHTTP was set)")
	}
	if desc.Type != "server" {
		t.Errorf("Descriptor.Type = %q, want %q", desc.Type, "server")
	}
}

// TestResolveSourceServerRequiresURL pins that a server source without a URL
// fails fast, mirroring TestResolveSourceGitRequiresURL, instead of building
// an unusable ServerSource that only fails once sync tries to use it.
func TestResolveSourceServerRequiresURL(t *testing.T) {
	base := t.TempDir()
	cenv := config.Env{
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return "/home/user", nil },
	}
	devCfg := config.DeviceFileConfig{Source: config.Source{Type: config.SourceTypeServer}}

	if _, _, _, err := resolveSource(devCfg, cenv, base); err == nil {
		t.Fatal("resolveSource() must fail when source.url is empty")
	}
}

// TestResolveSourceServerRequiresRegistration pins that resolveSource fails
// fast, naming `aliasdeck register`, when config.yaml already declares a
// server source but no credentials file exists yet — rather than building a
// ServerSource with an empty Token that would only fail once Resolve makes
// an unauthenticated request.
func TestResolveSourceServerRequiresRegistration(t *testing.T) {
	base := t.TempDir()
	cenv := config.Env{
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return "/home/user", nil },
	}
	devCfg := config.DeviceFileConfig{
		Source: config.Source{Type: config.SourceTypeServer, URL: "https://aliases.example.com"},
	}

	if _, _, _, err := resolveSource(devCfg, cenv, base); err == nil {
		t.Fatal("resolveSource() must fail when no credentials file exists yet")
	}
}

// TestResolveSourceServerValidatesURLBeforeReturningASource pins design
// decision 13's transport guard at the resolveSource boundary, the same way
// TestResolveSourceGitPathEscapingCheckoutRejected pins decision 16 for git:
// a non-loopback http:// URL without the explicit --allow-insecure opt-out
// must fail immediately, here, rather than only once a later sync call
// reaches *source.ServerSource.Resolve's own internal check.
//
// Mutation check: deleting resolveServerSource's ValidateServerURL call
// makes this test the one that fails — resolveSource would then return a
// source successfully instead of erroring, even though the later Resolve
// call would still (separately) catch it. That is the exact "skip
// ValidateServerURL in resolveSource" mutation this project's test standard
// requires proving.
func TestResolveSourceServerValidatesURLBeforeReturningASource(t *testing.T) {
	base := t.TempDir()
	cenv := config.Env{
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return "/home/user", nil },
	}
	seedCredentials(t, base, config.Credentials{DeviceToken: "adt_lookup.secret"})

	devCfg := config.DeviceFileConfig{
		Source: config.Source{Type: config.SourceTypeServer, URL: "http://aliases.example.com"},
	}

	if _, _, _, err := resolveSource(devCfg, cenv, base); err == nil {
		t.Fatal("resolveSource() must reject an insecure, non-loopback http:// server URL without --allow-insecure")
	}
}

// seedCredentials writes a credentials.json under base so resolveSource's
// server arm finds a device token.
func seedCredentials(t *testing.T, base string, creds config.Credentials) {
	t.Helper()
	if err := config.SaveCredentials(config.CredentialsFile(base), creds); err != nil {
		t.Fatalf("seeding credentials.json: %v", err)
	}
}
