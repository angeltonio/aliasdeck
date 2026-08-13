package main

import (
	"strings"
	"testing"
)

// wantServerCLICommands is every command Milestone 4 adds to the root
// command tree, plus the pre-existing ones, so a regression that drops any
// single one — new or old — is caught in the same place.
var wantRootCommands = []string{
	"init", "sync", "status", "list", "doctor", "edit", "uninstall",
	"serve", "login", "register", "logout",
}

// TestRootCommandRegistersEveryServerCLICommand is task 8.14's own proof:
// after Phase 8, `serve`, `login`, `register`, and `logout` must all be
// reachable from the root command tree — not merely implemented as
// free-standing newXCmd() constructors nobody wires in (as login/register/
// logout were before this task). Deleting any one of them from
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
	for _, name := range []string{"serve", "login", "register", "logout"} {
		if !strings.Contains(stdout, name) {
			t.Errorf("--help output does not mention %q:\n%s", name, stdout)
		}
	}
}
