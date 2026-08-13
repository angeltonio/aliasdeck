package sync

import (
	"context"
	"fmt"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
)

// Resolve is the server's only resolution path (design decision 4). It loads
// the store's full alias set — enabled and disabled alike, targeting intact
// — and hands it straight to domain.Resolve, the exact function
// FileSource/GitSource call locally against a parsed aliases.yaml.
//
// This function deliberately contains no targeting logic of its own: no
// platform/shell/profile/device comparison lives here, because
// domain.Alias.AppliesTo already is that comparison, and a second
// implementation is a second set of resolution bugs (design decision 4's own
// rationale) plus a byte-identity break with standalone mode (server-sync
// spec, "Server-Side Resolution Reuses domain.Resolve"). Every targeting
// dimension — including DeviceIDs pinning, which has no equivalent in
// aliases.yaml's schema — is exercised by calling this same domain.Resolve,
// not by re-deriving it.
func Resolve(ctx context.Context, st store.Store, dev domain.Device) (domain.ResolvedConfig, error) {
	aliases, err := st.Aliases().List(ctx)
	if err != nil {
		return domain.ResolvedConfig{}, fmt.Errorf("sync: loading aliases: %w", err)
	}
	return domain.Resolve(dev, aliases), nil
}
