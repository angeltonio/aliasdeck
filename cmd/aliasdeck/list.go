package main

import (
	"fmt"

	"github.com/angeltonio/aliasdeck/internal/app"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show the aliases declared in the active source, marking which apply to this device.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			env := app.OSEnv()
			report, err := app.List(cmd.Context(), env, app.Options{Shell: shellFlag(cmd)})
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for _, e := range report.Entries {
				if e.Active {
					fmt.Fprintf(out, "[active]  %-20s %s\n", e.Alias.Name, e.Alias.Command)
				} else {
					fmt.Fprintf(out, "[skipped] %-20s %s (%s)\n", e.Alias.Name, e.Alias.Command, e.Reason)
				}
			}
			return nil
		},
	}
}
