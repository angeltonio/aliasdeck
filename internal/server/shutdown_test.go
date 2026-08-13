package server

import (
	"net"
	"net/http"
	"testing"
	"time"
)

// TestShutdownDrainsInFlightRequestThenReturns is the positive case of the
// server-runtime spec's "Graceful shutdown drains in-flight requests"
// scenario: a request already being handled when shutdown begins must
// still complete successfully, and shutdown() must wait for it (not sever
// the connection out from under it) before returning.
func TestShutdownDrainsInFlightRequestThenReturns(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})

	srv := newHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseRequest
		w.WriteHeader(http.StatusOK)
	}))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	respErr := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/")
		if err == nil {
			resp.Body.Close()
		}
		respErr <- err
	}()
	<-requestStarted

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- shutdown(srv, serveErr, 2*time.Second) }()

	// Release the handler only after shutdown has been asked to begin, so
	// this exercises Shutdown() actually waiting for the request rather
	// than an already-finished handler.
	close(releaseRequest)

	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("shutdown() = %v, want nil — the in-flight request finished well within the drain bound", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown() did not return")
	}

	select {
	case err := <-respErr:
		if err != nil {
			t.Fatalf("in-flight request error = %v, want it to complete successfully across the shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request never completed")
	}
}

// TestShutdownClosesUnconditionallyWhenARequestNeverFinishes is the
// mutation-sensitive half: a handler that never returns on its own (block
// is never closed) must not prevent shutdown() from returning. This only
// happens because shutdown() calls srv.Close() unconditionally after
// Shutdown's drain deadline elapses — Shutdown alone cannot forcibly sever
// a connection that ignores its deadline, only Close does. A drain that
// hangs must never become the fifth unbounded operation this project has
// shipped.
func TestShutdownClosesUnconditionallyWhenARequestNeverFinishes(t *testing.T) {
	requestStarted := make(chan struct{})
	block := make(chan struct{}) // deliberately never closed

	srv := newHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-block
	}))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	// clientDone closes once the client's round trip returns — which, for
	// a handler that never writes a response, can only happen once
	// something forcibly severs the underlying connection. Serve()'s own
	// Accept loop returns as soon as Shutdown closes the listener,
	// regardless of whether any in-flight connection is still open, so
	// waiting on serveErr alone (as an earlier draft of this test did)
	// cannot detect a skipped Close(): only the client's own hung
	// connection can.
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		resp, err := http.Get("http://" + ln.Addr().String() + "/")
		if err == nil {
			resp.Body.Close()
		}
	}()
	<-requestStarted

	// A short drain bound: this test proves Close() runs unconditionally
	// once the drain deadline elapses, not that the production 10s bound
	// is itself short.
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- shutdown(srv, serveErr, 20*time.Millisecond) }()

	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("shutdown() = %v, want nil — a drain that times out is expected and handled, not surfaced as a caller error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown() did not return: a request that never finishes must not block shutdown forever — srv.Close() must run unconditionally after the drain deadline")
	}

	select {
	case <-clientDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the in-flight request's connection was never terminated — srv.Close() must run unconditionally to forcibly sever a connection that ignores the drain deadline, or it leaks forever")
	}
}
