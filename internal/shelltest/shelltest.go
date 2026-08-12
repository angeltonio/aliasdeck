// Package shelltest holds the one rule shared by every test that runs a real
// shell binary.
//
// It exists because of how these tests fail when a shell is missing. Skipping
// is right on a contributor's laptop, where zsh may simply not be installed —
// but a skip reads as a pass in CI output, and the tests it silences are the
// ones proving AliasDeck does not write executable code into someone's shell.
// A green pipeline that never ran them is worse than a red one.
package shelltest

import (
	"os"
	"os/exec"
	"testing"
)

// RequireEnv is set in CI. When present, a missing shell is a failure rather
// than a skip.
const RequireEnv = "ALIASDECK_REQUIRE_SHELLS"

// LookPath returns the path to the named shell.
//
// It skips the test when the shell is absent locally, and fails it when
// RequireEnv is set, so an environment that promised to exercise the shells
// cannot quietly decline to.
func LookPath(t *testing.T, name string) string {
	t.Helper()

	bin, err := exec.LookPath(name)
	if err == nil {
		return bin
	}

	if os.Getenv(RequireEnv) != "" {
		t.Fatalf("%s is not installed but %s is set: this environment promised to run the "+
			"real-shell tests and must not skip them", name, RequireEnv)
	}

	t.Skipf("%s is not installed on this machine", name)
	return ""
}
