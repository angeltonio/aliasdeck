package main

import (
	"fmt"
	"time"

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
			if report.PowerShellEdition != "" {
				// "Profile" rather than "PowerShell" so the label column stays
				// aligned; the Shell line directly above already says which
				// shell this profile belongs to.
				fmt.Fprintf(out, "Profile:   %s edition, %s (%s)\n",
					report.PowerShellEdition, report.PowerShellProfilePath, report.PowerShellProvenance)
			}
			fmt.Fprintf(out, "Source:    %s (%s)\n", report.Source.Type, report.Source.Ref)
			if report.Source.Type == "git" && report.SourceRef != "" {
				fmt.Fprintf(out, "Git ref:   %s%s\n", report.SourceRef, staleSuffix(report.SourceStale, report.SourceFetchedAt))
			}
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

// staleSuffix renders a explicit staleness warning appended to the git ref
// line, or nothing at all when the checkout is current — status must never
// let a stale-but-unremarked ref look identical to a fresh one.
func staleSuffix(stale bool, fetchedAt time.Time) string {
	if !stale {
		return ""
	}
	return " — STALE, using cached content" + fetchedSuffix(fetchedAt)
}
