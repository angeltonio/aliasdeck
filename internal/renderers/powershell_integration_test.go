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

// TestGeneratedFileIsInertInRealPowerShell is the PowerShell counterpart to
// TestGeneratedFileIsInertInRealShells (shell_integration_test.go), and the
// reason this whole package exists. It dot-sources AliasDeck's actual output
// in a real `pwsh` and proves that hostile alias commands execute nothing at
// source time — the failure mode design decision 1 warns about: a `}` in raw
// code would close the function block early and run whatever follows
// immediately on load, not on call.
//
// Unlike the POSIX version, this file carries no `//go:build !windows` tag:
// pwsh is the same interpreter on every OS AliasDeck targets, so the proof
// is meant to run on Windows CI too, not just macOS/Linux.
func TestGeneratedFileIsInertInRealPowerShell(t *testing.T) {
	const canaryName = "aliasdeck_injection_canary"

	bin := shelltest.LookPath(t, "pwsh")

	dir := t.TempDir()
	canary := filepath.Join(dir, canaryName)

	// Each command tries a different way out of the single-quoted literal
	// that quotePowerShell produces: closing it and chaining with a semicolon,
	// closing it inside a subexpression, embedding a raw quote, and finally
	// abusing the `''` doubling mechanism itself to try to desynchronize the
	// escaper. touch is spelled as a PowerShell statement so an escape would
	// be unambiguous: it either ran, or it did not.
	touch := "New-Item -ItemType File -Path '" + canary + "' -Force | Out-Null"
	cfg := domain.ResolvedConfig{
		Device: domain.Device{Name: "victim", Platform: domain.PlatformWindows, Shell: domain.ShellPowerShell},
		Aliases: []domain.Alias{
			{Name: "attack_brace", Enabled: true,
				Command: `Write-Output a } ` + touch + ` function evil {`},
			{Name: "attack_semicolon", Enabled: true,
				Command: `Write-Output b; ` + touch},
			{Name: "attack_subshell", Enabled: true,
				Command: `Write-Output c $(` + touch + `)`},
			{Name: "attack_quote", Enabled: true,
				Command: `Write-Output d' ` + touch + ` Write-Output '`},
			{Name: "attack_double_quote_confusion", Enabled: true,
				Command: `Write-Output e'' ` + touch + ` ''`},
			// Benign: the reference used to prove arguments still forward
			// correctly once the file is safely dot-sourced.
			{Name: "showargs", Enabled: true,
				Command: `Write-Output`},
		},
	}
	cfg.Revision = cfg.ComputeRevision()

	rendered, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	generated := filepath.Join(dir, "aliases.ps1")
	if err := os.WriteFile(generated, []byte(rendered), 0o600); err != nil {
		t.Fatalf("writing generated file: %v", err)
	}

	t.Run("hostile commands execute nothing at source time", func(t *testing.T) {
		script := ". '" + generated + "'\n"

		out, err := exec.Command(bin, "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
		if err != nil {
			t.Fatalf("pwsh could not dot-source the generated file: %v\n%s", err, out)
		}

		if _, statErr := os.Stat(canary); statErr == nil {
			t.Fatalf("INJECTION: pwsh executed an embedded command while dot-sourcing.\ngenerated file:\n%s", rendered)
		}

		// Dot-sourcing must not have produced any output either — a function
		// *definition* is silent. Any visible "a", "b", "c", "d" or "e" from
		// Write-Output would mean the payload ran as code, not that it was
		// merely stored as an inert string.
		if strings.TrimSpace(string(out)) != "" {
			t.Fatalf("dot-sourcing produced output, meaning something executed:\n%s", out)
		}
	})

	t.Run("arguments forward intact through both @args", func(t *testing.T) {
		// A benign alias called with two arguments, one containing a space,
		// must reach the aliased command as two arguments with the space
		// preserved — the positive guarantee behind the double-@args form
		// (design decision 2, §6.3). Each -join line makes the argument
		// count and boundaries unambiguous in the captured output.
		script := ". '" + generated + "'\n" +
			`$result = showargs first "second with spaces"` + "\n" +
			`Write-Output "COUNT=$($result.Count)"` + "\n" +
			`Write-Output "ARG0=$($result[0])"` + "\n" +
			`Write-Output "ARG1=$($result[1])"` + "\n"

		out, err := exec.Command(bin, "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
		if err != nil {
			t.Fatalf("pwsh could not run the generated alias: %v\n%s", err, out)
		}

		got := string(out)
		for _, want := range []string{"COUNT=2", "ARG0=first", "ARG1=second with spaces"} {
			if !strings.Contains(got, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, got)
			}
		}
	})
}
