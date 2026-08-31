package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
)

// syncServer implements exactly GET /api/v1/sync against wantToken, mirroring
// internal/api/sync.go's response shape. That endpoint is the only one that
// tells a device its own id, which is why adoption verifies through it.
func syncServer(t *testing.T, wantToken, deviceID string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/sync" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+wantToken {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "invalid_token", "message": "this device's token is missing, invalid, expired, or revoked"},
			})
			return
		}
		// The revision has to be the one the client will recompute from
		// this body. ServerSource rejects a response that misreports its own
		// revision, which is the "server response is hostile input" defense
		// — a fixture that hardcoded a value would only ever exercise that
		// rejection.
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
	}))
}

func adoptTestEnv(t *testing.T) *testEnv {
	t.Helper()
	te := newTestEnv(t)
	writeConfigYAML(t, te.Base, nativeDeviceConfig("macbook"))
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")
	return te
}

// TestAdoptDeviceTokenKeepsTheServerSideIdentity is the reason this path
// exists. Enrolling again would mint a second device; adopting a rotated
// credential keeps the id the server already knows, so aliases pinned to
// this machine keep reaching it.
func TestAdoptDeviceTokenKeepsTheServerSideIdentity(t *testing.T) {
	srv := syncServer(t, "adt_rotated.secret", "device-abc123")
	defer srv.Close()

	te := adoptTestEnv(t)

	report, err := Register(context.Background(), te.Env, RegisterOptions{
		URL:         srv.URL,
		DeviceToken: "adt_rotated.secret",
	})
	if err != nil {
		t.Fatalf("Register() returned an error: %v", err)
	}
	if report.DeviceID != "device-abc123" {
		t.Errorf("DeviceID = %q, want the id the server reported", report.DeviceID)
	}

	creds, err := config.LoadCredentials(config.CredentialsFile(te.Base))
	if err != nil {
		t.Fatalf("LoadCredentials() returned an error: %v", err)
	}
	if creds.DeviceToken != "adt_rotated.secret" {
		t.Errorf("DeviceToken = %q, want the adopted token", creds.DeviceToken)
	}
	if creds.DeviceID != "device-abc123" {
		t.Errorf("DeviceID = %q, want the id learned from the sync response", creds.DeviceID)
	}

	loaded, err := config.Load(config.ConfigFile(te.Base))
	if err != nil {
		t.Fatalf("config.Load() returned an error: %v", err)
	}
	if loaded.Source.Type != config.SourceTypeServer || loaded.Source.URL != srv.URL {
		t.Errorf("source = %+v, want a server source pointed at %s", loaded.Source, srv.URL)
	}
}

// TestAdoptRejectedTokenLeavesTheWorkingCredentialInPlace is the property
// that matters most. Someone adopting a rotated token is usually recovering
// from a leak; writing a token the server refuses would take the machine
// offline and leave them worse off than before they started.
func TestAdoptRejectedTokenLeavesTheWorkingCredentialInPlace(t *testing.T) {
	srv := syncServer(t, "adt_correct.secret", "device-abc123")
	defer srv.Close()

	te := adoptTestEnv(t)
	credsPath := config.CredentialsFile(te.Base)
	if err := config.SaveCredentials(credsPath, config.Credentials{
		Version: 1, ServerURL: srv.URL, DeviceID: "device-abc123", DeviceToken: "adt_correct.secret",
	}); err != nil {
		t.Fatalf("seeding credentials: %v", err)
	}

	_, err := Register(context.Background(), te.Env, RegisterOptions{
		URL:         srv.URL,
		DeviceToken: "adt_wrong.secret",
		Force:       true,
	})
	if err == nil {
		t.Fatal("Register() accepted a token the server refused")
	}
	if !strings.Contains(err.Error(), "nothing on this machine was changed") {
		t.Errorf("error = %v, want it to say the machine was left alone", err)
	}

	after, err := config.LoadCredentials(credsPath)
	if err != nil {
		t.Fatalf("LoadCredentials() returned an error: %v", err)
	}
	if after.DeviceToken != "adt_correct.secret" {
		t.Fatalf("DeviceToken = %q, want the previously working token untouched", after.DeviceToken)
	}
}

// TestAdoptRefusesWhenAlreadyRegisteredWithoutForce keeps adoption behind the
// same guard enrolment has. Replacing a credential is exactly the operation a
// mistyped command should not perform silently.
func TestAdoptRefusesWhenAlreadyRegisteredWithoutForce(t *testing.T) {
	srv := syncServer(t, "adt_rotated.secret", "device-abc123")
	defer srv.Close()

	te := adoptTestEnv(t)
	if err := config.SaveCredentials(config.CredentialsFile(te.Base), config.Credentials{
		Version: 1, ServerURL: srv.URL, DeviceID: "device-abc123", DeviceToken: "adt_old.secret",
	}); err != nil {
		t.Fatalf("seeding credentials: %v", err)
	}

	_, err := Register(context.Background(), te.Env, RegisterOptions{
		URL:         srv.URL,
		DeviceToken: "adt_rotated.secret",
	})
	if err == nil {
		t.Fatal("Register() replaced an existing credential without --force")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("error = %v, want the already-registered refusal", err)
	}
	// The refusal must describe what --force does on *this* path. Repeating
	// enrolment's "mints a second device and abandons this one" would talk an
	// operator out of the safe recovery, since adoption does the opposite.
	if strings.Contains(err.Error(), "abandons this one") {
		t.Errorf("error = %v, want the adoption consequence, not enrolment's", err)
	}
	if !strings.Contains(err.Error(), "changes only its token") {
		t.Errorf("error = %v, want it to say the device is kept", err)
	}
}

func TestAdoptWithForceReplacesTheCredential(t *testing.T) {
	srv := syncServer(t, "adt_rotated.secret", "device-abc123")
	defer srv.Close()

	te := adoptTestEnv(t)
	credsPath := config.CredentialsFile(te.Base)
	if err := config.SaveCredentials(credsPath, config.Credentials{
		Version: 1, ServerURL: srv.URL, DeviceID: "device-abc123", DeviceToken: "adt_old.secret",
	}); err != nil {
		t.Fatalf("seeding credentials: %v", err)
	}

	if _, err := Register(context.Background(), te.Env, RegisterOptions{
		URL: srv.URL, DeviceToken: "adt_rotated.secret", Force: true,
	}); err != nil {
		t.Fatalf("Register() returned an error: %v", err)
	}

	after, err := config.LoadCredentials(credsPath)
	if err != nil {
		t.Fatalf("LoadCredentials() returned an error: %v", err)
	}
	if after.DeviceToken != "adt_rotated.secret" {
		t.Errorf("DeviceToken = %q, want the rotated token", after.DeviceToken)
	}
	// The identity is the point: it must not change when only the secret did.
	if after.DeviceID != "device-abc123" {
		t.Errorf("DeviceID = %q, want it unchanged by a rotation", after.DeviceID)
	}
}

func TestRegisterRefusesBothTokenKinds(t *testing.T) {
	te := adoptTestEnv(t)

	_, err := Register(context.Background(), te.Env, RegisterOptions{
		URL: "https://aliases.example.com", Token: "adx_enroll.secret", DeviceToken: "adt_rotated.secret",
	})
	if err == nil {
		t.Fatal("Register() accepted both token kinds at once")
	}
	if !strings.Contains(err.Error(), "alternatives") {
		t.Errorf("error = %v, want it to say the two flags are alternatives", err)
	}
}

func TestRegisterRefusesNeitherTokenKind(t *testing.T) {
	te := adoptTestEnv(t)

	_, err := Register(context.Background(), te.Env, RegisterOptions{URL: "https://aliases.example.com"})
	if err == nil {
		t.Fatal("Register() accepted no token at all")
	}
	if !strings.Contains(err.Error(), "--device-token") {
		t.Errorf("error = %v, want it to mention the adoption alternative", err)
	}
}

// revokedThenRegisterServer answers the probe with 401 — a device revoked in
// the control plane — and accepts a fresh enrollment token, which is exactly
// the sequence an operator hits after revoking a machine and re-enrolling it.
func revokedThenRegisterServer(t *testing.T, enrollmentToken string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync":
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "invalid_token", "message": "this device's token is missing, invalid, expired, or revoked"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/devices/register":
			if r.Header.Get("Authorization") != "Bearer "+enrollmentToken {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"deviceId": "device-new", "deviceToken": "add_fresh.secret",
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestRegisterAfterRevocationNeedsNoForce is the bug this probe fixes. The
// local credential survives a revoke, so the old guard refused on its mere
// presence and sent the operator to `aliasdeck sync`, which could only fail
// with the same 401. Nothing was protected: the credential it guarded was
// already dead.
func TestRegisterAfterRevocationNeedsNoForce(t *testing.T) {
	srv := revokedThenRegisterServer(t, "adx_fresh.enroll")
	defer srv.Close()

	te := adoptTestEnv(t)
	if err := config.SaveCredentials(config.CredentialsFile(te.Base), config.Credentials{
		Version: 1, ServerURL: srv.URL, DeviceID: "device-revoked", DeviceToken: "add_revoked.secret",
	}); err != nil {
		t.Fatalf("seeding the revoked credential: %v", err)
	}

	report, err := Register(context.Background(), te.Env, RegisterOptions{
		URL:   srv.URL,
		Token: "adx_fresh.enroll",
	})
	if err != nil {
		t.Fatalf("Register() after a revocation returned an error: %v", err)
	}
	if report.DeviceID != "device-new" {
		t.Errorf("DeviceID = %q, want the newly enrolled device", report.DeviceID)
	}

	creds, err := config.LoadCredentials(config.CredentialsFile(te.Base))
	if err != nil {
		t.Fatalf("LoadCredentials(): %v", err)
	}
	if creds.DeviceToken != "add_fresh.secret" {
		t.Errorf("DeviceToken = %q, want the replacement", creds.DeviceToken)
	}
}

// TestRegisterRefusesWhenTheCredentialCannotBeChecked is the other half, and
// the more important one. Being unable to reach the server is not evidence
// that a credential is dead, so an unreachable server must not become a way
// to overwrite one that works.
func TestRegisterRefusesWhenTheCredentialCannotBeChecked(t *testing.T) {
	// A server that accepts nothing and is then closed: every request fails
	// at the transport, which is what a network outage looks like.
	srv := revokedThenRegisterServer(t, "adx_fresh.enroll")
	url := srv.URL
	srv.Close()

	te := adoptTestEnv(t)
	credsPath := config.CredentialsFile(te.Base)
	if err := config.SaveCredentials(credsPath, config.Credentials{
		Version: 1, ServerURL: url, DeviceID: "device-existing", DeviceToken: "add_working.secret",
	}); err != nil {
		t.Fatalf("seeding credentials: %v", err)
	}

	_, err := Register(context.Background(), te.Env, RegisterOptions{
		URL:               url,
		Token:             "adx_fresh.enroll",
		AllowInsecureHTTP: true,
	})
	if err == nil {
		t.Fatal("Register() replaced a credential it could not verify was dead")
	}
	for _, want := range []string{"already registered", "device-existing", "could not be checked", "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %q", err, want)
		}
	}

	after, err := config.LoadCredentials(credsPath)
	if err != nil {
		t.Fatalf("LoadCredentials(): %v", err)
	}
	if after.DeviceToken != "add_working.secret" {
		t.Fatalf("DeviceToken = %q, want the existing credential untouched", after.DeviceToken)
	}
}
