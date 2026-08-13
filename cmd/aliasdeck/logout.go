package main

import (
	"fmt"

	"github.com/angeltonio/aliasdeck/internal/app"
	"github.com/spf13/cobra"
)

// newLogoutCmd builds `aliasdeck logout`: it removes the locally stored
// operator session only, and never contacts the server (design decision 17;
// cli-commands spec, "logout Clears the Locally Stored Session"). Registered
// on the root command alongside `login`/`register`/`serve` (task 8.14).
func newLogoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Clear the locally stored operator session (never contacts the server).",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			env := app.OSEnv()
			report, err := app.Logout(cmd.Context(), env, app.Options{})
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if report.SessionCleared {
				fmt.Fprintln(out, "Logged out.")
			} else {
				fmt.Fprintln(out, "No local session was stored.")
			}
			return nil
		},
	}
	return cmd
}
