package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/angeltonio/aliasdeck/internal/app"
	"github.com/spf13/cobra"
)

const defaultWatchInterval = 5 * time.Minute

type watchTimer interface {
	Chan() <-chan time.Time
	Stop() bool
}

type standardWatchTimer struct {
	*time.Timer
}

func (t standardWatchTimer) Chan() <-chan time.Time { return t.C }

type watchDeps struct {
	heartbeat func(context.Context) error
	sync      func(context.Context) error
	newTimer  func(time.Duration) watchTimer
	stdout    io.Writer
	stderr    io.Writer
}

func newWatchCmd() *cobra.Command {
	var interval time.Duration

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Report reachability and synchronize aliases periodically.",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if interval <= 0 {
				return fmt.Errorf("interval must be greater than zero")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			env := app.OSEnv()
			opts := app.Options{Shell: shellFlag(cmd)}

			return runWatch(cmd.Context(), interval, watchDeps{
				heartbeat: func(ctx context.Context) error {
					_, err := app.Heartbeat(ctx, env, opts)
					return err
				},
				sync: func(ctx context.Context) error {
					_, err := app.Sync(ctx, env, opts)
					return err
				},
				newTimer: func(d time.Duration) watchTimer {
					return standardWatchTimer{Timer: time.NewTimer(d)}
				},
				stdout: cmd.OutOrStdout(),
				stderr: cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().DurationVar(&interval, "interval", defaultWatchInterval, "Time between heartbeat and sync runs")
	return cmd
}

// runWatch performs a heartbeat followed by a sync immediately, then repeats
// after every interval. Operational errors are reported but do not terminate
// the foreground watcher; cancellation is the only normal exit.
func runWatch(ctx context.Context, interval time.Duration, deps watchDeps) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		heartbeatErr := deps.heartbeat(ctx)
		if heartbeatErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(deps.stderr, "watch heartbeat: %v\n", heartbeatErr)
		}
		syncErr := deps.sync(ctx)
		if syncErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(deps.stderr, "watch sync: %v\n", syncErr)
		}
		if deps.stdout != nil && heartbeatErr == nil && syncErr == nil {
			fmt.Fprintln(deps.stdout, "watch: heartbeat ok; sync ok")
		}

		timer := deps.newTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.Chan():
		}
	}
}
