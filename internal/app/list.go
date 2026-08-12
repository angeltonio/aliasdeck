package app

import (
	"context"
	"fmt"
	"os"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
)

// AliasListing pairs one declared alias with whether it is active for the
// current device and, when it is not, why.
type AliasListing struct {
	Alias  domain.Alias
	Active bool
	Reason string
}

// ListReport is every alias declared in the active source, annotated for
// this device (cli-commands spec, "list Shows Resolved Aliases").
type ListReport struct {
	Device  domain.Device
	Entries []AliasListing
}

// List reports every declared alias, marking which apply to this device
// after platform/shell/profile filtering.
//
// It reads and parses aliases.yaml directly rather than going through
// Source.Resolve, which already drops everything that does not apply — the
// same reason Doctor performs its own independent pass.
func List(_ context.Context, env Env, opts Options) (ListReport, error) {
	dc, err := loadDeviceContext(env, opts)
	if err != nil {
		return ListReport{}, err
	}

	data, err := os.ReadFile(dc.AliasesPath)
	if err != nil {
		return ListReport{}, fmt.Errorf("reading %s: %w", dc.AliasesPath, err)
	}
	doc, err := config.ParseAliases(data)
	if err != nil {
		return ListReport{}, ConfigError{Err: fmt.Errorf("parsing %s: %w", dc.AliasesPath, err)}
	}

	sorted := make([]domain.Alias, len(doc.Aliases))
	copy(sorted, doc.Aliases)
	domain.SortAliases(sorted)

	entries := make([]AliasListing, 0, len(sorted))
	for _, a := range sorted {
		entries = append(entries, AliasListing{
			Alias:  a,
			Active: a.AppliesTo(dc.Device),
			Reason: skipReason(a, dc.Device),
		})
	}

	return ListReport{Device: dc.Device, Entries: entries}, nil
}

// skipReason explains why a is not active for dev, or "" when it is.
func skipReason(a domain.Alias, dev domain.Device) string {
	switch {
	case !a.Enabled:
		return "disabled"
	case !a.TargetsPlatform(dev.Platform):
		return fmt.Sprintf("not targeted at platform %q", dev.Platform)
	case !a.TargetsShell(dev.Shell):
		return fmt.Sprintf("not targeted at shell %q", dev.Shell)
	case !a.TargetsProfiles(dev.ProfileIDs):
		return "no matching profile"
	case !a.TargetsDevice(dev.ID):
		return "not targeted at this device"
	default:
		return ""
	}
}
