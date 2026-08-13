package renderers

import (
	"slices"
	"strings"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// powershellRenderer produces PowerShell functions for aliases.
//
// It shares nothing with posixRenderer beyond the package-level writeHeader
// and sanitizeComment helpers (design decision 1): the grammars are
// unrelated, and the escaping mechanism below is the reason a separate type
// exists rather than a shared "quote and wrap" abstraction.
//
// One instance covers both PowerShell editions. Desktop (5.1) and Core (7.x)
// agree on function syntax, single-quote escaping and @args splatting, so
// there is no per-edition field — unlike domain.Shell, which does need to
// distinguish "powershell" from "zsh"/"bash" to pick this renderer at all.
type powershellRenderer struct{}

func (r powershellRenderer) Shell() domain.Shell { return domain.ShellPowerShell }

// Render emits each alias as a function that stores its command in a
// variable and compiles it with [scriptblock]::Create only inside the
// function body (PROJECT.md §6.3, §6.4).
//
// This indirection is the entire point. `function dps { docker ps @args }`
// would put the alias body directly inside the function's own code block: a
// command containing `}` closes that block early, and everything after it
// runs the moment the file is dot-sourced — not when the alias is called.
// Storing the command as data in $__aliasdeck_cmd and only ever compiling it
// keeps it a string until the caller explicitly invokes the function.
//
// @args appears twice, and both are load-bearing (design decision 2, §6.3):
//   - inside the compiled string, so the scriptblock's own parameter binding
//     sees the caller's arguments;
//   - at the invocation, so those arguments are actually passed in.
//
// Splatting at only one of the two positions compiles without error and
// looks equivalent, but silently discards every argument at call time — an
// alias for `git checkout` would then ignore the branch name entirely.
// TestRenderArgsForwardedTwice pins this in the unit suite, and the real-pwsh
// integration test proves it end to end.
func (r powershellRenderer) Render(cfg domain.ResolvedConfig) (string, error) {
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
		b.WriteString("function ")
		b.WriteString(a.Name)
		b.WriteString(" {\n")
		b.WriteString("    $__aliasdeck_cmd = ")
		b.WriteString(quotePowerShell(a.Command))
		b.WriteString("\n")
		b.WriteString("    & ([scriptblock]::Create($__aliasdeck_cmd + ' @args')) @args\n")
		b.WriteString("}\n")
	}

	return b.String(), nil
}

// quotePowerShell wraps s in single quotes so PowerShell treats it as a
// literal, with every embedded single quote doubled.
//
// PowerShell single-quoted strings have no backslash escape at all — `\'`
// terminates the string just like a bare `'` would, because `\` is not
// special inside them. Writing the quote character twice, back to back, is
// the only mechanism the language offers to represent one literal quote
// inside a single-quoted string, as shown here:
//
//	don't  ->  'don''t'
//
// This is the direct analogue of quotePOSIX's escaping, which closes the
// quote, inserts a backslash-escaped quote, and reopens it — produced by an
// entirely different mechanism because the two shells' quoting grammars do
// not correspond character for character.
func quotePowerShell(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
