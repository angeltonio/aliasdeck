// Command aliasdeck-server is the AliasDeck control plane: it migrates its
// SQLite database and serves the REST API (and, later, the embedded web UI)
// that registered devices and operators talk to
// (docs/WHAT-WE-ARE-BUILDING.md, "aliasdeck-server — the control plane").
//
// This binary has exactly one job, so it has no subcommands — the root
// command itself serves, the way `caddy run` needs no verb beyond its own
// name and `nginx` does not ask for a "start" argument. A `serve` subcommand
// would only ever have one sibling: itself. `aliasdeck-server --addr
// 0.0.0.0:8080` reads as "run the server on this address", which is the only
// sentence this program exists to make true; `aliasdeck-server serve --addr
// ...` says the same thing with an extra word.
//
// It never imports internal/renderers: the server transmits data, the
// client produces shell syntax (design decision 2, docs/PROJECT.md
// §12.2/§6.1). It is the only binary allowed to import internal/store,
// internal/api, internal/server, internal/sync, and modernc.org/sqlite —
// cmd/aliasdeck (the client) is forbidden from importing any of them by
// internal/archtest.TestClientBinaryNeverImportsServerPackages.
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

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the root command and returns the process exit code. It is
// separated from main so cmd/aliasdeck-server itself stays testable without
// calling os.Exit — mirroring cmd/aliasdeck/main.go's own split.
//
// Unlike cmd/aliasdeck's run(), there is no business-logic/usage-error exit
// code split here: that granularity earns its complexity only when several
// subcommands need distinguishable exit codes for their own automation
// (cmd/aliasdeck's four exit codes exist because init/sync/status/doctor
// each fail in different ways a script might want to branch on). This
// binary has one command and one long-running action; cobra's own error
// printing already tells an operator what went wrong, so a plain 0/1 split
// is all a single-job process needs.
func run(args []string, stdout, stderr io.Writer) int {
	cmd := newRootCmd()
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.Execute(); err != nil {
		return 1
	}
	return 0
}

// defaultServeAddr is the address aliasdeck-server binds when --addr is not
// given: loopback only (design decision 21, bounded-review finding).
//
// An empty host (an earlier default, ":8080") binds every interface, so a
// zero-flag start was reachable from the LAN behind nothing but a bootstrap
// password printed once — and it directly contradicted this project's own
// client-side policy: decision 13 makes ServerSource refuse any non-HTTPS
// URL unless the host is loopback, re-checked on every sync. Widening the
// bind (e.g. behind a reverse proxy on another interface, which a
// self-hosted deployment may legitimately want) is still fully supported
// via --addr; it must be a deliberate operator choice, not the unflagged
// default.
const defaultServeAddr = "127.0.0.1:8080"

// serverDBFileName is the SQLite database file server.Run migrates and
// serves, resolved under config.Base's directory when --db is not given.
const serverDBFileName = "server.db"

// bootstrapPasswordFileName is where auth.Bootstrap writes a freshly
// generated operator password when stdout is not a terminal (design
// decision 22), resolved as a sibling of the database file.
const bootstrapPasswordFileName = "bootstrap-password.txt"

const setupCredentialFileName = "setup-credential.txt"

// newRootCmd builds the aliasdeck-server command tree: flags and
// environment resolve to a server.Config, and server.Run owns everything
// from there (migration, bootstrap, the bounded http.Server, and bounded
// shutdown).
func newRootCmd() *cobra.Command {
	var addr string
	var dbPath string

	cmd := &cobra.Command{
		Use:   "aliasdeck-server",
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
				SetupCredentialFile:   filepath.Join(filepath.Dir(resolvedDB), setupCredentialFileName),
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
// MkdirAll is a no-op on an existing directory, so a call site here using a
// different mode would have left the directory's actual mode dependent on
// which command happened to run first — not a deliberate choice. The
// database file itself is already 0600 regardless (decision 19): this mode
// governs only the shared directory, which also holds config.yaml and
// aliases.yaml when the client and server share a machine (e.g. local
// development).
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
// the generated password directly to stdout (isTerminal is true — stdout is
// a console a person will read); a non-empty result is the 0600 file path
// auth.Bootstrap must write it to instead, placed as a sibling of dbPath so
// both files live under the same operator-owned directory.
func bootstrapPasswordFilePath(isTerminal bool, dbPath string) string {
	if isTerminal {
		return ""
	}
	return filepath.Join(filepath.Dir(dbPath), bootstrapPasswordFileName)
}

// isTerminalWriter reports whether w is a real console a person could read
// from, using github.com/mattn/go-isatty against the underlying file
// descriptor. Only an *os.File can be inspected this way; anything else (a
// bytes.Buffer in a test, or any writer wrapping something other than a
// live file descriptor) is treated as not a terminal — the safer default
// when we cannot prove a person is watching is to route the secret to a
// 0600 file instead of assuming a console exists to print it to.
func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}
