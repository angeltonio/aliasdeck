package source

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// recordedCall captures one Run invocation for assertions, so a test can
// pin the exact argv sequence GitSource emits without spawning a real git
// process (design decision 12, "Run is the unit-test seam").
type recordedCall struct {
	dir  string
	args []string
}

var testDev = domain.Device{Platform: domain.PlatformLinux, Shell: domain.ShellBash}

const gitAliasesYAML = `version: 1
aliases:
  - name: gs
    command: git status
`

// seedCheckout fakes a prior successful clone/fetch by writing the files a
// real git operation would have produced, without ever running git — the
// injected Run only records argv, it never touches disk.
func seedCheckout(t *testing.T, cacheDir, aliasesContent string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(cacheDir, ".git"), 0o755); err != nil {
		t.Fatalf("seeding fake .git dir: %v", err)
	}
	if aliasesContent != "" {
		if err := os.WriteFile(filepath.Join(cacheDir, "aliases.yaml"), []byte(aliasesContent), 0o600); err != nil {
			t.Fatalf("seeding aliases.yaml: %v", err)
		}
	}
}

func assertCallSequence(t *testing.T, calls []recordedCall, want [][]string) {
	t.Helper()
	if len(calls) != len(want) {
		t.Fatalf("call sequence = %d calls, want %d: got %+v, want %+v", len(calls), len(want), calls, want)
	}
	for i, w := range want {
		if !reflect.DeepEqual(calls[i].args, w) {
			t.Errorf("call %d args = %v, want %v", i, calls[i].args, w)
		}
	}
}

// TestGitSourceClonesWhenNoCheckoutExists pins decision 12's clone branch
// and decision 13's implicit-ref default-branch resolution.
func TestGitSourceClonesWhenNoCheckoutExists(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")

	var calls []recordedCall
	run := func(_ context.Context, dir string, args ...string) ([]byte, error) {
		calls = append(calls, recordedCall{dir: dir, args: append([]string{}, args...)})
		if args[0] == "clone" {
			seedCheckout(t, cacheDir, gitAliasesYAML)
		}
		if args[0] == "rev-parse" {
			return []byte("abc123def4567890\n"), nil
		}
		return nil, nil
	}

	url := "https://example.com/dotfiles.git"
	s := &GitSource{URL: url, CacheDir: cacheDir, Run: run}

	cfg, err := s.Resolve(context.Background(), testDev)
	if err != nil {
		t.Fatalf("Resolve() returned an error: %v", err)
	}
	if len(cfg.Aliases) != 1 || cfg.Aliases[0].Name != "gs" {
		t.Fatalf("Resolve() aliases = %+v, want the single seeded alias", cfg.Aliases)
	}

	assertCallSequence(t, calls, [][]string{
		{"clone", "--quiet", "--", url, cacheDir},
		{"remote", "set-head", "origin", "--auto"},
		{"reset", "--hard", "refs/remotes/origin/HEAD"},
		{"rev-parse", "HEAD"},
	})
	if calls[0].dir != filepath.Dir(cacheDir) {
		t.Errorf("clone dir = %q, want the cache's not-yet-existing parent %q", calls[0].dir, filepath.Dir(cacheDir))
	}
	for _, c := range calls[1:] {
		if c.dir != cacheDir {
			t.Errorf("call %v ran with dir %q, want the cache dir %q", c.args, c.dir, cacheDir)
		}
	}
}

// TestGitSourceClonesWithExplicitRefSkipsDefaultBranchResolution pins that an
// explicit source.ref resets straight to it, without ever asking git to
// resolve the remote's default branch.
func TestGitSourceClonesWithExplicitRefSkipsDefaultBranchResolution(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")

	var calls []recordedCall
	run := func(_ context.Context, dir string, args ...string) ([]byte, error) {
		calls = append(calls, recordedCall{dir: dir, args: append([]string{}, args...)})
		if args[0] == "clone" {
			seedCheckout(t, cacheDir, gitAliasesYAML)
		}
		if args[0] == "rev-parse" {
			return []byte("deadbeefcafef00d\n"), nil
		}
		return nil, nil
	}

	url := "https://example.com/dotfiles.git"
	s := &GitSource{URL: url, Ref: "release", CacheDir: cacheDir, Run: run}

	if _, err := s.Resolve(context.Background(), testDev); err != nil {
		t.Fatalf("Resolve() returned an error: %v", err)
	}

	assertCallSequence(t, calls, [][]string{
		{"clone", "--quiet", "--", url, cacheDir},
		{"reset", "--hard", "release"},
		{"rev-parse", "HEAD"},
	})
}

// TestGitSourceFetchesWhenCheckoutExists pins decision 12's fetch branch:
// an existing .git means fetch + prune, never a second clone.
func TestGitSourceFetchesWhenCheckoutExists(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	seedCheckout(t, cacheDir, gitAliasesYAML)

	var calls []recordedCall
	run := func(_ context.Context, dir string, args ...string) ([]byte, error) {
		calls = append(calls, recordedCall{dir: dir, args: append([]string{}, args...)})
		if args[0] == "rev-parse" {
			return []byte("1111111111111111\n"), nil
		}
		return nil, nil
	}

	url := "https://example.com/dotfiles.git"
	s := &GitSource{URL: url, CacheDir: cacheDir, Run: run}

	if _, err := s.Resolve(context.Background(), testDev); err != nil {
		t.Fatalf("Resolve() returned an error: %v", err)
	}

	assertCallSequence(t, calls, [][]string{
		{"fetch", "--quiet", "--prune", "origin"},
		{"remote", "set-head", "origin", "--auto"},
		{"reset", "--hard", "refs/remotes/origin/HEAD"},
		{"rev-parse", "HEAD"},
	})
	for _, c := range calls {
		if c.dir != cacheDir {
			t.Errorf("call %v ran with dir %q, want the cache dir %q", c.args, c.dir, cacheDir)
		}
	}
}

// TestGitSourceHostileURLRejectedBeforeAnyExec is the RED test for the
// "Git repository selection" threat-matrix row: a URL git would read as a
// flag, or the ext:: transport (remote command execution by design), must
// never reach Run at all.
func TestGitSourceHostileURLRejectedBeforeAnyExec(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "leading dash is read as a flag", url: "-oProxyCommand=touch /tmp/pwned"},
		{name: "leading double-dash flag", url: "--upload-pack=touch /tmp/pwned"},
		{name: "ext:: transport executes a command", url: "ext::sh -c touch%20/tmp/pwned"},
		{name: "ext:: transport, mixed case", url: "Ext::sh -c touch%20/tmp/pwned"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			cacheDir := filepath.Join(root, "cache")

			called := false
			run := func(context.Context, string, ...string) ([]byte, error) {
				called = true
				return nil, nil
			}

			s := &GitSource{URL: tt.url, CacheDir: cacheDir, Run: run}
			cfg, err := s.Resolve(context.Background(), testDev)
			if err == nil {
				t.Fatal("Resolve() must reject a hostile URL")
			}
			if called {
				t.Error("Run was invoked for a hostile URL; it must be rejected before any exec")
			}
			if cfg.Revision != "" || len(cfg.Aliases) != 0 {
				t.Errorf("Resolve() returned a partially applied config on error: %+v", cfg)
			}
		})
	}
}

// TestGitSourceOfflineWithCacheResolvesStale pins the "Unreachable remote
// with a prior checkout" scenario (config-source spec, "GitSource Offline
// Behavior and Staleness"): a fetch failure with an existing checkout must
// not fail sync, and must be reported as stale via LastResolve.
func TestGitSourceOfflineWithCacheResolvesStale(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	seedCheckout(t, cacheDir, gitAliasesYAML)

	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "fetch" {
			return nil, errors.New("could not resolve host")
		}
		return nil, nil
	}

	s := &GitSource{URL: "https://example.com/dotfiles.git", CacheDir: cacheDir, Run: run}

	cfg, err := s.Resolve(context.Background(), testDev)
	if err != nil {
		t.Fatalf("Resolve() must fall back to the cached checkout instead of failing: %v", err)
	}
	if len(cfg.Aliases) != 1 || cfg.Aliases[0].Name != "gs" {
		t.Fatalf("Resolve() aliases = %+v, want the cached alias", cfg.Aliases)
	}

	info := s.LastResolve()
	if !info.Stale {
		t.Error("LastResolve().Stale = false, want true when the fetch failed but a checkout existed")
	}
}

// TestGitSourceOfflineWithoutCacheFailsHard pins the "Unreachable remote
// with no prior checkout" scenario: no partial state, a hard error naming
// the source.
func TestGitSourceOfflineWithoutCacheFailsHard(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")

	run := func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("could not resolve host")
	}

	url := "https://example.com/dotfiles.git"
	s := &GitSource{URL: url, CacheDir: cacheDir, Run: run}

	cfg, err := s.Resolve(context.Background(), testDev)
	if err == nil {
		t.Fatal("Resolve() must fail when the remote is unreachable and no checkout exists")
	}
	if !strings.Contains(err.Error(), url) {
		t.Errorf("error %q does not name the unreachable source %q", err, url)
	}
	if cfg.Revision != "" || len(cfg.Aliases) != 0 {
		t.Errorf("Resolve() returned a partially applied config on error: %+v", cfg)
	}
}

// TestGitSourcePathEscapingCheckoutRejected pins design decision 16 /
// config-source spec scenario "Path escaping the checkout is rejected":
// resolution must fail before any file outside the checkout is read.
func TestGitSourcePathEscapingCheckoutRejected(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	seedCheckout(t, cacheDir, "")

	// A file outside the checkout that Resolve must never read.
	canary := filepath.Join(root, "passwd")
	if err := os.WriteFile(canary, []byte("root:x:0:0"), 0o600); err != nil {
		t.Fatalf("seeding canary file: %v", err)
	}

	run := func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	s := &GitSource{URL: "https://example.com/dotfiles.git", Path: "../passwd", CacheDir: cacheDir, Run: run}

	cfg, err := s.Resolve(context.Background(), testDev)
	if err == nil {
		t.Fatal("Resolve() must reject a source.git.path that escapes the checkout")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error %q does not explain the path escapes the checkout", err)
	}
	if cfg.Revision != "" || len(cfg.Aliases) != 0 {
		t.Errorf("Resolve() returned a partially applied config on error: %+v", cfg)
	}
}

// TestGitSourcePathPresentResolvesRelativeToCheckoutRoot pins the positive
// counterpart: a well-formed relative source.git.path is read from inside
// the checkout.
func TestGitSourcePathPresentResolvesRelativeToCheckoutRoot(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	if err := os.MkdirAll(filepath.Join(cacheDir, ".git"), 0o755); err != nil {
		t.Fatalf("seeding fake .git dir: %v", err)
	}
	nested := filepath.Join(cacheDir, "config")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("seeding nested dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "aliases.yaml"), []byte(gitAliasesYAML), 0o600); err != nil {
		t.Fatalf("seeding nested aliases.yaml: %v", err)
	}

	run := func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	s := &GitSource{
		URL:      "https://example.com/dotfiles.git",
		Path:     "config/aliases.yaml",
		CacheDir: cacheDir,
		Run:      run,
	}

	cfg, err := s.Resolve(context.Background(), testDev)
	if err != nil {
		t.Fatalf("Resolve() returned an error: %v", err)
	}
	if len(cfg.Aliases) != 1 || cfg.Aliases[0].Name != "gs" {
		t.Fatalf("Resolve() aliases = %+v, want the alias from config/aliases.yaml", cfg.Aliases)
	}
}

// TestGitSourceResolveFiltersHostileInputIdenticallyToFileSource pins
// config-source spec's "Git-sourced hostile entry filtered identically"
// scenario: a Git-hosted aliases.yaml is exactly as hostile as a local one.
func TestGitSourceResolveFiltersHostileInputIdenticallyToFileSource(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	seedCheckout(t, cacheDir, `version: 1
aliases:
  - name: safe
    command: echo safe
  - name: "evil rm"
    command: rm -rf /
`)

	run := func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	s := &GitSource{URL: "https://example.com/dotfiles.git", CacheDir: cacheDir, Run: run}

	cfg, err := s.Resolve(context.Background(), testDev)
	if err != nil {
		t.Fatalf("Resolve() returned an error: %v", err)
	}
	if len(cfg.Aliases) != 1 || cfg.Aliases[0].Name != "safe" {
		t.Fatalf("Resolve() aliases = %+v, want only the safe alias", cfg.Aliases)
	}
}

// TestGitCacheDirIsHashedAndDeterministic pins design decision 11: a
// hashed segment cannot traverse, collide with another path, or leak a
// credential-bearing URL onto disk as a directory name.
func TestGitCacheDirIsHashedAndDeterministic(t *testing.T) {
	base := "/home/user/.config/aliasdeck"
	url := "https://user:token@example.com/dotfiles.git"

	got := GitCacheDir(base, url)
	again := GitCacheDir(base, url)
	if got != again {
		t.Errorf("GitCacheDir() is not deterministic: %q != %q", got, again)
	}
	if strings.Contains(got, "token") || strings.Contains(got, "example.com") {
		t.Errorf("GitCacheDir() leaks the URL onto disk: %q", got)
	}
	if strings.Contains(got, "..") {
		t.Errorf("GitCacheDir() must not be able to traverse: %q", got)
	}

	other := GitCacheDir(base, "https://example.com/other.git")
	if other == got {
		t.Error("GitCacheDir() collided for two different URLs")
	}

	wantPrefix := filepath.Join(base, "cache", "git")
	if !strings.HasPrefix(got, wantPrefix+string(filepath.Separator)) {
		t.Errorf("GitCacheDir() = %q, want it under %q", got, wantPrefix)
	}
}

// TestGitSourceDescriptorIncludesResolvedCommit pins design decision 14:
// Descriptor.Ref becomes <url>#<ref>@<short-sha> once a commit is known.
func TestGitSourceDescriptorIncludesResolvedCommit(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	seedCheckout(t, cacheDir, gitAliasesYAML)

	url := "https://example.com/dotfiles.git"
	s := &GitSource{URL: url, Ref: "main", CacheDir: cacheDir}

	before := s.Descriptor()
	if before.Type != "git" || before.Ref != url+"#main" {
		t.Errorf("Descriptor() before Resolve = %+v, want Ref %q", before, url+"#main")
	}

	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "rev-parse" {
			return []byte("0123456789abcdef0123456789abcdef01234567\n"), nil
		}
		return nil, nil
	}
	s.Run = run

	if _, err := s.Resolve(context.Background(), testDev); err != nil {
		t.Fatalf("Resolve() returned an error: %v", err)
	}

	after := s.Descriptor()
	want := url + "#main@" + "0123456789ab"
	if after.Ref != want {
		t.Errorf("Descriptor() after Resolve = %q, want %q", after.Ref, want)
	}
}
