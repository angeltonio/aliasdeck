package validate

import (
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

func TestName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		shell   domain.Shell
		wantErr bool
	}{
		{name: "simple", input: "dps", shell: domain.ShellZsh},
		{name: "underscore start", input: "_private", shell: domain.ShellZsh},
		{name: "hyphen inside", input: "git-cleanup", shell: domain.ShellZsh},
		{name: "dot inside", input: "docker.up", shell: domain.ShellZsh},
		{name: "digits inside", input: "k8s2", shell: domain.ShellZsh},
		{name: "uppercase", input: "DPS", shell: domain.ShellZsh},

		{name: "empty", input: "", shell: domain.ShellZsh, wantErr: true},
		{name: "leading digit", input: "2fast", shell: domain.ShellZsh, wantErr: true},
		{name: "leading hyphen", input: "-flag", shell: domain.ShellZsh, wantErr: true},
		{name: "space", input: "my alias", shell: domain.ShellZsh, wantErr: true},
		{name: "slash", input: "a/b", shell: domain.ShellZsh, wantErr: true},
		{name: "dollar", input: "a$b", shell: domain.ShellZsh, wantErr: true},
		{name: "single quote", input: "a'b", shell: domain.ShellZsh, wantErr: true},
		{name: "semicolon", input: "a;b", shell: domain.ShellZsh, wantErr: true},
		{name: "newline", input: "a\nb", shell: domain.ShellZsh, wantErr: true},
		{name: "equals", input: "a=b", shell: domain.ShellZsh, wantErr: true},
		{name: "non-ascii", input: "café", shell: domain.ShellZsh, wantErr: true},
		{name: "too long", input: strings.Repeat("a", MaxNameLen+1), shell: domain.ShellZsh, wantErr: true},

		{name: "posix keyword if", input: "if", shell: domain.ShellZsh, wantErr: true},
		{name: "posix keyword function", input: "function", shell: domain.ShellBash, wantErr: true},
		{name: "posix keyword is case sensitive", input: "IF", shell: domain.ShellZsh},

		{name: "powershell keyword", input: "param", shell: domain.ShellPowerShell, wantErr: true},
		{name: "powershell keyword is case insensitive", input: "Param", shell: domain.ShellPowerShell, wantErr: true},
		{name: "powershell allows posix keyword nocorrect", input: "nocorrect", shell: domain.ShellPowerShell},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Name(tt.input, tt.shell)
			if (err != nil) != tt.wantErr {
				t.Errorf("Name(%q, %s) error = %v, wantErr %v", tt.input, tt.shell, err, tt.wantErr)
			}
		})
	}
}

func TestCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "simple", input: "docker ps"},
		{name: "pipes and redirects", input: "ps aux | grep -i go > /tmp/out"},
		{name: "quotes", input: `echo "hello" 'world'`},
		{name: "expansion stays allowed", input: "cd $HOME && ls -la"},
		{name: "command substitution", input: "echo $(date)"},
		{name: "tab is allowed", input: "echo\tindented"},

		{name: "empty", input: "", wantErr: true},
		{name: "whitespace only", input: "   ", wantErr: true},
		{name: "newline", input: "echo one\necho two", wantErr: true},
		{name: "carriage return", input: "echo one\recho two", wantErr: true},
		{name: "null byte", input: "echo \x00", wantErr: true},
		{name: "escape character", input: "echo \x1b[31m", wantErr: true},
		{name: "too long", input: strings.Repeat("x", MaxCommandLen+1), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Command(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Command(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestDescription(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty is fine", input: ""},
		{name: "plain text", input: "Start the Compose stack"},
		{name: "hash is fine", input: "Runs #1 most often"},

		{name: "newline escapes the comment", input: "ok\nalias evil='rm -rf ~'", wantErr: true},
		{name: "carriage return", input: "ok\rmore", wantErr: true},
		{name: "too long", input: strings.Repeat("d", MaxDescriptionLen+1), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Description(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Description(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestConfigDetectsDuplicates(t *testing.T) {
	cfg := domain.ResolvedConfig{
		Device: domain.Device{Platform: domain.PlatformMacOS, Shell: domain.ShellZsh},
		Aliases: []domain.Alias{
			{Name: "dps", Command: "docker ps", Enabled: true},
			{Name: "dps", Command: "docker ps -a", Enabled: true},
		},
	}

	issues := Config(cfg)
	if !issues.HasErrors() {
		t.Fatal("duplicate alias names were not reported")
	}

	var found bool
	for _, i := range issues {
		if strings.Contains(i.Message, "duplicate") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a duplicate-name issue, got %v", issues)
	}
}

func TestConfigDuplicateCaseSensitivityFollowsShell(t *testing.T) {
	aliases := []domain.Alias{
		{Name: "dps", Command: "docker ps", Enabled: true},
		{Name: "DPS", Command: "docker ps -a", Enabled: true},
	}

	posix := Config(domain.ResolvedConfig{
		Device:  domain.Device{Platform: domain.PlatformMacOS, Shell: domain.ShellZsh},
		Aliases: aliases,
	})
	if posix.HasErrors() {
		t.Errorf("zsh should allow dps and DPS to coexist, got %v", posix)
	}

	powershell := Config(domain.ResolvedConfig{
		Device:  domain.Device{Platform: domain.PlatformWindows, Shell: domain.ShellPowerShell},
		Aliases: aliases,
	})
	if !powershell.HasErrors() {
		t.Error("powershell resolves names case-insensitively, dps and DPS must collide")
	}
}

// TestFilterValidKeepsTheGoodOnes encodes the product decision that one broken
// alias must not cost the user their other forty.
func TestFilterValidKeepsTheGoodOnes(t *testing.T) {
	cfg := domain.ResolvedConfig{
		Device: domain.Device{Platform: domain.PlatformMacOS, Shell: domain.ShellZsh},
		Aliases: []domain.Alias{
			{Name: "good", Command: "echo ok", Enabled: true},
			{Name: "bad name", Command: "echo nope", Enabled: true},
			{Name: "alsogood", Command: "echo fine", Enabled: true},
			{Name: "empty", Command: "", Enabled: true},
		},
	}

	filtered, issues := FilterValid(cfg)

	if len(filtered.Aliases) != 2 {
		t.Fatalf("kept %d aliases, want 2: %+v", len(filtered.Aliases), filtered.Aliases)
	}
	if filtered.Aliases[0].Name != "good" || filtered.Aliases[1].Name != "alsogood" {
		t.Errorf("wrong aliases survived: %+v", filtered.Aliases)
	}
	if len(issues.Errors()) != 2 {
		t.Errorf("expected 2 blocking issues, got %d: %v", len(issues.Errors()), issues)
	}
}

func TestConfigRejectsUnknownDeviceFields(t *testing.T) {
	cfg := domain.ResolvedConfig{
		Device: domain.Device{Platform: domain.Platform("plan9"), Shell: domain.Shell("rc")},
	}

	if !Config(cfg).HasErrors() {
		t.Error("unknown platform and shell should be reported")
	}
}
