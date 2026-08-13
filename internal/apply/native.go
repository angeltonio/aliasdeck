package apply

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// NativeBackend writes the rendered aliases file directly to
// <Base>/aliases.<ext> and is the only SyncBackend implemented in v0.1
// (design decision 9).
type NativeBackend struct {
	// Base is AliasDeck's configuration base directory (config.Base()).
	Base string
}

// Name identifies this backend for status/doctor output.
func (b NativeBackend) Name() string { return "native" }

// OutputPath returns <Base>/aliases.<ext>, where ext depends on dev.Shell.
func (b NativeBackend) OutputPath(dev domain.Device) (string, error) {
	ext, err := shellFileExt(dev.Shell)
	if err != nil {
		return "", err
	}
	return filepath.Join(b.Base, "aliases."+ext), nil
}

// Apply atomically writes rendered to OutputPath(cfg.Device).
//
// writeFileAtomic guarantees an interrupted apply never leaves a truncated
// file: a reader sees either the prior valid content or the new content,
// never a partial write (native-apply spec, "Atomic Write").
func (b NativeBackend) Apply(_ context.Context, cfg domain.ResolvedConfig, rendered string) error {
	path, err := b.OutputPath(cfg.Device)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(rendered), 0o644)
}

// shellFileExt maps a shell to its generated-file extension. Only the
// shells AliasDeck can currently render have one.
func shellFileExt(sh domain.Shell) (string, error) {
	switch sh {
	case domain.ShellZsh:
		return "zsh", nil
	case domain.ShellBash:
		return "bash", nil
	case domain.ShellPowerShell:
		return "ps1", nil
	default:
		return "", fmt.Errorf("no generated-file extension defined for shell %q", sh)
	}
}

// errChezmoiNotImplemented is returned by every ChezmoiBackend method.
var errChezmoiNotImplemented = errors.New(`backend "chezmoi" is not implemented in v0.1`)

// ChezmoiBackend is an interface-only stub.
//
// The SyncBackend seam exists so a real chezmoi integration can land later
// without changing the interface, but selecting it today is a hard, explicit
// error rather than a silent no-op: it must not write a generated file or
// edit any rc file (native-apply spec, "Chezmoi Backend Fails Explicitly").
type ChezmoiBackend struct{}

func (b ChezmoiBackend) Name() string { return "chezmoi" }

func (b ChezmoiBackend) OutputPath(domain.Device) (string, error) {
	return "", errChezmoiNotImplemented
}

func (b ChezmoiBackend) Apply(context.Context, domain.ResolvedConfig, string) error {
	return errChezmoiNotImplemented
}
