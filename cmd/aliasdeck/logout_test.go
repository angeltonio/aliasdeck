package main

import (
	"strings"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/config"
)

// TestLogoutCommandClearsSessionEndToEnd runs the real `aliasdeck logout`
// command through its own RunE (bounded-review finding, correction pass,
// WARNING 3: no test previously executed any of login/register/logout's
// RunE). logout takes no flags, so there is no flag binding to mutate —
// this test's job is simply proving the command is wired to app.Logout at
// all, end to end through the real credentials file on disk.
func TestLogoutCommandClearsSessionEndToEnd(t *testing.T) {
	base := initForServerCommands(t)

	credsPath := config.CredentialsFile(base)
	seeded := config.Credentials{
		Version:          1,
		ServerURL:        "https://aliases.example.com",
		DeviceID:         "device-1",
		DeviceToken:      "adt_lookup.secret",
		ObtainedAt:       time.Now(),
		SessionToken:     "ads_lookup.secret",
		SessionExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := config.SaveCredentials(credsPath, seeded); err != nil {
		t.Fatalf("seeding credentials.json: %v", err)
	}

	stdout, stderr, code := runCmd(t, "logout")
	if code != exitOK {
		t.Fatalf("logout exit code = %d, want %d (stderr: %s)", code, exitOK, stderr)
	}
	if !strings.Contains(stdout, "Logged out") {
		t.Errorf("stdout = %q, want it to confirm the logout", stdout)
	}

	creds, err := config.LoadCredentials(credsPath)
	if err != nil {
		t.Fatalf("LoadCredentials() returned an error: %v", err)
	}
	if creds.SessionToken != "" {
		t.Errorf("SessionToken = %q, want empty after logout", creds.SessionToken)
	}
	if creds.DeviceToken != "adt_lookup.secret" {
		t.Errorf("DeviceToken = %q, want it left untouched by logout", creds.DeviceToken)
	}
}

// TestLogoutCommandSucceedsWithNoStoredSession proves the "already logged
// out" case reports success, not an error, matching app.Logout's own
// documented contract.
func TestLogoutCommandSucceedsWithNoStoredSession(t *testing.T) {
	initForServerCommands(t)

	stdout, stderr, code := runCmd(t, "logout")
	if code != exitOK {
		t.Fatalf("logout exit code = %d, want %d (stderr: %s)", code, exitOK, stderr)
	}
	if !strings.Contains(stdout, "No local session") {
		t.Errorf("stdout = %q, want it to report no session was stored", stdout)
	}
}
