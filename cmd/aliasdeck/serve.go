package main

import (
	"os"
	"path/filepath"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/server"
	"github.com/spf13/cobra"
)

// defaultServeAddr is the address `aliasdeck serve` binds when --addr is
// not given.
const defaultServeAddr = ":8080"

// serverDBFileName is the SQLite database file server.Run migrates and
// serves, resolved under config.Base's directory when --db is not given.
// internal/config is unmodified this phase (design's File Changes table
// only adds CredentialsFile to it in Phase 7): the server owns its own
// database file name here rather than gaining a package-level helper with
// exactly one caller.
const serverDBFileName = "server.db"

// newServeCmd builds `aliasdeck serve`: it is the only file under
// cmd/aliasdeck that imports internal/server (design decision 1) — flags
// and environment resolve to a server.Config, and server.Run owns
// everything from there (migration, bootstrap, the bounded http.Server,
// and bounded shutdown).
func newServeCmd() *cobra.Command {
	var addr string
	var dbPath string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the AliasDeck server: migrate its database and serve the control-plane API.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			resolvedDB, err := resolveServeDBPath(dbPath)
			if err != nil {
				return err
			}

			return server.Run(cmd.Context(), server.Config{
				Addr:   addr,
				DBPath: resolvedDB,
				Getenv: os.Getenv,
				Stdout: cmd.OutOrStdout(),
			})
		},
	}

	cmd.Flags().StringVar(&addr, "addr", defaultServeAddr, "address to listen on")
	cmd.Flags().StringVar(&dbPath, "db", "",
		"path to the SQLite database file (default: <base>/"+serverDBFileName+")")
	return cmd
}

// resolveServeDBPath returns explicit unconditionally, or else derives the
// default database path from config.Base and ensures that base directory
// exists (mirroring the base-directory creation `init` already performs
// for config.yaml/aliases.yaml).
func resolveServeDBPath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	base, err := config.Base(config.OSEnv())
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(base, serverDBFileName), nil
}
