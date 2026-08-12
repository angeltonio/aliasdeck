package main

import (
	"fmt"

	"github.com/angeltonio/aliasdeck/internal/app"
	"github.com/spf13/cobra"
)

func newEditCmd() *cobra.Command {
	var editConfig bool

	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Open aliases.yaml (or config.yaml with --config) in $EDITOR.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			target := app.EditTargetAliases
			if editConfig {
				target = app.EditTargetConfig
			}

			env := app.OSEnv()
			report, err := app.Edit(cmd.Context(), env, app.EditOptions{
				Options: app.Options{Shell: shellFlag(cmd)},
				Target:  target,
			})
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Edited %s\n", report.Path)
			return nil
		},
	}

	cmd.Flags().BoolVar(&editConfig, "config", false, "edit config.yaml instead of aliases.yaml")
	return cmd
}
