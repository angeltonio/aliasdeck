package app

import (
	"context"
	"testing"
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
