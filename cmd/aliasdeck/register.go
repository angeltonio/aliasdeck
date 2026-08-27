package main

import (
	"fmt"

	"github.com/angeltonio/aliasdeck/internal/app"
	"github.com/spf13/cobra"
)

// newRegisterCmd builds `aliasdeck register`: it exchanges a single-use
// enrollment token for a device token, stores it separately from
// config.yaml at 0600, and flips config.yaml's source.type to server (design
// decision 14; cli-commands spec, "register Consumes a Single-Use
// Enrollment Token"). Registered on the root command alongside
// `login`/`logout` (task 8.14).
func newRegisterCmd() *cobra.Command {
	var (
		url               string
		token             string
		deviceToken       string
		allowInsecureHTTP bool
		force             bool
	)

	cmd := &cobra.Command{
		Use:   "register",
		Short: "Exchange an enrollment token for a device token and switch this device to a server source.",
		Long: "Exchange an enrollment token for a device token and switch this device to a server source.\n\n" +
			"With --device-token instead of --token, adopt a credential the server has already issued — what an\n" +
			"operator gets by rotating this machine's token. That path keeps this machine's server-side identity;\n" +
			"enrolling again would create a second device and abandon the aliases pinned to the first.\n\n" +
			"Either way the token is verified against the server before anything on this machine is written, so a\n" +
			"token that does not work cannot replace one that does.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			env := app.OSEnv()
			report, err := app.Register(cmd.Context(), env, app.RegisterOptions{
				Options:           app.Options{Shell: shellFlag(cmd)},
				URL:               url,
				Token:             token,
				DeviceToken:       deviceToken,
				AllowInsecureHTTP: allowInsecureHTTP,
				Force:             force,
			})
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Registered device %s with %s\n", report.DeviceID, report.ServerURL)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false,
		"register again even if this device already holds a device token; "+
			"this mints a second device on the server and abandons the current one")
	cmd.Flags().StringVar(&url, "url", "", "the server's base URL, e.g. https://aliases.example.com (required)")
	cmd.Flags().StringVar(&token, "token", "", "the single-use enrollment token issued by the server operator")
	cmd.Flags().StringVar(&deviceToken, "device-token", "",
		"adopt an already-issued device token instead of enrolling, for example after an operator rotated "+
			"this machine's credential; keeps this device's server-side identity")
	// Deliberately not MarkFlagsOneRequired/MarkFlagsMutuallyExclusive.
	// app.Register already enforces both rules, and cobra's group messages
	// intercept before RunE with wording that names neither --url nor which
	// token kind was expected — strictly less useful than what Register
	// already says.
	cmd.Flags().BoolVar(&allowInsecureHTTP, "allow-insecure", false,
		"permit a non-loopback http:// url; persisted into config.yaml so every future sync honors it too")
	return cmd
}
