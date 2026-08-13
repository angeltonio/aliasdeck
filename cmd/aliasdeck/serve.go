package main

import (
	"io"
	"os"
	"path/filepath"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/server"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// defaultServeAddr is the address `aliasdeck serve` binds when --addr is
// not given: loopback only (design decision 21, bounded-review finding).
//
// An empty host (the previous default, ":8080") binds every interface, so
// a zero-flag `aliasdeck serve` was reachable from the LAN behind nothing
// but a bootstrap password printed once — and it directly contradicted
// this project's own client-side policy: decision 13 makes ServerSource
// refuse any non-HTTPS URL unless the host is loopback, re-checked on
// every sync. The default server configuration was producing exactly what
// the default client refuses to talk to. Widening the bind (e.g. behind a
// reverse proxy on another interface, which a self-hosted deployment may
// legitimately want) is still fully supported via --addr; it must now be
// a deliberate operator choice, not the unflagged default.
const defaultServeAddr = "127.0.0.1:8080"

// serverDBFileName is the SQLite database file server.Run migrates and
// serves, resolved under config.Base's directory when --db is not given.
// internal/config is unmodified this phase (design's File Changes table
// only adds CredentialsFile to it in Phase 7): the server owns its own
// database file name here rather than gaining a package-level helper with
// exactly one caller.
const serverDBFileName = "server.db"

// bootstrapPasswordFileName is where auth.Bootstrap writes a freshly
// generated operator password when stdout is not a terminal (design
// decision 22), resolved as a sibling of the database file.
const bootstrapPasswordFileName = "bootstrap-password.txt"

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

			stdout := cmd.OutOrStdout()

			return server.Run(cmd.Context(), server.Config{
				Addr:                  addr,
				DBPath:                resolvedDB,
				Getenv:                os.Getenv,
				Stdout:                stdout,
				BootstrapPasswordFile: bootstrapPasswordFilePath(isTerminalWriter(stdout), resolvedDB),
			})
		},
	}

	cmd.Flags().StringVar(&addr, "addr", defaultServeAddr,
		"address to listen on (default "+defaultServeAddr+", loopback only; "+
			"pass an explicit host, e.g. 0.0.0.0:8080, to accept connections from other machines — "+
			"do this deliberately, such as behind a reverse proxy on another interface)")
	cmd.Flags().StringVar(&dbPath, "db", "",
		"path to the SQLite database file (default: <base>/"+serverDBFileName+")")
	return cmd
}

// resolveServeDBPath returns explicit unconditionally, or else derives the
// default database path from config.Base and ensures that base directory
// exists, at mode 0755 — the same mode `init` (internal/app/init.go),
// config.yaml's own writer (internal/config/device.go), and state.json's
// writer (internal/state/state.go) already use for this same directory.
// MkdirAll is a no-op on an existing directory, so a call site here using
// a different mode (0700, as a previous version of this function did)
// would have left the directory's actual mode dependent on which command
// happened to run first — not a deliberate choice. The database file
// itself is already 0600 regardless (decision 19): this mode governs only
// the shared directory, which also holds config.yaml and aliases.yaml.
func resolveServeDBPath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	base, err := config.Base(config.OSEnv())
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(base, serverDBFileName), nil
}

// bootstrapPasswordFilePath is the routing decision behind design decision
// 22, isolated from the OS-level terminal probe so it is fully unit
// testable: an empty result tells server.Run to let auth.Bootstrap print
// the generated password directly to stdout (isTerminal is true — stdout
// is a console a person will read); a non-empty result is the 0600 file
// path auth.Bootstrap must write it to instead, placed as a sibling of
// dbPath so both files live under the same operator-owned directory.
func bootstrapPasswordFilePath(isTerminal bool, dbPath string) string {
	if isTerminal {
		return ""
	}
	return filepath.Join(filepath.Dir(dbPath), bootstrapPasswordFileName)
}

// isTerminalWriter reports whether w is a real console a person could read
// from, using github.com/mattn/go-isatty against the underlying file
// descriptor. Only an *os.File can be inspected this way; anything else
// (a bytes.Buffer in a test, or any writer wrapping something other than a
// live file descriptor) is treated as not a terminal — the opposite
// default from internal/app/prompt.go's isInteractive, deliberately: that
// helper's non-interactive-by-default-false exists so scripted test input
// still works when reading, but here the safer default when we cannot
// prove a person is watching is to route the secret to a 0600 file instead
// of assuming a console exists to print it to.
func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}
