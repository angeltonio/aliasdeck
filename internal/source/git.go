package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/validate"
)

// ResolveInfo reports what a GitSource's most recent Resolve call actually
// did, so a caller can record staleness without ConfigSource.Resolve itself
// growing a return value that every future ConfigSource (ServerSource,
// Milestone 4) would have to carry too (design decision 14; PROJECT.md §7's
// ConfigSource signature is verbatim and shared).
type ResolveInfo struct {
	// Ref is the ref that was actually resolved: the configured
	// source.git.ref, or "HEAD" when the remote's default branch was used.
	Ref string
	// Commit is the full resolved commit SHA, or empty when it could not be
	// determined (e.g. rev-parse itself failed after an otherwise
	// successful fetch/reset).
	Commit string
	// FetchedAt is when this checkout was last actually fetched from the
	// remote — not necessarily "now": a stale resolution reports the time
	// of the last successful fetch, not the time of the failed attempt.
	FetchedAt time.Time
	// Stale is true when this Resolve call could not reach the remote and
	// fell back to the last successful checkout instead.
	Stale bool
}

// ResolveReporter is an additive, optional interface: a ConfigSource that
// can report what its last Resolve call did. FileSource does not implement
// it — there is nothing to be stale about. syncWithContext type-asserts for
// it rather than widening ConfigSource.Resolve's signature (design
// decision 14).
type ResolveReporter interface {
	LastResolve() ResolveInfo
}

// gitRunFunc is GitSource's unit-test seam (design decision 12): every git
// invocation goes through it as an explicit argv; dir is the directory git
// is told to -C into. It is never handed to a shell.
type gitRunFunc func(ctx context.Context, dir string, args ...string) ([]byte, error)

// GitSource resolves a device's aliases from a Git repository, cloned or
// fetched into a local cache (PROJECT.md §7; design decisions 11-16).
//
// Its methods have pointer receivers: a successful Resolve records what it
// did (the resolved commit, fetch time, staleness) so a later
// LastResolve/Descriptor call can report it. Callers must therefore use
// *GitSource, not a GitSource value, wherever it is stored as a
// ConfigSource.
type GitSource struct {
	// URL is the repository to clone/fetch. It is hostile input (design
	// decision 15): a leading "-" or the ext:: transport are rejected
	// before any git process ever runs.
	URL string
	// Ref is the branch, tag or commit to resolve. Empty means the
	// remote's default branch (design decision 13).
	Ref string
	// Path is the location of aliases.yaml relative to the checkout root.
	// Empty means the checkout root itself (design decision 16).
	Path string
	// CacheDir is this source's checkout directory. GitCacheDir computes
	// the value design decision 11 requires; callers should use it rather
	// than deriving their own.
	CacheDir string
	// Run executes one git invocation. Nil defaults to RunGit, the real
	// subprocess implementation; tests inject a fake that records argv and
	// never touches the network.
	Run gitRunFunc

	last ResolveInfo
}

// GitCacheDir returns the hashed cache directory design decision 11
// requires: a directory name derived from url cannot traverse, collide, or
// leak a credential-bearing URL onto disk.
func GitCacheDir(base, url string) string {
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(base, "cache", "git", hex.EncodeToString(sum[:])[:12])
}

// GitAliasesPath resolves where aliases.yaml lives inside a GitSource
// checkout rooted at cacheDir, given the optional source.git.path relPath
// (design decision 16). It is a pure, filesystem-independent computation —
// callable before any checkout exists — so a caller can name the intended
// path, or reject an escaping one, without ever touching disk.
func GitAliasesPath(cacheDir, relPath string) (string, error) {
	if relPath == "" {
		return filepath.Join(cacheDir, "aliases.yaml"), nil
	}

	joined := filepath.Join(cacheDir, relPath)
	rel, err := filepath.Rel(cacheDir, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source.git.path %q escapes the checkout", relPath)
	}
	return joined, nil
}

// ShortSHA truncates a git commit SHA to the short form used in
// Descriptor.Ref and state.State.SourceRef (design decision 14).
func ShortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// Descriptor identifies this source for `status`. Before the first
// successful Resolve, Ref names only the configured (or default) branch;
// afterwards it includes the resolved commit (design decision 14).
func (s *GitSource) Descriptor() Descriptor {
	ref := s.Ref
	if ref == "" {
		ref = "HEAD"
	}
	r := s.URL + "#" + ref
	if s.last.Commit != "" {
		r += "@" + ShortSHA(s.last.Commit)
	}
	return Descriptor{Type: "git", Ref: r}
}

// LastResolve implements ResolveReporter.
func (s *GitSource) LastResolve() ResolveInfo { return s.last }

// Resolve implements ConfigSource (design decisions 11-16).
//
// It never returns a populated config alongside an error (config-source
// spec, "Resolve error is not partially applied"): every early return uses
// the zero domain.ResolvedConfig.
func (s *GitSource) Resolve(ctx context.Context, dev domain.Device) (domain.ResolvedConfig, error) {
	if err := validateGitURL(s.URL); err != nil {
		return domain.ResolvedConfig{}, fmt.Errorf("git source %s: %w", s.URL, err)
	}

	run := s.Run
	if run == nil {
		run = RunGit
	}

	hadCheckout, err := dirExists(filepath.Join(s.CacheDir, ".git"))
	if err != nil {
		return domain.ResolvedConfig{}, fmt.Errorf("git source %s: checking cache: %w", s.URL, err)
	}

	fetchErr := s.fetchOrClone(ctx, run, hadCheckout)
	stale := false
	if fetchErr != nil {
		if !hadCheckout {
			return domain.ResolvedConfig{}, fmt.Errorf(
				"git source %s: %w (no prior checkout to fall back to)", s.URL, fetchErr)
		}
		stale = true
	}

	commit, _ := s.resolveCommit(ctx, run)
	resolvedRef := s.Ref
	if resolvedRef == "" {
		resolvedRef = "HEAD"
	}
	s.last = ResolveInfo{
		Ref:       resolvedRef,
		Commit:    commit,
		FetchedAt: s.checkoutFetchedAt(),
		Stale:     stale,
	}

	path, err := GitAliasesPath(s.CacheDir, s.Path)
	if err != nil {
		return domain.ResolvedConfig{}, fmt.Errorf("git source %s: %w", s.URL, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return domain.ResolvedConfig{}, fmt.Errorf("reading %s: %w", path, err)
	}

	doc, err := config.ParseAliases(data)
	if err != nil {
		return domain.ResolvedConfig{}, fmt.Errorf("parsing %s: %w", path, err)
	}

	resolved := domain.Resolve(dev, doc.Aliases)
	filtered, _ := validate.FilterValid(resolved)
	return filtered, nil
}

// fetchOrClone brings CacheDir up to date and resets its worktree to the
// resolved ref (design decisions 12, 13). It never commits or pushes
// (config-source spec, "GitSource Is Read-Only in v0.2").
func (s *GitSource) fetchOrClone(ctx context.Context, run gitRunFunc, hadCheckout bool) error {
	if !hadCheckout {
		parent := filepath.Dir(s.CacheDir)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("creating cache directory: %w", err)
		}
		if _, err := run(ctx, parent, "clone", "--quiet", "--", s.URL, s.CacheDir); err != nil {
			return fmt.Errorf("cloning: %w", err)
		}
	} else {
		if _, err := run(ctx, s.CacheDir, "fetch", "--quiet", "--prune", "origin"); err != nil {
			return fmt.Errorf("fetching: %w", err)
		}
	}

	if s.Ref != "" {
		if _, err := run(ctx, s.CacheDir, "reset", "--hard", s.Ref); err != nil {
			return fmt.Errorf("resetting to %s: %w", s.Ref, err)
		}
		return nil
	}

	if _, err := run(ctx, s.CacheDir, "remote", "set-head", "origin", "--auto"); err != nil {
		return fmt.Errorf("resolving the default branch: %w", err)
	}
	if _, err := run(ctx, s.CacheDir, "reset", "--hard", "refs/remotes/origin/HEAD"); err != nil {
		return fmt.Errorf("resetting to refs/remotes/origin/HEAD: %w", err)
	}
	return nil
}

// resolveCommit reads the checkout's current HEAD. A failure here is not
// fatal to Resolve — it only means ResolveInfo.Commit stays empty and the
// reported ref omits the short SHA; the aliases themselves have already
// been correctly checked out by fetchOrClone's reset.
func (s *GitSource) resolveCommit(ctx context.Context, run gitRunFunc) (string, error) {
	out, err := run(ctx, s.CacheDir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// checkoutFetchedAt reports when CacheDir was last actually fetched, using
// .git/FETCH_HEAD's modification time — written by both `git clone` and
// `git fetch` — rather than time.Now(), so a stale resolution (whose fetch
// just failed) reports the time of the last successful fetch, not the time
// of the failed attempt.
func (s *GitSource) checkoutFetchedAt() time.Time {
	info, err := os.Stat(filepath.Join(s.CacheDir, ".git", "FETCH_HEAD"))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// validateGitURL rejects the two hostile shapes design decision 15 calls
// out: a leading "-" would be read as a git flag rather than a URL, and the
// ext:: transport runs an arbitrary command by design.
func validateGitURL(url string) error {
	if url == "" {
		return fmt.Errorf("source.git.url is required")
	}
	if strings.HasPrefix(url, "-") {
		return fmt.Errorf("url %q starts with '-', which git would read as a flag, not a URL", url)
	}
	if strings.HasPrefix(strings.ToLower(url), "ext::") {
		return fmt.Errorf("url %q uses the ext:: transport, which runs an arbitrary command and is refused", url)
	}
	return nil
}

// dirExists reports whether path exists and is a directory, distinguishing
// "absent" from "unreadable" so the latter is surfaced as an error rather
// than silently treated as a fresh checkout.
func dirExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.IsDir(), nil
}
