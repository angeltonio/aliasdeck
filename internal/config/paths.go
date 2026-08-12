// Package config resolves AliasDeck's on-disk configuration paths and
// parses its two YAML schemas (aliases.yaml, config.yaml).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Env supplies the environment inputs config resolution depends on:
// environment variables and the user's home directory. Tests inject fakes so
// path resolution never touches (or depends on) the real machine.
type Env struct {
	Getenv  func(key string) string
	HomeDir func() (string, error)
}

// OSEnv returns an Env backed by the real process environment.
//
// It deliberately uses os.UserHomeDir, never os.UserConfigDir: the latter
// returns ~/Library/Application Support on macOS, which contradicts the
// ~/.config/aliasdeck path documented in PROJECT.md §3.4.
func OSEnv() Env {
	return Env{Getenv: os.Getenv, HomeDir: os.UserHomeDir}
}

// Base resolves AliasDeck's base configuration directory.
//
// Precedence: $ALIASDECK_HOME → $XDG_CONFIG_HOME/aliasdeck → ~/.config/aliasdeck.
func Base(env Env) (string, error) {
	if v := env.Getenv("ALIASDECK_HOME"); v != "" {
		return ExpandPath(v, env)
	}

	if v := env.Getenv("XDG_CONFIG_HOME"); v != "" {
		expanded, err := ExpandPath(v, env)
		if err != nil {
			return "", err
		}
		return filepath.Join(expanded, "aliasdeck"), nil
	}

	home, err := env.HomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving AliasDeck base directory: %w", err)
	}
	return filepath.Join(home, ".config", "aliasdeck"), nil
}

// ConfigFile returns the path to config.yaml under base.
func ConfigFile(base string) string { return filepath.Join(base, "config.yaml") }

// AliasesFile returns the path to aliases.yaml under base.
func AliasesFile(base string) string { return filepath.Join(base, "aliases.yaml") }

// StateFile returns the path to state.json under base.
func StateFile(base string) string { return filepath.Join(base, "state.json") }

// ExpandPath expands a leading "~" and any embedded "$HOME" in path against
// env's home directory. It is used both for Base's overrides and for
// user-supplied paths such as config.yaml's source.path (PROJECT.md §7.3).
func ExpandPath(path string, env Env) (string, error) {
	if path == "" {
		return "", nil
	}

	needsHome := path == "~" || strings.HasPrefix(path, "~/") || strings.Contains(path, "$HOME")
	if !needsHome {
		return path, nil
	}

	home, err := env.HomeDir()
	if err != nil {
		return "", fmt.Errorf("expanding %q: %w", path, err)
	}

	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		path = filepath.Join(home, path[len("~/"):])
	}
	return strings.ReplaceAll(path, "$HOME", home), nil
}
