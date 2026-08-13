package renderers

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

var update = flag.Bool("update", false, "rewrite golden files instead of comparing against them")

// TestGolden pins the exact bytes of every generated file.
//
// Golden files are the right tool here because the output is a contract with
// the user's shell: an accidental change to spacing or quoting is invisible in
// a unit assertion but very visible in a diff. Regenerate deliberately with
// `go test ./internal/renderers -update` and read the diff before committing.
func TestGolden(t *testing.T) {
	cases := []struct {
		name  string
		shell domain.Shell
		cfg   func() domain.ResolvedConfig
	}{
		{
			name:  "zsh_basic",
			shell: domain.ShellZsh,
			cfg:   fixture,
		},
		{
			name:  "bash_basic",
			shell: domain.ShellBash,
			cfg:   fixture,
		},
		{
			name:  "zsh_empty",
			shell: domain.ShellZsh,
			cfg: func() domain.ResolvedConfig {
				cfg := fixture()
				cfg.Aliases = nil
				cfg.Revision = cfg.ComputeRevision()
				return cfg
			},
		},
		{
			name:  "zsh_awkward_commands",
			shell: domain.ShellZsh,
			cfg: func() domain.ResolvedConfig {
				cfg := fixture()
				cfg.Aliases = []domain.Alias{
					{
						Name:        "gwip",
						Command:     `git commit -m 'wip: don't ask'`,
						Description: "Nested and unbalanced quotes",
						Enabled:     true,
					},
					{
						Name:    "ports",
						Command: `lsof -nP -iTCP -sTCP:LISTEN | awk '{print $1, $9}'`,
						Enabled: true,
					},
					{
						Name:        "reload",
						Command:     `source "$HOME/.zshrc" && echo $(date)`,
						Description: "Expansion must stay literal in the file",
						Enabled:     true,
					},
				}
				cfg.Revision = cfg.ComputeRevision()
				return cfg
			},
		},
		{
			name:  "powershell_basic",
			shell: domain.ShellPowerShell,
			cfg:   fixture,
		},
		{
			name:  "powershell_empty",
			shell: domain.ShellPowerShell,
			cfg: func() domain.ResolvedConfig {
				cfg := fixture()
				cfg.Aliases = nil
				cfg.Revision = cfg.ComputeRevision()
				return cfg
			},
		},
		{
			name:  "powershell_awkward_commands",
			shell: domain.ShellPowerShell,
			cfg: func() domain.ResolvedConfig {
				cfg := fixture()
				cfg.Aliases = []domain.Alias{
					{
						Name:        "gwip",
						Command:     `git commit -m 'wip: don't ask'`,
						Description: "Nested and unbalanced quotes",
						Enabled:     true,
					},
					{
						Name:    "brace",
						Command: `Get-Process | Where-Object { $_.CPU -gt 10 }`,
						Enabled: true,
					},
					{
						Name:        "reload",
						Command:     "& \"$HOME/reload.ps1\" `-Force",
						Description: "Backtick and expansion must stay literal in the file",
						Enabled:     true,
					},
				}
				cfg.Revision = cfg.ComputeRevision()
				return cfg
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg()
			cfg.Device.Shell = tc.shell
			cfg.Revision = cfg.ComputeRevision()

			got, err := Render(cfg)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}

			path := filepath.Join("testdata", tc.name+".golden")

			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatalf("creating testdata: %v", err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatalf("writing golden: %v", err)
				}
				t.Logf("updated %s", path)
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading golden (run with -update to create it): %v", err)
			}

			if got != string(want) {
				t.Errorf("output does not match %s\n--- got ---\n%s\n--- want ---\n%s",
					path, got, string(want))
			}
		})
	}
}
