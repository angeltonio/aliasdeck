package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/config"
)

// registerCmdTestServer is a minimal httptest.Server implementing exactly
// POST /api/v1/devices/register against wantToken, mirroring
// internal/app/register_test.go's own registerServer helper — duplicated
// here rather than imported because internal/app's helper is unexported in
// a different package, and this file's whole point is proving the wiring
// cmd/aliasdeck/register.go itself performs, through the real Cobra RunE,
// which internal/app's own tests (calling Register directly) cannot prove
// (bounded-review finding, correction pass, WARNING 3: "root_test.go proves
// login/register/logout are registered by name ... no test executes their
// RunE").
func registerCmdTestServer(t *testing.T, wantToken string) *httptest.Server {
	t.Helper()
	consumed := false
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/devices/register" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+wantToken || consumed {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "invalid_token", "message": "invalid or already-used token"},
			})
			return
		}
		consumed = true
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"deviceId":    "device-cmd-test",
			"deviceToken": "adt_lookupcmd.secretcmd",
		})
	}))
}

// initForServerCommands seeds a fresh $ALIASDECK_HOME with a real
// config.yaml (via the real `init` command, exactly like main_test.go's own
// setup) so register/login/logout have a base directory and device
// identity to work against, without touching the real machine.
func initForServerCommands(t *testing.T) string {
	t.Helper()
	base := filepath.Join(t.TempDir(), ".config", "aliasdeck")
	t.Setenv("ALIASDECK_HOME", base)
	t.Setenv("ALIASDECK_PLATFORM", "macos")
	t.Setenv("ALIASDECK_SHELL", "zsh")

	if _, stderr, code := runCmd(t, "init", "--no-bootstrap"); code != exitOK {
		t.Fatalf("init exit code = %d, want %d (stderr: %s)", code, exitOK, stderr)
	}
	return base
}

// TestRegisterCommandRequiresURLAndToken proves --url and --token are wired
// through to app.RegisterOptions at all: with neither supplied the command
// must fail before ever making a network request.
func TestRegisterCommandRequiresURLAndToken(t *testing.T) {
	initForServerCommands(t)

	_, stderr, code := runCmd(t, "register")
	if code == exitOK {
		t.Fatal("register with no flags must not succeed")
	}
	if !strings.Contains(stderr, "--url") {
		t.Errorf("stderr = %q, want it to name --url", stderr)
	}

	_, stderr, code = runCmd(t, "register", "--url", "https://aliases.example.com")
	if code == exitOK {
		t.Fatal("register with --url but no --token must not succeed")
	}
	if !strings.Contains(stderr, "--token") {
		t.Errorf("stderr = %q, want it to name --token", stderr)
	}
}

// TestRegisterCommandWiresFlagsIntoOptionsEndToEnd runs the real
// `aliasdeck register` command, through its own RunE, against a real
// httptest server: this is the proof that --url, --token, and (elsewhere)
// --allow-insecure actually reach app.RegisterOptions, not just that the
// flags exist on the *cobra.Command (which cmd/aliasdeck/serve_test.go's
// own pattern already proves is not, by itself, sufficient).
//
// Mutation check: changing register.go's `Token: token` to `Token: ""`
// makes this test fail immediately with "--token is required" (Register's
// own empty-token guard) rather than succeeding — see apply-progress for
// the verbatim output.
func TestRegisterCommandWiresFlagsIntoOptionsEndToEnd(t *testing.T) {
	base := initForServerCommands(t)

	srv := registerCmdTestServer(t, "adx_cmd-enroll.secret")
	defer srv.Close()

	stdout, stderr, code := runCmd(t, "register", "--url", srv.URL, "--token", "adx_cmd-enroll.secret")
	if code != exitOK {
		t.Fatalf("register exit code = %d, want %d (stderr: %s)", code, exitOK, stderr)
	}
	if !strings.Contains(stdout, "device-cmd-test") {
		t.Errorf("stdout = %q, want it to name the registered device id", stdout)
	}

	loaded, err := config.Load(config.ConfigFile(base))
	if err != nil {
		t.Fatalf("config.Load() returned an error: %v", err)
	}
	if loaded.Source.Type != config.SourceTypeServer {
		t.Errorf("Source.Type = %q, want %q — --url/--token did not reach app.Register", loaded.Source.Type, config.SourceTypeServer)
	}
	if loaded.Source.URL != srv.URL {
		t.Errorf("Source.URL = %q, want %q", loaded.Source.URL, srv.URL)
	}

	creds, err := config.LoadCredentials(config.CredentialsFile(base))
	if err != nil {
		t.Fatalf("LoadCredentials() returned an error: %v", err)
	}
	if creds.DeviceToken != "adt_lookupcmd.secretcmd" {
		t.Errorf("DeviceToken = %q, want the server-issued device token", creds.DeviceToken)
	}
}

// TestRegisterCommandRejectsInsecureURLWithoutAllowInsecureFlag proves
// --allow-insecure's own wiring: the identical --url, run without the flag,
// must be refused before any request ever leaves the machine.
func TestRegisterCommandRejectsInsecureURLWithoutAllowInsecureFlag(t *testing.T) {
	initForServerCommands(t)

	_, stderr, code := runCmd(t, "register", "--url", "http://aliases.example.com", "--token", "adx_x.y")
	if code == exitOK {
		t.Fatal("register must reject a non-loopback http:// URL without --allow-insecure")
	}
	if !strings.Contains(stderr, "http") {
		t.Errorf("stderr = %q, want it to mention the rejected URL/scheme", stderr)
	}
}
