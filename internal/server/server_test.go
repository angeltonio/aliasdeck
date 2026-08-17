package server

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
// Authorization header must succeed, and — bounded-review finding — the
// response body and Content-Type are asserted exactly, not just the status
// code, so a regression that starts leaking a schema version, build info,
// or a path (handleHealth's own comment promises none of that) fails this
// test instead of passing silently.
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
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("reading /api/v1/health body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/health without an Authorization header = %d, want %d — the health endpoint must require no auth", resp.StatusCode, http.StatusOK)
	}

	const wantContentType = "application/json; charset=utf-8"
	if ct := resp.Header.Get("Content-Type"); ct != wantContentType {
		t.Errorf("Content-Type = %q, want %q", ct, wantContentType)
	}
	const wantBody = `{"status":"ok"}` + "\n"
	if string(body) != wantBody {
		t.Errorf("body = %q, want exactly %q — the health endpoint must expose nothing beyond readiness", body, wantBody)
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

// TestRunAnnouncesTheRealBoundListenerAddress is the bounded-review
// regression test for finding 5 ("serve announces nothing on startup"):
// with --addr 127.0.0.1:0, cfg.Addr itself never names the real ephemeral
// port, so Run must print the listener's own Addr() — not cfg.Addr — to
// cfg.Stdout once Listen succeeds. This also proves the line carries
// nothing else: no schema version, no operator/device identity, no build
// metadata (design decision 35).
//
// Mutation check: reverting Run to print cfg.Addr — or to print nothing at
// all — fails this test; see apply-progress for the verbatim before/after
// output.
func TestRunAnnouncesTheRealBoundListenerAddress(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "server.db")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stdout bytes.Buffer
	lnReady := make(chan net.Listener, 1)
	cfg := Config{
		DBPath: dbPath,
		Stdout: &stdout,
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

	// The announcement happens synchronously right after Listen succeeds
	// and before Serve starts accepting, but is written to stdout from a
	// different goroutine than this test — wait for the health endpoint to
	// answer as a reliable signal that Run has moved well past the
	// announcement line before asserting stdout's content, without a real
	// sleep.
	waitForHealthy(t, ln)

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after ctx cancellation")
	}

	got := stdout.String()
	wantAddr := ln.Addr().String()
	if !strings.Contains(got, wantAddr) {
		t.Fatalf("stdout = %q, want it to name the real bound address %q (not cfg.Addr)", got, wantAddr)
	}
	if strings.Contains(got, "127.0.0.1:0") {
		t.Errorf("stdout = %q, want the resolved port, not the unresolved cfg.Addr literal", got)
	}
}

func TestRunLogsTrustedLocalSetupBoundaryWarning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stdout bytes.Buffer
	lnReady := make(chan net.Listener, 1)
	cfg := Config{
		Getenv: func(name string) string {
			if name == localSetupEnv {
				return "true"
			}
			return ""
		},
		Stdout:    &stdout,
		OpenStore: func(context.Context) (store.Store, error) { return &fakeStore{}, nil },
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
	waitForHealthy(t, ln)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	got := stdout.String()
	for _, fragment := range []string{localSetupEnv + "=true", "trusts the external network boundary", "-p 127.0.0.1:18082:8080"} {
		if !strings.Contains(got, fragment) {
			t.Errorf("stdout = %q, want warning fragment %q", got, fragment)
		}
	}
}

func TestRunRejectsMalformedLocalSetupConfigurationBeforeStoreOpen(t *testing.T) {
	openCalled := false
	err := Run(context.Background(), Config{
		Getenv: func(name string) string {
			if name == localSetupEnv {
				return "yes"
			}
			return ""
		},
		OpenStore: func(context.Context) (store.Store, error) {
			openCalled = true
			return &fakeStore{}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), localSetupEnv+" must be exactly true or false") {
		t.Fatalf("Run() = %v, want malformed %s error", err, localSetupEnv)
	}
	if openCalled {
		t.Fatal("Run() opened the store despite malformed local setup configuration")
	}
}

// waitForHealthy polls GET /api/v1/health until it answers or the bound
// elapses, giving Run's own goroutine time to reach Serve without a real
// sleep in the test itself.
func waitForHealthy(t *testing.T, ln net.Listener) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		resp, err := http.Get("http://" + ln.Addr().String() + "/api/v1/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatal("server never became healthy")
		}
	}
}

// TestRunReturnsNilWhenCancelledDuringOpenStore is the regression test for
// the bounded-review finding that a SIGTERM (or any ctx cancellation)
// arriving while OpenStore is still running — the up-to-30s migration
// window — surfaced as a wrapped "server: opening store: ..." error,
// indistinguishable in logs from a genuine startup failure. An
// operator-initiated stop must report the same way (nil) no matter which
// bounded operation it lands during; this test cancels ctx while OpenStore
// is deliberately still blocked on it, using the same
// "call blocks until ctx.Done()" seam TestRunOpensAndMigratesStoreBeforeListening
// already relies on for ordering.
func TestRunReturnsNilWhenCancelledDuringOpenStore(t *testing.T) {
	openStoreEntered := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := Config{
		OpenStore: func(ctx context.Context) (store.Store, error) {
			close(openStoreEntered)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()

	select {
	case <-openStoreEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() never called OpenStore")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() cancelled during OpenStore = %v, want nil — an operator-initiated stop must not read as a startup failure", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after ctx cancellation during OpenStore")
	}
}

// TestRunReturnsNilWhenCancelledDuringBootstrap is
// TestRunReturnsNilWhenCancelledDuringOpenStore's sibling for the other
// bounded operation named in the same finding: a stop arriving while
// auth.Bootstrap is still running must also report nil, not a wrapped
// "server: bootstrapping operator: ..." error.
func TestRunReturnsNilWhenCancelledDuringBootstrap(t *testing.T) {
	bootstrapEntered := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := Config{
		OpenStore: func(_ context.Context) (store.Store, error) {
			return &blockingBootstrapStore{fakeStore: &fakeStore{}, entered: bootstrapEntered}, nil
		},
	}

	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()

	select {
	case <-bootstrapEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() never reached auth.Bootstrap's operator count check")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() cancelled during Bootstrap = %v, want nil — an operator-initiated stop must not read as a startup failure", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after ctx cancellation during Bootstrap")
	}
}

// TestRunClosesStoreAfterReturning is the regression test for the
// bounded-review finding that `defer st.Close()` was verified by nothing:
// deleting that line left the whole package green. fakeStore's isClosed
// accessor already existed but was never called from anywhere — dead
// scaffolding that made the property look covered when it was not.
func TestRunClosesStoreAfterReturning(t *testing.T) {
	st := &fakeStore{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lnReady := make(chan struct{}, 1)
	cfg := Config{
		OpenStore: func(_ context.Context) (store.Store, error) { return st, nil },
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
		t.Fatal("Run() never called Listen")
	}

	if st.isClosed() {
		t.Fatal("store reported closed before shutdown began")
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

	if !st.isClosed() {
		t.Fatal("Run() returned without closing the store — defer st.Close() must run")
	}
}

// TestRunBootstrapsOperatorOnEmptyStore is the regression test for the
// bounded-review finding that fakeStore.Operators().Count() always
// returning 1 left Run's auth.Bootstrap call's Create branch entirely
// unexercised: newFakeStoreWithEmptyOperators reports 0 instead, so this
// test actually drives Create.
func TestRunBootstrapsOperatorOnEmptyStore(t *testing.T) {
	st := newFakeStoreWithEmptyOperators()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lnReady := make(chan struct{}, 1)
	cfg := Config{
		OpenStore: func(_ context.Context) (store.Store, error) { return st, nil },
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
		t.Fatal("Run() never called Listen — Bootstrap must not block startup on an empty store")
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

	if !st.createWasCalled() {
		t.Fatal("Run() returned successfully but never called Operators().Create — auth.Bootstrap's Create branch was not actually exercised")
	}
}

// TestRunWrapsBootstrapErrorFromOperatorCreate is
// TestRunBootstrapsOperatorOnEmptyStore's error-path sibling: the same
// bounded-review finding noted that Bootstrap's error path inside Run was
// never exercised either, since the always-count-1 fake never let Create
// run at all, let alone fail.
func TestRunWrapsBootstrapErrorFromOperatorCreate(t *testing.T) {
	wantErr := errors.New("operator create failed")
	st := newFakeStoreWithOperatorCreateError(wantErr)

	cfg := Config{
		OpenStore: func(_ context.Context) (store.Store, error) { return st, nil },
		Listen: func() (net.Listener, error) {
			return nil, errors.New("Listen must not be called when Bootstrap fails")
		},
	}

	err := Run(context.Background(), cfg)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() = %v, want an error wrapping %v", err, wantErr)
	}
	if !st.createWasCalled() {
		t.Fatal("Run() failed but never called Operators().Create — the error did not actually come from the Create branch under test")
	}
}

// blockingBootstrapStore wraps a *fakeStore whose Operators() reports 0
// existing operators, but blocks inside Count until ctx is cancelled —
// simulating a slow store call so a test can assert Run's behavior when a
// stop signal lands specifically inside auth.Bootstrap rather than
// OpenStore.
type blockingBootstrapStore struct {
	*fakeStore
	entered chan struct{}
	once    sync.Once
}

func (b *blockingBootstrapStore) Operators() store.OperatorRepo {
	return blockingOperatorRepo{entered: b.entered, once: &b.once}
}

type blockingOperatorRepo struct {
	entered chan struct{}
	once    *sync.Once
}

func (r blockingOperatorRepo) Create(_ context.Context, o store.Operator) (store.Operator, error) {
	return o, nil
}

func (blockingOperatorRepo) Get(_ context.Context, _ string) (store.Operator, error) {
	return store.Operator{}, store.ErrNotFound
}

func (blockingOperatorRepo) ByUsername(_ context.Context, _ string) (store.Operator, error) {
	return store.Operator{}, store.ErrNotFound
}

func (r blockingOperatorRepo) Count(ctx context.Context) (int, error) {
	r.once.Do(func() { close(r.entered) })
	<-ctx.Done()
	return 0, ctx.Err()
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
