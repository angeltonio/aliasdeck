package app

import (
	"context"
	"fmt"

	"github.com/angeltonio/aliasdeck/internal/source"
)

// HeartbeatReport summarizes one explicit device-reachability report.
type HeartbeatReport struct {
	Source source.Descriptor
}

// Heartbeat tells the active server source that this device is reachable. It
// neither resolves aliases nor writes rendered output or sync state.
func Heartbeat(ctx context.Context, env Env, opts Options) (HeartbeatReport, error) {
	dc, err := loadDeviceContext(env, opts)
	if err != nil {
		return HeartbeatReport{}, err
	}

	heartbeater, ok := dc.Source.(source.Heartbeater)
	if !ok {
		return HeartbeatReport{}, fmt.Errorf("heartbeat requires a server source; active source is %s", dc.SourceDesc.Type)
	}
	if err := heartbeater.Heartbeat(ctx); err != nil {
		return HeartbeatReport{}, err
	}
	return HeartbeatReport{Source: dc.SourceDesc}, nil
}
