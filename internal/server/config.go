package server

import (
	"context"
	"io"
	"net"
	"os"
	"time"

	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/angeltonio/aliasdeck/internal/store/sqlitestore"
)

// Config configures Run. Every field that would otherwise touch the real
// machine — a fixed port, the real process environment, a hardcoded
// database path — is overridable so tests bind ephemeral listeners, never
// read real environment variables, and never touch a database outside
// t.TempDir().
type Config struct {
	// Addr is passed to net.Listen("tcp", Addr) when Listen is nil. Tests
	// must use "127.0.0.1:0" (an ephemeral port), never a fixed port.
	Addr string

	// DBPath is the SQLite database file OpenStore opens (and migrates)
	// when OpenStore is nil.
	DBPath string

	// Getenv supplies auth.Bootstrap's ALIASDECK_ADMIN_PASSWORD lookup.
	// Defaults to os.Getenv.
	Getenv func(string) string

	// Stdout receives auth.Bootstrap's one-time generated operator
	// password, or — when BootstrapPasswordFile is set — a short notice
	// naming that file instead of the password itself (design decision
	// 22). Defaults to io.Discard.
	Stdout io.Writer

	// BootstrapPasswordFile, when non-empty, is where auth.Bootstrap
	// writes a freshly generated operator password instead of printing it
	// to Stdout (design decision 22): under systemd's default
	// StandardOutput=journal, Stdout is a persistent log, and server-auth
	// spec.md forbids writing the password to any log. The caller
	// (cmd/aliasdeck-server/main.go) is the one place that knows whether Stdout
	// is really a console a person will read, and resolves this field
	// accordingly; Run and Config never inspect Stdout themselves. Empty
	// means "Stdout is a console — print the password directly", which is
	// also this field's zero-value default, so every existing test that
	// leaves it unset keeps the original console-printing behavior.
	BootstrapPasswordFile string

	// SetupCredentialFile is the 0600 one-time credential file for interactive
	// first-run operator setup.
	SetupCredentialFile string

	// ShutdownTimeout bounds the graceful drain Run gives in-flight
	// requests once shutdown starts (design's Bounded Operations table:
	// "Shutdown ... srv.Shutdown(ctx) with a 10s drain"). Defaults to
	// defaultShutdownTimeout. Tests inject a short bound so a deliberately
	// non-draining request does not make the suite itself wait 10 real
	// seconds; production always gets the 10s default via
	// cmd/aliasdeck-server/main.go leaving this field zero.
	ShutdownTimeout time.Duration

	// OpenStore opens the store Run migrates and serves. Defaults to
	// sqlitestore.Open against DBPath — the only backend this milestone
	// ships (design decision 3). Tests substitute a fake to observe
	// migration-before-accept ordering, or point DBPath at a database
	// forged to look newer than this binary to prove the refusal surfaces
	// through Run without ever calling Listen.
	OpenStore func(ctx context.Context) (store.Store, error)

	// Listen creates the listener Run serves on. Defaults to
	// net.Listen("tcp", Addr). Tests substitute one bound to port 0 so the
	// suite never breaks on a developer machine with something already
	// bound to a fixed port.
	Listen func() (net.Listener, error)
}

// defaultShutdownTimeout is the production drain bound (design's Bounded
// Operations table).
const defaultShutdownTimeout = 10 * time.Second

// withDefaults returns a copy of cfg with every nil/zero field replaced by
// its production default. It never mutates cfg itself.
func (cfg Config) withDefaults() Config {
	if cfg.Getenv == nil {
		cfg.Getenv = os.Getenv
	}
	if cfg.Stdout == nil {
		cfg.Stdout = io.Discard
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = defaultShutdownTimeout
	}
	if cfg.OpenStore == nil {
		dbPath := cfg.DBPath
		cfg.OpenStore = func(ctx context.Context) (store.Store, error) {
			return sqlitestore.Open(ctx, dbPath)
		}
	}
	if cfg.Listen == nil {
		addr := cfg.Addr
		cfg.Listen = func() (net.Listener, error) {
			return net.Listen("tcp", addr)
		}
	}
	return cfg
}
