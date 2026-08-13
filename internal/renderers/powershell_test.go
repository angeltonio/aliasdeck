package renderers

import (
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

func TestQuotePowerShell(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain command needs only wrapping",
			in:   "docker ps",
			want: `'docker ps'`,
		},
		{
			name: "double quotes are literal inside single quotes",
			in:   `echo "hello world"`,
			want: `'echo "hello world"'`,
		},
		{
			name: "single quote is doubled, never backslash-escaped",
			in:   "don't",
			want: `'don''t'`,
		},
		{
			name: "variables are not expanded at generation time",
			in:   "cd $HOME && ls",
			want: `'cd $HOME && ls'`,
		},
		{
			name: "backslashes are literal — PowerShell has no backslash escape",
			in:   `grep -E '\d+'`,
			want: `'grep -E ''\d+'''`,
		},
		{
			name: "empty string still produces a quoted empty literal",
			in:   "",
			want: `''`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quotePowerShell(tt.in); got != tt.want {
				t.Errorf("quotePowerShell(%q)\n got: %s\nwant: %s", tt.in, got, tt.want)
			}
		})
	}
}

// TestQuotePowerShellNeutralizesBreakout mirrors TestQuotePOSIXNeutralizesBreakout
// for the PowerShell escaping mechanism, which is entirely different: doubling,
// never a backslash.
//
// A command crafted to close the single-quoted literal early, or to abuse
// the back-to-back doubling itself, must still end up as one inert string.
func TestQuotePowerShellNeutralizesBreakout(t *testing.T) {
	attacks := []string{
		`x'; Remove-Item -Recurse -Force ~; function y { z`,
		`x' -and (Remove-Item -Recurse -Force ~) -and echo '`,
		`'; Write-Output pwned; '`,
		// Escape-confusion: the payload already contains a doubled quote,
		// trying to desynchronize the doubling that quotePowerShell performs.
		`x'' ; Write-Output pwned ; ''`,
	}

	for _, attack := range attacks {
		quoted := quotePowerShell(attack)

		// Every single quote in the output must be part of a doubled pair,
		// never a lone quote that lets PowerShell resume parsing code.
		// Stripping every doubled pair must leave exactly the two delimiters.
		stripped := strings.ReplaceAll(quoted, "''", "")
		if got := strings.Count(stripped, "'"); got != 2 {
			t.Errorf("quotePowerShell(%q) = %s\nleaves %d unescaped quotes, want exactly 2 delimiters",
				attack, quoted, got)
		}
		if !strings.HasPrefix(quoted, "'") || !strings.HasSuffix(quoted, "'") {
			t.Errorf("quotePowerShell(%q) = %s: not wrapped in single quotes", attack, quoted)
		}
	}
}

// TestRenderArgsForwardedTwice is a byte assertion on the one detail this
// entire renderer exists to get right (design decision 2, §6.3): @args must
// appear both inside the compiled command string and at the scriptblock
// invocation. A candidate rendering that splats only once compiles and looks
// plausible, but silently drops every argument at call time.
func TestRenderArgsForwardedTwice(t *testing.T) {
	cfg := domain.ResolvedConfig{
		Device: domain.Device{Name: "victim", Platform: domain.PlatformWindows, Shell: domain.ShellPowerShell},
		Aliases: []domain.Alias{
			{Name: "dps", Command: "docker ps", Enabled: true},
		},
	}
	cfg.Revision = cfg.ComputeRevision()

	got, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if n := strings.Count(got, "@args"); n != 2 {
		t.Fatalf("Render() contains %d occurrences of @args, want exactly 2:\n%s", n, got)
	}

	want := "function dps {\n" +
		"    $__aliasdeck_cmd = 'docker ps'\n" +
		"    & ([scriptblock]::Create($__aliasdeck_cmd + ' @args')) @args\n" +
		"}\n"
	if !strings.Contains(got, want) {
		t.Errorf("Render() does not contain the expected function block.\n--- got ---\n%s\n--- want substring ---\n%s", got, want)
	}
}

// TestPowerShellRendererShell confirms the registered powershellRenderer
// reports the shell it targets, exactly like TestPosixRendererShell does for
// zsh and bash.
func TestPowerShellRendererShell(t *testing.T) {
	r, err := For(domain.ShellPowerShell)
	if err != nil {
		t.Fatalf("For(ShellPowerShell): %v", err)
	}
	if got := r.Shell(); got != domain.ShellPowerShell {
		t.Errorf("Shell() = %q, want %q", got, domain.ShellPowerShell)
	}
}
