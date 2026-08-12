package main

import (
	"fmt"

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
