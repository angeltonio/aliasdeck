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
func Run(ctx context.Context, cfg Config) error {
	cfg = cfg.withDefaults()

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := cfg.OpenStore(ctx)
	if err != nil {
		return fmt.Errorf("server: opening store: %w", err)
	}
	defer st.Close()

	if err := auth.Bootstrap(ctx, st, cfg.Getenv, cfg.Stdout); err != nil {
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
