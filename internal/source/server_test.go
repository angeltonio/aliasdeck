package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// infiniteReader never returns EOF and never errors — every Read fills the
// entire buffer it is given. It exists so an unbounded response body read
// has nothing to terminate on except a real limit: if ServerSource's
// io.LimitReader is ever removed, reading this response body has no way to
// return on its own, and the test using it must hang past its own bound and
// fail loudly instead of silently passing (mirrors
// internal/api/middleware_test.go's infiniteReader exactly, this project's
// own established pattern for this class of bounded-op test).
type infiniteReader struct{}

func (infiniteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

func testDevice() domain.Device {
	return domain.Device{ID: "device-1", Name: "laptop", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh}
}

// validSyncBody returns a sync response body whose revision is correctly
// computed for dev and aliases, so tests exercising something other than
// the revision check don't accidentally trip it.
func validSyncBody(t *testing.T, dev domain.Device, aliases []domain.Alias) []byte {
	t.Helper()

	enabled := make([]domain.Alias, 0, len(aliases))
	for _, a := range aliases {
		a.Enabled = true
		enabled = append(enabled, a)
	}
	resolved := domain.Resolve(dev, enabled)

	wireAliases := make([]serverSyncAlias, 0, len(aliases))
	for _, a := range resolved.Aliases {
		wireAliases = append(wireAliases, serverSyncAlias{Name: a.Name, Command: a.Command, Description: a.Description})
	}

	wire := serverSyncResponse{
		Revision: resolved.Revision,
		Device: serverSyncDevice{
			ID:       dev.ID,
			Name:     dev.Name,
			Platform: dev.Platform.String(),
			Shell:    dev.Shell.String(),
		},
		Aliases: wireAliases,
	}

	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshaling test fixture: %v", err)
	}
	return data
}

func newTestServerSource(t *testing.T, handler http.HandlerFunc) *ServerSource {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &ServerSource{URL: srv.URL, Token: "add_test.secret", Client: srv.Client()}
}

// TestServerSourceResolveHappyPath is the baseline round trip every other
// test in this file is a deliberate deviation from.
func TestServerSourceResolveHappyPath(t *testing.T) {
	dev := testDevice()
	aliases := []domain.Alias{{Name: "dps", Command: "docker ps"}}

	s := newTestServerSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(validSyncBody(t, dev, aliases))
	})

	cfg, err := s.Resolve(context.Background(), dev)
	if err != nil {
		t.Fatalf("Resolve() returned an error: %v", err)
	}
	if len(cfg.Aliases) != 1 || cfg.Aliases[0].Name != "dps" {
		t.Errorf("Resolve() aliases = %+v, want one alias named dps", cfg.Aliases)
	}
}

// TestServerSourceRevisionMismatchRejected pins task 7.5's "revision
// mismatch rejected" case: a server that lies about its own revision (or
// whose aliases were tampered with in transit) must never be trusted.
func TestServerSourceRevisionMismatchRejected(t *testing.T) {
	dev := testDevice()
	s := newTestServerSource(t, func(w http.ResponseWriter, r *http.Request) {
		wire := serverSyncResponse{
			Revision: "0000deadbeef",
			Device:   serverSyncDevice{ID: dev.ID, Platform: dev.Platform.String(), Shell: dev.Shell.String()},
			Aliases:  []serverSyncAlias{{Name: "dps", Command: "docker ps"}},
		}
		data, _ := json.Marshal(wire)
		w.Write(data)
	})

	if _, err := s.Resolve(context.Background(), dev); err == nil {
		t.Fatal("Resolve() = nil error, want a revision-mismatch rejection")
	} else if !strings.Contains(err.Error(), "revision mismatch") {
		t.Errorf("Resolve() error = %q, want it to name a revision mismatch", err)
	}
}

// TestServerSourceOversizeBodyTruncatedAndFailed pins task 7.5's exact
// bounded-op requirement: io.LimitReader(resp.Body, 1<<20) must cap the
// read itself, not merely the eventual outcome.
//
// The handler streams an endless body (infiniteReader, never EOF) instead
// of a large-but-finite one deliberately: a finite oversized body would
// still get rejected by an explicit post-hoc length check even with the
// io.LimitReader removed, which would make this test pass regardless of
// whether the bound is real — exactly the "test that cannot fail" shape
// this project has hit before. Only a genuinely unbounded source proves the
// read is capped: with the limiter in place, Resolve returns quickly, well
// inside the bound; with it removed, io.ReadAll(resp.Body) never
// terminates on its own, and the test hangs past its own 2s bound and
// fails loudly instead of silently passing.
func TestServerSourceOversizeBodyTruncatedAndFailed(t *testing.T) {
	dev := testDevice()
	s := newTestServerSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(w, infiniteReader{})
	})

	done := make(chan error, 1)
	go func() {
		_, err := s.Resolve(context.Background(), dev)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Resolve() = nil error, want a rejection for an oversized response")
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("%d byte limit", ServerResponseLimit)) {
			t.Errorf("Resolve() error = %q, want it to name the %d byte limit", err, ServerResponseLimit)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Resolve() did not return within the bound — an unbounded read against an " +
			"endlessly streaming response never terminated on its own, meaning io.LimitReader " +
			"was not actually capping it (removing the wrapper reproduces exactly this hang)")
	}
}

// TestServerSourceNon2xxMapped pins task 7.5's "non-2xx mapped" case: a
// rejection from the server (e.g. a revoked token) must surface as an
// error rather than being silently treated as an empty configuration.
func TestServerSourceNon2xxMapped(t *testing.T) {
	dev := testDevice()
	s := newTestServerSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"code":"invalid_token","message":"this device's token is invalid"}}`))
	})

	_, err := s.Resolve(context.Background(), dev)
	if err == nil {
		t.Fatal("Resolve() = nil error, want a rejection for a 401 response")
	}
	if !strings.Contains(err.Error(), "401") && !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("Resolve() error = %q, want it to name the HTTP status", err)
	}
	if !strings.Contains(err.Error(), "this device's token is invalid") {
		t.Errorf("Resolve() error = %q, want it to surface the server's own message", err)
	}
}

// TestServerSourceOfflineHardErrorNamingURL pins task 7.5's "offline hard
// error naming the URL, no cache" case: an unreachable server must fail
// loudly, naming the URL, with no fallback to any previous response.
func TestServerSourceOfflineHardErrorNamingURL(t *testing.T) {
	dev := testDevice()
	// A server that is stood up and immediately closed, so the port is
	// guaranteed unreachable, without binding a fixed port ourselves.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	s := &ServerSource{URL: url, Token: "add_test.secret", Client: &http.Client{Timeout: 2 * time.Second}}

	_, err := s.Resolve(context.Background(), dev)
	if err == nil {
		t.Fatal("Resolve() = nil error, want a hard error for an unreachable server")
	}
	if !strings.Contains(err.Error(), url) {
		t.Errorf("Resolve() error = %q, want it to name the URL %q", err, url)
	}

	if info := s.LastResolve(); info.Stale {
		t.Error("LastResolve().Stale = true after a failed Resolve, want false: there is no cache to be stale about")
	}
}

// TestServerSourceStaleAlwaysFalse pins design decision 11: Stale can never
// be true, because ServerSource never falls back to a cached response.
func TestServerSourceStaleAlwaysFalse(t *testing.T) {
	dev := testDevice()
	fixedNow := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	s := newTestServerSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write(validSyncBody(t, dev, nil))
	})
	s.Now = func() time.Time { return fixedNow }

	if _, err := s.Resolve(context.Background(), dev); err != nil {
		t.Fatalf("Resolve() returned an error: %v", err)
	}

	info := s.LastResolve()
	if info.Stale {
		t.Error("LastResolve().Stale = true, want false unconditionally")
	}
	if !info.FetchedAt.Equal(fixedNow) {
		t.Errorf("LastResolve().FetchedAt = %v, want %v", info.FetchedAt, fixedNow)
	}
}

// TestServerSourceDefaultClientTimeoutIs30Seconds pins the bounded-op
// requirement directly on the constructed value, the same style
// internal/server's TestNewHTTPServerAppliesEveryBound uses, rather than
// waiting out a real timeout.
func TestServerSourceDefaultClientTimeoutIs30Seconds(t *testing.T) {
	s := &ServerSource{URL: "https://example.com"}
	client := s.httpClient()
	if client.Timeout != 30*time.Second {
		t.Errorf("default client Timeout = %v, want 30s", client.Timeout)
	}
}

// TestServerSourceNoRetries pins task 7.5's "no retries" bounded-op
// requirement: a failing request must reach the server exactly once.
func TestServerSourceNoRetries(t *testing.T) {
	dev := testDevice()
	var calls atomic.Int32
	s := newTestServerSource(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"code":"internal","message":"boom"}}`))
	})

	if _, err := s.Resolve(context.Background(), dev); err == nil {
		t.Fatal("Resolve() = nil error, want a rejection for a 500 response")
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("handler was called %d times, want exactly 1 (no retries)", got)
	}
}

// TestServerSourceResolveChecksURLOnEveryCall pins design decision 13's
// "re-checked on every sync" half: a caller that mutates URL to an
// insecure, non-loopback http:// value between two Resolve calls must be
// refused on the very next call, not only remembered from construction.
func TestServerSourceResolveChecksURLOnEveryCall(t *testing.T) {
	dev := testDevice()
	s := newTestServerSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write(validSyncBody(t, dev, nil))
	})

	if _, err := s.Resolve(context.Background(), dev); err != nil {
		t.Fatalf("first Resolve() returned an error: %v", err)
	}

	s.URL = "http://aliases.example.com"
	if _, err := s.Resolve(context.Background(), dev); err == nil {
		t.Fatal("second Resolve() = nil error, want a rejection after URL was downgraded to a remote http:// value")
	}
}

// TestServerSourceResolveUnfilteredMakesOneRequest pins design decision 12's
// rejected alternative — a second HTTP call from doctor — by asserting
// ResolveUnfiltered itself makes exactly one request, the same as Resolve.
func TestServerSourceResolveUnfilteredMakesOneRequest(t *testing.T) {
	dev := testDevice()
	var calls atomic.Int32
	s := newTestServerSource(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write(validSyncBody(t, dev, nil))
	})

	if _, err := s.ResolveUnfiltered(context.Background(), dev); err != nil {
		t.Fatalf("ResolveUnfiltered() returned an error: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("handler was called %d times, want exactly 1", got)
	}
}

var _ ResolveReporter = (*ServerSource)(nil)
var _ UnfilteredResolver = (*ServerSource)(nil)
