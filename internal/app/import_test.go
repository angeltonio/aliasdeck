package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/config"
)

// seedForImport gives a device a config.yaml and an aliases.yaml holding the
// aliases named, and returns the path to a throwaway rc file.
func seedForImport(t *testing.T, te *testEnv, existing string) string {
	t.Helper()
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")

	if _, err := Init(context.Background(), te.Env, InitOptions{NoBootstrap: true}); err != nil {
		t.Fatalf("seeding device: %v", err)
	}
	if err := os.WriteFile(filepath.Join(te.Base, "aliases.yaml"), []byte(existing), 0o644); err != nil {
		t.Fatalf("seeding aliases.yaml: %v", err)
	}

	rc := filepath.Join(te.Home, ".zshrc")
	return rc
}

func writeRC(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing rc file: %v", err)
	}
}

func readAliases(t *testing.T, te *testEnv) config.AliasesDocument {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(te.Base, "aliases.yaml"))
	if err != nil {
		t.Fatalf("reading aliases.yaml: %v", err)
	}
	doc, err := config.ParseAliases(data)
	if err != nil {
		t.Fatalf("aliases.yaml is not parseable after import: %v\n%s", err, data)
	}
	return doc
}

// TestImportWithoutWriteChangesNothing is the default, and it is the default
// because the input is a file of shell written over years and the output is
// the file holding every alias the user owns. A first run must be able to
// tell you what is about to happen.
func TestImportWithoutWriteChangesNothing(t *testing.T) {
	te := newTestEnv(t)
	rc := seedForImport(t, te, "version: 1\naliases: []\n")
	writeRC(t, rc, "alias gs='git status'\nalias gp='git push'\n")

	before, err := os.ReadFile(filepath.Join(te.Base, "aliases.yaml"))
	if err != nil {
		t.Fatalf("reading aliases.yaml: %v", err)
	}

	report, err := Import(context.Background(), te.Env, ImportOptions{From: rc})
	if err != nil {
		t.Fatalf("Import() returned an error: %v", err)
	}

	if len(report.New) != 2 {
		t.Errorf("found %d aliases to import, want 2: %+v", len(report.New), report.New)
	}
	if report.Written {
		t.Error("Written is true without --write")
	}

	after, _ := os.ReadFile(filepath.Join(te.Base, "aliases.yaml"))
	if string(after) != string(before) {
		t.Errorf("aliases.yaml changed without --write:\n%s", after)
	}
}

func TestImportWriteMergesIntoAliasesYAML(t *testing.T) {
	te := newTestEnv(t)
	rc := seedForImport(t, te, "version: 1\naliases:\n  - name: existing\n    command: echo existing\n")
	writeRC(t, rc, "alias gs='git status'\n")

	report, err := Import(context.Background(), te.Env, ImportOptions{From: rc, Write: true})
	if err != nil {
		t.Fatalf("Import() returned an error: %v", err)
	}
	if !report.Written {
		t.Fatal("Written is false after --write")
	}

	doc := readAliases(t, te)
	names := map[string]string{}
	for _, a := range doc.Aliases {
		names[a.Name] = a.Command
	}
	if names["existing"] != "echo existing" {
		t.Errorf("the alias that was already there did not survive: %+v", doc.Aliases)
	}
	if names["gs"] != "git status" {
		t.Errorf("gs was not imported: %+v", doc.Aliases)
	}
	if !doc.Aliases[len(doc.Aliases)-1].Enabled {
		t.Error("an imported alias arrived disabled")
	}
}

// TestImportNeverOverwritesAnExistingAlias is the property that decides
// whether this command is safe to run twice. The user's aliases.yaml is
// theirs; a name collision is a decision for them, not a silent replace.
func TestImportNeverOverwritesAnExistingAlias(t *testing.T) {
	te := newTestEnv(t)
	rc := seedForImport(t, te, "version: 1\naliases:\n  - name: gp\n    command: git push\n")
	writeRC(t, rc, "alias gp='git push --force-with-lease'\n")

	report, err := Import(context.Background(), te.Env, ImportOptions{From: rc, Write: true})
	if err != nil {
		t.Fatalf("Import() returned an error: %v", err)
	}

	if len(report.Conflicts) != 1 || report.Conflicts[0].Name != "gp" {
		t.Fatalf("conflicts = %+v, want one naming gp", report.Conflicts)
	}
	if len(report.New) != 0 {
		t.Errorf("imported %+v despite the conflict", report.New)
	}

	doc := readAliases(t, te)
	if len(doc.Aliases) != 1 || doc.Aliases[0].Command != "git push" {
		t.Fatalf("the existing definition was not preserved: %+v", doc.Aliases)
	}
}

// TestImportIsIdempotent covers the second run. An import that re-adds what
// it already added produces a file with duplicate names, which is worse than
// a file it refused to touch.
func TestImportIsIdempotent(t *testing.T) {
	te := newTestEnv(t)
	rc := seedForImport(t, te, "version: 1\naliases: []\n")
	writeRC(t, rc, "alias gs='git status'\nalias ll='ls -la'\n")

	opts := ImportOptions{From: rc, Write: true}
	if _, err := Import(context.Background(), te.Env, opts); err != nil {
		t.Fatalf("first Import(): %v", err)
	}
	first := readAliases(t, te)

	second, err := Import(context.Background(), te.Env, opts)
	if err != nil {
		t.Fatalf("second Import(): %v", err)
	}

	if len(second.New) != 0 {
		t.Errorf("the second run wanted to import %+v again", second.New)
	}
	if len(second.Unchanged) != 2 {
		t.Errorf("Unchanged = %v, want both names reported as already present", second.Unchanged)
	}
	if got := readAliases(t, te); len(got.Aliases) != len(first.Aliases) {
		t.Errorf("aliases grew from %d to %d on a re-run", len(first.Aliases), len(got.Aliases))
	}
}

// TestImportRefusesAnAliasItCannotValidate proves the import cannot put
// something in aliases.yaml that the rest of the product would reject. The
// file has to stay loadable by `sync`, which is the only reason it exists.
func TestImportRefusesAnAliasItCannotValidate(t *testing.T) {
	te := newTestEnv(t)
	rc := seedForImport(t, te, "version: 1\naliases: []\n")
	// A name with a slash is not a shell identifier, and an empty command
	// is not a command.
	writeRC(t, rc, "alias bad/name='echo hi'\nalias empty=''\nalias good='echo ok'\n")

	report, err := Import(context.Background(), te.Env, ImportOptions{From: rc, Write: true})
	if err != nil {
		t.Fatalf("Import() returned an error: %v", err)
	}

	if len(report.New) != 1 || report.New[0].Name != "good" {
		t.Fatalf("imported %+v, want only the valid one", report.New)
	}
	if len(report.Skipped) != 2 {
		t.Fatalf("skipped %d lines, want the two invalid ones: %+v", len(report.Skipped), report.Skipped)
	}
	for _, s := range report.Skipped {
		if s.Reason == "" {
			t.Errorf("an alias was skipped with no reason: %+v", s)
		}
	}
	// The written file must still round-trip; readAliases fails the test
	// otherwise.
	readAliases(t, te)
}

// TestImportTakesTheLastDefinitionOfARepeatedName matches the shell. A name
// defined twice in one file is not ambiguous — the shell keeps the last one,
// so that is what the user actually has.
func TestImportTakesTheLastDefinitionOfARepeatedName(t *testing.T) {
	te := newTestEnv(t)
	rc := seedForImport(t, te, "version: 1\naliases: []\n")
	writeRC(t, rc, "alias g='git'\nalias g='git --no-pager'\n")

	report, err := Import(context.Background(), te.Env, ImportOptions{From: rc, Write: true})
	if err != nil {
		t.Fatalf("Import() returned an error: %v", err)
	}
	if len(report.New) != 1 || report.New[0].Command != "git --no-pager" {
		t.Fatalf("imported %+v, want only the later definition", report.New)
	}
	if len(report.Skipped) != 1 || !strings.Contains(report.Skipped[0].Reason, "redefined") {
		t.Errorf("the shadowed definition was dropped without saying so: %+v", report.Skipped)
	}
}

// TestImportSkipsAliasDecksOwnGeneratedBlock is the loop this command could
// most easily fall into: a user who ran `aliasdeck init` has a block in their
// rc file, and re-importing it would feed AliasDeck's output back into its
// input on every run.
func TestImportSkipsAliasDecksOwnGeneratedBlock(t *testing.T) {
	te := newTestEnv(t)
	rc := seedForImport(t, te, "version: 1\naliases: []\n")
	writeRC(t, rc, strings.Join([]string{
		"alias mine='echo mine'",
		"# >>> aliasdeck >>>",
		"alias fromdeck='echo generated'",
		"# <<< aliasdeck <<<",
		"",
	}, "\n"))

	report, err := Import(context.Background(), te.Env, ImportOptions{From: rc})
	if err != nil {
		t.Fatalf("Import() returned an error: %v", err)
	}
	for _, a := range report.New {
		if a.Name == "fromdeck" {
			t.Fatalf("imported an alias out of AliasDeck's own block: %+v", report.New)
		}
	}
	if len(report.New) != 1 {
		t.Fatalf("imported %+v, want only the user's own alias", report.New)
	}
}

// TestImportFailsUnderServerSourceAndTouchesNoFile mirrors Edit's refusal.
// A server-backed device has no local aliases.yaml that anything reads, so
// writing one would leave a file that looks authoritative and is inert —
// the user would edit it for a week wondering why nothing changed.
func TestImportFailsUnderServerSourceAndTouchesNoFile(t *testing.T) {
	te := newTestEnv(t)
	cfg := nativeDeviceConfig("server-device")
	cfg.Source = config.Source{Type: config.SourceTypeServer, URL: "https://aliases.example.com"}
	writeConfigYAML(t, te.Base, cfg)
	te.setenv("ALIASDECK_PLATFORM", "macos")
	te.setenv("ALIASDECK_SHELL", "zsh")
	seedCredentials(t, te.Base, config.Credentials{DeviceToken: "adt_lookup.secret"})

	rc := filepath.Join(te.Home, ".zshrc")
	writeRC(t, rc, "alias gs='git status'\n")

	_, err := Import(context.Background(), te.Env, ImportOptions{From: rc, Write: true})
	if err != ErrImportUnderServerSource {
		t.Fatalf("Import() error = %v, want ErrImportUnderServerSource", err)
	}
	if !strings.Contains(err.Error(), "server") {
		t.Errorf("error %q does not point at the server as the place to manage aliases", err)
	}
	if _, statErr := os.Stat(filepath.Join(te.Base, "aliases.yaml")); statErr == nil {
		t.Error("Import() created a local aliases.yaml under a server source")
	}
}

// TestImportDefaultsToThisDevicesRCFile covers the no-flag path: `aliasdeck
// import` with nothing else should already be pointed at the right file.
func TestImportDefaultsToThisDevicesRCFile(t *testing.T) {
	te := newTestEnv(t)
	rc := seedForImport(t, te, "version: 1\naliases: []\n")
	writeRC(t, rc, "alias gs='git status'\n")

	report, err := Import(context.Background(), te.Env, ImportOptions{})
	if err != nil {
		t.Fatalf("Import() returned an error: %v", err)
	}
	if report.SourcePath != rc {
		t.Errorf("SourcePath = %q, want this device's rc file %q", report.SourcePath, rc)
	}
	if len(report.New) != 1 {
		t.Errorf("found %+v, want the one alias in the default rc file", report.New)
	}
}

// TestImportExpandsATildeInFrom matters because a quoted --from reaches the
// process with the tilde intact, and `--rc-file` already expands it. Two
// flags naming a path in the same tool should not disagree about what ~ is.
func TestImportExpandsATildeInFrom(t *testing.T) {
	te := newTestEnv(t)
	rc := seedForImport(t, te, "version: 1\naliases: []\n")
	writeRC(t, rc, "alias gs='git status'\n")

	report, err := Import(context.Background(), te.Env, ImportOptions{From: "~/.zshrc"})
	if err != nil {
		t.Fatalf("Import() returned an error: %v", err)
	}
	if report.SourcePath != rc {
		t.Errorf("SourcePath = %q, want the expanded %q", report.SourcePath, rc)
	}
	if len(report.New) != 1 {
		t.Errorf("found %+v, want the alias in the tilde-named file", report.New)
	}
}

// TestImportRefusesPowerShellWithoutAnExplicitFile keeps the failure honest:
// this parser reads POSIX startup files, and a PowerShell profile defines
// aliases with Set-Alias. Reporting "0 aliases found" would be a lie about a
// profile that has twenty.
func TestImportRefusesPowerShellWithoutAnExplicitFile(t *testing.T) {
	te := newTestEnv(t)
	te.setenv("ALIASDECK_PLATFORM", "windows")
	te.setenv("ALIASDECK_SHELL", "powershell")
	if _, err := Init(context.Background(), te.Env, InitOptions{NoBootstrap: true}); err != nil {
		t.Fatalf("seeding device: %v", err)
	}

	if _, err := Import(context.Background(), te.Env, ImportOptions{}); err != ErrImportShellUnsupported {
		t.Fatalf("Import() error = %v, want ErrImportShellUnsupported", err)
	}
}
