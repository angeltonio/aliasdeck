package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
)

// registerServer returns an httptest.Server implementing exactly
// POST /api/v1/devices/register against wantToken, mirroring
// internal/api/auth.go's handleDevicesRegister wire shape and its
// already-consumed-token behavior (single use: consumed becomes true only
// after the first successful call).
func registerServer(t *testing.T, wantToken string) *httptest.Server {
	t.Helper()
	consumed := false
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Register now probes an existing credential before refusing to
		// replace it, so this stub has to answer the sync route too. It
		// accepts the token it issued, which is what a real server does —
		// a stub that refused its own credential would quietly exercise the
		// dead-credential branch in every test that reaches here.
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync" {
			writeStubSync(w, "device-abc123")
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/devices/register" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+wantToken || consumed {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "invalid_token", "message": "the enrollment token is invalid or already used"},
			})
			return
		}

		var in struct {
			Name     string `json:"name"`
			Platform string `json:"platform"`
			Shell    string `json:"shell"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if in.Name == "" {
			t.Error("registration request carried no device name")
		}

		consumed = true
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"deviceId":    "device-abc123",
			"deviceToken": "adt_lookup789.secret012",
		})
	}))
}

func TestRegisterRequiresURLAndToken(t *testing.T) {
	te := newTestEnv(t)
	writeConfigYAML(t, te.Base, nativeDeviceConfig("macbook"))

	if _, err := Register(context.Background(), te.Env, RegisterOptions{Token: "adx_x.y"}); err == nil {
		t.Fatal("Register() must fail when --url is empty")
	}
	if _, err := Register(context.Background(), te.Env, RegisterOptions{URL: "https://aliases.example.com"}); err == nil {
		t.Fatal("Register() must fail when --token is empty")
	}
}

// TestRegisterSuccessConfiguresServerSource pins "Successful registration
// configures ServerSource": the device token lands separately from
// config.yaml at 0600, and config.yaml's source.type becomes server.
func TestRegisterSuccessConfiguresServerSource(t *testing.T) {
	srv := registerServer(t, "adx_enroll.secret")
	defer srv.Close()

	te := newTestEnv(t)
	writeConfigYAML(t, te.Base, nativeDeviceConfig("macbook"))
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	report, err := Register(context.Background(), te.Env, RegisterOptions{
		URL:   srv.URL,
		Token: "adx_enroll.secret",
	})
	if err != nil {
		t.Fatalf("Register() returned an error: %v", err)
	}
	if report.DeviceID != "device-abc123" {
		t.Errorf("DeviceID = %q, want %q", report.DeviceID, "device-abc123")
	}

	creds, err := config.LoadCredentials(config.CredentialsFile(te.Base))
	if err != nil {
		t.Fatalf("LoadCredentials() returned an error: %v", err)
	}
	if creds.DeviceToken != "adt_lookup789.secret012" {
		t.Errorf("DeviceToken = %q, want the server-issued device token", creds.DeviceToken)
	}
	if creds.DeviceID != "device-abc123" {
		t.Errorf("DeviceID = %q, want %q", creds.DeviceID, "device-abc123")
	}

	loaded, err := config.Load(config.ConfigFile(te.Base))
	if err != nil {
		t.Fatalf("config.Load() returned an error: %v", err)
	}
	if loaded.Source.Type != config.SourceTypeServer {
		t.Errorf("Source.Type = %q, want %q", loaded.Source.Type, config.SourceTypeServer)
	}
	if loaded.Source.URL != srv.URL {
		t.Errorf("Source.URL = %q, want %q", loaded.Source.URL, srv.URL)
	}
}

// TestRegisterInvalidTokenLeavesConfigUnchanged pins "Invalid or consumed
// token leaves config unchanged": the exact task 8.4 requirement — a bad
// token must exit non-zero (a non-nil error here; cmd/aliasdeck maps it to
// a non-zero exit code), store no device token, and leave config.yaml
// byte-for-byte as it was.
func TestRegisterInvalidTokenLeavesConfigUnchanged(t *testing.T) {
	srv := registerServer(t, "adx_enroll.secret")
	defer srv.Close()

	te := newTestEnv(t)
	original := nativeDeviceConfig("macbook")
	writeConfigYAML(t, te.Base, original)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	beforeBytes := readFile(t, config.ConfigFile(te.Base))

	_, err := Register(context.Background(), te.Env, RegisterOptions{
		URL:   srv.URL,
		Token: "adx_wrong-token.secret",
	})
	if err == nil {
		t.Fatal("Register() must fail for an invalid enrollment token")
	}

	afterBytes := readFile(t, config.ConfigFile(te.Base))
	if string(beforeBytes) != string(afterBytes) {
		t.Errorf("config.yaml changed after a failed registration:\nbefore: %s\nafter:  %s", beforeBytes, afterBytes)
	}

	creds, loadErr := config.LoadCredentials(config.CredentialsFile(te.Base))
	if loadErr != nil {
		t.Fatalf("LoadCredentials() returned an error: %v", loadErr)
	}
	if creds.DeviceToken != "" {
		t.Errorf("DeviceToken = %q, want empty after a rejected registration", creds.DeviceToken)
	}
}

// TestRegisterReplayedTokenLeavesConfigUnchanged is the "already-consumed"
// half of the same scenario: a token that already succeeded once must be
// refused on a second use (threat matrix: token handling, mirrored
// client-side), again leaving config.yaml untouched.
func TestRegisterReplayedTokenLeavesConfigUnchanged(t *testing.T) {
	srv := registerServer(t, "adx_enroll.secret")
	defer srv.Close()

	te := newTestEnv(t)
	writeConfigYAML(t, te.Base, nativeDeviceConfig("macbook"))
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	if _, err := Register(context.Background(), te.Env, RegisterOptions{
		URL:   srv.URL,
		Token: "adx_enroll.secret",
	}); err != nil {
		t.Fatalf("first Register() returned an error: %v", err)
	}

	beforeBytes := readFile(t, config.ConfigFile(te.Base))

	if _, err := Register(context.Background(), te.Env, RegisterOptions{
		URL:   srv.URL,
		Token: "adx_enroll.secret",
	}); err == nil {
		t.Fatal("second Register() with a replayed token must fail")
	}

	afterBytes := readFile(t, config.ConfigFile(te.Base))
	if string(beforeBytes) != string(afterBytes) {
		t.Errorf("config.yaml changed after a replayed-token registration attempt:\nbefore: %s\nafter:  %s", beforeBytes, afterBytes)
	}
}

// TestRegisterRejectsInsecureURLWithoutOptOut pins design decision 13 at
// register's own request too: the enrollment token travels as a bearer
// credential and deserves the identical transport guard login's own request
// gets.
func TestRegisterRejectsInsecureURLWithoutOptOut(t *testing.T) {
	te := newTestEnv(t)
	writeConfigYAML(t, te.Base, nativeDeviceConfig("macbook"))
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	_, err := Register(context.Background(), te.Env, RegisterOptions{
		URL:   "http://aliases.example.com",
		Token: "adx_enroll.secret",
	})
	if err == nil {
		t.Fatal("Register() must reject a non-loopback http:// URL without --allow-insecure")
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return data
}

// TestRegisterRefusesWhenAlreadyRegistered is the regression test for a
// defect found by using the product, not by reading it: registering twice on
// one machine minted two devices server-side with the same hostname,
// platform and shell — identical in every column the operator's device list
// shows — and only the newest could ever sync again.
//
// The guard runs before the enrollment-token exchange, so a refused
// re-registration also does not burn the token; otherwise recovering from a
// command that did nothing would cost another token.
func TestRegisterRefusesWhenAlreadyRegistered(t *testing.T) {
	te := newTestEnv(t)
	writeConfigYAML(t, te.Base, nativeDeviceConfig("already-registered"))
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")
	seedCredentials(t, te.Base, config.Credentials{
		Version:     1,
		ServerURL:   "https://aliases.example.com",
		DeviceID:    "device-already-here",
		DeviceToken: "adt_lookup.secret",
	})

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, err := Register(context.Background(), te.Env, RegisterOptions{
		URL: srv.URL, Token: "ade_lookup.secret", AllowInsecureHTTP: true,
	})
	if err == nil {
		t.Fatal("Register() must refuse when this device already holds a device token")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("error %q does not say the device is already registered", err.Error())
	}
	if !strings.Contains(err.Error(), "device-already-here") {
		t.Errorf("error %q does not name the existing device", err.Error())
	}
	if called {
		t.Error("Register() contacted the server, burning the enrollment token, before refusing")
	}
}

func TestRegisterForceAllowsReRegistration(t *testing.T) {
	te := newTestEnv(t)
	writeConfigYAML(t, te.Base, nativeDeviceConfig("already-registered"))
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")
	seedCredentials(t, te.Base, config.Credentials{
		Version: 1, DeviceID: "old-device", DeviceToken: "adt_old.secret",
	})

	srv := registerServer(t, "ade_lookup.secret")
	defer srv.Close()

	if _, err := Register(context.Background(), te.Env, RegisterOptions{
		URL: srv.URL, Token: "ade_lookup.secret", AllowInsecureHTTP: true, Force: true,
	}); err != nil {
		t.Fatalf("Register() with --force: %v", err)
	}
}

// writeStubSync answers GET /api/v1/sync with an empty but internally
// consistent alias set. The revision has to be the one the client recomputes:
// ServerSource rejects a response that misreports its own revision.
func writeStubSync(w http.ResponseWriter, deviceID string) {
	resolved := domain.ResolvedConfig{
		Device:  domain.Device{ID: deviceID, Name: "macbook", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh},
		Aliases: []domain.Alias{},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"revision": resolved.ComputeRevision(),
		"device": map[string]any{
			"id": deviceID, "name": "macbook", "platform": "macos", "shell": "zsh", "profileIds": []string{},
		},
		"aliases":     []any{},
		"generatedAt": "2030-01-01T00:00:00Z",
	})
}
