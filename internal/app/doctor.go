package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/source"
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

// Doctor performs its own independent read-and-validate pass: the same
// domain.Resolve → validate.Config sequence a ConfigSource's Resolve runs
// internally, except the issues are returned instead of discarded.
//
// For a file-backed or Git-backed device it re-reads aliases.yaml directly
// and never calls Source.Resolve, so it stays read-only and offline for
// those sources. A server-backed device has no local file to re-read, so
// when Source implements source.UnfilteredResolver (design decision 12,
// ServerSource's additive interface), Doctor calls ResolveUnfiltered
// instead: the same resolved-but-not-yet-filtered configuration Resolve
// itself would have filtered, letting Doctor explain exactly what
// validate.FilterValid would drop and why (server-source spec's success
// criterion 3) without a second HTTP call and without widening
// ConfigSource.Resolve's own signature.
func Doctor(ctx context.Context, env Env, opts Options) (DoctorReport, error) {
	dc, err := loadDeviceContext(env, opts)
	if err != nil {
		return DoctorReport{}, err
	}
	return doctorFromContext(ctx, env, dc)
}

// doctorFromContext is Doctor's implementation over an already-resolved
// deviceContext, so a test can exercise the server-source branch (task 7.8)
// against a fake source.UnfilteredResolver without needing config.yaml's
// source.type: server to already resolve through resolveSource — that
// wiring is Phase 8's own task (8.1), not this one.
func doctorFromContext(ctx context.Context, env Env, dc deviceContext) (DoctorReport, error) {
	var issues validate.Issues
	var profileWarnings []string

	if ur, ok := dc.Source.(source.UnfilteredResolver); ok {
		resolved, err := ur.ResolveUnfiltered(ctx, dc.Device)
		if err != nil {
			return DoctorReport{}, err
		}
		issues = validate.Config(resolved)
	} else {
		data, err := os.ReadFile(dc.AliasesPath)
		if err != nil {
			return DoctorReport{}, fmt.Errorf("reading %s: %w", dc.AliasesPath, err)
		}
		doc, err := config.ParseAliases(data)
		if err != nil {
			return DoctorReport{}, ConfigError{Err: fmt.Errorf("parsing %s: %w", dc.AliasesPath, err)}
		}

		resolved := domain.Resolve(dc.Device, doc.Aliases)
		issues = validate.Config(resolved)
		profileWarnings = config.ProfileWarnings(doc.Profiles, doc.Aliases)
	}

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
