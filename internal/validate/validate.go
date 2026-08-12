// Package validate enforces what AliasDeck is willing to write into a user's
// shell configuration.
//
// This package is the project's last line of defense. Because rendering happens
// on the client, AliasDeck cannot assume its input is trustworthy: an
// aliases.yaml pulled from a Git repository deserves exactly the same scrutiny
// as a response from a compromised server. Every source is treated as hostile.
//
// The rules here are deliberately stricter than what the shells themselves
// accept. Restrictions can be relaxed later without breaking anyone; they
// cannot be tightened once users depend on the loophole.
package validate

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// Limits applied to every alias, regardless of source.
const (
	MaxNameLen        = 64
	MaxCommandLen     = 4096
	MaxDescriptionLen = 256
	MaxAliases        = 5000
)

// Severity determines what the caller does with an Issue.
type Severity int

const (
	// SeverityError means the alias is unsafe or unusable and must be skipped.
	SeverityError Severity = iota
	// SeverityWarning means the alias is written but something looks wrong.
	SeverityWarning
)

func (s Severity) String() string {
	if s == SeverityWarning {
		return "warning"
	}
	return "error"
}

// Issue is a single problem found in a configuration, shaped for reporting by
// `aliasdeck doctor` rather than for aborting a sync.
//
// Validation collects issues instead of returning on the first failure: one bad
// alias must never stop the other forty from reaching the machine.
type Issue struct {
	AliasName string
	Field     string
	Severity  Severity
	Message   string
}

func (i Issue) String() string {
	name := i.AliasName
	if name == "" {
		name = "<unnamed>"
	}
	return fmt.Sprintf("%s: %s (%s): %s", i.Severity, name, i.Field, i.Message)
}

// Issues is the result of validating a configuration.
type Issues []Issue

// HasErrors reports whether any issue is severe enough to skip an alias.
func (is Issues) HasErrors() bool {
	for _, i := range is {
		if i.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Errors returns only the blocking issues.
func (is Issues) Errors() Issues {
	out := make(Issues, 0, len(is))
	for _, i := range is {
		if i.Severity == SeverityError {
			out = append(out, i)
		}
	}
	return out
}

// Alias validates a single alias against the rules of the target shell.
//
// A nil or empty result means the alias is safe to render.
func Alias(a domain.Alias, sh domain.Shell) Issues {
	var issues Issues
	add := func(field string, sev Severity, format string, args ...any) {
		issues = append(issues, Issue{
			AliasName: a.Name,
			Field:     field,
			Severity:  sev,
			Message:   fmt.Sprintf(format, args...),
		})
	}

	if err := Name(a.Name, sh); err != nil {
		add("name", SeverityError, "%s", err)
	}
	if err := Command(a.Command); err != nil {
		add("command", SeverityError, "%s", err)
	}
	if err := Description(a.Description); err != nil {
		add("description", SeverityError, "%s", err)
	}

	for _, p := range a.Platforms {
		if !p.Valid() {
			add("platforms", SeverityError, "unknown platform %q", p)
		}
	}
	for _, s := range a.Shells {
		if !s.Valid() {
			add("shells", SeverityError, "unknown shell %q", s)
		}
	}

	return issues
}

// Config validates a whole resolved configuration, including the cross-alias
// checks that cannot be made one alias at a time.
func Config(cfg domain.ResolvedConfig) Issues {
	var issues Issues

	if !cfg.Device.Platform.Valid() {
		issues = append(issues, Issue{
			Field:    "device.platform",
			Severity: SeverityError,
			Message:  fmt.Sprintf("unknown platform %q", cfg.Device.Platform),
		})
	}
	if !cfg.Device.Shell.Valid() {
		issues = append(issues, Issue{
			Field:    "device.shell",
			Severity: SeverityError,
			Message:  fmt.Sprintf("unknown shell %q", cfg.Device.Shell),
		})
	}

	if len(cfg.Aliases) > MaxAliases {
		issues = append(issues, Issue{
			Field:    "aliases",
			Severity: SeverityError,
			Message: fmt.Sprintf("configuration declares %d aliases, limit is %d",
				len(cfg.Aliases), MaxAliases),
		})
	}

	seen := make(map[string]struct{}, len(cfg.Aliases))
	for _, a := range cfg.Aliases {
		issues = append(issues, Alias(a, cfg.Device.Shell)...)

		// Duplicate names are reported rather than silently resolved. Last one
		// wins is a defensible rule, but a user who defined the same alias
		// twice with different commands needs to be told, not guessed at.
		key := duplicateKey(a.Name, cfg.Device.Shell)
		if _, dup := seen[key]; dup {
			issues = append(issues, Issue{
				AliasName: a.Name,
				Field:     "name",
				Severity:  SeverityError,
				Message:   "duplicate alias name for this shell",
			})
		}
		seen[key] = struct{}{}
	}

	return issues
}

// duplicateKey normalizes a name for collision detection.
//
// PowerShell resolves function names case-insensitively, POSIX shells do not,
// so "DPS" and "dps" collide on Windows but coexist on macOS.
func duplicateKey(name string, sh domain.Shell) string {
	if sh == domain.ShellPowerShell {
		return strings.ToLower(name)
	}
	return name
}

// FilterValid splits a configuration into the aliases that are safe to render
// and the issues explaining what was dropped.
//
// This is the function sync uses: render what is good, report what is not, and
// never let a single malformed entry take down the whole file.
func FilterValid(cfg domain.ResolvedConfig) (domain.ResolvedConfig, Issues) {
	issues := Config(cfg)

	blocked := make(map[string]struct{})
	for _, i := range issues.Errors() {
		if i.AliasName != "" {
			blocked[i.AliasName] = struct{}{}
		}
	}

	kept := make([]domain.Alias, 0, len(cfg.Aliases))
	for _, a := range cfg.Aliases {
		if _, bad := blocked[a.Name]; bad {
			continue
		}
		kept = append(kept, a)
	}

	cfg.Aliases = kept
	return cfg, issues
}

// countRunes is utf8.RuneCountInString, named for readability at call sites
// where the point is "length as a user perceives it, not bytes".
func countRunes(s string) int { return utf8.RuneCountInString(s) }
