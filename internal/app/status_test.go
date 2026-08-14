package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/state"
)

func TestStatusReportsActiveSource(t *testing.T) {
	te := newTestEnv(t)
	seedSyncableDevice(t, te)

	if _, err := Sync(context.Background(), te.Env, Options{}); err != nil {
		t.Fatalf("Sync() returned an error: %v", err)
	}

	report, err := Status(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("Status() returned an error: %v", err)
	}

	if report.Source.Type != "file" {
		t.Errorf("Source.Type = %q, want %q", report.Source.Type, "file")
	}
	if report.Device.Name != "test-device" {
		t.Errorf("Device.Name = %q, want %q", report.Device.Name, "test-device")
	}
	if report.State.LastSyncAt.IsZero() {
		t.Error("State.LastSyncAt is zero after a successful sync")
	}
	if !report.UpToDate {
		t.Error("UpToDate = false right after a successful sync")
	}
}

func TestStatusReportsNotInitialized(t *testing.T) {
	te := newTestEnv(t)

	_, err := Status(context.Background(), te.Env, Options{})
	if err != ErrNotInitialized {
		t.Errorf("Status() error = %v, want ErrNotInitialized", err)
	}
}

// TestStatusReportsPowerShellProfileEditionAndPath pins cli-commands spec's
// "status reports PowerShell edition and profile" scenario: the resolved
// $PROFILE path, the chosen edition, and the provenance explaining the
// choice must all be inspectable rather than a silent guess (design
// decision 8, non-negotiable constraint 2).
func TestStatusReportsPowerShellProfileEditionAndPath(t *testing.T) {
	te := newTestEnv(t)
	writeConfigYAML(t, te.Base, nativeDeviceConfig("pwsh-device"))
	te.setenv("ALIASDECK_PLATFORM", "windows")
	te.setenv("ALIASDECK_SHELL", "powershell")
	te.Env.LookPath = lookPathFake("pwsh")

	report, err := Status(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("Status() returned an error: %v", err)
	}

	wantPath := filepath.Join(te.Home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
	if report.PowerShellEdition != "Core" {
		t.Errorf("PowerShellEdition = %q, want %q", report.PowerShellEdition, "Core")
	}
	if report.PowerShellProfilePath != wantPath {
		t.Errorf("PowerShellProfilePath = %q, want %q", report.PowerShellProfilePath, wantPath)
	}
	if report.PowerShellProvenance == "" {
		t.Error("PowerShellProvenance is empty; the edition choice must be inspectable, not a silent guess")
	}
}

// TestStatusOmitsPowerShellFieldsForNonPowerShellDevice pins that the new
// PowerShell fields stay at their zero value for zsh/bash devices, so their
// output shape does not change (non-negotiable constraint 2).
func TestStatusOmitsPowerShellFieldsForNonPowerShellDevice(t *testing.T) {
	te := newTestEnv(t)
	seedSyncableDevice(t, te)

	report, err := Status(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("Status() returned an error: %v", err)
	}
	if report.PowerShellEdition != "" {
		t.Errorf("PowerShellEdition = %q, want empty for a non-PowerShell device", report.PowerShellEdition)
	}
	if report.PowerShellProfilePath != "" {
		t.Errorf("PowerShellProfilePath = %q, want empty for a non-PowerShell device", report.PowerShellProfilePath)
	}
	if report.PowerShellProvenance != "" {
		t.Errorf("PowerShellProvenance = %q, want empty for a non-PowerShell device", report.PowerShellProvenance)
	}
}

// TestStatusReportsGitRefAndStaleness pins cli-commands spec's "status
// reports git ref and staleness" scenario. status never calls
// GitSource.Resolve (it would spawn a git process); it reads the resolved
// ref and staleness recorded by the last successful sync instead.
func TestStatusReportsGitRefAndStaleness(t *testing.T) {
	te := newTestEnv(t)
	cfg := nativeDeviceConfig("git-device")
	cfg.Source = config.Source{Type: config.SourceTypeGit, Git: config.GitSourceConfig{URL: "https://example.com/dotfiles.git"}}
	writeConfigYAML(t, te.Base, cfg)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	fetchedAt := time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC)
	wantRef := "https://example.com/dotfiles.git#HEAD@0123456789ab"
	seeded := state.State{
		Version:         1,
		SourceType:      "git",
		SourceRef:       wantRef,
		SourceStale:     true,
		SourceFetchedAt: fetchedAt,
	}
	if err := state.Save(config.StateFile(te.Base), seeded); err != nil {
		t.Fatalf("seeding state.json: %v", err)
	}

	report, err := Status(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("Status() returned an error: %v", err)
	}
	if report.SourceRef != wantRef {
		t.Errorf("SourceRef = %q, want %q", report.SourceRef, wantRef)
	}
	if !report.SourceStale {
		t.Error("SourceStale = false, want true")
	}
	if !report.SourceFetchedAt.Equal(fetchedAt) {
		t.Errorf("SourceFetchedAt = %v, want %v", report.SourceFetchedAt, fetchedAt)
	}
}

// TestStatusReportsServerSourceURLWithoutTheDeviceToken pins task 8.8/8.9 and
// cli-commands spec's "status reports server URL without the token": under
// source.type: server, status must name ServerSource and its URL, and the
// device token value must never appear anywhere in the reported output — not
// truncated, not prefixed, absent (server-source spec, "Credential file").
//
// Descriptor already carries only {Type, Ref: URL} (design decision 11), and
// StatusReport never reads config.Credentials at all, so this test also
// proves that stays true rather than merely asserting it once. Formatting
// the full report with "%+v" (rather than checking report.Source alone)
// means a future field added anywhere on StatusReport that happened to carry
// the token would still be caught here.
func TestStatusReportsServerSourceURLWithoutTheDeviceToken(t *testing.T) {
	te := newTestEnv(t)
	url := "https://aliases.example.com"
	cfg := nativeDeviceConfig("server-device")
	cfg.Source = config.Source{Type: config.SourceTypeServer, URL: url}
	writeConfigYAML(t, te.Base, cfg)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	const deviceToken = "adt_verysecretlookup.verysecretvalue"
	seedCredentials(t, te.Base, config.Credentials{DeviceToken: deviceToken})

	report, err := Status(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("Status() returned an error: %v", err)
	}

	if report.Source.Type != "server" {
		t.Errorf("Source.Type = %q, want %q", report.Source.Type, "server")
	}
	if report.Source.Ref != url {
		t.Errorf("Source.Ref = %q, want %q", report.Source.Ref, url)
	}

	rendered := fmt.Sprintf("%+v", report)
	if strings.Contains(rendered, deviceToken) {
		t.Errorf("rendered status output contains the device token: %s", rendered)
	}
}
