package importers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePOSIXReadsDefinitions(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []Found
		skipped int
	}{
		{
			name: "single quoted",
			in:   `alias gs='git status'`,
			want: []Found{{Line: 1, Name: "gs", Command: "git status"}},
		},
		{
			name: "bare value",
			in:   `alias ll=ls`,
			want: []Found{{Line: 1, Name: "ll", Command: "ls"}},
		},
		{
			name: "indented",
			in:   "\t  alias gs='git status'",
			want: []Found{{Line: 1, Name: "gs", Command: "git status"}},
		},
		{
			name: "double quoted without expansion",
			in:   `alias gs="git status"`,
			want: []Found{{Line: 1, Name: "gs", Command: "git status"}},
		},
		{
			// The idiom for an apostrophe inside single quotes: three
			// concatenated segments. A parser that stops at the first
			// closing quote gets "it" and loses the rest.
			name: "escaped quote by concatenation",
			in:   `alias whose='echo it'\''s mine'`,
			want: []Found{{Line: 1, Name: "whose", Command: "echo it's mine"}},
		},
		{
			name: "escapes inside double quotes",
			in:   `alias say="printf \"%s\\n\" hi"`,
			want: []Found{{Line: 1, Name: "say", Command: `printf "%s\n" hi`}},
		},
		{
			name: "two definitions in one statement",
			in:   `alias a='one' b='two'`,
			want: []Found{
				{Line: 1, Name: "a", Command: "one"},
				{Line: 1, Name: "b", Command: "two"},
			},
		},
		{
			name: "trailing comment is not part of the command",
			in:   `alias ll='ls -la'   # long listing`,
			want: []Found{{Line: 1, Name: "ll", Command: "ls -la"}},
		},
		{
			name: "double dash ends options",
			in:   `alias -- gs='git status'`,
			want: []Found{{Line: 1, Name: "gs", Command: "git status"}},
		},
		{
			name: "carriage returns survive",
			in:   "alias gs='git status'\r\nalias gp='git push'\r\n",
			want: []Found{
				{Line: 1, Name: "gs", Command: "git status"},
				{Line: 2, Name: "gp", Command: "git push"},
			},
		},
		{
			name: "unterminated quote takes the remainder rather than guessing",
			in:   `alias oops='git status`,
			want: []Found{{Line: 1, Name: "oops", Command: "git status"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParsePOSIX([]byte(tt.in))
			if len(got.Found) != len(tt.want) {
				t.Fatalf("found %d aliases, want %d: %+v", len(got.Found), len(tt.want), got.Found)
			}
			for i, w := range tt.want {
				g := got.Found[i]
				if g.Name != w.Name || g.Command != w.Command || g.Line != w.Line {
					t.Errorf("alias %d = %+v, want line %d %s=%q", i, g, w.Line, w.Name, w.Command)
				}
			}
			if len(got.Skipped) != tt.skipped {
				t.Errorf("skipped %d lines, want %d: %+v", len(got.Skipped), tt.skipped, got.Skipped)
			}
		})
	}
}

// TestParsePOSIXIgnoresWhatIsNotADefinition covers the lines a naive
// "does it start with alias" check gets wrong. Importing any of these would
// put something in the user's aliases.yaml that was never an alias.
func TestParsePOSIXIgnoresWhatIsNotADefinition(t *testing.T) {
	for _, line := range []string{
		`# alias gs='git status'`,
		`  #alias gs='git status'`,
		`aliases=(one two three)`,
		`alias`,
		`echo alias gs=nope`,
		``,
	} {
		if got := ParsePOSIX([]byte(line)); len(got.Found) != 0 {
			t.Errorf("%q yielded %+v, want nothing", line, got.Found)
		}
	}
}

// TestParsePOSIXSkipsWhatItCannotTranslateOutLoud is the promise the whole
// command rests on: an alias that is not imported is named, with a reason.
// Silence would be the real failure — a user finds out three of their forty
// aliases are missing weeks later.
func TestParsePOSIXSkipsWhatItCannotTranslateOutLoud(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"global alias", `alias -g G='| grep'`, "-g"},
		{"suffix alias", `alias -s md=bat`, "-s"},
		{"query not a definition", `alias gs`, "no '='"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParsePOSIX([]byte(tt.in))
			if len(got.Found) != 0 {
				t.Fatalf("imported %+v, want it refused", got.Found)
			}
			if len(got.Skipped) != 1 {
				t.Fatalf("skipped %d lines, want exactly one: %+v", len(got.Skipped), got.Skipped)
			}
			if !strings.Contains(got.Skipped[0].Reason, tt.want) {
				t.Errorf("reason = %q, want it to mention %q", got.Skipped[0].Reason, tt.want)
			}
			if got.Skipped[0].Line != 1 {
				t.Errorf("Line = %d, want 1 so the user can go look at it", got.Skipped[0].Line)
			}
		})
	}
}

// TestParsePOSIXFlagsExpansionThatMovesInTime documents the one case where
// the text is read correctly but the meaning shifts. The shell expands an
// unquoted or double-quoted $ when the file is sourced; AliasDeck stores the
// text and renders it single-quoted, so expansion happens when the alias
// runs. For $HOME that is the same answer. For $PWD it is not, and a user
// who is not told will be debugging it later.
func TestParsePOSIXFlagsExpansionThatMovesInTime(t *testing.T) {
	noted := map[string]bool{
		`alias here="echo $PWD"`:   true,
		`alias now="echo $(date)"`: true,
		`alias cdp=~/projects`:     true,
		`alias back="echo \$PWD"`:  false,
		`alias safe='echo $PWD'`:   false,
		`alias gs='git status'`:    false,
	}

	for line, wantNote := range noted {
		got := ParsePOSIX([]byte(line))
		if len(got.Found) != 1 {
			t.Fatalf("%q: found %+v, want one alias", line, got.Found)
		}
		if hasNote := got.Found[0].Note != ""; hasNote != wantNote {
			t.Errorf("%q: note=%q, want a note: %v", line, got.Found[0].Note, wantNote)
		}
	}
}

// TestParsePOSIXSkipsAliasDecksOwnBlock stops the import from eating its own
// output. A user who ran `aliasdeck init` has a block in their rc file that
// sources the generated file; without this the importer would walk straight
// back into it.
func TestParsePOSIXSkipsAliasDecksOwnBlock(t *testing.T) {
	rc := strings.Join([]string{
		`alias mine='echo mine'`,
		beginMarker,
		`alias generated='echo generated'`,
		endMarker,
		`alias alsomine='echo also'`,
	}, "\n")

	got := ParsePOSIX([]byte(rc))
	for _, f := range got.Found {
		if f.Name == "generated" {
			t.Fatalf("imported an alias from AliasDeck's own block: %+v", got.Found)
		}
	}
	if len(got.Found) != 2 {
		t.Fatalf("found %+v, want the two aliases outside the block", got.Found)
	}
}

// TestBootstrapMarkersMatchTheOnesApplyWrites guards the duplication above.
// If internal/apply ever changes its markers, the block-skipping test keeps
// passing against the stale constants while real rc files stop being
// recognised — the failure would be invisible until a user reported it.
func TestBootstrapMarkersMatchTheOnesApplyWrites(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "apply", "bootstrap.go"))
	if err != nil {
		t.Fatalf("reading internal/apply/bootstrap.go: %v", err)
	}
	for _, marker := range []string{beginMarker, endMarker} {
		if !strings.Contains(string(src), `"`+marker+`"`) {
			t.Errorf("internal/apply no longer defines %q; this package's copy is stale", marker)
		}
	}
}
