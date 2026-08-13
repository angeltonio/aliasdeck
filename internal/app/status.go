package app

import (
	"context"
	"time"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/source"
	"github.com/angeltonio/aliasdeck/internal/state"
)

// StatusReport is what `aliasdeck status` always prints (cli-commands
// spec, "status Always Reports the Active Source").
type StatusReport struct {
	Base    string
	Source  source.Descriptor
	Backend string
	Device  domain.Device

	PlatformProvenance string
	ShellProvenance    string

	// PowerShellEdition, PowerShellProfilePath and PowerShellProvenance are
	// populated only for a PowerShell device (design decision 8), so a
	// zsh/bash StatusReport keeps them at their zero value. The choice of
	// edition is a heuristic (LookPath precedence, possibly OneDrive
	// redirection); reporting it, and why it was made, turns a silent wrong
	// guess into an obvious one (non-negotiable constraint 2).
	PowerShellEdition     string
	PowerShellProfilePath string
	PowerShellProvenance  string

	// SourceRef, SourceStale and SourceFetchedAt come from the last
	// successful sync's recorded state, not a live re-resolve: status must
	// never spawn a git process just to report on one (design decision 14).
	// For a GitSource, SourceRef includes the resolved commit
	// (<url>#<ref>@<short-sha>); for a FileSource these mirror state.State's
	// zero value and are not meaningful.
	SourceRef       string
	SourceStale     bool
	SourceFetchedAt time.Time

	State    state.State
	UpToDate bool
}

// Status reports the active source, device identity, last sync time, and
// whether the generated file is current, every time it is called.
func Status(_ context.Context, env Env, opts Options) (StatusReport, error) {
	dc, err := loadDeviceContext(env, opts)
	if err != nil {
		return StatusReport{}, err
	}

	st, err := state.Load(config.StateFile(dc.Base))
	if err != nil {
		return StatusReport{}, err
	}

	upToDate := false
	if outputPath, err := dc.Backend.OutputPath(dc.Device); err == nil {
		upToDate = st.OutputPath == outputPath && diskHashMatches(outputPath, st.OutputHash)
	}

	report := StatusReport{
		Base:               dc.Base,
		Source:             dc.SourceDesc,
		Backend:            dc.Backend.Name(),
		Device:             dc.Device,
		PlatformProvenance: dc.PlatformProvenance,
		ShellProvenance:    dc.ShellProvenance,
		SourceRef:          st.SourceRef,
		SourceStale:        st.SourceStale,
		SourceFetchedAt:    st.SourceFetchedAt,
		State:              st,
		UpToDate:           upToDate,
	}

	if dc.Device.Shell == domain.ShellPowerShell {
		profile, err := resolvePowerShellProfile(env, dc.Device.Platform)
		if err != nil {
			return StatusReport{}, err
		}
		report.PowerShellEdition = string(profile.Edition)
		report.PowerShellProfilePath = profile.Path
		report.PowerShellProvenance = profile.Provenance
	}

	return report, nil
}
