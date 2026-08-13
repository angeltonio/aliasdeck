package main

import (
	"fmt"
	"time"

	"github.com/angeltonio/aliasdeck/internal/app"
	"github.com/spf13/cobra"
)

func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Resolve, validate, render, and apply the active configuration to this device.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			env := app.OSEnv()
			report, err := app.Sync(cmd.Context(), env, app.Options{Shell: shellFlag(cmd)})
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()

			// Printed before the outcome, not after. A user who reads one
			// line reads this one, and "your aliases are older than you
			// think" changes how they read whatever follows.
			if report.SourceStale {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"Warning: could not reach %s; using the last content fetched%s\n",
					report.Source.Ref, fetchedSuffix(report.SourceFetchedAt))
			}

			if report.Skipped {
				fmt.Fprintf(out, "Up to date: %d alias(es), no write needed (%s)\n",
					report.AliasCount, report.OutputPath)
			} else {
				fmt.Fprintf(out, "Applied %d alias(es) to %s\n", report.AliasCount, report.OutputPath)
			}
			return nil
		},
	}
}

// fetchedSuffix renders when a source last reached its origin, or nothing at
// all when that is unknown.
//
// A zero time formats as year one, which would read as a bug rather than as
// "we have never managed to fetch this".
func fetchedSuffix(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return " on " + at.Format(time.RFC3339)
}
