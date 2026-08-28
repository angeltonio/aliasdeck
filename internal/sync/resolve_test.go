package sync_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/source"
	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/angeltonio/aliasdeck/internal/sync"
)

// fixtureYAML is the ONE fixture this file's central test shares between
// both resolution paths: the exact bytes a standalone FileSource reads, and
// (via config.ParseAliases, the same parser FileSource itself calls) the
// exact []domain.Alias a server would have persisted from an equivalent
// create call. Using one source of truth for both sides is what makes a
// divergence between them visible here instead of being hidden by two
// fixtures that quietly drifted apart. It exercises every targeting
// dimension aliases.yaml can express: platform, shell, profile membership,
// and the enabled flag. DeviceIDs pinning has no equivalent in this file
// format (PROJECT.md §5: "In standalone mode ... DeviceIDs is unused"), so
// that dimension is covered separately below, directly in domain terms.
//
// linuxonly and zshonly (bounded-review finding 5, post-Phase-6 correction)
// exist so platform and shell each have at least one alias/device pair
// where that single dimension is the SOLE discriminator between inclusion
// and exclusion — every other alias in this fixture couples its platform
// restriction to a shell or profile restriction too (dcu, pve, winonly), or
// its shell restriction to every shell there is (dps's
// [zsh, bash, powershell] is domain.AllShells verbatim, so it never
// actually excludes anyone). Before this correction, forcing every alias's
// Platforms to nil inside sync.Resolve (a reviewer's disproven CRITICAL
// claim, see apply-progress) only ever changed a field's *content* on an
// alias that was included either way (dcu for dev-macos-zsh-dev, winonly
// for dev-windows-pwsh-none) — reflect.DeepEqual caught that, but not
// because platform ever changed which aliases were included. linuxonly
// (platform-restricted, no shell restriction) closes that gap: mutating
// platform targeting now changes the *included alias set itself* for
// dev-linux-bash-homelab, not merely a struct field on an alias that was
// going to be included regardless.
const fixtureYAML = `version: 1

profiles:
  - development
  - homelab

aliases:
  - name: dcu
    command: docker compose up -d
    description: Start Docker Compose stack
    platforms: [macos, linux]
    shells: [zsh, bash]
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

  - name: winonly
    command: Get-Process
    platforms: [windows]
    shells: [powershell]

  - name: linuxonly
    command: echo linux, any shell, any profile
    platforms: [linux]

  - name: zshonly
    command: echo zsh, any platform, any profile
    shells: [zsh]

  - name: retired
    command: echo this should never appear
    enabled: false
`

// fixtureDevices is every device shape the round-trip test resolves
// against, chosen so at least one alias from fixtureYAML is included and at
// least one is excluded for a different reason on each row (platform,
// shell, profile, and the disabled alias, which every row must exclude).
func fixtureDevices() []domain.Device {
	return []domain.Device{
		{ID: "dev-macos-zsh-dev", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh, ProfileIDs: []string{"development"}},
		{ID: "dev-linux-bash-homelab", Platform: domain.PlatformLinux, Shell: domain.ShellBash, ProfileIDs: []string{"homelab"}},
		{ID: "dev-windows-pwsh-none", Platform: domain.PlatformWindows, Shell: domain.ShellPowerShell, ProfileIDs: nil},
		{ID: "dev-macos-zsh-none", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh, ProfileIDs: nil},
	}
}

// TestResolveMatchesLocalFileSourceResolution is this milestone's most
// important test (Phase 6, task 6.1): it proves the server's sync.Resolve
// filters IDENTICALLY to the CLI's own FileSource.Resolve, from one shared
// fixture, for every targeting dimension the fixture exercises. Both sides
// go through production code, not a hand-rolled restatement of either:
// FileSource.Resolve is called verbatim, and sync.Resolve is called against
// a fake store.Store whose Aliases().List returns the exact same parsed
// slice. If these two ever disagree, this is the only test that says so —
// see the mutation proof recorded in apply-progress for what happens when
// sync.Resolve's own filtering is deliberately made to diverge.
func TestResolveMatchesLocalFileSourceResolution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aliases.yaml")
	if err := os.WriteFile(path, []byte(fixtureYAML), 0o600); err != nil {
		t.Fatalf("writing fixture aliases.yaml: %v", err)
	}

	doc, err := config.ParseAliases([]byte(fixtureYAML))
	if err != nil {
		t.Fatalf("config.ParseAliases(fixtureYAML): %v", err)
	}

	fileSrc := source.FileSource{Path: path}
	st := &stubStore{aliases: fixedAliasRepo{aliases: doc.Aliases}}

	for _, dev := range fixtureDevices() {
		t.Run(dev.ID, func(t *testing.T) {
			local, err := fileSrc.Resolve(context.Background(), dev)
			if err != nil {
				t.Fatalf("FileSource.Resolve(%s): %v", dev.ID, err)
			}

			serverSide, err := sync.Resolve(context.Background(), st, dev)
			if err != nil {
				t.Fatalf("sync.Resolve(%s): %v", dev.ID, err)
			}

			if local.Revision != serverSide.Revision {
				t.Fatalf("revision mismatch for %s: local=%q server=%q", dev.ID, local.Revision, serverSide.Revision)
			}
			if !reflect.DeepEqual(local.Aliases, serverSide.Aliases) {
				t.Fatalf("alias set mismatch for %s:\n local=%#v\nserver=%#v", dev.ID, local.Aliases, serverSide.Aliases)
			}

			// The disabled fixture alias ("retired") must never survive
			// either path, for any device — this is the one row that
			// should be identically absent everywhere, not merely
			// identically present.
			for _, a := range serverSide.Aliases {
				if a.Name == "retired" {
					t.Fatalf("disabled alias %q present in server resolution for %s", a.Name, dev.ID)
				}
			}
		})
	}
}

// TestResolveAppliesDevicePinning covers the one targeting dimension
// fixtureYAML cannot express (DeviceIDs — aliases.yaml has no such field).
// It proves sync.Resolve passes dev.ID through to domain.Resolve rather than
// dropping it, by pinning an alias to one specific device id and confirming
// only that device receives it.
func TestResolveAppliesDevicePinning(t *testing.T) {
	pinned := domain.Alias{
		ID: "pinned", Name: "pinned", Command: "echo pinned", Enabled: true,
		DeviceIDs: []string{"device-a"},
	}
	unpinned := domain.Alias{ID: "everyone", Name: "everyone", Command: "echo everyone", Enabled: true}

	st := &stubStore{aliases: fixedAliasRepo{aliases: []domain.Alias{pinned, unpinned}}}

	a, err := sync.Resolve(context.Background(), st, domain.Device{ID: "device-a", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh})
	if err != nil {
		t.Fatalf("sync.Resolve(device-a): %v", err)
	}
	if len(a.Aliases) != 2 {
		t.Fatalf("device-a: got %d aliases, want 2 (both): %#v", len(a.Aliases), a.Aliases)
	}

	b, err := sync.Resolve(context.Background(), st, domain.Device{ID: "device-b", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh})
	if err != nil {
		t.Fatalf("sync.Resolve(device-b): %v", err)
	}
	if len(b.Aliases) != 1 || b.Aliases[0].Name != "everyone" {
		t.Fatalf("device-b: got %#v, want only the unpinned alias", b.Aliases)
	}
}

// TestResolveWrapsAliasListError proves a store failure is surfaced, not
// swallowed into an empty, successful-looking ResolvedConfig.
func TestResolveWrapsAliasListError(t *testing.T) {
	wantErr := errListFailed
	st := &stubStore{aliases: erroringAliasRepo{err: wantErr}}

	_, err := sync.Resolve(context.Background(), st, domain.Device{Platform: domain.PlatformMacOS, Shell: domain.ShellZsh})
	if err == nil {
		t.Fatal("sync.Resolve(...) = nil error, want the store's List error wrapped")
	}
}

var errListFailed = &listError{"boom"}

type listError struct{ msg string }

func (e *listError) Error() string { return e.msg }

// fixedAliasRepo implements store.AliasRepo, returning a fixed slice from
// List. sync.Resolve calls List and nothing else, so every other method
// panics if reached — a reliable signal this test's assumption about
// sync.Resolve's own store usage stopped holding.
type fixedAliasRepo struct{ aliases []domain.Alias }

func (fixedAliasRepo) Create(context.Context, domain.Alias) (domain.Alias, error) {
	panic("sync.Resolve must not call AliasRepo.Create")
}
func (r fixedAliasRepo) Get(context.Context, string) (domain.Alias, error) {
	panic("sync.Resolve must not call AliasRepo.Get")
}
func (r fixedAliasRepo) List(context.Context) ([]domain.Alias, error) { return r.aliases, nil }
func (fixedAliasRepo) Update(context.Context, domain.Alias) (domain.Alias, error) {
	panic("sync.Resolve must not call AliasRepo.Update")
}
func (fixedAliasRepo) Delete(context.Context, string) error {
	panic("sync.Resolve must not call AliasRepo.Delete")
}

// erroringAliasRepo's List always fails, for TestResolveWrapsAliasListError.
type erroringAliasRepo struct{ err error }

func (erroringAliasRepo) Create(context.Context, domain.Alias) (domain.Alias, error) {
	panic("not used")
}
func (erroringAliasRepo) Get(context.Context, string) (domain.Alias, error) { panic("not used") }
func (r erroringAliasRepo) List(context.Context) ([]domain.Alias, error)    { return nil, r.err }
func (erroringAliasRepo) Update(context.Context, domain.Alias) (domain.Alias, error) {
	panic("not used")
}
func (erroringAliasRepo) Delete(context.Context, string) error { panic("not used") }

// stubStore implements store.Store with only Aliases() meaningful —
// sync.Resolve (per its own doc comment) touches nothing else on Store.
type stubStore struct{ aliases store.AliasRepo }

func (s *stubStore) Aliases() store.AliasRepo { return s.aliases }
func (s *stubStore) Devices() store.DeviceRepo {
	panic("sync.Resolve must not call Store.Devices")
}
func (s *stubStore) Profiles() store.ProfileRepo {
	panic("sync.Resolve must not call Store.Profiles")
}
func (s *stubStore) Tokens() store.TokenRepo {
	panic("sync.Resolve must not call Store.Tokens")
}
func (s *stubStore) Operators() store.OperatorRepo {
	panic("sync.Resolve must not call Store.Operators")
}

// This fake records nothing: the code under test here does not audit.
// A nil repo would panic the moment it did, which is the failure this
// should have rather than a silently dropped record.
func (s *stubStore) Audit() store.AuditRepo { return noopAuditRepo{} }
func (s *stubStore) Close() error           { return nil }
