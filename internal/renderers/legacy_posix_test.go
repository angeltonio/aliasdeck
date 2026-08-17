package renderers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

func TestMigrateLegacyPOSIXOutputRejectsAnythingButExactGeneratedFormat(t *testing.T) {
	current := renderTestPOSIX(t, domain.ShellZsh, nil)
	valid := readLegacyFixture(t, domain.ShellZsh)
	tests := []struct {
		name     string
		previous string
	}{
		{name: "handwritten aliases", previous: "alias mine='echo mine'\n"},
		{name: "forged partial header", previous: legacyHeaderLine + "\nalias mine='echo mine'\n"},
		{name: "extra executable line", previous: valid + "touch /tmp/not-executed\n"},
		{name: "invalid alias name", previous: strings.Replace(valid, "alias foo-bar=", "alias 'bad name'=", 1)},
		{name: "non-generated quoting", previous: strings.Replace(valid, "alias dot.name='printf dot'", "alias dot.name=\"printf dot\"", 1)},
		{name: "wrong declared count", previous: strings.Replace(valid, "# Aliases:  3", "# Aliases:  4", 1)},
		{name: "unsorted aliases", previous: strings.Replace(valid, "alias _under='printf under'", "alias zed='printf under'", 1)},
		{name: "current output without migration marker", previous: current},
		{name: "handwritten migration marker", previous: legacyMigrationPrefix + quotePOSIX("personal") + legacyMigrationSuffix + managedAliasesGuardLine},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MigrateLegacyPOSIXOutput(current, []byte(tt.previous), domain.ShellZsh); got != current {
				t.Fatalf("untrusted previous output changed the generated file:\n%s", got)
			}
		})
	}
}

func TestMigrateLegacyPOSIXOutputCarriesMigrationAcrossRevisions(t *testing.T) {
	legacy := readLegacyFixture(t, domain.ShellZsh)
	first := MigrateLegacyPOSIXOutput(renderTestPOSIX(t, domain.ShellZsh, nil), []byte(legacy), domain.ShellZsh)
	secondCanonical := renderTestPOSIX(t, domain.ShellZsh, []domain.Alias{{Name: "new-name", Command: "printf new", Enabled: true}})
	if got := MigrateLegacyPOSIXOutput(secondCanonical, []byte(first), domain.ShellZsh); got != secondCanonical {
		t.Fatal("strict legacy migration trusted a current marker without caller-owned hash evidence")
	}
	second := CarryForwardLegacyPOSIXMigration(secondCanonical, []byte(first), domain.ShellZsh)
	for _, name := range []string{"_under", "dot.name", "foo-bar"} {
		if !strings.Contains(second, name) {
			t.Errorf("carried migration missing %q:\n%s", name, second)
		}
	}
	if got := strings.Count(second, legacyMigrationPrefix); got != 1 {
		t.Fatalf("migration block count = %d, want 1", got)
	}
}

func TestMigrateLegacyPOSIXOutputDoesNotAffectPowerShell(t *testing.T) {
	const current = "function Test-Alias { Write-Output ok }\n"
	if got := MigrateLegacyPOSIXOutput(current, []byte(readLegacyFixture(t, domain.ShellZsh)), domain.ShellPowerShell); got != current {
		t.Fatalf("PowerShell output changed: %q", got)
	}
}

func renderTestPOSIX(t *testing.T, shell domain.Shell, aliases []domain.Alias) string {
	t.Helper()
	cfg := domain.ResolvedConfig{
		Device:  domain.Device{Name: "test", Platform: domain.PlatformMacOS, Shell: shell},
		Aliases: aliases,
	}
	cfg.Revision = cfg.ComputeRevision()
	got, err := Render(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func readLegacyFixture(t *testing.T, shell domain.Shell) string {
	t.Helper()
	path := filepath.Join("testdata", "v053_"+shell.String()+"_legacy.golden")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
