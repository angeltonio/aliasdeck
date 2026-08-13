package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/config"
)

func seedBootstrappedDevice(t *testing.T, te *testEnv, priorRCContent string) (rcPath string) {
	t.Helper()
	seedSyncableDevice(t, te)

	rcPath = te.Home + "/.zshrc"
	if priorRCContent != "" {
		if err := os.WriteFile(rcPath, []byte(priorRCContent), 0o644); err != nil {
			t.Fatalf("seeding rc file: %v", err)
		}
	}

	if _, err := Init(context.Background(), te.Env, InitOptions{
		Confirm: func(string) (bool, error) { return true, nil },
	}); err != nil {
		t.Fatalf("Init() returned an error: %v", err)
	}
	return rcPath
}

func TestUninstallRestoresRCFileByteIdentically(t *testing.T) {
	te := newTestEnv(t)
	prior := "# my own stuff\nexport PATH=\"$PATH:/usr/local/bin\"\n"
	rcPath := seedBootstrappedDevice(t, te, prior)

	report, err := Uninstall(context.Background(), te.Env, UninstallOptions{Yes: true})
	if err != nil {
		t.Fatalf("Uninstall() returned an error: %v", err)
	}

	if !report.BootstrapExact {
		t.Error("BootstrapExact = false, want true for an untouched, exact-block removal")
	}

	got, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file after uninstall: %v", err)
	}
	if string(got) != prior {
		t.Errorf("rc file after uninstall = %q, want byte-identical to the original %q", got, prior)
	}

	if !report.OutputRemoved {
		t.Error("OutputRemoved = false, want true")
	}
	if _, err := os.Stat(report.OutputPath); err == nil {
		t.Errorf("generated file %q still exists after uninstall", report.OutputPath)
	}
}

func TestUninstallYesSkipsPrompt(t *testing.T) {
	te := newTestEnv(t)
	seedBootstrappedDevice(t, te, "")

	confirmCalled := false
	_, err := Uninstall(context.Background(), te.Env, UninstallOptions{
		Yes: true,
		Confirm: func(string) (bool, error) {
			confirmCalled = true
			return true, nil
		},
	})
	if err != nil {
		t.Fatalf("Uninstall() returned an error: %v", err)
	}
	if confirmCalled {
		t.Error("--yes must skip the confirmation prompt entirely")
	}
}

func TestUninstallInteractivePromptsBeforeModifying(t *testing.T) {
	te := newTestEnv(t)
	rcPath := seedBootstrappedDevice(t, te, "")

	before, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file: %v", err)
	}

	asked := false
	report, err := Uninstall(context.Background(), te.Env, UninstallOptions{
		Confirm: func(string) (bool, error) {
			asked = true
			return false, nil
		},
	})
	if err != nil {
		t.Fatalf("Uninstall() returned an error: %v", err)
	}
	if !asked {
		t.Error("Uninstall() without --yes must prompt for confirmation")
	}
	if !report.Cancelled {
		t.Error("report.Cancelled = false, want true when the user declines")
	}

	after, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file: %v", err)
	}
	if string(before) != string(after) {
		t.Error("declining the prompt must leave the rc file untouched")
	}
	if _, err := os.Stat(config.StateFile(te.Base)); err != nil {
		t.Error("declining the prompt must leave state.json in place")
	}
}

func TestUninstallExactFalseWhenUserEditedInsideBlock(t *testing.T) {
	te := newTestEnv(t)
	prior := "# my own stuff\nexport PATH=\"$PATH:/usr/local/bin\"\n"
	rcPath := seedBootstrappedDevice(t, te, prior)

	data, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file: %v", err)
	}
	// Edit *inside* the marker block, which is what forces the fallback.
	//
	// Appending to the end of the file would leave the recorded block intact
	// and byte-exact removal would still succeed — the fixture has to break
	// the recorded bytes while leaving the markers findable, or it tests
	// nothing the name claims.
	const beginMarker = "# >>> aliasdeck >>>"
	if !strings.Contains(string(data), beginMarker) {
		t.Fatalf("fixture is missing the begin marker, cannot force the fallback:\n%s", data)
	}
	edited := []byte(strings.Replace(
		string(data),
		beginMarker+"\n",
		beginMarker+"\n# a line the user added inside AliasDeck's block\n",
		1,
	))
	if bytes.Equal(edited, data) {
		t.Fatal("fixture did not modify the rc file")
	}
	if err := os.WriteFile(rcPath, edited, 0o644); err != nil {
		t.Fatalf("editing rc file: %v", err)
	}

	report, err := Uninstall(context.Background(), te.Env, UninstallOptions{Yes: true})
	if err != nil {
		t.Fatalf("Uninstall() returned an error: %v", err)
	}
	if !report.BootstrapRemoved {
		t.Error("BootstrapRemoved = false, want true even via the fallback path")
	}

	// The assertion this test is named for. Without it, a regression that made
	// the fallback claim an exact removal would pass here — and the user would
	// be told their rc file was restored byte-for-byte when it was not.
	if report.BootstrapExact {
		t.Error("BootstrapExact = true after the user edited inside the block; " +
			"the marker-scan fallback cannot restore byte-for-byte and must not claim it did")
	}

	got, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file after uninstall: %v", err)
	}
	// The user's own content, which lived outside our block, must survive
	// intact. The fallback is allowed to be imprecise about the block it
	// removes; it is not allowed to take anything else with it.
	if !strings.Contains(string(got), "export PATH=\"$PATH:/usr/local/bin\"") {
		t.Errorf("the fallback destroyed content the user owned:\n%s", got)
	}
	if strings.Contains(string(got), "aliasdeck") {
		t.Errorf("the fallback left an AliasDeck marker behind:\n%s", got)
	}
}

// TestUninstallRemovesTheSourceCache covers what a Git-sourced device leaves
// behind.
//
// A clone records the source url in its own .git/config, and that url may
// carry credentials. Leaving the checkout after uninstall would mean the
// command that exists to undo everything left a secret on disk, in a directory
// the user never created and has no reason to look for.
//
// The design said the cache was "removed by uninstall" before anything removed
// it. That gap was found by reading the two together.
func TestUninstallRemovesTheSourceCache(t *testing.T) {
	te := newTestEnv(t)
	seedBootstrappedDevice(t, te, "")

	// Stand in for a checkout: uninstall must not care which source made it.
	cacheDir := filepath.Join(config.CacheDir(te.Base), "git", "deadbeef1234")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("seeding a cache directory: %v", err)
	}
	secret := filepath.Join(cacheDir, ".git", "config")
	if err := os.MkdirAll(filepath.Dir(secret), 0o700); err != nil {
		t.Fatalf("seeding .git: %v", err)
	}
	if err := os.WriteFile(secret, []byte("[remote \"origin\"]\n\turl = https://user:token@example.com/x.git\n"), 0o600); err != nil {
		t.Fatalf("seeding .git/config: %v", err)
	}

	report, err := Uninstall(context.Background(), te.Env, UninstallOptions{Yes: true})
	if err != nil {
		t.Fatalf("Uninstall() returned an error: %v", err)
	}
	if !report.CacheRemoved {
		t.Error("CacheRemoved = false, want true")
	}
	if _, err := os.Stat(config.CacheDir(te.Base)); !os.IsNotExist(err) {
		t.Errorf("the source cache still exists after uninstall: %v", err)
	}
}

// TestUninstallRemovesCredentialsFile pins task 8.12/8.13: credentials.json
// holds a live server session and/or device token (design decision 14), a
// stronger case for removal than the Git cache's "may carry a URL-embedded
// credential" — this file exists for no other reason. Uninstall must remove
// it alongside its existing cleanup, regardless of the device's active
// source type (a leftover credentials file can outlive a switch back to a
// file/git source).
func TestUninstallRemovesCredentialsFile(t *testing.T) {
	te := newTestEnv(t)
	seedBootstrappedDevice(t, te, "")
	seedCredentials(t, te.Base, config.Credentials{
		DeviceToken:      "adt_lookup.secret",
		SessionToken:     "ads_lookup.secret",
		SessionExpiresAt: fixedNow,
	})

	credsPath := config.CredentialsFile(te.Base)
	if _, err := os.Stat(credsPath); err != nil {
		t.Fatalf("fixture did not seed credentials.json: %v", err)
	}

	report, err := Uninstall(context.Background(), te.Env, UninstallOptions{Yes: true})
	if err != nil {
		t.Fatalf("Uninstall() returned an error: %v", err)
	}
	if !report.CredentialsRemoved {
		t.Error("CredentialsRemoved = false, want true")
	}
	if _, err := os.Stat(credsPath); !os.IsNotExist(err) {
		t.Errorf("credentials.json still exists after uninstall: %v", err)
	}
}

// TestUninstallSucceedsWhenCredentialsFileNeverExisted proves the common
// case — a device that never ran `register` and so has no credentials.json
// at all — still uninstalls cleanly: os.Remove's ErrNotExist must be
// tolerated identically to every other cleanup step here, not surfaced as a
// failure.
func TestUninstallSucceedsWhenCredentialsFileNeverExisted(t *testing.T) {
	te := newTestEnv(t)
	seedBootstrappedDevice(t, te, "")

	if _, err := os.Stat(config.CredentialsFile(te.Base)); !os.IsNotExist(err) {
		t.Fatalf("fixture unexpectedly has a credentials.json: %v", err)
	}

	report, err := Uninstall(context.Background(), te.Env, UninstallOptions{Yes: true})
	if err != nil {
		t.Fatalf("Uninstall() returned an error: %v", err)
	}
	if report.CredentialsRemoved {
		t.Error("CredentialsRemoved = true, want false when no credentials file ever existed")
	}
}
