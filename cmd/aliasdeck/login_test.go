package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// loginCmdTestServer is a minimal httptest.Server implementing exactly
// POST /api/v1/auth/login against wantPassword, mirroring
// internal/app/login_test.go's own loginServer helper — duplicated here for
// the same reason registerCmdTestServer is: this file proves the wiring
// cmd/aliasdeck/login.go itself performs through the real Cobra RunE
// (bounded-review finding, correction pass, WARNING 3), which
// internal/app's own tests (calling Login directly) cannot prove.
func loginCmdTestServer(t *testing.T, wantPassword string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/login" {
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
		if in.Username != "admin" || in.Password != wantPassword {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "invalid_credentials", "message": "invalid username or password"},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":     "ads_cmdlookup.cmdsecret",
			"expiresAt": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		})
	}))
}

// withReplacedStdin replaces the real os.Stdin with r for the duration of
// the test, restoring the original on cleanup. This is the only way to
// control what `login`'s RunE reads: cmd/aliasdeck/login.go calls
// app.OSEnv(), which hardcodes os.Stdin — there is no injectable Env at the
// Cobra layer. Every use below sets a pipe whose write end is closed
// immediately or promptly, so a read against it never blocks past an
// instantaneous EOF or a single already-buffered line — this is the
// TestRunNeverReadsStdin/TestLoginNeverPromptsOnATerminalLessStdin
// technique applied deliberately in reverse (a controlled, non-hanging
// stdin), never a live terminal a human might be attached to.
func withReplacedStdin(t *testing.T, r *os.File) {
	t.Helper()
	original := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = original })
}

// TestLoginCommandRequiresURL proves --url reaches app.LoginOptions.URL at
// all: omitting it must fail before any stdin read or network call.
func TestLoginCommandRequiresURL(t *testing.T) {
	initForServerCommands(t)

	_, stderr, code := runCmd(t, "login")
	if code == exitOK {
		t.Fatal("login with no --url must not succeed")
	}
	if !strings.Contains(stderr, "--url") {
		t.Errorf("stderr = %q, want it to name --url", stderr)
	}
}

// TestLoginCommandWiresAllowInsecureAndPasswordStdinFlags proves --url,
// --allow-insecure, and --password-stdin all reach app.LoginOptions,
// without ever depending on a real network call or a stdin that could hang:
// os.Stdin is replaced with a pipe whose write end is closed immediately
// (an instantaneous EOF), so the exact failure message login returns
// distinguishes which flags actually took effect.
//
//   - A non-loopback http:// URL without --allow-insecure must fail at
//     ValidateServerURL, before stdin is ever touched.
//   - The identical URL with --allow-insecure and --password-stdin must
//     fail differently — with the password-resolution error ("no password
//     was provided on stdin") — proving both flags reached Options: had
//     --allow-insecure not been wired, the URL error would still fire; had
//     --password-stdin not been wired, isInteractive would report the
//     closed pipe as non-interactive and fail with a --password-stdin
//     hint instead, a third distinct message.
//
// Mutation check: hardcoding cmd/aliasdeck/login.go's
// `AllowInsecureHTTP: allowInsecureHTTP` to `AllowInsecureHTTP: false`
// makes the second case fail with the insecure-URL error again instead of
// the password error — see apply-progress for the verbatim output.
func TestLoginCommandWiresAllowInsecureAndPasswordStdinFlags(t *testing.T) {
	initForServerCommands(t)

	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	writeEnd.Close() // instantaneous EOF, never a hang
	defer readEnd.Close()
	withReplacedStdin(t, readEnd)

	_, stderr, code := runCmd(t, "login", "--url", "http://aliases.example.com")
	if code == exitOK {
		t.Fatal("login must reject a non-loopback http:// URL without --allow-insecure")
	}
	if !strings.Contains(stderr, "http") && !strings.Contains(stderr, "loopback") {
		t.Errorf("stderr = %q, want it to mention the rejected URL/scheme", stderr)
	}

	_, stderr, code = runCmd(t, "login", "--url", "http://aliases.example.com", "--allow-insecure", "--password-stdin")
	if code == exitOK {
		t.Fatal("login against a closed, empty --password-stdin pipe must not succeed")
	}
	if strings.Contains(stderr, "loopback") {
		t.Errorf("stderr = %q, want --allow-insecure to have bypassed the URL rejection", stderr)
	}
	if !strings.Contains(stderr, "stdin") {
		t.Errorf("stderr = %q, want the password-resolution error naming stdin, proving --password-stdin reached Options", stderr)
	}
}

// TestLoginCommandSuccessEndToEnd runs the real `aliasdeck login` command
// through its own RunE against a real httptest server, with a real
// --password-stdin pipe that is written to and then closed — the complete,
// successful path, not merely a rejection.
func TestLoginCommandSuccessEndToEnd(t *testing.T) {
	initForServerCommands(t)

	srv := loginCmdTestServer(t, "hunter2")
	defer srv.Close()

	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer readEnd.Close()
	go func() {
		writeEnd.WriteString("hunter2\n")
		writeEnd.Close()
	}()
	withReplacedStdin(t, readEnd)

	stdout, stderr, code := runCmd(t, "login", "--url", srv.URL, "--password-stdin")
	if code != exitOK {
		t.Fatalf("login exit code = %d, want %d (stderr: %s)", code, exitOK, stderr)
	}
	if !strings.Contains(stdout, srv.URL) {
		t.Errorf("stdout = %q, want it to name the server URL", stdout)
	}
}
