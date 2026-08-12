package main

import (
	"fmt"

	"github.com/angeltonio/aliasdeck/internal/app"
	"github.com/spf13/cobra"
)

func newUninstallCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the generated file and the shell rc bootstrap line.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			env := app.OSEnv()
			report, err := app.Uninstall(cmd.Context(), env, app.UninstallOptions{
				Options: app.Options{Shell: shellFlag(cmd)},
				Yes:     yes,
			})
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if report.Cancelled {
				fmt.Fprintln(out, "Uninstall cancelled.")
				return nil
			}

			if report.OutputRemoved {
				fmt.Fprintf(out, "Removed %s\n", report.OutputPath)
			}
			if report.BootstrapRemoved {
				fmt.Fprintf(out, "Removed AliasDeck's bootstrap line from %s\n", report.RCPath)
				if !report.BootstrapExact {
					fmt.Fprintf(out,
						"Warning: %s had been edited inside AliasDeck's block; "+
							"removal used a fallback marker scan and may not be byte-identical to the original.\n",
						report.RCPath)
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "f", false, "skip the confirmation prompt")
	return cmd
}
