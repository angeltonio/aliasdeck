// Package importers translates an existing shell startup file into AliasDeck
// aliases. It is the inverse of internal/renderers, and it is deliberately a
// *parser*: it reads the text of an rc file and never executes a byte of it.
//
// That restriction is the project's own thesis applied to itself. AliasDeck
// refuses to let a server send shell code to a client because running code
// someone else wrote is the thing the whole design exists to avoid. The
// obvious way to discover a user's aliases is to source their .zshrc and ask
// the shell what it now knows — which is running an arbitrary program in
// order to read a configuration file, the same trade made in the other
// direction. So this package parses.
//
// The price is real and is paid deliberately: an alias assembled at run time
// (built in a loop, sourced from a file this one does not name, defined only
// inside an `if`) is invisible here. Missing such an alias is the correct
// failure. Executing the file to find it is not.
package importers

import "strings"

// beginMarker and endMarker delimit AliasDeck's own block inside a user's rc
// file. internal/apply owns them; they are duplicated here rather than
// exported because the coupling runs the wrong way — an importer that knows
// how to skip a block is not a reason to widen internal/apply's API. The
// duplication is guarded by a test that reads internal/apply's source.
const (
	beginMarker = "# >>> aliasdeck >>>"
	endMarker   = "# <<< aliasdeck <<<"
)

// Found is one alias definition literally present in the parsed file.
type Found struct {
	// Line is 1-based, so it can be quoted back at a user who wants to go
	// look at what was read.
	Line    int
	Name    string
	Command string
	// Note is set when the text was faithfully read but its *meaning* will
	// differ once AliasDeck renders it. Empty for the common case.
	Note string
}

// Skipped is one alias-looking line the parser refused to translate, with the
// reason. Nothing is ever dropped silently: an importer that quietly loses
// three of your forty aliases is worse than one that imports none, because
// you find out months later.
type Skipped struct {
	Line   int
	Text   string
	Reason string
}

// Result is everything one file yielded.
type Result struct {
	Found   []Found
	Skipped []Skipped
}

// ParsePOSIX reads the alias definitions out of a POSIX-family shell startup
// file (zsh, bash). It never fails: an unparseable line becomes a Skipped
// entry, because one malformed line in a 400-line .zshrc should not cost the
// user the other 399.
func ParsePOSIX(data []byte) Result {
	var res Result
	inOwnBlock := false

	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		lineNo := i + 1

		// AliasDeck's own bootstrap block is skipped before anything else.
		// Without this, importing the file AliasDeck already bootstrapped
		// would re-import AliasDeck's own output back into its input.
		if strings.HasPrefix(line, beginMarker) {
			inOwnBlock = true
			continue
		}
		if strings.HasPrefix(line, endMarker) {
			inOwnBlock = false
			continue
		}
		// No test for "" or a leading # here: afterAliasKeyword already
		// requires the line to *start* with the keyword, so a comment and a
		// blank line fall out there. A second check would read as
		// load-bearing while killing no mutant.
		if inOwnBlock {
			continue
		}

		rest, ok := afterAliasKeyword(line)
		if !ok {
			continue
		}

		rest, optErr := stripOptions(rest)
		if optErr != "" {
			res.Skipped = append(res.Skipped, Skipped{Line: lineNo, Text: line, Reason: optErr})
			continue
		}

		// `alias a=1 b=2` is one statement defining two aliases.
		for {
			rest = strings.TrimLeft(rest, " \t")
			// A trailing comment ends the statement.
			if rest == "" || strings.HasPrefix(rest, "#") {
				break
			}

			name, value, note, remainder, err := parseAssignment(rest)
			if err != "" {
				res.Skipped = append(res.Skipped, Skipped{Line: lineNo, Text: line, Reason: err})
				break
			}
			res.Found = append(res.Found, Found{Line: lineNo, Name: name, Command: value, Note: note})
			rest = remainder
		}
	}

	return res
}

// afterAliasKeyword returns what follows the `alias` keyword, or false if
// this line is not an alias statement at all.
func afterAliasKeyword(line string) (string, bool) {
	const kw = "alias"
	if !strings.HasPrefix(line, kw) {
		return "", false
	}
	rest := line[len(kw):]
	// A bare `alias` lists the shell's aliases; it defines nothing.
	if rest == "" {
		return "", false
	}
	// `aliases=(...)` is a variable whose name merely starts with "alias".
	if rest[0] != ' ' && rest[0] != '\t' {
		return "", false
	}
	return strings.TrimLeft(rest, " \t"), true
}

// stripOptions consumes the option words between `alias` and the first
// assignment, returning a non-empty reason if one of them changes what the
// statement means.
func stripOptions(s string) (rest, reason string) {
	for strings.HasPrefix(s, "-") {
		field := s
		if i := strings.IndexAny(s, " \t"); i >= 0 {
			field = s[:i]
		}
		s = strings.TrimLeft(s[len(field):], " \t")

		// `--` ends option parsing; everything after it is a definition.
		if field == "--" {
			return s, ""
		}
		// zsh's -g (global) and -s (suffix) aliases expand in positions a
		// plain alias never does. Importing one as an ordinary alias would
		// silently change what it does, so it is refused by name.
		return "", "uses the " + field + " option, which has no AliasDeck equivalent"
	}
	return s, ""
}

// parseAssignment reads one `name=value` pair off the front of s.
func parseAssignment(s string) (name, value, note, rest, reason string) {
	eq := strings.IndexByte(s, '=')
	// A word with no '=' before the next space is `alias foo`, which asks the
	// shell what foo is rather than defining it.
	sp := strings.IndexAny(s, " \t")
	if eq < 0 || (sp >= 0 && sp < eq) {
		word := s
		if sp >= 0 {
			word = s[:sp]
		}
		return "", "", "", "", "`" + word + "` has no '=', so it queries an alias rather than defining one"
	}

	name = s[:eq]
	if name == "" {
		return "", "", "", "", "the alias name is empty"
	}

	value, note, rest = lexValue(s[eq+1:])
	return name, value, note, rest, ""
}

// lexValue reads a shell word, concatenating its quoted and unquoted
// segments the way the shell does, and stops at the first unquoted space.
//
// Concatenation is what makes the standard single-quote escape work:
// 'it'\”s' is three segments — 'it', an escaped quote, and 's' — which is
// the only way to get an apostrophe inside single quotes.
//
// note is set when a segment would have been expanded by the shell at the
// moment the file was sourced. AliasDeck stores the text and renders it
// single-quoted, so expansion moves to when the alias runs. For $HOME that
// is the same answer; for $PWD or $(date) it is not.
func lexValue(s string) (value, note, rest string) {
	var b strings.Builder
	expandable := false
	i := 0

loop:
	for i < len(s) {
		switch c := s[i]; {
		case c == ' ' || c == '\t':
			break loop

		case c == '\'':
			// No escapes exist inside single quotes; an unterminated one
			// takes the remainder verbatim rather than guessing.
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				b.WriteString(s[i+1:])
				i = len(s)
				break loop
			}
			b.WriteString(s[i+1 : i+1+j])
			i += j + 2

		case c == '"':
			seg, sawExpansion, n := lexDoubleQuoted(s[i+1:])
			b.WriteString(seg)
			expandable = expandable || sawExpansion
			i += n + 1

		case c == '\\':
			if i+1 < len(s) {
				b.WriteByte(s[i+1])
				i += 2
			} else {
				i++
			}

		default:
			// Unquoted: the shell expands these at definition time. A
			// leading ~ counts for the same reason $ does.
			if c == '$' || c == '`' || c == '~' {
				expandable = true
			}
			b.WriteByte(c)
			i++
		}
	}

	if expandable {
		note = "the shell expanded this when the file was sourced; AliasDeck stores it as written and expands it when the alias runs"
	}
	return b.String(), note, s[i:]
}

// lexDoubleQuoted reads up to the closing quote, returning the unescaped
// contents, whether an expansion was present, and how many bytes were
// consumed including that quote.
func lexDoubleQuoted(s string) (string, bool, int) {
	var b strings.Builder
	expanded := false

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			return b.String(), expanded, i + 1

		case '\\':
			// Inside double quotes a backslash is literal unless it precedes
			// one of exactly four characters.
			if i+1 < len(s) {
				if next := s[i+1]; next == '"' || next == '\\' || next == '$' || next == '`' {
					b.WriteByte(next)
					i++
					continue
				}
			}
			b.WriteByte('\\')

		case '$', '`':
			expanded = true
			b.WriteByte(s[i])

		default:
			b.WriteByte(s[i])
		}
	}

	return b.String(), expanded, len(s)
}
