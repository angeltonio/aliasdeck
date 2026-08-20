package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/store/sqlitestore"
	"github.com/spf13/cobra"
)

// resetPasswordFileName is where reset-password writes a freshly generated
// operator password when stdout is not a terminal, resolved as a sibling of
// the database file. It is deliberately not bootstrapPasswordFileName: an
// operator recovering access should not have to work out whether the file
// beside their database is from first start or from the reset they just ran,
// and a reset must never appear to have left the original file untouched.
const resetPasswordFileName = "reset-password.txt"

// defaultOperatorUsername is the account auth.Bootstrap creates. It is
// repeated here rather than exported from internal/auth because this is a
// CLI default an operator can override, not a shared invariant: a
// deployment that later grows a second operator changes what this flag
// should point at without changing what bootstrap once created.
const defaultOperatorUsername = "admin"

// newResetPasswordCmd builds `aliasdeck-server reset-password`.
//
// This is the one recovery path for an operator who can no longer log in.
// Nothing else in the binary can do it: the web setup flow refuses once an
// operator exists, ALIASDECK_ADMIN_PASSWORD is only read while the database
// has none, and the store's Create rejects a username already taken — so
// without this the only route back into a deployment was deleting the
// database, and with it every alias, profile, device and token.
//
// It takes no password argument by design. The new password comes from
// ALIASDECK_ADMIN_PASSWORD or is generated, never from a flag: argv is
// readable by any process on the machine via ps and is written to shell
// history, so a --password flag would publish the new credential to the
// same local observers a reset may be defending against.
func newResetPasswordCmd() *cobra.Command {
	var dbPath string
	var username string

	cmd := &cobra.Command{
		Use:   "reset-password",
		Short: "Replace an operator's password and revoke their sessions.",
		Long: "Replace the password of an operator who can no longer log in, and revoke every " +
			"session they hold.\n\n" +
			"Authorization is access to the database itself: whoever can open it can already read " +
			"every hash and token in it, so this grants nothing that filesystem access did not " +
			"already carry. Run it on the machine that holds the data directory — inside the " +
			"container for a Compose deployment.\n\n" +
			"The new password is taken from " + auth.AdminPasswordEnv + " when that is set, and " +
			"generated otherwise. It is never accepted as a flag, because argv is visible to " +
			"other processes and is recorded in shell history.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			resolvedDB, err := resolveServeDBPath(dbPath)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			// Opens and migrates exactly as serving does. A reset does not
			// need the server stopped: decision 7's WAL mode plus
			// busy_timeout means a second process writing one row contends
			// rather than fails, and revoking the sessions below is what
			// makes the change take effect on a running server.
			st, err := sqlitestore.Open(ctx, resolvedDB)
			if err != nil {
				return err
			}
			defer st.Close()

			stdout := cmd.OutOrStdout()
			return auth.ResetPasswordFromEnvOrGenerated(
				ctx, st, time.Now, os.Getenv, stdout,
				resetPasswordFilePath(isTerminalWriter(stdout), resolvedDB),
				username,
			)
		},
	}

	cmd.Flags().StringVar(&dbPath, "db", "",
		"path to the SQLite database file (default: <base>/"+serverDBFileName+")")
	cmd.Flags().StringVar(&username, "username", defaultOperatorUsername,
		"operator whose password to replace")
	return cmd
}

// resetPasswordFilePath is bootstrapPasswordFilePath's routing decision
// (design decision 22) applied to the reset path, kept separate from the OS
// terminal probe for the same reason: an empty result means stdout is a
// console the password can be printed to, and a non-empty result is the 0600
// file it must be written to instead.
func resetPasswordFilePath(isTerminal bool, dbPath string) string {
	if isTerminal {
		return ""
	}
	return filepath.Join(filepath.Dir(dbPath), resetPasswordFileName)
}
