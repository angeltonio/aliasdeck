package app

import (
	"context"

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

	return StatusReport{
		Base:               dc.Base,
		Source:             dc.SourceDesc,
		Backend:            dc.Backend.Name(),
		Device:             dc.Device,
		PlatformProvenance: dc.PlatformProvenance,
		ShellProvenance:    dc.ShellProvenance,
		State:              st,
		UpToDate:           upToDate,
	}, nil
}
