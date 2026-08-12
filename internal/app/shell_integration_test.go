//go:build !windows

package app

import (
	"context"
	"os/exec"
	"testing"
)

// TestSyncedFileSourcesCleanlyInRealShells feeds a full Sync's generated
// output to real bash and zsh binaries and confirms sourcing it exits
// clean.
//
// internal/renderers' TestGeneratedFileIsInertInRealShells already proves a
// rendered string stays inert against hostile alias bodies. That test
// never goes through internal/app: it calls Render directly on a
// hand-built ResolvedConfig. This test instead drives the actual
// Milestone-2 path a real "aliasdeck init && aliasdeck sync" takes — YAML
// parsing, device resolution, rendering, and the atomic write in
// internal/apply — and proves the file that pipeline puts on disk is
// valid syntax a real shell accepts, not only that its escaping is safe.
//
// It skips entirely under -short (it launches real subprocesses) and skips
// per shell when the binary is absent, so contributors without both shells
// installed are not blocked.
func TestSyncedFileSourcesCleanlyInRealShells(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-shell integration test under -short")
	}

	shells := []struct {
		bin   string
		shell string
	}{
		{bin: "bash", shell: "bash"},
		{bin: "zsh", shell: "zsh"},
	}

	for _, sh := range shells {
		t.Run(sh.bin, func(t *testing.T) {
			binPath, err := exec.LookPath(sh.bin)
			if err != nil {
				t.Skipf("%s not installed on this machine", sh.bin)
			}

			te := newTestEnv(t)
			writeConfigYAML(t, te.Base, nativeDeviceConfig("test-device"))
			writeAliasesYAML(t, te.Base, testAliasesYAML)
			te.setenv("ALIASDECK_PLATFORM", "linux")
			te.setenv("ALIASDECK_SHELL", sh.shell)

			report, err := Sync(context.Background(), te.Env, Options{})
			if err != nil {
				t.Fatalf("Sync: %v", err)
			}
			if report.AliasCount == 0 {
				t.Fatal("sync produced no aliases; nothing meaningful was sourced")
			}

			out, err := exec.Command(binPath, "-c", "source "+report.OutputPath).CombinedOutput()
			if err != nil {
				t.Fatalf("%s could not source the generated file: %v\n%s", sh.bin, err, out)
			}
		})
	}
}
