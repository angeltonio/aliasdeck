package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
)

// resolveRCPath returns the rc file AliasDeck should bootstrap for sh on
// platform (design, "Paths, Detection, Exit Codes" §"rc file"):
//
//   - override (--rc-file) always wins.
//   - zsh: $ZDOTDIR/.zshrc when $ZDOTDIR is set, else ~/.zshrc.
//   - bash: the first existing of (~/.bash_profile, ~/.bashrc) on macOS, or
//     (~/.bashrc, ~/.bash_profile) on Linux; when neither exists yet, the
//     platform's first candidate is returned as the file to create.
func resolveRCPath(env Env, sh domain.Shell, platform domain.Platform, override string) (string, error) {
	if override != "" {
		return config.ExpandPath(override, env.ConfigEnv())
	}

	home, err := env.HomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}

	switch sh {
	case domain.ShellZsh:
		if zdotdir := env.Getenv("ZDOTDIR"); zdotdir != "" {
			return filepath.Join(zdotdir, ".zshrc"), nil
		}
		return filepath.Join(home, ".zshrc"), nil

	case domain.ShellBash:
		candidates := []string{filepath.Join(home, ".bash_profile"), filepath.Join(home, ".bashrc")}
		if platform == domain.PlatformLinux {
			candidates = []string{filepath.Join(home, ".bashrc"), filepath.Join(home, ".bash_profile")}
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
		}
		return candidates[0], nil

	default:
		return "", fmt.Errorf("no rc file convention defined for shell %q", sh)
	}
}
