//go:build !windows

package renderers

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/shelltest"
)

// TestGeneratedFileIsInertInRealShells feeds AliasDeck's output to actual bash
// and zsh binaries and proves that hostile input stays data.
//
// Every other test in this package checks that the escaping logic does what we
// believe it does. This one checks that what we believe is true, by asking the
// only authority that matters. Unit tests encode an assumption about shell
// grammar; this executes it.
//
// The test skips rather than fails when a shell is missing, so it strengthens
// CI where the shells exist without blocking contributors who lack them.
func TestGeneratedFileIsInertInRealShells(t *testing.T) {
	const canaryName = "aliasdeck_injection_canary"

	shells := []struct {
		bin   string
		shell domain.Shell
		// bash needs to be told to expand aliases when not interactive.
		preamble string
	}{
		{bin: "bash", shell: domain.ShellBash, preamble: "shopt -s expand_aliases\n"},
		{bin: "zsh", shell: domain.ShellZsh},
	}

	for _, sh := range shells {
		t.Run(sh.bin, func(t *testing.T) {
			bin := shelltest.LookPath(t, sh.bin)

			dir := t.TempDir()
			canary := filepath.Join(dir, canaryName)

			// Each command tries a different way out of the quoted region:
			// closing the quote and starting a new statement, closing it and
			// chaining with &&, and closing it inside a substitution.
			cfg := domain.ResolvedConfig{
				Device: domain.Device{Name: "victim", Platform: domain.PlatformLinux, Shell: sh.shell},
				Aliases: []domain.Alias{
					{Name: "attack_semicolon", Enabled: true,
						Command: `echo a'; touch ` + canary + `; echo '`},
					{Name: "attack_chain", Enabled: true,
						Command: `echo b' && touch ` + canary + ` && echo '`},
					{Name: "attack_subshell", Enabled: true,
						Command: `echo c'$(touch ` + canary + `)'`},
					{Name: "attack_backtick", Enabled: true,
						Command: "echo d'`touch " + canary + "`'"},
				},
			}
			cfg.Revision = cfg.ComputeRevision()

			rendered, err := Render(cfg)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}

			generated := filepath.Join(dir, "aliases."+sh.bin)
			if err := os.WriteFile(generated, []byte(rendered), 0o600); err != nil {
				t.Fatalf("writing generated file: %v", err)
			}

			// The invocations must live in their own file. Both shells expand
			// aliases at parse time, and they parse a `-c` string as a single
			// unit, so an alias defined by `source` on one line is not yet
			// known to a call on the next. A separately sourced file is parsed
			// after the definitions have taken effect — which is also exactly
			// how a real user's shell startup works.
			runner := filepath.Join(dir, "run.sh")
			invocations := "attack_semicolon\nattack_chain\nattack_subshell\nattack_backtick\n"
			if err := os.WriteFile(runner, []byte(invocations), 0o600); err != nil {
				t.Fatalf("writing runner: %v", err)
			}

			script := sh.preamble +
				"source " + generated + "\n" +
				"source " + runner + "\n"

			out, err := exec.Command(bin, "-c", script).CombinedOutput()
			if err != nil {
				t.Fatalf("%s could not source the generated file: %v\n%s", sh.bin, err, out)
			}

			if _, err := os.Stat(canary); err == nil {
				t.Fatalf("INJECTION: %s executed an embedded command.\ngenerated file:\n%s", sh.bin, rendered)
			}

			// The payload must have reached echo as literal text. If the shell
			// had swallowed it as syntax, these fragments would be gone.
			for _, want := range []string{"; touch", "&& touch", "$(touch", "`touch"} {
				if !strings.Contains(string(out), want) {
					t.Errorf("%s: expected %q to survive as literal text, output was:\n%s",
						sh.bin, want, out)
				}
			}
		})
	}
}
