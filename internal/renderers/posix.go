package renderers

import (
	"slices"
	"strings"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// posixRenderer produces `alias name='command'` lines for bash and zsh.
//
// The two shells share alias syntax and quoting rules completely, so they share
// an implementation and differ only in which file they are written to and how
// the header identifies them. They stay separate registry entries because that
// equivalence is a fact about today's feature set, not a guarantee.
type posixRenderer struct {
	shell domain.Shell
}

func (r posixRenderer) Shell() domain.Shell { return r.shell }

func (r posixRenderer) Render(cfg domain.ResolvedConfig) (string, error) {
	if err := guard(cfg); err != nil {
		return "", err
	}

	aliases := slices.Clone(cfg.Aliases)
	domain.SortAliases(aliases)

	var b strings.Builder
	writeHeader(&b, "#", cfg, len(aliases))

	for _, a := range aliases {
		b.WriteString("\n")
		if desc := sanitizeComment(a.Description); desc != "" {
			b.WriteString("# ")
			b.WriteString(desc)
			b.WriteString("\n")
		}
		b.WriteString("alias ")
		b.WriteString(a.Name)
		b.WriteString("=")
		b.WriteString(quotePOSIX(a.Command))
		b.WriteString("\n")
	}

	return b.String(), nil
}

// quotePOSIX wraps s in single quotes so the shell treats it as a literal.
//
// Inside single quotes every character is literal and no expansion happens,
// which is exactly what an alias body needs — the command should run as the
// user wrote it, not as the generated file's own shell decides to expand it.
//
// A single quote is the one character that cannot appear inside single quotes,
// so each occurrence closes the string, emits an escaped quote, and reopens:
//
//	don't  ->  'don'\''t'
func quotePOSIX(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sanitizeComment makes a string safe to emit inside a `#` comment.
//
// Validation already rejects control characters in descriptions, so this should
// never have anything to do. It runs anyway: a description that smuggles a
// newline past validation would close the comment and leave whatever follows as
// executable shell code, and defense in depth is cheap when the failure mode is
// arbitrary code execution.
func sanitizeComment(s string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(cleaned)
}
