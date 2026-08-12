// Package source resolves a device's aliases from wherever they are
// declared — a local file today, Git or a server in a later milestone — into
// the neutral domain.ResolvedConfig every renderer accepts.
//
// Every implementation treats its input as hostile (PROJECT.md §12.1): a
// local aliases.yaml gets no less scrutiny than a Git checkout or a server
// response would.
package source

import (
	"context"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// ConfigSource resolves a device's configuration.
//
// Implementations MUST return either a fully resolved ResolvedConfig or a
// non-nil error, never both a populated config and an error, so a caller can
// never mistake a partial result for a complete one (config-source spec,
// "Resolve error is not partially applied").
type ConfigSource interface {
	Resolve(ctx context.Context, dev domain.Device) (domain.ResolvedConfig, error)
}

// Descriptor names the active source so `status` can report where a
// device's aliases come from without reaching back into config.yaml.
type Descriptor struct {
	Type string
	Ref  string
}
