package main

import (
	"strings"
	"testing"
)

// wantRootCommands is every command cmd/aliasdeck registers, so a
// regression that drops any single one is caught in the same place.
// `serve` is deliberately absent: cmd/aliasdeck is client-only — the server
// lives in cmd/aliasdeck-server, which has no subcommands of its own
// (design decision reversing the earlier single-binary model,
// docs/WHAT-WE-ARE-BUILDING.md).
var wantRootCommands = []string{
	"init", "sync", "heartbeat", "watch", "status", "list", "doctor", "edit", "uninstall",
	"login", "register", "logout", "agent",
}

// TestRootCommandRegistersEveryServerCLICommand is task 8.14's own proof,
// extended by the client/server split: `login`, `register`, and `logout`
// must all be reachable from the root command tree — not merely implemented
// as free-standing newXCmd() constructors nobody wires in (as login/
// register/logout were before task 8.14). Deleting any one of them from
// root.AddCommand's argument list (cmd/aliasdeck/root.go) makes this test
// fail, naming exactly which command went missing.
func TestRootCommandRegistersEveryServerCLICommand(t *testing.T) {
	root := newRootCmd()

	got := make(map[string]bool, len(wantRootCommands))
	for _, c := range root.Commands() {
		got[c.Name()] = true
	}

	for _, name := range wantRootCommands {
		if !got[name] {
			t.Errorf("root command tree is missing %q; task 8.14 requires it registered in cmd/aliasdeck/root.go", name)
		}
	}
}

// TestRootCommandHelpNamesEveryServerCLICommandWithADescription proves what
// a real user actually sees: `aliasdeck --help`'s rendered output must list
// every command by name alongside a non-empty Short description, not just
// that Commands() contains it internally (which a typo in the registered
// name, or a command with no Short text, would not catch).
func TestRootCommandHelpNamesEveryServerCLICommandWithADescription(t *testing.T) {
	root := newRootCmd()
	for _, c := range root.Commands() {
		if strings.TrimSpace(c.Short) == "" {
			t.Errorf("command %q has an empty Short description", c.Name())
		}
	}

	stdout, _, code := runCmd(t, "--help")
	if code != exitOK {
		t.Fatalf("--help exit code = %d, want %d", code, exitOK)
	}
	for _, name := range []string{"login", "register", "logout"} {
		if !strings.Contains(stdout, name) {
			t.Errorf("--help output does not mention %q:\n%s", name, stdout)
		}
	}
}

// TestRootCommandNeverRegistersServe is the regression test for the client/
// server split: cmd/aliasdeck must never regain a `serve` command. Its
// structural counterpart is
// internal/archtest.TestClientBinaryNeverImportsServerPackages, which fails
// the build the first time cmd/aliasdeck imports internal/server at all;
// this test fails just as fast if a `serve` command is ever added back
// without importing internal/server directly (e.g. by shelling out).
func TestRootCommandNeverRegistersServe(t *testing.T) {
	root := newRootCmd()
	for _, c := range root.Commands() {
		if c.Name() == "serve" {
			t.Fatal("root command tree registers \"serve\": cmd/aliasdeck is client-only, serve belongs to cmd/aliasdeck-server")
		}
	}
}

func TestInitCommandRegistersSkipInitialSyncFlag(t *testing.T) {
	flag := newInitCmd().Flags().Lookup("skip-initial-sync")
	if flag == nil {
		t.Fatal("init command does not register --skip-initial-sync")
	}
	if flag.DefValue != "false" {
		t.Fatalf("--skip-initial-sync default = %q, want false", flag.DefValue)
	}
}
