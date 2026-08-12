package main

import (
	"fmt"

	"github.com/angeltonio/aliasdeck/internal/app"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var source string
	var noBootstrap bool
	var assumeYes bool
	var rcFile string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create AliasDeck's configuration files and detect this device's platform and shell.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			env := app.OSEnv()
			report, err := app.Init(cmd.Context(), env, app.InitOptions{
				Source:      source,
				NoBootstrap: noBootstrap,
				AssumeYes:   assumeYes,
				Shell:       shellFlag(cmd),
				RCFile:      rcFile,
			})
			if err != nil {
				return err
			}
			printInitReport(cmd, report)
			return nil
		},
	}

	cmd.Flags().StringVar(&source, "source", "",
		"use an existing aliases.yaml at this path instead of creating a new one")
	cmd.Flags().BoolVar(&noBootstrap, "no-bootstrap", false,
		"create configuration files without editing any shell rc file")
	cmd.Flags().BoolVarP(&assumeYes, "yes", "f", false,
		"add the shell rc bootstrap line without asking (for non-interactive installs)")
	cmd.Flags().StringVar(&rcFile, "rc-file", "", "override the shell rc file path used for bootstrapping")
	return cmd
}

func printInitReport(cmd *cobra.Command, r app.InitReport) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Base directory: %s\n", r.Base)
	if r.ConfigCreated {
		fmt.Fprintln(out, "Created config.yaml")
	}
	if r.AliasesCreated {
		fmt.Fprintln(out, "Created aliases.yaml")
	}
	fmt.Fprintf(out, "Device: %s (platform=%s, shell=%s)\n",
		r.Device.Name, r.Device.Platform, r.Device.Shell)
	fmt.Fprintf(out, "Synced %d alias(es) to %s\n", r.Sync.AliasCount, r.Sync.OutputPath)

	switch {
	case r.BootstrapAdded:
		fmt.Fprintf(out, "Added AliasDeck's bootstrap line to %s\n", r.RCPath)
	case r.BootstrapSkippedReason != "":
		fmt.Fprintf(out, "Bootstrap line not added (%s). Add it manually to your shell rc file:\n  %s\n",
			r.BootstrapSkippedReason, r.ManualBootstrapLine)
	}
}
