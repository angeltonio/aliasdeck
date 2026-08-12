package main

import (
	"fmt"

	"github.com/angeltonio/aliasdeck/internal/app"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report the active source, device identity, and sync state.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			env := app.OSEnv()
			report, err := app.Status(cmd.Context(), env, app.Options{Shell: shellFlag(cmd)})
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Device:    %s (platform=%s, shell=%s)\n",
				report.Device.Name, report.Device.Platform, report.Device.Shell)
			fmt.Fprintf(out, "Platform:  %s (%s)\n", report.Device.Platform, report.PlatformProvenance)
			fmt.Fprintf(out, "Shell:     %s (%s)\n", report.Device.Shell, report.ShellProvenance)
			fmt.Fprintf(out, "Source:    %s (%s)\n", report.Source.Type, report.Source.Ref)
			fmt.Fprintf(out, "Backend:   %s\n", report.Backend)
			if report.State.LastSyncAt.IsZero() {
				fmt.Fprintln(out, "Last sync: never")
			} else {
				fmt.Fprintf(out, "Last sync: %s\n", report.State.LastSyncAt.Format("2006-01-02T15:04:05Z07:00"))
			}
			if report.UpToDate {
				fmt.Fprintln(out, "Status:    up to date")
			} else {
				fmt.Fprintln(out, "Status:    out of date — run `aliasdeck sync`")
			}
			return nil
		},
	}
}
