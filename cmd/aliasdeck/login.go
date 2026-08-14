package main

import (
	"fmt"

	"github.com/angeltonio/aliasdeck/internal/app"
	"github.com/spf13/cobra"
)

// newLoginCmd builds `aliasdeck login`: it authenticates the operator
// against a running server and stores the resulting session token outside
// config.yaml (design decision 17). Registered on the root command alongside
// `register`/`logout`/`serve` (task 8.14).
func newLoginCmd() *cobra.Command {
	var (
		url               string
		allowInsecureHTTP bool
		passwordStdin     bool
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate the operator against a self-hosted AliasDeck server.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			env := app.OSEnv()
			report, err := app.Login(cmd.Context(), env, app.LoginOptions{
				URL:               url,
				AllowInsecureHTTP: allowInsecureHTTP,
				PasswordStdin:     passwordStdin,
			})
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Logged in to %s (session expires %s)\n",
				report.ServerURL, report.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"))
			return nil
		},
	}

	cmd.Flags().StringVar(&url, "url", "", "the server's base URL, e.g. https://aliases.example.com (required)")
	cmd.Flags().BoolVar(&allowInsecureHTTP, "allow-insecure", false,
		"permit a non-loopback http:// url for this request (login only; use `register --allow-insecure` "+
			"to persist the same opt-out for every future sync)")
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false,
		"read the operator password from stdin instead of prompting interactively "+
			"(required when stdin is not a terminal, e.g. in a script or CI)")
	return cmd
}
