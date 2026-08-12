package source

import (
	"context"
	"fmt"
	"os"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/validate"
)

// FileSource resolves a device's aliases from a single local aliases.yaml
// file (PROJECT.md §7.1).
//
// It reads exactly Path: no merge, no fallback to another location. A device
// resolves through exactly one ConfigSource, and this is the file-backed
// implementation of that contract.
type FileSource struct {
	// Path is the absolute, already-expanded location of aliases.yaml.
	Path string
}

// Descriptor identifies this source for `status`.
func (s FileSource) Descriptor() Descriptor {
	return Descriptor{Type: "file", Ref: s.Path}
}

// Resolve reads Path, filters aliases for dev, and drops anything
// validate.FilterValid considers unsafe before it can reach a renderer.
//
// Filtering happens here, not in a caller, so every ConfigSource
// implementation guarantees the same safety property regardless of where its
// bytes came from (config-source spec, "Every Source Is Hostile Input").
// Dropped entries are not reported through this call: `doctor` performs its
// own independent read-and-validate pass for diagnostics (PROJECT.md §7.4).
func (s FileSource) Resolve(_ context.Context, dev domain.Device) (domain.ResolvedConfig, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return domain.ResolvedConfig{}, fmt.Errorf("reading %s: %w", s.Path, err)
	}

	doc, err := config.ParseAliases(data)
	if err != nil {
		return domain.ResolvedConfig{}, fmt.Errorf("parsing %s: %w", s.Path, err)
	}

	resolved := domain.Resolve(dev, doc.Aliases)
	filtered, _ := validate.FilterValid(resolved)
	return filtered, nil
}
