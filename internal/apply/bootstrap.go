package apply

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// beginMarker and endMarker delimit AliasDeck's own block inside a user's rc
// file. They are the whole contract: everything between and including them
// belongs to AliasDeck, everything outside them belongs to the user and MUST
// never be touched (native-apply spec, "Non-Destructive to User Files").
const (
	beginMarker = "# >>> aliasdeck >>>"
	endMarker   = "# <<< aliasdeck <<<"
)

// BootstrapLine returns the sourcing line for generatedPath: a POSIX `[ -f
// ... ] && . ...` guard that works unmodified in both bash and zsh, even in
// `sh` compatibility mode, using `.` rather than `source` (design decision 6).
//
// When generatedPath is under home, the line uses a literal "$HOME"-relative
// form instead of the expanded absolute path, so the same rc file keeps
// working if the account or machine changes.
func BootstrapLine(generatedPath, home string) string {
	display := generatedPath
	if home != "" {
		if rel, ok := strings.CutPrefix(generatedPath, home); ok && (rel == "" || rel[0] == '/') {
			display = "$HOME" + rel
		}
	}
	return fmt.Sprintf(`[ -f %q ] && . %q`, display, display)
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
func AddBootstrap(rcPath, generatedPath, home string) (block string, err error) {
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

	block = buildBlock(existing, BootstrapLine(generatedPath, home))

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

// buildBlock returns the exact bytes AddBootstrap appends after existing:
// padding + separator + begin + "\n" + line + "\n" + end + "\n" (design
// decision 6). padding is a lone "\n" only when existing has content but no
// trailing newline; separator is a lone "\n" only when existing is
// non-empty. An empty file gets no leading blank line at all.
func buildBlock(existing []byte, line string) string {
	var b strings.Builder

	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		b.WriteString("\n")
	}
	if len(existing) > 0 {
		b.WriteString("\n")
	}
	b.WriteString(beginMarker)
	b.WriteString("\n")
	b.WriteString(line)
	b.WriteString("\n")
	b.WriteString(endMarker)
	b.WriteString("\n")

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
	// caller happens to have the exact recorded block.
	if beginIdx >= 1 && content[beginIdx-1] == '\n' &&
		(beginIdx == 1 || content[beginIdx-2] == '\n') {
		beginIdx--
	}

	lineEnd := endIdx + len(endMarker)
	if lineEnd < len(content) && content[lineEnd] == '\n' {
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
		atLineEnd := end == len(content) || content[end] == '\n'

		if atLineStart && atLineEnd {
			return idx
		}
		offset = idx + 1
	}
}
