package validate

import (
	"fmt"
	"strings"
)

// Command validates the command body of an alias.
//
// The command is written inside a quoted region and is escaped by the renderer,
// so ordinary shell metacharacters are safe and expected here: a command is
// supposed to contain pipes, quotes and dollar signs. What is rejected is the
// class of characters that cannot survive a single-line construct.
func Command(cmd string) error {
	if strings.TrimSpace(cmd) == "" {
		return fmt.Errorf("command is empty")
	}
	if countRunes(cmd) > MaxCommandLen {
		return fmt.Errorf("command is longer than %d characters", MaxCommandLen)
	}
	if r, bad := firstControlRune(cmd); bad {
		if r == '\n' || r == '\r' {
			// Multi-line bodies are not an escaping problem — quoted newlines
			// survive fine — but an alias is a single-line construct by
			// definition. Anything needing multiple lines is a shell function,
			// which is a separate, deliberate feature rather than something a
			// user should discover by accident.
			return fmt.Errorf(
				"command spans multiple lines; aliases are single-line, use a shell function instead")
		}
		return fmt.Errorf("command contains a control character (%U)", r)
	}
	return nil
}

// Description validates the human-readable note attached to an alias.
//
// Descriptions are emitted as comments in the generated file. A description
// containing a newline could close the comment and continue as executable
// shell code, which makes this field an injection vector despite looking
// entirely cosmetic.
func Description(desc string) error {
	if desc == "" {
		return nil
	}
	if countRunes(desc) > MaxDescriptionLen {
		return fmt.Errorf("description is longer than %d characters", MaxDescriptionLen)
	}
	if r, bad := firstControlRune(desc); bad {
		return fmt.Errorf("description contains a control character (%U)", r)
	}
	return nil
}

// firstControlRune returns the first disallowed control character in s.
//
// Tab is permitted; every other C0 control character and DEL is not.
func firstControlRune(s string) (rune, bool) {
	for _, r := range s {
		if r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return r, true
		}
	}
	return 0, false
}
