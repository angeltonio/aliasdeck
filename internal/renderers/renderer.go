// Package renderers turns a neutral ResolvedConfig into shell syntax.
//
// This package is imported by both the CLI, where its output is written to
// disk, and the server, where the same output is shown as a preview in the web
// UI. One implementation, one set of escaping rules, no drift between what a
// user is shown and what lands on their machine.
//
// Renderers never trust their input. Every configuration is validated again
// here even though the caller is expected to have validated it already,
// because the cost of the duplicated check is nothing and the cost of writing
// unescaped input into a shell profile is a compromised machine.
package renderers

import (
	"fmt"
	"slices"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/validate"
)

// Renderer produces the contents of a generated configuration file for one
// shell.
type Renderer interface {
	// Shell reports which shell this renderer targets.
	Shell() domain.Shell

	// Render returns the complete file contents for cfg.
	//
	// Output is byte-deterministic: the same configuration always produces the
	// same bytes, so callers can decide whether anything changed by comparing
	// hashes instead of diffing files.
	Render(cfg domain.ResolvedConfig) (string, error)
}

// registry holds every renderer that actually exists.
//
// domain.AllShells is the wider set of shells the model can describe; this map
// is the narrower set AliasDeck can currently write. Keeping them separate lets
// a configuration mention powershell on a machine whose CLI predates the
// PowerShell renderer, and fail with a clear message rather than a nil map.
var registry = map[domain.Shell]Renderer{
	domain.ShellZsh:        posixRenderer{shell: domain.ShellZsh},
	domain.ShellBash:       posixRenderer{shell: domain.ShellBash},
	domain.ShellPowerShell: powershellRenderer{},
}

// ErrUnsupportedShell is returned when no renderer exists for a shell.
type ErrUnsupportedShell struct {
	Shell domain.Shell
}

func (e ErrUnsupportedShell) Error() string {
	return fmt.Sprintf("no renderer available for shell %q in this version of AliasDeck", e.Shell)
}

// For returns the renderer targeting sh.
func For(sh domain.Shell) (Renderer, error) {
	r, ok := registry[sh]
	if !ok {
		return nil, ErrUnsupportedShell{Shell: sh}
	}
	return r, nil
}

// Supported lists the shells this build can render, in stable order.
//
// The CLI reports this in `aliasdeck doctor` so a user can tell the difference
// between "my config is wrong" and "my client is too old".
func Supported() []domain.Shell {
	out := make([]domain.Shell, 0, len(registry))
	for sh := range registry {
		out = append(out, sh)
	}
	slices.SortFunc(out, func(a, b domain.Shell) int {
		return slices.Index(domain.AllShells, a) - slices.Index(domain.AllShells, b)
	})
	return out
}

// Render is the package-level entry point: pick the renderer for the device's
// shell and run it.
func Render(cfg domain.ResolvedConfig) (string, error) {
	r, err := For(cfg.Device.Shell)
	if err != nil {
		return "", err
	}
	return r.Render(cfg)
}

// guard re-validates cfg and refuses to render anything unsafe.
func guard(cfg domain.ResolvedConfig) error {
	if issues := validate.Config(cfg).Errors(); len(issues) > 0 {
		return fmt.Errorf("refusing to render invalid configuration: %s", issues[0])
	}
	return nil
}
