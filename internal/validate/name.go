package validate

import (
	"fmt"
	"strings"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// validNameRunes reports whether every rune in name is allowed: an ASCII letter
// or underscore first, then letters, digits, underscores, dots or hyphens.
//
// Real shells are far more permissive than this. Bash will happily accept an
// alias named `a'b` or one containing a newline, and that is precisely the
// problem: a name is written outside the quoted region of the generated line,
// so anything able to terminate that construct escapes into executable code.
// Rather than trying to escape names, AliasDeck refuses to write the ones that
// would need escaping.
//
// Implemented by hand instead of with regexp so this stays allocation-free and
// obvious; the rule is small enough that a regular expression would obscure it.
func validNameRunes(name string) bool {
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
			// Always allowed.
		case r >= '0' && r <= '9', r == '.', r == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// Name validates an alias name for the given shell.
func Name(name string, sh domain.Shell) error {
	if name == "" {
		return fmt.Errorf("name is empty")
	}
	if countRunes(name) > MaxNameLen {
		return fmt.Errorf("name is longer than %d characters", MaxNameLen)
	}
	if !validNameRunes(name) {
		return fmt.Errorf(
			"name %q contains characters that are not allowed; "+
				"use letters, digits, underscores, dots or hyphens, starting with a letter or underscore",
			name)
	}
	if word, reserved := isReservedWord(name, sh); reserved {
		return fmt.Errorf("name %q is a reserved word in %s", word, sh)
	}
	return nil
}

// posixReserved lists keywords that carry syntactic meaning in bash and zsh.
//
// Shadowing one of these does not merely override a command, it corrupts the
// grammar of every script that sources the generated file. An alias named `if`
// is not a bad idea, it is a broken shell.
var posixReserved = map[string]struct{}{
	"case": {}, "coproc": {}, "declare": {}, "do": {}, "done": {},
	"elif": {}, "else": {}, "end": {}, "esac": {}, "fi": {},
	"for": {}, "foreach": {}, "function": {}, "if": {}, "in": {},
	"nocorrect": {}, "repeat": {}, "select": {}, "then": {}, "time": {},
	"until": {}, "while": {},
}

// powershellReserved lists PowerShell language keywords. PowerShell matches
// these case-insensitively, so the lookup lowercases first.
var powershellReserved = map[string]struct{}{
	"begin": {}, "break": {}, "catch": {}, "class": {}, "continue": {},
	"data": {}, "define": {}, "do": {}, "dynamicparam": {}, "else": {},
	"elseif": {}, "end": {}, "enum": {}, "exit": {}, "filter": {},
	"finally": {}, "for": {}, "foreach": {}, "from": {}, "function": {},
	"hidden": {}, "if": {}, "in": {}, "param": {}, "process": {},
	"return": {}, "static": {}, "switch": {}, "throw": {}, "trap": {},
	"try": {}, "until": {}, "using": {}, "var": {}, "while": {},
}

func isReservedWord(name string, sh domain.Shell) (string, bool) {
	if sh == domain.ShellPowerShell {
		lower := strings.ToLower(name)
		_, found := powershellReserved[lower]
		return lower, found
	}
	_, found := posixReserved[name]
	return name, found
}
