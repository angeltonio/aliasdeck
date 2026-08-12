package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"slices"
	"strings"
	"time"
)

// ResolvedConfig is the contract every ConfigSource must satisfy and the only
// input a renderer accepts.
//
// It is already filtered: every alias in it applies to Device. Whether that
// filtering happened locally against an aliases.yaml or remotely on the server
// is invisible from here, which is exactly what makes standalone and
// control-plane mode share a code path.
type ResolvedConfig struct {
	// Revision identifies this configuration's content. For local sources it
	// is a content hash; for the server it is the server's revision marker.
	Revision string `json:"revision"`

	Device  Device  `json:"device"`
	Aliases []Alias `json:"aliases"`

	// GeneratedAt is recorded in local sync state.
	//
	// Renderers must NOT emit it. Rendered output has to be byte-deterministic
	// so that "did anything actually change?" is a hash comparison rather than
	// a diff, and a wall-clock timestamp in the file would make every sync look
	// like a change.
	GeneratedAt time.Time `json:"generatedAt,omitzero"`
}

// Resolve filters aliases for dev and returns a ResolvedConfig with a computed
// revision. This is the local resolution path used by FileSource and GitSource.
func Resolve(dev Device, aliases []Alias) ResolvedConfig {
	matched := make([]Alias, 0, len(aliases))
	for _, a := range aliases {
		if a.AppliesTo(dev) {
			matched = append(matched, a)
		}
	}
	SortAliases(matched)

	cfg := ResolvedConfig{Device: dev, Aliases: matched}
	cfg.Revision = cfg.ComputeRevision()
	return cfg
}

// SortAliases orders aliases by name, in place.
//
// Determinism is a requirement, not a nicety: unordered output produces noisy
// diffs and breaks revision hashing.
func SortAliases(aliases []Alias) {
	slices.SortStableFunc(aliases, func(a, b Alias) int {
		return strings.Compare(a.Name, b.Name)
	})
}

// ComputeRevision returns a short content hash over the parts of the
// configuration that can change rendered output.
//
// Fields that do not affect output (timestamps, tags, IDs) are excluded so that
// cosmetic edits upstream do not trigger pointless rewrites on every device.
func (c ResolvedConfig) ComputeRevision() string {
	h := sha256.New()

	writeField := func(parts ...string) {
		for _, p := range parts {
			io.WriteString(h, p)
			h.Write([]byte{0x00})
		}
		h.Write([]byte{0x1e})
	}

	writeField(c.Device.Platform.String(), c.Device.Shell.String())

	sorted := slices.Clone(c.Aliases)
	SortAliases(sorted)
	for _, a := range sorted {
		writeField(a.Name, a.Command, a.Description)
	}

	return hex.EncodeToString(h.Sum(nil))[:12]
}
