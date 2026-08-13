package api

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// infiniteReader never returns EOF and never errors — every Read fills the
// entire buffer it is given. It exists so an unbounded body read has
// nothing to terminate on except a real limit: if withMaxBytes's wrapper
// is ever removed, io.Copy against this reader has no way to return on its
// own, and the test below must therefore hang past its own bound and fail
// loudly instead of silently passing. consumed records exactly how many
// bytes the reader produced, so a passing test can also assert the read
// stopped near the configured limit rather than after buffering
// everything — the property a test that reads to completion and only then
// checks the status code cannot prove.
type infiniteReader struct {
	consumed int64
}

func (r *infiniteReader) Read(p []byte) (int, error) {
	atomic.AddInt64(&r.consumed, int64(len(p)))
	return len(p), nil
}

// TestMaxBytesMiddlewareRejectsOversizedBodyBeforeFullyReadingIt is the
// threat-matrix RED test for "Request body" (Bounded Operations table):
// http.MaxBytesReader must reject a body that exceeds maxBodyBytes before
// the handler can ever finish reading it — not after buffering the whole
// thing and then checking its length, which is the inverted, always-green
// version of this property. Removing the http.MaxBytesReader wrapper in
// withMaxBytes leaves this test hanging on an infinite read until its own
// bound fires, then failing.
func TestMaxBytesMiddlewareRejectsOversizedBodyBeforeFullyReadingIt(t *testing.T) {
	src := &infiniteReader{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/whatever", src)
	rec := httptest.NewRecorder()

	var readErr error
	done := make(chan struct{})
	handler := withMaxBytes(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.Copy(io.Discard, r.Body)
		close(done)
	}))

	go handler.ServeHTTP(rec, req)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return within the bound — the body read against an infinite source never terminated, meaning it was not size-limited at all (removing the MaxBytesReader wrapper reproduces exactly this hang)")
	}

	var maxErr *http.MaxBytesError
	if !errors.As(readErr, &maxErr) {
		t.Fatalf("io.Copy(io.Discard, r.Body) error = %v, want a *http.MaxBytesError — an oversized body must be rejected, not silently accepted", readErr)
	}

	// A generous margin above maxBodyBytes accounts for io.Copy's internal
	// buffer size; the important property is "nowhere close to unbounded",
	// which any multiple of the buffer size well below the infinite source
	// this test feeds it already proves.
	const margin = 1 << 20
	if consumed := atomic.LoadInt64(&src.consumed); consumed > maxBodyBytes+margin {
		t.Fatalf("underlying reader was pulled for %d bytes, want at most %d — the body must be rejected before it is fully read, not after buffering far more than the configured limit", consumed, maxBodyBytes+margin)
	}
}

// TestMaxBytesMiddlewareAllowsABodyWithinTheLimit is the GREEN-path
// counterpart: a body at or under maxBodyBytes must reach the handler
// intact, so the wrapper is a ceiling, not an accidental truncation of
// every request.
func TestMaxBytesMiddlewareAllowsABodyWithinTheLimit(t *testing.T) {
	want := []byte("a small, well within limit request body")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/whatever", bytes.NewReader(want))
	rec := httptest.NewRecorder()

	var got []byte
	var readErr error
	handler := withMaxBytes(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, readErr = io.ReadAll(r.Body)
	}))

	handler.ServeHTTP(rec, req)

	if readErr != nil {
		t.Fatalf("io.ReadAll(r.Body) error = %v, want nil for a body within the limit", readErr)
	}
	if string(got) != string(want) {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
