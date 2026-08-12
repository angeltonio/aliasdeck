// Package domain contains the entities shared by the AliasDeck CLI and server.
//
// It has no dependencies outside the standard library on purpose: every other
// package in the project may import it, and it must stay importable from both
// the CLI and the server without dragging in transport or storage concerns.
package domain

import "fmt"

// Platform is an operating system AliasDeck can target.
type Platform string

const (
	PlatformMacOS   Platform = "macos"
	PlatformLinux   Platform = "linux"
	PlatformWindows Platform = "windows"
)

// AllPlatforms lists every supported platform, in stable order.
var AllPlatforms = []Platform{PlatformMacOS, PlatformLinux, PlatformWindows}

func (p Platform) String() string { return string(p) }

// Valid reports whether p is a platform AliasDeck knows about.
func (p Platform) Valid() bool {
	switch p {
	case PlatformMacOS, PlatformLinux, PlatformWindows:
		return true
	}
	return false
}

// ParsePlatform converts external input (YAML, JSON, flags) into a Platform.
func ParsePlatform(s string) (Platform, error) {
	p := Platform(s)
	if !p.Valid() {
		return "", fmt.Errorf("unknown platform %q", s)
	}
	return p, nil
}

// Shell is a shell dialect AliasDeck can render for.
type Shell string

const (
	ShellZsh        Shell = "zsh"
	ShellBash       Shell = "bash"
	ShellPowerShell Shell = "powershell"
)

// AllShells lists every shell in the domain model, in stable order.
//
// Being listed here does not imply a renderer exists yet. Ask the renderers
// package what it can actually produce.
var AllShells = []Shell{ShellZsh, ShellBash, ShellPowerShell}

func (s Shell) String() string { return string(s) }

// Valid reports whether s is a shell AliasDeck knows about.
func (s Shell) Valid() bool {
	switch s {
	case ShellZsh, ShellBash, ShellPowerShell:
		return true
	}
	return false
}

// IsPOSIX reports whether s uses POSIX-style alias syntax and quoting rules.
//
// This drives both renderer selection and the escaping strategy, so it is a
// domain fact rather than a renderer detail.
func (s Shell) IsPOSIX() bool {
	switch s {
	case ShellZsh, ShellBash:
		return true
	}
	return false
}

// ParseShell converts external input (YAML, JSON, flags) into a Shell.
func ParseShell(s string) (Shell, error) {
	sh := Shell(s)
	if !sh.Valid() {
		return "", fmt.Errorf("unknown shell %q", s)
	}
	return sh, nil
}

// DefaultShellFor returns the shell most likely to be in use on p.
//
// It is a starting guess for `aliasdeck init`, not a substitute for detection.
func DefaultShellFor(p Platform) Shell {
	switch p {
	case PlatformMacOS:
		return ShellZsh
	case PlatformWindows:
		return ShellPowerShell
	default:
		return ShellBash
	}
}
