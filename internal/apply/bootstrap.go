package apply

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// beginMarker and endMarker delimit AliasDeck's own block inside a user's rc
// file. They are the whole contract: everything between and including them
// belongs to AliasDeck, everything outside them belongs to the user and MUST
// never be touched (native-apply spec, "Non-Destructive to User Files").
const (
	beginMarker = "# >>> aliasdeck >>>"
	endMarker   = "# <<< aliasdeck <<<"
)

// BootstrapLine returns the shell integration block for generatedPath.
//
// For zsh and bash it installs an aliasdeck function that delegates to the
// binary and sources the generated aliases after a successful sync. For
// PowerShell it remains a `Test-Path`/dot-source guard using the same
// marker-delimited block mechanism (design decision 3).
//
// When generatedPath is under home, the line uses a literal "$HOME"-relative
// form instead of the expanded absolute path, so the same rc file keeps
// working if the account or machine changes. The relative computation uses
// filepath.Rel — never a hardcoded separator — and rejects any result that
// escapes home via ".." (design decision 4, fixing the previous
// strings.CutPrefix-based check, which assumed '/' and silently never fired
// on Windows).
func BootstrapLine(sh domain.Shell, generatedPath, home string) string {
	if sh == domain.ShellPowerShell {
		return bootstrapLinePowerShell(generatedPath, home)
	}
	display := homeRelativeDisplay(generatedPath, home)
	if sh == domain.ShellZsh {
		return fmt.Sprintf(`aliasdeck() {
  local aliasdeck_status=0
  command aliasdeck "$@" || aliasdeck_status=$?
  if [ "$aliasdeck_status" -eq 0 ] && [ "$#" -gt 0 ] && [ "$1" = sync ] && [ -f %q ]; then
    . %q
  fi
  return "$aliasdeck_status"
}
[ ! -f %q ] || . %q`, display, display, display, display)
	}
	return fmt.Sprintf(`aliasdeck() {
  local aliasdeck_status=0
  command aliasdeck "$@" || aliasdeck_status=$?
  if [ "$aliasdeck_status" -eq 0 ] && [ "$#" -gt 0 ] && [ "$1" = sync ] && [ -f %q ]; then
    . %q
  fi
  return "$aliasdeck_status"
}`, display, display)
}

// bootstrapLinePowerShell renders the PowerShell form of BootstrapLine
// (design decision 3): a `Test-Path -LiteralPath` guard followed by a
// dot-source, both operating on the same double-quoted path string.
//
// The double-quoted-context escaper (design decision 5) is applied only to
// the part of the path after a literal "$HOME/" prefix is prepended, so
// "$HOME" itself is left for PowerShell to expand as its automatic variable
// rather than being escaped into a literal "`$HOME".
func bootstrapLinePowerShell(generatedPath, home string) string {
	display := escapePowerShellDoubleQuoted(generatedPath)
	if home != "" {
		if rel, ok := relUnderHome(generatedPath, home); ok {
			display = "$HOME/" + escapePowerShellDoubleQuoted(filepath.ToSlash(rel))
		}
	}
	return fmt.Sprintf(`if (Test-Path -LiteralPath "%s") { . "%s" }`, display, display)
}

// homeRelativeDisplay is the POSIX-branch counterpart of
// bootstrapLinePowerShell's rewrite: same filepath.Rel-based logic (design
// decision 4), emitting a plain "$HOME/..." string with no additional
// escaping, since %q (Go's double-quote escaping) is applied by the caller
// and is what the existing POSIX byte-identical output already used.
func homeRelativeDisplay(generatedPath, home string) string {
	if home == "" {
		return generatedPath
	}
	rel, ok := relUnderHome(generatedPath, home)
	if !ok {
		return generatedPath
	}
	return "$HOME/" + filepath.ToSlash(rel)
}

// relUnderHome reports the path of generatedPath relative to home, and
// whether that relative path actually stays under home.
//
// It is the single separator-correct primitive behind design decision 4:
// filepath.Rel is OS-native (it will use '\' on a real Windows build and
// '/' everywhere else), so this function never assumes a separator itself.
// A result of ".." or one beginning with ".."+separator means generatedPath
// is not under home (or home is not actually an ancestor of it, e.g. the
// "/home/user" vs "/home/user2" prefix-collision case) and is rejected.
func relUnderHome(generatedPath, home string) (string, bool) {
	rel, err := filepath.Rel(home, generatedPath)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// escapePowerShellDoubleQuoted escapes s for use inside a PowerShell
// double-quoted string (design decision 5): a backtick doubles, a double
// quote doubles, and a dollar sign is backtick-escaped so it is not read as
// the start of a variable or subexpression. This is deliberately not
// fmt.Sprintf's %q (Go escaping): %q turns '\' into "\\", which PowerShell
// reads as two literal backslashes, and PowerShell has no backslash escape
// at all in this context.
func escapePowerShellDoubleQuoted(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '`':
			b.WriteString("``")
		case '"':
			b.WriteString(`""`)
		case '$':
			b.WriteString("`$")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// AddBootstrap inserts a marker-delimited block sourcing generatedPath into
// the rc file at rcPath, unless one is already present (native-apply spec,
// "Repeated init does not duplicate"). block is empty when Add is a no-op.
//
// rcPath is resolved through filepath.EvalSymlinks first, and the write
// targets the resolved path rather than renaming over rcPath itself: a
// dotfiles-managed ~/.zshrc is usually a symlink, and replacing the link
// would sever it from the dotfiles repository it belongs to (design
// decision 7).
//
// On success, block holds the exact bytes appended — including any leading
// padding or separator — ready to be stored verbatim (e.g. in
// state.Bootstrap.Block) so a later removal is a single bytes.Replace rather
// than a heuristic reconstruction (design decision 6).
func AddBootstrap(rcPath string, sh domain.Shell, generatedPath, home string) (block string, err error) {
	resolved, mode, err := resolveRCPath(rcPath)
	if err != nil {
		return "", err
	}

	existing, err := os.ReadFile(resolved)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("reading %s: %w", rcPath, err)
	}

	if bytes.Contains(existing, []byte(beginMarker)) {
		// Idempotent per the spec: any prior marker, even one this exact code
		// never produced, means AliasDeck considers the rc file already
		// bootstrapped.
		return "", nil
	}

	block = buildBlock(existing, BootstrapLine(sh, generatedPath, home), detectEOL(existing))

	updated := make([]byte, 0, len(existing)+len(block))
	updated = append(updated, existing...)
	updated = append(updated, []byte(block)...)

	if err := writeFileAtomic(resolved, updated, mode); err != nil {
		return "", fmt.Errorf("writing %s: %w", rcPath, err)
	}
	return block, nil
}

// RemoveBootstrap deletes block from the rc file at rcPath and reports
// whether the removal was byte-exact.
//
// block MUST be the exact bytes a prior AddBootstrap returned. When those
// bytes are found verbatim, RemoveBootstrap cuts exactly that span with a
// single replace and every other byte of the file is untouched (native-apply
// spec, "Uninstall Restores Byte-Identical Files"); exact is true.
//
// When the user edited inside the marker block, the exact bytes no longer
// appear anywhere in the file. RemoveBootstrap then falls back to scanning
// for the begin/end marker lines themselves and removes everything between
// and including them; exact is false, signaling that byte-identical
// restoration is no longer guaranteed and the caller should warn about it.
//
// If neither the exact block nor a well-formed marker pair is found — for
// example a hostile rc file that merely contains marker-like text inside an
// unrelated line — RemoveBootstrap leaves the file untouched and reports
// exact as true: there was nothing unsafe to do, so nothing was done.
func RemoveBootstrap(rcPath, block string) (exact bool, err error) {
	resolved, mode, err := resolveRCPath(rcPath)
	if err != nil {
		return false, err
	}

	existing, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("reading %s: %w", rcPath, err)
	}

	if block != "" {
		if idx := bytes.Index(existing, []byte(block)); idx >= 0 {
			updated := make([]byte, 0, len(existing)-len(block))
			updated = append(updated, existing[:idx]...)
			updated = append(updated, existing[idx+len(block):]...)
			if err := writeFileAtomic(resolved, updated, mode); err != nil {
				return false, fmt.Errorf("writing %s: %w", rcPath, err)
			}
			return true, nil
		}
	}

	updated, found := removeMarkerScan(existing)
	if !found {
		return true, nil
	}
	if err := writeFileAtomic(resolved, updated, mode); err != nil {
		return false, fmt.Errorf("writing %s: %w", rcPath, err)
	}
	return false, nil
}

// resolveRCPath resolves rcPath through filepath.EvalSymlinks so the caller
// writes onto the real target instead of replacing a symlink, and reports
// the mode to preserve on write.
//
// A missing rc file is not an error: it resolves to rcPath itself (nothing to
// follow yet) with the conventional default rc mode, so init can bootstrap a
// shell that has no rc file at all.
func resolveRCPath(rcPath string) (resolved string, mode os.FileMode, err error) {
	resolved, err = filepath.EvalSymlinks(rcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return rcPath, 0o644, nil
		}
		return "", 0, fmt.Errorf("resolving %s: %w", rcPath, err)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", 0, fmt.Errorf("stat %s: %w", resolved, err)
	}
	return resolved, info.Mode().Perm(), nil
}

// detectEOL reports the line-ending convention AddBootstrap must use for its
// own block (design decision 6): "\r\n" if and only if existing already
// contains one, else a plain "\n". It never depends on the rendering
// machine's OS, only on the rc file's own pre-existing bytes, so the same
// $PROFILE keeps whichever convention its owner (or PowerShell itself, which
// writes CRLF by default) already gave it.
func detectEOL(existing []byte) string {
	if bytes.Contains(existing, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

// buildBlock returns the exact bytes AddBootstrap appends after existing:
// padding + separator + begin + eol + line + eol + end + eol (design
// decision 6). padding is a lone eol only when existing has content but no
// trailing newline; separator is a lone eol only when existing is
// non-empty. An empty file gets no leading blank line at all. eol is
// detectEOL(existing), threaded in by the caller rather than recomputed
// here so every write in one AddBootstrap call agrees on it.
func buildBlock(existing []byte, line, eol string) string {
	var b strings.Builder

	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		b.WriteString(eol)
	}
	if len(existing) > 0 {
		b.WriteString(eol)
	}
	b.WriteString(beginMarker)
	b.WriteString(eol)
	b.WriteString(line)
	b.WriteString(eol)
	b.WriteString(endMarker)
	b.WriteString(eol)

	return b.String()
}

// removeMarkerScan is the documented fallback for a block whose exact bytes
// no longer appear in the file: it removes everything from the line
// containing beginMarker through the line containing endMarker, inclusive.
//
// It refuses to guess when the pair is not well-formed (missing, out of
// order, or embedded inside an unrelated line) rather than risk deleting
// content that only happens to mention marker-like text.
func removeMarkerScan(content []byte) (updated []byte, found bool) {
	beginIdx := indexOfLine(content, beginMarker)
	if beginIdx < 0 {
		return content, false
	}
	endIdx := indexOfLine(content, endMarker)
	if endIdx < beginIdx {
		return content, false
	}

	// If the marker block is preceded by a blank separator line (the common
	// case AddBootstrap itself produces), consume it too: this is what makes
	// the fallback byte-identical in the ordinary case, not only when a
	// caller happens to have the exact recorded block. stripTrailingEOL
	// accepts either a bare "\n" or a "\r\n" terminator (design decision 7),
	// so this works the same way on an LF or a CRLF rc file.
	if p, ok := stripTrailingEOL(content, beginIdx); ok {
		if _, ok2 := stripTrailingEOL(content, p); ok2 || p == 0 {
			beginIdx = p
		}
	}

	lineEnd := endIdx + len(endMarker)
	if lineEnd+1 < len(content) && content[lineEnd] == '\r' && content[lineEnd+1] == '\n' {
		lineEnd += 2
	} else if lineEnd < len(content) && content[lineEnd] == '\n' {
		lineEnd++
	}

	updated = make([]byte, 0, len(content)-(lineEnd-beginIdx))
	updated = append(updated, content[:beginIdx]...)
	updated = append(updated, content[lineEnd:]...)
	return updated, true
}

// indexOfLine returns the byte offset of marker when it appears as an entire
// line (start-of-line to end-of-line), or -1 if it never does.
//
// This is deliberately stricter than bytes.Index: marker-like text embedded
// inside an unrelated line — a comment mentioning it, a quoted string — must
// not be mistaken for AliasDeck's own block.
//
// atLineEnd accepts either a bare "\n" or a "\r\n" terminator (design
// decision 7): the original check required content[end] == '\n' exactly,
// which a CRLF-terminated marker line fails on the '\r' byte — a latent bug
// that AliasDeck's own LF-only markers never triggered, but that preserving
// a CRLF rc file's convention (decision 6) activates.
func indexOfLine(content []byte, marker string) int {
	m := []byte(marker)
	offset := 0
	for {
		i := bytes.Index(content[offset:], m)
		if i < 0 {
			return -1
		}
		idx := offset + i

		atLineStart := idx == 0 || content[idx-1] == '\n'
		end := idx + len(m)
		atLineEnd := end == len(content) || content[end] == '\n' ||
			(content[end] == '\r' && end+1 < len(content) && content[end+1] == '\n')

		if atLineStart && atLineEnd {
			return idx
		}
		offset = idx + 1
	}
}

// stripTrailingEOL reports the position immediately before the line-ending
// sequence ("\r\n" or "\n") ending at pos, and whether one was found there.
// It is the shared primitive behind removeMarkerScan's blank-separator-line
// and trailing-newline handling, generalized for CRLF (design decision 7).
func stripTrailingEOL(content []byte, pos int) (int, bool) {
	if pos >= 2 && content[pos-2] == '\r' && content[pos-1] == '\n' {
		return pos - 2, true
	}
	if pos >= 1 && content[pos-1] == '\n' {
		return pos - 1, true
	}
	return pos, false
}

// BootstrapTargets reports whether rcPath's existing AliasDeck block already
// sources generatedPath.
//
// AddBootstrap is idempotent by design: any prior marker means it leaves the
// file alone, which is right — a block a user hand-edited must not be
// clobbered. But "left alone" and "correct" are different claims, and only
// the caller can decide what to do about a block pointing somewhere else.
// Without this there was no way to tell them apart, so `init` reported having
// added a line while a stale block kept sourcing a different file.
//
// present is false when there is no block at all; matches is meaningless
// then.
func BootstrapTargets(rcPath string, sh domain.Shell, generatedPath, home string) (present, matches bool, err error) {
	resolved, _, err := resolveRCPath(rcPath)
	if err != nil {
		return false, false, err
	}

	existing, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("reading %s: %w", rcPath, err)
	}
	if !bytes.Contains(existing, []byte(beginMarker)) {
		return false, false, nil
	}

	// Compared against the line this run would have written, rather than by
	// parsing the block: the renderer is the only thing that knows how a path
	// is spelled for a given shell, including the $HOME rewrite.
	want := BootstrapLine(sh, generatedPath, home)
	return true, bytes.Contains(existing, []byte(want)), nil
}
