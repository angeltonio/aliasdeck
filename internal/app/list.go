package app

import (
	"context"
	"errors"
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

// ErrListAliasesUnderServerSource is returned when the active source is a
// server. List's whole value is showing aliases that are declared but
// *inactive*, with the reason — which requires the declared set, and under
// a server source that set lives on the server. What the device can fetch
// is already resolved: the server applied the targeting, so the inactive
// entries and their reasons are exactly what is missing.
//
// Reading aliases.yaml anyway produced `reading : open : no such file or
// directory`, because resolveServerSource leaves AliasesPath empty. This
// mirrors ErrEditAliasesUnderServerSource rather than inventing a second
// way of saying the same thing.
var ErrListAliasesUnderServerSource = errors.New(
	"aliases live on the server for this device; `aliasdeck status` reports what is applied, " +
		"and the server's API lists what is declared")

// List reports every declared alias, marking which apply to this device
// after platform/shell/profile filtering.
//
// It reads and parses aliases.yaml directly rather than going through
// Source.Resolve, which already drops everything that does not apply — the
// same reason Doctor performs its own independent pass. That is also why
// it cannot serve a server source; see ErrListAliasesUnderServerSource.
func List(_ context.Context, env Env, opts Options) (ListReport, error) {
	dc, err := loadDeviceContext(env, opts)
	if err != nil {
		return ListReport{}, err
	}

	if dc.SourceDesc.Type == "server" {
		return ListReport{}, ErrListAliasesUnderServerSource
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
//
// The decision of *which* dimension failed belongs to domain.Alias.Miss, so
// this only phrases it. Deriving it here as well would let the CLI and the
// server's preview disagree about the same alias.
func skipReason(a domain.Alias, dev domain.Device) string {
	switch a.Miss(dev) {
	case domain.MissDisabled:
		return "disabled"
	case domain.MissPlatform:
		return fmt.Sprintf("not targeted at platform %q", dev.Platform)
	case domain.MissShell:
		return fmt.Sprintf("not targeted at shell %q", dev.Shell)
	case domain.MissProfile:
		return "no matching profile"
	case domain.MissDevice:
		return "not targeted at this device"
	default:
		return ""
	}
}
