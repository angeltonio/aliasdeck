package verify

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/app"
	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
)

const (
	fullFlowAliasName    = "verifyfullflow"
	fullFlowAliasCommand = "echo full-flow sync worked"
)

// newFullFlowEnv builds an internal/app.Env isolated entirely under home
// (a t.TempDir()): $ALIASDECK_HOME points at it, HomeDir resolves to it,
// and nothing here ever touches the real machine's actual home directory,
// environment, or stdin. It also writes the config.yaml register's own
// precondition requires (loadDeviceIdentity returns ErrNotInitialized
// against a directory with no config.yaml yet) — a native, file-sourced
// device named explicitly, so device.platform/device.shell resolve
// deterministically regardless of which OS actually runs this test
// (config.DetectPlatform/DetectShell both honor an explicit config.yaml
// override ahead of runtime.GOOS or $SHELL).
func newFullFlowEnv(t *testing.T, home string) app.Env {
	t.Helper()

	getenv := func(key string) string {
		if key == "ALIASDECK_HOME" {
			return home
		}
		return ""
	}

	env := app.Env{
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Getenv:   getenv,
		HomeDir:  func() (string, error) { return home, nil },
		Now:      time.Now,
		LookPath: func(string) (string, error) { return "", os.ErrNotExist },
	}

	cfg := config.DeviceFileConfig{
		Version: 1,
		Device: config.DeviceConfig{
			Name:     "verify-fullflow-device",
			Platform: "linux",
			Shell:    "bash",
		},
		Source:  config.Source{Type: config.SourceTypeFile},
		Backend: config.BackendNative,
	}
	if err := config.Write(config.ConfigFile(home), cfg); err != nil {
		t.Fatalf("writing initial config.yaml: %v", err)
	}

	return env
}

// TestServeLoginEnrollRegisterSyncEndToEnd is task 9.2: the complete
// serve -> login -> enrollment token -> register -> sync chain, in one
// process. It is this milestone's first test that exercises the whole
// product as a user would, rather than one layer at a time:
//
//   - "serve" is newTestServer(t): the real production router
//     (internal/api.NewRouter, the exact handler internal/server.Run
//     installs) over a real httptest.Server (an ephemeral loopback
//     listener — never a fixed port), backed by a real SQLite database in
//     t.TempDir(), with one operator bootstrapped through the real
//     auth.Bootstrap.
//   - "login" and "register" call internal/app.Login and
//     internal/app.Register directly — the exact functions
//     cmd/aliasdeck/login.go and cmd/aliasdeck/register.go call. There is
//     no CLI-level wrapper yet for minting an enrollment token (that is an
//     operator action with no `aliasdeck` subcommand in this milestone —
//     only the Web UI, M5, will add one), so that one step goes through
//     the real HTTP endpoint directly via the harness, exactly as a
//     hand-rolled `curl` would.
//   - "sync" calls internal/app.Sync — the exact function
//     cmd/aliasdeck/sync.go calls — which resolves the now-registered
//     server source, renders through the real renderer registry, and
//     writes the generated file through the real NativeBackend.
//
// t.Cleanup shuts the server down; the store, the config directory and the
// generated file all live under one t.TempDir(); nothing here sleeps or
// binds a fixed port.
//
// Mutation this test detects: internal/app.Register's whole safety
// property is its write ORDER — the enrollment-token exchange must
// succeed, then the device credential must save, and only then does
// config.yaml's source.type flip to "server" (task 8.5's doc comment).
// Reproduced directly against this test: commenting out Register's final
// config.Write call (so source.type is left at "file") makes the
// config.Load assertion below fail immediately, and — had it not — would
// have made the subsequent app.Sync resolve against the original,
// unrelated local aliases.yaml instead of the server, which the generated-
// file content assertion at the end would also have caught. Reverted after
// confirming the failure; see apply-progress for the verbatim output.
//
// A second mutation was verified against the server side of this same
// chain, not the client side: internal/api/router.go's sync route
// temporarily had its RequiredKind changed from store.TokenKindDevice to
// store.TokenKindSession. The freshly-issued device token this test
// obtains from register is a device-kind token, so RequireKind's own kind
// check rejected it, and app.Sync's call below failed with a wrapped 401
// exactly as expected — proving this test's final Sync call is actually
// exercising real token-kind enforcement, not merely reaching a handler
// that would have accepted anything. Reverted after confirming the
// failure; see apply-progress for the verbatim output.
func TestServeLoginEnrollRegisterSyncEndToEnd(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()

	// Seeding one alias directly through the store stands in for the
	// operator action this milestone has no CLI for yet (alias CRUD is
	// reachable only through the API — a Web UI is M5); internal/api's own
	// handler tests already cover that write path in isolation, so this
	// test's job is what happens to an alias once it exists, not how an
	// operator would have created it.
	if _, err := ts.Store.Aliases().Create(ctx, domain.Alias{
		Name:    fullFlowAliasName,
		Command: fullFlowAliasCommand,
		Enabled: true,
	}); err != nil {
		t.Fatalf("seeding server alias: %v", err)
	}

	sessionToken := ts.login(t, harnessAdminPassword)
	enrollmentToken := ts.mintEnrollmentToken(t, sessionToken, nil)

	home := t.TempDir()
	env := newFullFlowEnv(t, home)

	regReport, err := app.Register(ctx, env, app.RegisterOptions{
		URL:   ts.URL,
		Token: enrollmentToken,
	})
	if err != nil {
		t.Fatalf("app.Register: %v", err)
	}
	if regReport.ServerURL != ts.URL {
		t.Fatalf("RegisterReport.ServerURL = %q, want %q", regReport.ServerURL, ts.URL)
	}
	if regReport.DeviceID == "" {
		t.Fatal("RegisterReport.DeviceID is empty")
	}

	loaded, err := config.Load(config.ConfigFile(home))
	if err != nil {
		t.Fatalf("config.Load after register: %v", err)
	}
	if loaded.Source.Type != config.SourceTypeServer {
		t.Fatalf("Source.Type after register = %q, want %q", loaded.Source.Type, config.SourceTypeServer)
	}
	if loaded.Source.URL != ts.URL {
		t.Fatalf("Source.URL after register = %q, want %q", loaded.Source.URL, ts.URL)
	}

	syncReport, err := app.Sync(ctx, env, app.Options{})
	if err != nil {
		t.Fatalf("app.Sync: %v", err)
	}
	if syncReport.Skipped {
		t.Fatal("SyncReport.Skipped = true on the very first sync, want false")
	}
	if syncReport.AliasCount != 1 {
		t.Fatalf("SyncReport.AliasCount = %d, want 1", syncReport.AliasCount)
	}
	if syncReport.Revision == "" {
		t.Fatal("SyncReport.Revision is empty")
	}

	generated, err := os.ReadFile(syncReport.OutputPath)
	if err != nil {
		t.Fatalf("reading generated file %s: %v", syncReport.OutputPath, err)
	}
	if !strings.Contains(string(generated), fullFlowAliasName) {
		t.Fatalf("generated file %s does not contain alias %q:\n%s", syncReport.OutputPath, fullFlowAliasName, generated)
	}
	if !strings.Contains(string(generated), fullFlowAliasCommand) {
		t.Fatalf("generated file %s does not contain command %q:\n%s", syncReport.OutputPath, fullFlowAliasCommand, generated)
	}

	// A second sync with nothing changed server-side must be a true no-op
	// (sync-state spec, "No-Op Skip When Unchanged") — proving this
	// end-to-end chain also reaches the revision/state comparison, not
	// just a fresh write every time.
	secondReport, err := app.Sync(ctx, env, app.Options{})
	if err != nil {
		t.Fatalf("second app.Sync: %v", err)
	}
	if !secondReport.Skipped {
		t.Fatal("second SyncReport.Skipped = false, want true — nothing changed server-side since the first sync")
	}
}
