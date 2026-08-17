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

func TestGeneratedPOSIXFileRemovesOnlyStaleManagedAliases(t *testing.T) {
	for _, sh := range []struct {
		bin   string
		shell domain.Shell
	}{
		{bin: "bash", shell: domain.ShellBash},
		{bin: "zsh", shell: domain.ShellZsh},
	} {
		t.Run(sh.bin, func(t *testing.T) {
			bin := shelltest.LookPath(t, sh.bin)
			dir := t.TempDir()
			render := func(name string, aliases []domain.Alias) string {
				t.Helper()
				cfg := domain.ResolvedConfig{Device: domain.Device{Name: "test", Platform: domain.PlatformMacOS, Shell: sh.shell}, Aliases: aliases}
				cfg.Revision = cfg.ComputeRevision()
				got, err := Render(cfg)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, name)
				if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
					t.Fatal(err)
				}
				return path
			}
			first := render("first", []domain.Alias{{Name: "removed", Command: "echo old", Enabled: true}, {Name: "kept", Command: "echo kept", Enabled: true}})
			second := render("second", []domain.Alias{{Name: "kept", Command: "echo updated", Enabled: true}})
			script := "source " + first + "\nalias unrelated='echo user'\nsource " + second + "\n" +
				"alias kept >/dev/null\n! alias removed >/dev/null 2>&1\nalias unrelated >/dev/null\n"
			if out, err := exec.Command(bin, "-c", script).CombinedOutput(); err != nil {
				t.Fatalf("stale-alias cleanup failed: %v\n%s", err, out)
			}
		})
	}
}

func TestGeneratedPOSIXFileMigratesV053AliasesInRealShells(t *testing.T) {
	for _, sh := range []struct {
		bin   string
		shell domain.Shell
	}{
		{bin: "bash", shell: domain.ShellBash},
		{bin: "zsh", shell: domain.ShellZsh},
	} {
		t.Run(sh.bin, func(t *testing.T) {
			bin := shelltest.LookPath(t, sh.bin)
			dir := t.TempDir()
			legacy := readLegacyFixture(t, sh.shell)
			current := MigrateLegacyPOSIXOutput(renderTestPOSIX(t, sh.shell, nil), []byte(legacy), sh.shell)
			legacyPath := filepath.Join(dir, "aliases.v053."+sh.bin)
			currentPath := filepath.Join(dir, "aliases.current."+sh.bin)
			if err := os.WriteFile(legacyPath, []byte(legacy), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(currentPath, []byte(current), 0o600); err != nil {
				t.Fatal(err)
			}
			script := "source " + legacyPath + "\n" +
				"alias unrelated='printf user'\n" +
				"source " + currentPath + "\n" +
				"! alias _under >/dev/null 2>&1\n" +
				"! alias dot.name >/dev/null 2>&1\n" +
				"! alias foo-bar >/dev/null 2>&1\n" +
				"alias unrelated >/dev/null\n" +
				"source " + currentPath + "\n" +
				"alias unrelated >/dev/null\n"
			if out, err := exec.Command(bin, "-c", script).CombinedOutput(); err != nil {
				t.Fatalf("v0.5.3 migration failed: %v\n%s\ncurrent output:\n%s", err, out, current)
			}
		})
	}
}

func TestGeneratedPOSIXFileCarriesMigrationUntilTheShellConsumesIt(t *testing.T) {
	for _, sh := range []struct {
		bin   string
		shell domain.Shell
	}{
		{bin: "bash", shell: domain.ShellBash},
		{bin: "zsh", shell: domain.ShellZsh},
	} {
		t.Run(sh.bin, func(t *testing.T) {
			bin := shelltest.LookPath(t, sh.bin)
			dir := t.TempDir()
			legacy := readLegacyFixture(t, sh.shell)
			first := MigrateLegacyPOSIXOutput(renderTestPOSIX(t, sh.shell, nil), []byte(legacy), sh.shell)
			second := CarryForwardLegacyPOSIXMigration(
				renderTestPOSIX(t, sh.shell, []domain.Alias{{Name: "new-name", Command: "printf new", Enabled: true}}),
				[]byte(first), sh.shell,
			)
			legacyPath := filepath.Join(dir, "aliases.v053."+sh.bin)
			secondPath := filepath.Join(dir, "aliases.second."+sh.bin)
			if err := os.WriteFile(legacyPath, []byte(legacy), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(secondPath, []byte(second), 0o600); err != nil {
				t.Fatal(err)
			}
			// The shell intentionally never sources the first migrated revision.
			script := "source " + legacyPath + "\nsource " + secondPath + "\n" +
				"! alias _under >/dev/null 2>&1\n! alias dot.name >/dev/null 2>&1\n! alias foo-bar >/dev/null 2>&1\nalias new-name >/dev/null\n"
			if out, err := exec.Command(bin, "-c", script).CombinedOutput(); err != nil {
				t.Fatalf("carried migration failed: %v\n%s", err, out)
			}
		})
	}
}

func TestGeneratedZshPromptHookReloadsChangedFile(t *testing.T) {
	bin := shelltest.LookPath(t, "zsh")
	dir := t.TempDir()
	render := func(path string, aliases []domain.Alias) {
		t.Helper()
		cfg := domain.ResolvedConfig{Device: domain.Device{Name: "test", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh}, Aliases: aliases}
		cfg.Revision = cfg.ComputeRevision()
		got, err := Render(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	active := filepath.Join(dir, "aliases.zsh")
	next := filepath.Join(dir, "aliases.next.zsh")
	render(active, []domain.Alias{{Name: "removed", Command: "echo old", Enabled: true}})
	render(next, []domain.Alias{{Name: "added", Command: "echo new", Enabled: true}})
	script := "source " + active + "\nalias unrelated='echo user'\ncp " + next + " " + active + "\n" +
		"_aliasdeck_reload_generated_aliases\n! alias removed >/dev/null 2>&1\nalias added >/dev/null\nalias unrelated >/dev/null\n"
	if out, err := exec.Command(bin, "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("zsh prompt reload failed: %v\n%s", err, out)
	}
}
