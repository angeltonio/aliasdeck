package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/state"
	"github.com/angeltonio/aliasdeck/internal/validate"
)

// DoctorReport is doctor's read-only diagnosis: every issue validation
// would drop and every undeclared-profile reference — none of which
// FileSource.Resolve exposes, since it already discards validate.Issues
// once it has filtered by them (cli-commands spec, "doctor Diagnoses
// Without Writing").
type DoctorReport struct {
	Device             domain.Device
	AliasesPath        string
	PlatformProvenance string
	ShellProvenance    string
	Issues             validate.Issues
	ProfileWarnings    []string

	// Warnings holds free-form diagnostics beyond validation and undeclared
	// profiles: the other PowerShell edition's profile existing unbootstrapped
	// (cli-commands spec, "Other-edition profile warning"), and a stale
	// GitSource checkout (cli-commands spec, "Stale GitSource checkout
	// reported"). Both are read-only checks — the first reads fields
	// resolvePowerShellProfile already computes, the second reads the
	// last sync's recorded state — so they never write anything and never
	// change Doctor's exit code (a warning is not an Issue).
	Warnings []string
}

// Doctor performs its own independent read-and-validate pass over the
// active aliases.yaml: the same domain.Resolve → validate.Config sequence
// FileSource.Resolve runs internally, except the issues are returned
// instead of discarded. It never calls Source.Resolve and never writes
// anything.
func Doctor(_ context.Context, env Env, opts Options) (DoctorReport, error) {
	dc, err := loadDeviceContext(env, opts)
	if err != nil {
		return DoctorReport{}, err
	}

	data, err := os.ReadFile(dc.AliasesPath)
	if err != nil {
		return DoctorReport{}, fmt.Errorf("reading %s: %w", dc.AliasesPath, err)
	}
	doc, err := config.ParseAliases(data)
	if err != nil {
		return DoctorReport{}, ConfigError{Err: fmt.Errorf("parsing %s: %w", dc.AliasesPath, err)}
	}

	resolved := domain.Resolve(dc.Device, doc.Aliases)
	issues := validate.Config(resolved)
	profileWarnings := config.ProfileWarnings(doc.Profiles, doc.Aliases)

	var warnings []string

	if dc.Device.Shell == domain.ShellPowerShell {
		profile, err := resolvePowerShellProfile(env, dc.Device.Platform)
		if err != nil {
			return DoctorReport{}, err
		}
		if profile.OtherExists {
			warnings = append(warnings, fmt.Sprintf(
				"the other PowerShell edition's profile exists and is not bootstrapped: %s (this device bootstraps %s at %s)",
				profile.OtherPath, profile.Edition, profile.Path))
		}
	}

	if dc.SourceDesc.Type == "git" {
		st, err := state.Load(config.StateFile(dc.Base))
		if err != nil {
			return DoctorReport{}, err
		}
		if st.SourceStale {
			warnings = append(warnings, "the git source checkout is stale"+gitStaleSuffix(st.SourceFetchedAt))
		}
	}

	return DoctorReport{
		Device:             dc.Device,
		AliasesPath:        dc.AliasesPath,
		PlatformProvenance: dc.PlatformProvenance,
		ShellProvenance:    dc.ShellProvenance,
		Issues:             issues,
		ProfileWarnings:    profileWarnings,
		Warnings:           warnings,
	}, nil
}

// gitStaleSuffix renders when the source last reached its origin, or
// nothing when that is unknown — mirroring cmd/aliasdeck/sync.go's
// fetchedSuffix, but local to internal/app since that helper lives in
// package main and cannot be imported from here.
func gitStaleSuffix(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return " (last reached the origin on " + at.Format(time.RFC3339) + ")"
}
