package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/config"
)

// loginServer returns an httptest.Server implementing exactly
// POST /api/v1/auth/login against wantPassword, mirroring
// internal/api/auth.go's handleLogin wire shape without importing
// internal/api.
func loginServer(t *testing.T, wantPassword string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/login" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if in.Username != operatorUsername || in.Password != wantPassword {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "invalid_credentials", "message": "invalid username or password"},
			})
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":     "ads_lookup123.secret456",
			"expiresAt": fixedNow.Add(24 * time.Hour).Format(time.RFC3339),
		})
	}))
}

func TestLoginRequiresURL(t *testing.T) {
	te := newTestEnv(t)

	if _, err := Login(context.Background(), te.Env, LoginOptions{PasswordStdin: true}); err == nil {
		t.Fatal("Login() must fail when --url is empty")
	}
}

// TestLoginRejectsInsecureURLWithoutOptOut pins design decision 13 at
// login's own boundary: a non-loopback http:// URL must be refused before
// any credential ever leaves the machine.
func TestLoginRejectsInsecureURLWithoutOptOut(t *testing.T) {
	te := newTestEnv(t)
	te.setStdin("hunter2\n")

	_, err := Login(context.Background(), te.Env, LoginOptions{
		URL:           "http://aliases.example.com",
		PasswordStdin: true,
	})
	if err == nil {
		t.Fatal("Login() must reject a non-loopback http:// URL without --allow-insecure")
	}
}

// TestLoginSuccessStoresSessionOutsideConfigAndDeviceToken pins the core
// cli-commands scenario "Successful operator login stores a session"
// (design decision 17): the session token lands in the credentials file,
// never in config.yaml (which login never even touches), and is distinct
// from any device token.
func TestLoginSuccessStoresSessionOutsideConfigAndDeviceToken(t *testing.T) {
	srv := loginServer(t, "hunter2")
	defer srv.Close()

	te := newTestEnv(t)
	te.setStdin("hunter2\n")

	report, err := Login(context.Background(), te.Env, LoginOptions{
		URL:           srv.URL,
		PasswordStdin: true,
	})
	if err != nil {
		t.Fatalf("Login() returned an error: %v", err)
	}
	if report.ServerURL != srv.URL {
		t.Errorf("ServerURL = %q, want %q", report.ServerURL, srv.URL)
	}

	if _, err := os.Stat(config.ConfigFile(te.Base)); err == nil {
		t.Error("login must never create or touch config.yaml")
	}

	creds, err := config.LoadCredentials(config.CredentialsFile(te.Base))
	if err != nil {
		t.Fatalf("LoadCredentials() returned an error: %v", err)
	}
	if creds.SessionToken != "ads_lookup123.secret456" {
		t.Errorf("SessionToken = %q, want the server-issued session token", creds.SessionToken)
	}
	if creds.DeviceToken != "" {
		t.Errorf("DeviceToken = %q, want empty — login must never populate a device token", creds.DeviceToken)
	}
}

// TestLoginRejectsWrongPassword pins "Incorrect password rejected": Login
// must fail and must not store anything.
func TestLoginRejectsWrongPassword(t *testing.T) {
	srv := loginServer(t, "hunter2")
	defer srv.Close()

	te := newTestEnv(t)
	te.setStdin("wrong-password\n")

	_, err := Login(context.Background(), te.Env, LoginOptions{
		URL:           srv.URL,
		PasswordStdin: true,
	})
	if err == nil {
		t.Fatal("Login() must fail for an incorrect password")
	}

	creds, loadErr := config.LoadCredentials(config.CredentialsFile(te.Base))
	if loadErr != nil {
		t.Fatalf("LoadCredentials() returned an error: %v", loadErr)
	}
	if creds.SessionToken != "" {
		t.Errorf("SessionToken = %q, want empty after a rejected login", creds.SessionToken)
	}
}

// TestLoginNeverPromptsOnATerminalLessStdin is this project's bounded-op
// proof for "login": design's Bounded Operations table states
// "--password-stdin reads a piped stream behind the existing isInteractive
// guard; never a terminal prompt". os.Stdin is replaced (via env.Stdin) with
// the read end of a pipe whose write end is never written to and never
// closed — the exact construction internal/server/server_test.go's
// TestRunNeverReadsStdin already uses for the same shaped hazard ("a stdin
// prompt on a pipe that never delivered", this project's own shipped bug).
// A real prompt attempt against this pipe would call bufio.Scanner.Scan()
// and block forever; Login must instead recognize the non-terminal stdin
// and fail immediately, naming --password-stdin, without ever calling Scan.
//
// Mutation check: removing resolveLoginPassword's `!isInteractive` guard (so
// it always attempts to prompt) makes Login hang against this exact pipe,
// which this test's bounded goroutine+select detects as a failure rather
// than letting it hang the test suite itself.
func TestLoginNeverPromptsOnATerminalLessStdin(t *testing.T) {
	blockingRead, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { writeEnd.Close() })
	t.Cleanup(func() { blockingRead.Close() })

	te := newTestEnv(t)
	te.Env.Stdin = blockingRead

	done := make(chan error, 1)
	go func() {
		_, err := Login(context.Background(), te.Env, LoginOptions{URL: "https://aliases.example.com"})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Login() succeeded against a stdin that never delivered a password")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Login() blocked reading a terminal-less stdin instead of failing immediately — " +
			"this is the exact hang this project has already shipped once")
	}
}

// TestLoginReadsPasswordFromAPipedStdin proves the other half of the same
// bounded operation: a real, deliberate --password-stdin invocation (a pipe
// that is written to and then closed, exactly as `echo $PASS | aliasdeck
// login --password-stdin` produces) must actually work, not merely "not
// hang".
func TestLoginReadsPasswordFromAPipedStdin(t *testing.T) {
	srv := loginServer(t, "hunter2")
	defer srv.Close()

	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { readEnd.Close() })

	go func() {
		fmt.Fprintln(writeEnd, "hunter2")
		writeEnd.Close()
	}()

	te := newTestEnv(t)
	te.Env.Stdin = readEnd

	report, err := Login(context.Background(), te.Env, LoginOptions{
		URL:           srv.URL,
		PasswordStdin: true,
	})
	if err != nil {
		t.Fatalf("Login() returned an error: %v", err)
	}
	if report.ServerURL != srv.URL {
		t.Errorf("ServerURL = %q, want %q", report.ServerURL, srv.URL)
	}
}
