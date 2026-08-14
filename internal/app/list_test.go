package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/config"
)

const testMultiPlatformAliasesYAML = `version: 1

aliases:
  - name: dcu
    command: docker compose up -d
    platforms: [macos, linux]
    shells: [zsh, bash]
  - name: pve
    command: ssh root@proxmox.local
    platforms: [linux]
    shells: [zsh]
  - name: disabled-one
    command: echo disabled
    enabled: false
`

func TestListShowsDeviceScopedEntries(t *testing.T) {
	te := newTestEnv(t)
	writeConfigYAML(t, te.Base, nativeDeviceConfig("test-device"))
	writeAliasesYAML(t, te.Base, testMultiPlatformAliasesYAML)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	report, err := List(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("List() returned an error: %v", err)
	}

	if len(report.Entries) != 3 {
		t.Fatalf("len(Entries) = %d, want 3", len(report.Entries))
	}

	byName := make(map[string]AliasListing, len(report.Entries))
	for _, e := range report.Entries {
		byName[e.Alias.Name] = e
	}

	if !byName["dcu"].Active {
		t.Error(`"dcu" targets macos/zsh and must be active`)
	}
	if byName["pve"].Active {
		t.Error(`"pve" only targets linux and must not be active on macos`)
	}
	if byName["pve"].Reason == "" {
		t.Error(`"pve" must report a reason for being inactive`)
	}
	if byName["disabled-one"].Active {
		t.Error(`"disabled-one" is disabled and must not be active`)
	}
	if byName["disabled-one"].Reason != "disabled" {
		t.Errorf(`"disabled-one" reason = %q, want %q`, byName["disabled-one"].Reason, "disabled")
	}
}

// TestListFailsUnderServerSourceInsteadOfReadingAnEmptyPath is the
// regression test for a gap a scope audit found: resolveServerSource leaves
// AliasesPath empty, so `aliasdeck list` under a server source failed with
// `reading : open : no such file or directory` — the raw OS complaint about
// an empty path, which tells a user nothing.
//
// List reads the declared set on purpose, because its whole value is showing
// aliases that are declared but inactive and why. A server source has no
// local declared set, and what the device can fetch is already resolved, so
// the honest answer is to say so rather than to half-answer.
func TestListFailsUnderServerSourceInsteadOfReadingAnEmptyPath(t *testing.T) {
	te := newTestEnv(t)
	cfg := nativeDeviceConfig("server-device")
	cfg.Source = config.Source{Type: config.SourceTypeServer, URL: "https://aliases.example.com"}
	writeConfigYAML(t, te.Base, cfg)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")
	seedCredentials(t, te.Base, config.Credentials{DeviceToken: "adt_lookup.secret"})

	_, err := List(context.Background(), te.Env, Options{})
	if err == nil {
		t.Fatal("List() must fail under a server source")
	}
	if !errors.Is(err, ErrListAliasesUnderServerSource) {
		t.Fatalf("List() error = %v, want ErrListAliasesUnderServerSource", err)
	}
	if strings.Contains(err.Error(), "no such file") {
		t.Errorf("error %q leaks the raw empty-path OS error this test exists to replace", err.Error())
	}
}
