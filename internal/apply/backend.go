package apply

import (
	"context"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// SyncBackend dispatches Apply to wherever a device's generated aliases file
// actually lives, selected by config.yaml's backend field (design decision
// 9). NativeBackend is the only backend implemented in v0.1; ChezmoiBackend
// ships as an interface-only stub (native-apply spec, "SyncBackend Seam").
type SyncBackend interface {
	// Name identifies this backend, e.g. for status/doctor output.
	Name() string

	// OutputPath reports where Apply would write for dev, without writing
	// anything. status and doctor use it to report the active target.
	OutputPath(dev domain.Device) (string, error)

	// Apply writes rendered to the backend's target for cfg.Device.
	Apply(ctx context.Context, cfg domain.ResolvedConfig, rendered string) error
}
