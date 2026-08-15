package main

import (
	"fmt"

	"github.com/angeltonio/aliasdeck/internal/app"
	"github.com/spf13/cobra"
)

func newHeartbeatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "heartbeat",
		Short: "Report this device as reachable without synchronizing aliases.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			report, err := app.Heartbeat(cmd.Context(), app.OSEnv(), app.Options{Shell: shellFlag(cmd)})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Heartbeat recorded with %s\n", report.Source.Ref)
			return nil
		},
	}
}
