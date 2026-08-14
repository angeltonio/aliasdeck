package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/renderers"
	"github.com/angeltonio/aliasdeck/internal/source"
)

// fixtureYAML is the ONE fixture this file's central test shares between
// both resolution paths (mirroring internal/sync/resolve_test.go's own
// fixtureYAML precedent): the exact bytes a standalone FileSource reads,
// and — parsed once via config.ParseAliases, the same parser FileSource
// itself calls — the exact []domain.Alias a server-side operator would
// have created through internal/api's own CRUD handlers.
//
// Every command below is chosen to actually exercise escaping — quotes,
// $ expansion, backticks, a semicolon-separated command, and a pipe — not
// just alphanumeric text a renderer could pass through unchanged by
// accident. gitwip/portscan/reload/chained are reused, verbatim, from
// internal/renderers/golden_test.go's own fixtures (already proven to
// render correctly); psgrep/psreload are the PowerShell-shaped pair this
// project's own shipped injection (a bare "}" closing a function block
// early) makes non-negotiable to cover here too.
const fixtureYAML = `version: 1

profiles:
  - ops

aliases:
  - name: gitwip
    command: "git commit -m 'wip: don't ask'"
    description: "single and double quotes inside a description must survive rendering"
    shells: [zsh, bash]
    profiles: [ops]

  - name: portscan
    command: "lsof -nP -iTCP -sTCP:LISTEN | awk '{print $1, $9}'"
    shells: [zsh, bash]

  - name: reload
    command: 'source "$HOME/.zshrc" && echo $(date)'
    description: "dollar expansion and double quotes must stay literal"
    shells: [zsh, bash]

  - name: chained
    command: echo start; echo done
    description: "a semicolon-separated command sequence"
    shells: [zsh, bash]

  - name: psgrep
    command: "Get-Process | Where-Object { $_.CPU -gt 10 }"
    shells: [powershell]
    platforms: [windows]

  - name: psreload
    command: '& "$HOME/reload.ps1" ` + "`" + `-Force'
    description: "backtick and dollar expansion must stay literal"
    shells: [powershell]
    platforms: [windows]
`

// seedServerAliases creates every profile doc declares, then every alias,
// directly against the store — the same persistence internal/api's own
// CRUD handlers would perform, without going through HTTP for the setup
// step (the assertion this test exists to make is about the READ path,
// GET /api/v1/sync, which every alias below still crosses for real).
func seedServerAliases(t *testing.T, ts *testServer, doc config.AliasesDocument) {
	t.Helper()
	ctx := context.Background()

	for _, name := range doc.Profiles {
		if _, err := ts.Store.Profiles().Create(ctx, domain.Profile{ID: name, Name: name}); err != nil {
			t.Fatalf("seeding profile %q: %v", name, err)
		}
	}
	for _, a := range doc.Aliases {
		if _, err := ts.Store.Aliases().Create(ctx, a); err != nil {
			t.Fatalf("seeding alias %q: %v", a.Name, err)
		}
	}
}

// TestByteIdentityAcrossFileAndServerSources is Milestone 4's headline
// proof (proposal.md success criterion 2; task 9.1): the same aliases,
// taken through two independent paths —
//
//	Path A: aliases.yaml -> source.FileSource -> domain.Resolve -> renderers.Render
//	Path B: the same aliases in a server store -> GET /api/v1/sync (real
//	        HTTP, real JSON, real SQLite) -> source.ServerSource -> renderers.Render
//
// — must produce byte-identical rendered output and an identical
// ComputeRevision. Path B crosses a real httptest.Server running the
// actual production router (api.NewRouter, the same handler
// internal/server.Run installs), not a scripted fake, and a real SQLite
// database in t.TempDir(), not an in-memory stand-in.
//
// Two devices are compared, on purpose: one POSIX (zsh) and one
// PowerShell, since the two renderers escape completely differently and
// PowerShell is exactly where this project already shipped a real
// injection (a bare "}" closing a generated function block early).
//
// Mutation this test detects, verified directly against four separate
// production edits to internal/api/sync.go (each applied, confirmed to
// fail, then reverted — see apply-progress for every verbatim before/after
// transcript):
//
//  1. Appending one extra space to every alias's wire Command: caught
//     earlier than this test's own comparison even runs — ServerSource's
//     own defense-in-depth (source.server.go's toResolvedConfig hard-fails
//     on a revision it recomputes from the wire aliases not matching the
//     server-declared Revision) rejects the response outright, since
//     Command is part of ComputeRevision. ServerSource.Resolve itself
//     returned an error.
//  2. Adding two leading/trailing spaces to every alias's wire
//     Description: caught the identical way, for the identical reason —
//     Description is also part of ComputeRevision, whitespace and all.
//  3. Appending "-mutated" to the wire response's Device.Name, leaving
//     every alias and the Revision field untouched: THIS is what this
//     test's own byte comparison exists for. ComputeRevision deliberately
//     excludes Device.Name, so both sides' Revision still matched exactly
//     — only the rendered header line differed. This is the precise
//     failure mode "compare bytes, not structs" (this test's own design)
//     is built to catch, and the one place the revision check alone would
//     have let a real difference through silently.
//  4. Adding a compile-time "id" field to the wire syncAlias struct: not
//     caught by this test at all (rendered bytes and revision both stayed
//     identical, since neither a renderer nor ComputeRevision ever reads
//     an alias id) — but IS caught by internal/api/sync_test.go's
//     TestSyncResolvesAndRespondsWithTheDesignatedShape and its golden
//     fixture, confirmed by actually running that suite against the same
//     edit. Reported honestly rather than silently assumed: this test's
//     byte/revision comparison is not the mechanism that guards against a
//     server leaking an alias id — the wire-shape test is, and that is
//     exactly the layer it exists at.
//
// A fifth attempt — reversing the wire alias order — passed both this
// test AND internal/api's entire existing suite unchanged: domain.Resolve,
// ComputeRevision and both renderers all independently re-sort by name, so
// response ordering is invisible everywhere it was checked. This is a real
// gap in what any test in this milestone verifies about order, reported
// here rather than silently assumed to be covered.
func TestByteIdentityAcrossFileAndServerSources(t *testing.T) {
	doc, err := config.ParseAliases([]byte(fixtureYAML))
	if err != nil {
		t.Fatalf("config.ParseAliases(fixtureYAML): %v", err)
	}

	dir := t.TempDir()
	aliasesPath := filepath.Join(dir, "aliases.yaml")
	if err := os.WriteFile(aliasesPath, []byte(fixtureYAML), 0o600); err != nil {
		t.Fatalf("writing fixture aliases.yaml: %v", err)
	}

	ts := newTestServer(t)
	seedServerAliases(t, ts, doc)
	sessionToken := ts.login(t, harnessAdminPassword)

	cases := []struct {
		name              string
		dev               domain.Device
		enrollProfileIDs  []string
		wantEscapeInFile  string // a substring only present if escaping actually ran
		wantMinAliasCount int
	}{
		{
			name: "posix-zsh-with-ops-profile",
			// Name is set explicitly, and identically, on both paths below
			// (FileSource reads it straight from this struct; ServerSource's
			// registration call is what assigns the server-side device its
			// own name) — renderers/header.go emits Device.Name in the
			// header when non-empty, so a byte-identity comparison across
			// the two paths requires the same name on both, exactly like it
			// requires the same platform and shell.
			dev:               domain.Device{Name: "macbook-pro", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh, ProfileIDs: []string{"ops"}},
			enrollProfileIDs:  []string{"ops"},
			wantEscapeInFile:  `'\''`, // quotePOSIX's escaped-single-quote sequence
			wantMinAliasCount: 4,      // gitwip, portscan, reload, chained
		},
		{
			name:              "windows-powershell-no-profile",
			dev:               domain.Device{Name: "gaming-pc", Platform: domain.PlatformWindows, Shell: domain.ShellPowerShell},
			enrollProfileIDs:  nil,
			wantEscapeInFile:  "`", // the literal backtick psreload's command carries
			wantMinAliasCount: 2,   // psgrep, psreload
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			fileSrc := source.FileSource{Path: aliasesPath}
			localResolved, err := fileSrc.Resolve(ctx, tc.dev)
			if err != nil {
				t.Fatalf("FileSource.Resolve: %v", err)
			}
			if len(localResolved.Aliases) < tc.wantMinAliasCount {
				t.Fatalf("FileSource resolved %d aliases, want at least %d — the comparison below would be too weak to mean anything",
					len(localResolved.Aliases), tc.wantMinAliasCount)
			}

			localRendered, err := renderers.Render(localResolved)
			if err != nil {
				t.Fatalf("renderers.Render(FileSource result): %v", err)
			}
			if !strings.Contains(localRendered, tc.wantEscapeInFile) {
				t.Fatalf("rendered output does not contain the expected escape sequence %q — the fixture is not exercising escaping:\n%s",
					tc.wantEscapeInFile, localRendered)
			}

			enrollmentToken := ts.mintEnrollmentToken(t, sessionToken, tc.enrollProfileIDs)
			_, deviceToken := ts.registerDevice(t, enrollmentToken, tc.dev.Name, tc.dev.Platform, tc.dev.Shell)

			serverSrc := &source.ServerSource{URL: ts.URL, Token: deviceToken}
			serverResolved, err := serverSrc.Resolve(ctx, tc.dev)
			if err != nil {
				t.Fatalf("ServerSource.Resolve: %v", err)
			}

			serverRendered, err := renderers.Render(serverResolved)
			if err != nil {
				t.Fatalf("renderers.Render(ServerSource result): %v", err)
			}

			if localResolved.Revision != serverResolved.Revision {
				t.Fatalf("ComputeRevision mismatch for %s:\n   file=%q\n server=%q", tc.name, localResolved.Revision, serverResolved.Revision)
			}
			if got, want := serverResolved.ComputeRevision(), localResolved.ComputeRevision(); got != want {
				t.Fatalf("recomputed ComputeRevision mismatch for %s:\n   file=%q\n server=%q", tc.name, want, got)
			}
			if localRendered != serverRendered {
				t.Fatalf("rendered bytes differ between FileSource and ServerSource for %s:\n--- file (%d bytes) ---\n%s\n--- server (%d bytes) ---\n%s",
					tc.name, len(localRendered), localRendered, len(serverRendered), serverRendered)
			}
		})
	}
}
