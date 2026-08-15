package main

import (
	"github.com/spf13/cobra"
)

// newRootCmd builds the `aliasdeck` command tree.
//
// SilenceErrors is always on: run() prints every error itself so it can
// choose the right exit code. SilenceUsage starts false and each
// subcommand's RunE sets it to true as its very first line — see
// exitCodeFor's doc comment for why that distinguishes a Cobra usage error
// from a business-logic error.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "aliasdeck",
		Short:         "Cross-shell alias manager: one source of truth, rendered per shell.",
		SilenceErrors: true,
	}

	root.PersistentFlags().String("shell", "", "override shell detection (zsh, bash)")

	root.AddCommand(
		newInitCmd(),
		newSyncCmd(),
		newHeartbeatCmd(),
		newWatchCmd(),
		newAgentCmd(),
		newStatusCmd(),
		newListCmd(),
		newDoctorCmd(),
		newEditCmd(),
		newUninstallCmd(),
		newLoginCmd(),
		newRegisterCmd(),
		newLogoutCmd(),
	)
	return root
}

// shellFlag reads the --shell flag shared by every command.
func shellFlag(cmd *cobra.Command) string {
	v, _ := cmd.Flags().GetString("shell")
	return v
}
