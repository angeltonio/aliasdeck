package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/angeltonio/aliasdeck/internal/auth"
)

// Bounded http.Server limits (design's Bounded Operations table, "Accept /
// read / write"). Each one has a test asserting it directly on the
// constructed server — dropping any of these fields is a test failure, not
// a silent regression.
//
// Rationale (bounded-review finding, suggestion 10 — house style per
// internal/source/gitrun.go's GitTimeout is that a magic number carries the
// failure or measurement that produced it): none of the five values below
// was derived from a measurement against this project's own traffic —
// there is no traffic yet to measure. They are conventional defaults for a
// single-operator personal control plane, chosen for shape rather than
// tuned for a load profile:
//   - readHeaderTimeout/readTimeout bound a slow or hostile client's header
//     and body delivery — 5s/15s track the values commonly recommended for
//     small internal APIs (e.g. Go's own net/http docs) rather than any
//     AliasDeck-specific ceiling.
//   - writeTimeout (30s) is double readTimeout: this API's slowest handler
//     is a store round trip already bounded by SQLite's 5s busy_timeout
//     (design decision 7) and the 20s http.TimeoutHandler wrapping every
//     handler (Phase 5), so 30s is headroom above both, not a measured p99.
//   - idleTimeout (60s) is a conventional keep-alive window; nothing in
//     this project depends on connections being reused for any specific
//     duration.
//   - maxHeaderBytes (64<<10) matches Go's own http.DefaultMaxHeaderBytes;
//     it is carried here explicitly so decision changes are visible in a
//     diff instead of riding a stdlib default.
//
// If a future load test contradicts one of these, replace this paragraph
// with the measurement, the same way GitTimeout's own comment was written
// after a failure was actually observed — not before.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
	maxHeaderBytes    = 64 << 10
)

// newHTTPServer builds the *http.Server Run serves, with every bound the
// design's Bounded Operations table requires and nothing else. It is
// exported to package-internal tests only (lowercase) so a test can inspect
// the constructed value directly instead of inferring it from behavior.
func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
}

// Run opens and migrates the store, bootstraps the first operator, and
// serves cfg's handler until ctx is cancelled or a SIGINT/SIGTERM arrives,
// then drains in-flight requests within cfg.ShutdownTimeout before
// returning.
//
// Ordering is the load-bearing property (server-runtime spec, "Migrations
// Apply on Startup"): cfg.OpenStore runs — and, in production, therefore
// migrates the database via sqlitestore.Open — strictly before cfg.Listen
// is ever called, so not one connection is accepted until migration has
// completed. A database whose recorded schema is newer than this binary
// makes OpenStore return store.ErrSchemaNewer; Run returns that error
// straight back to the caller and never calls Listen at all.
//
// Run never reads os.Stdin, directly or through anything it calls:
// auth.Bootstrap takes an io.Writer for the one-time generated password and
// no io.Reader whatsoever (bounded operation "Operator bootstrap ... Zero
// stdin reads in serve").
//
// An operator-initiated stop (SIGINT/SIGTERM, or ctx cancelled by the
// caller) reports the same way — a nil error — no matter which bounded
// operation it lands during. Before this correction, a signal arriving
// during OpenStore's up-to-30s migration window or during Bootstrap
// propagated out as a wrapped error, indistinguishable in logs from a real
// startup failure; the fix is narrow: after OpenStore or Bootstrap fails,
// check whether ctx itself was already cancelled before deciding this was
// a failure at all. A genuine failure never flips ctx.Err() on its own, so
// this cannot mask one — it only reclassifies the case where the two
// coincide, which is exactly the "the operator stopped it" case this
// project wants to report identically to a stop that lands after Listen.
func Run(ctx context.Context, cfg Config) error {
	cfg = cfg.withDefaults()

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := cfg.OpenStore(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("server: opening store: %w", err)
	}
	defer st.Close()

	if err := auth.Bootstrap(ctx, st, cfg.Getenv, cfg.Stdout, cfg.BootstrapPasswordFile); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("server: bootstrapping operator: %w", err)
	}

	ln, err := cfg.Listen()
	if err != nil {
		return fmt.Errorf("server: listening: %w", err)
	}

	srv := newHTTPServer(newHandler())

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ln)
	}()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server: serving: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	return shutdown(srv, serveErr, cfg.ShutdownTimeout)
}

// shutdown drains srv within timeout via srv.Shutdown, then calls
// srv.Close() unconditionally — whether the drain finished cleanly or timed
// out — and waits for the Serve goroutine (reporting through serveErr) to
// actually return.
//
// The unconditional Close is the part that matters (design's Bounded
// Operations table, "Shutdown ... then srv.Close() unconditionally; Run
// returns either way"): Shutdown's own context deadline bounds how long it
// waits for in-flight requests, but Shutdown alone never forcibly severs a
// connection that ignores that deadline — only Close does that. Skipping
// Close whenever Shutdown "already handled it" is exactly how a request
// that refuses to finish turns the drain into the fifth unbounded operation
// this project has shipped.
func shutdown(srv *http.Server, serveErr <-chan error, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	shutdownErr := srv.Shutdown(shutdownCtx)

	closeErr := srv.Close()

	<-serveErr

	if shutdownErr != nil && !errors.Is(shutdownErr, context.DeadlineExceeded) {
		return fmt.Errorf("server: shutting down: %w", shutdownErr)
	}
	if closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
		return fmt.Errorf("server: closing: %w", closeErr)
	}
	return nil
}
