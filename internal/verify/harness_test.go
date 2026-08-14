package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/api"
	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/angeltonio/aliasdeck/internal/store/sqlitestore"
)

// harnessAdminPassword is the fixed operator password every server this
// package boots up bootstraps with. auth.Bootstrap requires at least
// minAdminPasswordLength (12) characters; a fixed ALIASDECK_ADMIN_PASSWORD
// (rather than letting Bootstrap generate one) keeps every test in this
// package deterministic with nothing to capture from stdout.
const harnessAdminPassword = "verify-harness-admin-pw"

// testServer is a real SQLite-backed store behind the real production
// router (internal/api.NewRouter — the exact handler internal/server.Run
// itself installs) served over httptest.NewServer, which binds an
// ephemeral loopback port, never a fixed one. Every test in this package
// that needs "a server" gets one through here, so the comparison in
// byte_identity_test.go and the flow in fullflow_test.go both cross a real
// database, real JSON serialization and a real HTTP boundary — never a
// fake standing in for any of them.
type testServer struct {
	URL   string
	Store store.Store
}

// newTestServer bootstraps one operator (harnessAdminPassword) against a
// fresh SQLite database in t.TempDir() and serves api.NewRouter's handler
// over httptest.NewServer. t.Cleanup closes both the HTTP server and the
// store; nothing here binds a fixed port, sleeps, or leaks a goroutine
// past the test that created it.
func newTestServer(t *testing.T) *testServer {
	t.Helper()
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "verify.db")
	st, err := sqlitestore.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlitestore.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	getenv := func(key string) string {
		if key == auth.AdminPasswordEnv {
			return harnessAdminPassword
		}
		return ""
	}
	if err := auth.Bootstrap(ctx, st, getenv, io.Discard, ""); err != nil {
		t.Fatalf("auth.Bootstrap: %v", err)
	}

	handler, err := api.NewRouter(st, time.Now)
	if err != nil {
		t.Fatalf("api.NewRouter: %v", err)
	}

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &testServer{URL: srv.URL, Store: st}
}

// apiCall performs one bounded HTTP request against ts and, when out is
// non-nil, decodes a successful JSON response body into it. A non-2xx
// status (when out is non-nil) fails the test immediately with the raw
// response body included, so a wire-shape regression is diagnosable from
// the test output alone rather than a generic decode error.
func (ts *testServer) apiCall(t *testing.T, method, path, bearer string, body, out any) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encoding request body for %s %s: %v", method, path, err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, ts.URL+path, reader)
	if err != nil {
		t.Fatalf("building request %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body for %s %s: %v", method, path, err)
	}

	if out != nil {
		if resp.StatusCode >= 300 {
			t.Fatalf("%s %s = %s, want a 2xx response: %s", method, path, resp.Status, respBody)
		}
		if err := json.Unmarshal(respBody, out); err != nil {
			t.Fatalf("decoding %s %s response %s: %v", method, path, respBody, err)
		}
	}
}

// login performs the real POST /api/v1/auth/login request and returns the
// minted session token.
func (ts *testServer) login(t *testing.T, password string) string {
	t.Helper()
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	ts.apiCall(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"username": "admin",
		"password": password,
	}, &out)
	if out.Token == "" {
		t.Fatal("login: server returned an empty session token")
	}
	return out.Token
}

// mintEnrollmentToken performs the real POST /api/v1/enrollment-tokens
// request an authenticated operator uses to invite a new device, optionally
// pinning the registered device's profile membership (design's "server
// owns profile membership" — this is the one call that sets it).
func (ts *testServer) mintEnrollmentToken(t *testing.T, sessionToken string, profileIDs []string) string {
	t.Helper()
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	ts.apiCall(t, http.MethodPost, "/api/v1/enrollment-tokens", sessionToken, map[string]any{
		"profileIds": profileIDs,
	}, &out)
	if out.Token == "" {
		t.Fatal("mintEnrollmentToken: server returned an empty token")
	}
	return out.Token
}

// registerDevice performs the real POST /api/v1/devices/register request —
// the single-use enrollment-token exchange that mints a device token —
// mirroring internal/api/auth.go's registerRequest/deviceTokenResponse wire
// shape exactly.
func (ts *testServer) registerDevice(t *testing.T, enrollmentToken, name string, platform domain.Platform, shell domain.Shell) (deviceID, deviceToken string) {
	t.Helper()
	var out struct {
		DeviceID    string `json:"deviceId"`
		DeviceToken string `json:"deviceToken"`
	}
	ts.apiCall(t, http.MethodPost, "/api/v1/devices/register", enrollmentToken, map[string]any{
		"name":     name,
		"platform": string(platform),
		"shell":    string(shell),
	}, &out)
	if out.DeviceToken == "" {
		t.Fatal("registerDevice: server returned an empty device token")
	}
	return out.DeviceID, out.DeviceToken
}
