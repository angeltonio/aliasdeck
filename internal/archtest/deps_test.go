package archtest

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/shelltest"
)

// registerGuardedSourcesWithTestCache makes every file this guard reasons
// about an input to the test, so a violation invalidates the cached result.
//
// Go caches a test's outcome and replays it while the test's inputs are
// unchanged. It knows about imports and about files the test opens, but this
// guard learns the dependency graph by shelling out to `go list -deps`, and a
// subprocess is invisible to the cache. Measured before this existed: with a
// forbidden import added to internal/store, `go test ./internal/archtest`
// printed "ok (cached)" while `go test -count=1` failed.
//
// Reading the sources is what closes that gap: the test log records every file
// opened during a run, and any edit to one of them invalidates the entry.
//
// A guard that the person breaking the rule does not see fail is not a guard.
func registerGuardedSourcesWithTestCache(t *testing.T) {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving module root: %v", err)
	}

	// Every package on either side of the boundary, plus the renderers the
	// server must never reach, plus the client binary itself (design
	// decision reversing the single-binary model, docs/WHAT-WE-ARE-BUILDING.md).
	guarded := []string{
		"internal/server", "internal/api", "internal/auth", "internal/store", "internal/sync",
		"internal/source", "internal/app", "internal/renderers", "internal/web", "cmd/aliasdeck",
	}

	for _, rel := range guarded {
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(rel)),
			func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() || !strings.HasSuffix(path, ".go") {
					return nil
				}
				_, readErr := os.ReadFile(path)
				return readErr
			})
		if err != nil {
			t.Fatalf("reading %s: %v", rel, err)
		}
	}
}

// modulePath is AliasDeck's module path, used to build fully-qualified
// package patterns for `go list`.
const modulePath = "github.com/angeltonio/aliasdeck"

// requireGo mirrors internal/shelltest.LookPath's skip/fail rule — a missing
// go toolchain is a skip on a contributor's machine, but shelltest.RequireEnv
// promises a CI run that never silently declines to exercise this guard —
// while keeping wording accurate to what is actually missing (a Go
// toolchain, not a shell binary).
func requireGo(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("go"); err == nil {
		return
	}

	if os.Getenv(shelltest.RequireEnv) != "" {
		t.Fatalf("go is not installed but %s is set: this environment promised to run the "+
			"architecture guard and must not skip it", shelltest.RequireEnv)
	}

	t.Skip("go is not installed on this machine")
}

// listPackages resolves one or more `go list` package patterns (e.g.
// "<modulePath>/internal/server/...") to the set of import paths they match.
//
// It shells out rather than using go/packages so this guard has no import on
// tooling libraries of its own — the assertion is exactly what a contributor
// or CI would see running `go list` by hand.
func listPackages(t *testing.T, patterns ...string) []string {
	t.Helper()

	args := append([]string{"list"}, patterns...)
	out, err := exec.Command("go", args...).Output()
	if err != nil {
		t.Fatalf("go %s: %v", strings.Join(args, " "), err)
	}

	fields := strings.Fields(string(out))
	sort.Strings(fields)
	return fields
}

// depsOf returns the transitive import list of pkg — the package itself,
// every internal package it imports, and every third-party/stdlib package in
// between — including the imports of its test files.
//
// The -test flag is load-bearing. Without it `go list -deps` reports only a
// package's non-test sources, so a forbidden import placed in a _test.go file
// is invisible and this guard passes. Measured: a blank import of
// internal/renderers in internal/store/leak_probe_test.go produced a clean
// `go list -deps` and a green guard, and appeared immediately under -test.
//
// A test file is not a lesser place to break the boundary. It compiles against
// the same package, and an import that exists only to make a test convenient
// is exactly how a rule starts eroding — someone reaches for renderers.Render
// to build an expectation, and the next person moves that helper into
// production code because it was already there.
//
// This is the second hole found in this guard, after the test-cache replay the
// comment on registerGuardedSourcesWithTestCache describes. Both had the same
// shape: the guard could not observe the violation it was named for.
func depsOf(t *testing.T, pkg string) []string {
	t.Helper()

	out, err := exec.Command("go", "list", "-deps", "-test", pkg).Output()
	if err != nil {
		t.Fatalf("go list -deps -test %s: %v", pkg, err)
	}

	return strings.Fields(string(out))
}

// hasForbidden reports whether deps contains forbidden, or a subpackage of
// it (e.g. "modernc.org/sqlite/lib" counts as depending on
// "modernc.org/sqlite").
func hasForbidden(deps []string, forbidden string) bool {
	for _, dep := range deps {
		if dep == forbidden || strings.HasPrefix(dep, forbidden+"/") {
			return true
		}
	}
	return false
}

// TestServerPackagesNeverImportRenderers is design decision 2's mechanism:
// no package under internal/{server,api,auth,store,sync} may depend on
// internal/renderers. The server transmits data; the client produces shell
// syntax (docs/PROJECT.md §3.7/§6.1). A code-review convention is not a
// mechanism — this import-graph assertion fails the build the first time
// someone reaches for renderers.Render in a handler.
func TestServerPackagesNeverImportRenderers(t *testing.T) {
	requireGo(t)
	registerGuardedSourcesWithTestCache(t)

	const forbidden = modulePath + "/internal/renderers"

	pkgs := listPackages(t,
		modulePath+"/internal/server/...",
		modulePath+"/internal/api/...",
		modulePath+"/internal/auth/...",
		modulePath+"/internal/store/...",
		modulePath+"/internal/sync/...",
	)
	if len(pkgs) == 0 {
		t.Fatal("expected internal/{server,api,auth,store,sync} to resolve to at least one package; did the skeleton doc.go files disappear?")
	}

	for _, pkg := range pkgs {
		t.Run(pkg, func(t *testing.T) {
			deps := depsOf(t, pkg)
			if hasForbidden(deps, forbidden) {
				t.Fatalf("%s depends on %s: the server transmits data, the client produces shell syntax (design decision 2) — no server package may import internal/renderers", pkg, forbidden)
			}
		})
	}
}

// TestClientPackagesNeverImportServerPersistence guards the other half of
// the same boundary: internal/source and internal/app — the client's config
// resolution and CLI use cases — must never depend on internal/store or on
// modernc.org/sqlite. A standalone (FileSource/GitSource) install has no
// business linking the server's database layer at all.
func TestClientPackagesNeverImportServerPersistence(t *testing.T) {
	requireGo(t)
	registerGuardedSourcesWithTestCache(t)

	forbidden := []string{
		modulePath + "/internal/store",
		"modernc.org/sqlite",
	}

	pkgs := listPackages(t,
		modulePath+"/internal/source/...",
		modulePath+"/internal/app/...",
	)
	if len(pkgs) == 0 {
		t.Fatal("expected internal/{source,app} to resolve to at least one package")
	}

	for _, pkg := range pkgs {
		t.Run(pkg, func(t *testing.T) {
			deps := depsOf(t, pkg)
			for _, f := range forbidden {
				if hasForbidden(deps, f) {
					t.Fatalf("%s depends on %s: internal/source and internal/app must never depend on server persistence (design decision 2)", pkg, f)
				}
			}
		})
	}
}

// TestClientBinaryNeverImportsServerPackages is the mechanism behind
// AliasDeck shipping two binaries instead of one (design decision reversing
// the earlier single-binary model, docs/WHAT-WE-ARE-BUILDING.md): cmd/
// aliasdeck — the binary Homebrew and Scoop distribute — must never depend
// on internal/store, internal/api, internal/server, internal/sync,
// internal/web, or modernc.org/sqlite. Every one of those is either the
// server's persistence layer, its transport/composition root, or the web UI
// mounted only inside that composition root; cmd/aliasdeck-server is the
// only binary allowed to link them. internal/web never appears in
// cmd/aliasdeck-server's own dependency list here either — this asserts the
// client side of the same boundary, so a page handler pulled in for
// convenience fails the build the same day, not merely a code-review
// comment. A convention that the client "should not" import server code
// erodes the first time it is merely inconvenient — this assertion fails
// the build instead, the same day someone reaches for server.Config to wire
// a new client feature. This is what keeps the client at its measured
// 6.6 MB instead of drifting back toward the 11.7 MB combined binary that
// existed before the split.
func TestClientBinaryNeverImportsServerPackages(t *testing.T) {
	requireGo(t)
	registerGuardedSourcesWithTestCache(t)

	forbidden := []string{
		modulePath + "/internal/store",
		modulePath + "/internal/api",
		modulePath + "/internal/server",
		modulePath + "/internal/sync",
		modulePath + "/internal/web",
		"modernc.org/sqlite",
	}

	pkgs := listPackages(t, modulePath+"/cmd/aliasdeck/...")
	if len(pkgs) == 0 {
		t.Fatal("expected cmd/aliasdeck to resolve to at least one package")
	}

	for _, pkg := range pkgs {
		t.Run(pkg, func(t *testing.T) {
			deps := depsOf(t, pkg)
			for _, f := range forbidden {
				if hasForbidden(deps, f) {
					t.Fatalf("%s depends on %s: cmd/aliasdeck is the client binary and must never link server persistence or transport packages — that belongs to cmd/aliasdeck-server", pkg, f)
				}
			}
		})
	}
}
