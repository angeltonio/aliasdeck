package server

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/angeltonio/aliasdeck/internal/store/sqlitestore"
)

// TestNewHTTPServerAppliesEveryBound asserts every field in design.md's
// Bounded Operations table ("Accept / read / write") directly on the
// constructed *http.Server, not on observed behavior — so dropping any one
// of them (e.g. deleting ReadHeaderTimeout) fails this test immediately
// instead of only showing up as a slow-loris incident later.
func TestNewHTTPServerAppliesEveryBound(t *testing.T) {
	srv := newHTTPServer(http.NewServeMux())

	durations := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"ReadHeaderTimeout", srv.ReadHeaderTimeout, 5 * time.Second},
		{"ReadTimeout", srv.ReadTimeout, 15 * time.Second},
		{"WriteTimeout", srv.WriteTimeout, 30 * time.Second},
		{"IdleTimeout", srv.IdleTimeout, 60 * time.Second},
	}
	for _, d := range durations {
		if d.got != d.want {
			t.Errorf("%s = %v, want %v", d.name, d.got, d.want)
		}
	}
	if srv.MaxHeaderBytes != 64<<10 {
		t.Errorf("MaxHeaderBytes = %d, want %d", srv.MaxHeaderBytes, 64<<10)
	}
}

// TestRunOpensAndMigratesStoreBeforeListening proves the ordering the
// server-runtime spec requires ("Migrations Apply on Startup ... before
// accepting any HTTP connection"): OpenStore (which, with the production
// default, is sqlitestore.Open — migration included) must run before
// Listen. Both calls send their name to the same channel from Run's own
// goroutine, so the receive order in this test *is* the call order: this
// fails, not just "observes both happened", if Run is edited to call Listen
// first.
func TestRunOpensAndMigratesStoreBeforeListening(t *testing.T) {
	events := make(chan string, 4)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := Config{
		OpenStore: func(_ context.Context) (store.Store, error) {
			events <- "open-store"
			return &fakeStore{}, nil
		},
		Listen: func() (net.Listener, error) {
			events <- "listen"
			return net.Listen("tcp", "127.0.0.1:0")
		},
	}

	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()

	first := <-events
	second := <-events

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	if first != "open-store" || second != "listen" {
		t.Fatalf("call order = [%s %s], want [open-store listen]: OpenStore (which migrates in production) must complete before Listen is ever called", first, second)
	}
}

// TestRunRefusesNewerSchemaAndNeverListens is the server-runtime spec's
// "Newer schema version refused" scenario, exercised through Run itself
// against a real SQLite file: a database recording a schema version no
// migration in this binary embeds must make Run return an error wrapping
// store.ErrSchemaNewer, and Run must never call Listen — not one
// connection may be accepted.
func TestRunRefusesNewerSchemaAndNeverListens(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "server.db")

	seed, err := sqlitestore.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("seeding database via sqlitestore.Open: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("closing seed store: %v", err)
	}
	forgeNewerSchema(t, dbPath)

	listenCalled := false
	cfg := Config{
		DBPath: dbPath,
		Listen: func() (net.Listener, error) {
			listenCalled = true
			return nil, errors.New("Listen must not be called when the store refuses to open")
		},
	}

	err = Run(context.Background(), cfg)
	if !errors.Is(err, store.ErrSchemaNewer) {
		t.Fatalf("Run() = %v, want an error wrapping store.ErrSchemaNewer", err)
	}
	if listenCalled {
		t.Fatal("Run() called Listen after the store refused a newer schema — it must not accept a single connection")
	}
}

// forgeNewerSchema inserts a schema_migrations row recording a version
// number no migration in this binary embeds, simulating "this database was
// last migrated by a newer binary" without needing a second, ahead
// migration fixture. schema_migrations is goose's version table
// (design.md's Storage Schema section names it explicitly), with columns
// id/version_id/is_applied/tstamp.
func forgeNewerSchema(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening %s to forge a newer schema version: %v", dbPath, err)
	}
	defer db.Close()

	const farFutureVersion = 999999
	if _, err := db.Exec(
		`INSERT INTO schema_migrations (version_id, is_applied) VALUES (?, 1)`,
		farFutureVersion,
	); err != nil {
		t.Fatalf("inserting a forged schema_migrations row: %v", err)
	}
}

// TestRunHealthEndpointRequiresNoAuthentication is the server-runtime
// spec's "Health check succeeds after startup" scenario end-to-end through
// a real Run, listening on an ephemeral port: a plain GET with no
// Authorization header must succeed.
func TestRunHealthEndpointRequiresNoAuthentication(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "server.db")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lnReady := make(chan net.Listener, 1)
	cfg := Config{
		DBPath: dbPath,
		Listen: func() (net.Listener, error) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err == nil {
				lnReady <- ln
			}
			return ln, err
		},
	}

	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()

	var ln net.Listener
	select {
	case ln = <-lnReady:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() never called Listen")
	}

	resp, err := http.Get("http://" + ln.Addr().String() + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET /api/v1/health: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/health without an Authorization header = %d, want %d — the health endpoint must require no auth", resp.StatusCode, http.StatusOK)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after ctx cancellation")
	}
}

// TestRunNeverReadsStdin proves the bounded operation "Operator bootstrap
// ... Zero stdin reads in serve" behaviorally, not just by inspection.
// os.Stdin is replaced with the read end of a pipe whose write end is never
// written to and never closed during the test: a real read against it
// blocks forever, exactly reproducing the historical bug this project has
// already shipped once ("a stdin prompt on a pipe that never delivered").
// A closed stdin (systemd's actual default) would let a stray Read return
// immediately with io.EOF and silently succeed, which proves nothing; this
// pipe is deliberately the stronger, hang-on-any-read case.
func TestRunNeverReadsStdin(t *testing.T) {
	blockingRead, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { writeEnd.Close() })
	t.Cleanup(func() { blockingRead.Close() })

	original := os.Stdin
	os.Stdin = blockingRead
	t.Cleanup(func() { os.Stdin = original })

	dbPath := filepath.Join(t.TempDir(), "server.db")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lnReady := make(chan struct{}, 1)
	cfg := Config{
		DBPath: dbPath,
		Listen: func() (net.Listener, error) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err == nil {
				lnReady <- struct{}{}
			}
			return ln, err
		},
	}

	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()

	select {
	case <-lnReady:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not reach Listen within the bound — a blocked stdin read would hang exactly like this")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after ctx cancellation")
	}
}
