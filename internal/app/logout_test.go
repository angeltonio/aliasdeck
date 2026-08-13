package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/config"
)

// TestLogoutRemovesLocalSession pins "logout removes local session": a
// stored session is cleared and the device token, if any, is left alone —
// the two credentials are revoked independently (design decision 17).
func TestLogoutRemovesLocalSession(t *testing.T) {
	te := newTestEnv(t)
	credsPath := config.CredentialsFile(te.Base)
	if err := config.SaveCredentials(credsPath, config.Credentials{
		Version:          1,
		ServerURL:        "https://aliases.example.com",
		DeviceID:         "device-abc123",
		DeviceToken:      "adt_lookup.secret",
		SessionToken:     "ads_lookup.secret",
		SessionExpiresAt: fixedNow.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("seeding credentials.json: %v", err)
	}

	report, err := Logout(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("Logout() returned an error: %v", err)
	}
	if !report.SessionCleared {
		t.Error("SessionCleared = false, want true")
	}

	creds, err := config.LoadCredentials(credsPath)
	if err != nil {
		t.Fatalf("LoadCredentials() returned an error: %v", err)
	}
	if creds.SessionToken != "" {
		t.Errorf("SessionToken = %q after Logout(), want empty", creds.SessionToken)
	}
	if !creds.SessionExpiresAt.IsZero() {
		t.Errorf("SessionExpiresAt = %v after Logout(), want zero", creds.SessionExpiresAt)
	}
	if creds.DeviceToken != "adt_lookup.secret" {
		t.Errorf("DeviceToken = %q after Logout(), want it left untouched", creds.DeviceToken)
	}
}

// TestLogoutWithNoStoredSessionSucceeds pins that logging out when already
// logged out is not an error.
func TestLogoutWithNoStoredSessionSucceeds(t *testing.T) {
	te := newTestEnv(t)

	report, err := Logout(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("Logout() returned an error with no stored session: %v", err)
	}
	if report.SessionCleared {
		t.Error("SessionCleared = true, want false when nothing was stored")
	}
}

// TestLogoutNeverContactsTheServer keeps a real, live, reachable server up
// through the whole test — deliberately unlike
// TestLogoutSucceedsWithoutServerReachability below — specifically so a
// mutation that adds a best-effort, error-ignoring HTTP call to Logout
// cannot hide behind "the request happened to fail anyway". The handler
// itself fails the test the instant it receives any request; a mutation
// whose own error is silently discarded would still be caught here, because
// this server is fully able to answer.
//
// Mutation check: adding any HTTP request against creds.ServerURL to
// Logout — even one whose result is deliberately discarded — makes this
// test fail, naming the unexpected request. This is the "logout contacts
// the server" mutation this project's test standard requires proving.
func TestLogoutNeverContactsTheServer(t *testing.T) {
	contacted := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contacted <- r.Method + " " + r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	te := newTestEnv(t)
	if err := config.SaveCredentials(config.CredentialsFile(te.Base), config.Credentials{
		Version:          1,
		ServerURL:        srv.URL,
		SessionToken:     "ads_lookup.secret",
		SessionExpiresAt: fixedNow.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("seeding credentials.json: %v", err)
	}

	if _, err := Logout(context.Background(), te.Env, Options{}); err != nil {
		t.Fatalf("Logout() returned an error: %v", err)
	}

	select {
	case req := <-contacted:
		t.Fatalf("Logout() contacted the server (%s), want zero requests", req)
	case <-time.After(200 * time.Millisecond):
		// No request arrived within a short, generous bound: this is the
		// success path. There is nothing to wait longer for — Logout has
		// already returned above, so any request it was going to make has
		// already had every opportunity to arrive.
	}
}

// TestLogoutSucceedsWithoutServerReachability is the cli-commands scenario
// "logout succeeds without server reachability": an unreachable server must
// never turn into a Logout failure.
func TestLogoutSucceedsWithoutServerReachability(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	// Close immediately: from this point on, srv.URL is guaranteed
	// unreachable, not merely unresponsive.
	srv.Close()

	te := newTestEnv(t)
	if err := config.SaveCredentials(config.CredentialsFile(te.Base), config.Credentials{
		Version:          1,
		ServerURL:        srv.URL,
		SessionToken:     "ads_lookup.secret",
		SessionExpiresAt: fixedNow.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("seeding credentials.json: %v", err)
	}

	report, err := Logout(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("Logout() returned an error against an unreachable server: %v", err)
	}
	if !report.SessionCleared {
		t.Error("SessionCleared = false, want true")
	}

	creds, err := config.LoadCredentials(config.CredentialsFile(te.Base))
	if err != nil {
		t.Fatalf("LoadCredentials() returned an error: %v", err)
	}
	if creds.SessionToken != "" {
		t.Errorf("SessionToken = %q after Logout(), want empty", creds.SessionToken)
	}
}
