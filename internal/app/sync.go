package app

import (
	"context"
	"fmt"
	"time"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/renderers"
	"github.com/angeltonio/aliasdeck/internal/source"
	"github.com/angeltonio/aliasdeck/internal/state"
)

// SyncReport summarizes what one `sync` run did (cli-commands spec, "sync
// Runs the Full Pipeline").
type SyncReport struct {
	Device     domain.Device
	Source     source.Descriptor
	Backend    string
	OutputPath string
	Revision   string
	AliasCount int

	// Skipped is true when the resolved revision and the on-disk output
	// hash both already matched recorded state, so nothing was written
	// (sync-state spec, "No-Op Skip When Unchanged").
	Skipped bool

	// SourceStale reports that the source served content it had cached
	// because it could not reach its origin.
	//
	// It is set even when the sync is skipped, and that is the whole point.
	// The common shape of an offline sync is an unchanged configuration, so
	// reporting staleness only when something was written would stay silent
	// in exactly the case a user needs to hear about: aliases that look
	// current but describe a repository as it was days ago.
	SourceStale bool

	// SourceFetchedAt is when the source last reached its origin
	// successfully. Zero for sources that never go anywhere, such as a
	// local file.
	SourceFetchedAt time.Time
}

// Sync runs the full pipeline: resolve the active source, render its
// aliases for this device, write the result through the configured
// backend, and record the outcome in local state.
func Sync(ctx context.Context, env Env, opts Options) (SyncReport, error) {
	dc, err := loadDeviceContext(env, opts)
	if err != nil {
		return SyncReport{}, err
	}
	return syncWithContext(ctx, env, dc)
}

// syncWithContext runs the pipeline against an already-loaded
// deviceContext, so Init can perform its initial sync without resolving
// config.yaml a second time.
func syncWithContext(ctx context.Context, env Env, dc deviceContext) (SyncReport, error) {
	cfg, err := dc.Source.Resolve(ctx, dc.Device)
	if err != nil {
		return SyncReport{}, fmt.Errorf("resolving %s: %w", dc.SourceDesc.Ref, err)
	}
	cfg.GeneratedAt = env.Now()

	rendered, err := renderers.Render(cfg)
	if err != nil {
		return SyncReport{}, err
	}

	outputPath, err := dc.Backend.OutputPath(cfg.Device)
	if err != nil {
		return SyncReport{}, err
	}

	statePath := config.StateFile(dc.Base)
	prevState, err := state.Load(statePath)
	if err != nil {
		return SyncReport{}, err
	}

	report := SyncReport{
		Device:     cfg.Device,
		Source:     dc.SourceDesc,
		Backend:    dc.Backend.Name(),
		OutputPath: outputPath,
		Revision:   cfg.Revision,
		AliasCount: len(cfg.Aliases),
	}

	outputHash := hashBytes([]byte(rendered))

	// A source that can report what its last Resolve call actually did
	// (GitSource) is type-asserted here rather than widening
	// ConfigSource.Resolve's signature (design decision 14; PROJECT.md §7's
	// signature is verbatim and shared with ServerSource in M4). FileSource
	// does not implement it, so a file-sourced sync leaves these at their
	// zero value.
	//
	// This is read *before* the no-op skip, so a stale resolve is reported
	// even when nothing is written. Offline syncs are usually unchanged
	// syncs, and staleness that only surfaced alongside a write would stay
	// quiet in precisely the case that matters.
	sourceRef := dc.SourceDesc.Ref
	var sourceStale bool
	var sourceFetchedAt time.Time
	if rep, ok := dc.Source.(source.ResolveReporter); ok {
		info := rep.LastResolve()
		sourceStale = info.Stale
		sourceFetchedAt = info.FetchedAt
		if info.Commit != "" {
			sourceRef = dc.SourceDesc.Ref + "@" + source.ShortSHA(info.Commit)
		}
	}
	report.SourceStale = sourceStale
	report.SourceFetchedAt = sourceFetchedAt

	// No-op skip (design decision 5): the resolved revision alone is not
	// enough — the on-disk file must still match too, or a hand-edited or
	// deleted generated file would be silently reported as "up to date".
	//
	// Nothing is written here, state included: the guarantee is that a
	// repeated sync touches no file at all. Staleness reaches the user
	// through the report instead.
	if prevState.Revision == cfg.Revision && diskHashMatches(outputPath, prevState.OutputHash) {
		report.Skipped = true
		return report, nil
	}

	if err := dc.Backend.Apply(ctx, cfg, rendered); err != nil {
		return SyncReport{}, fmt.Errorf("applying to %s: %w", outputPath, err)
	}

	newState := state.State{
		Version:         1,
		Revision:        cfg.Revision,
		OutputPath:      outputPath,
		OutputHash:      outputHash,
		AliasCount:      len(cfg.Aliases),
		Platform:        cfg.Device.Platform,
		Shell:           cfg.Device.Shell,
		SourceType:      dc.SourceDesc.Type,
		SourceRef:       sourceRef,
		SourceStale:     sourceStale,
		SourceFetchedAt: sourceFetchedAt,
		LastSyncAt:      env.Now(),
		ClientVersion:   Version,
		Bootstrap:       prevState.Bootstrap,
	}
	if err := state.Save(statePath, newState); err != nil {
		return SyncReport{}, fmt.Errorf(
			"aliases are already applied to %s, but saving sync state failed: %w", outputPath, err)
	}

	return report, nil
}
