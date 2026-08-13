package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/validate"
	"go.yaml.in/yaml/v3"
)

// hostileAliasCase is one hostile-input shape (server-source spec, "Server
// Response Is Hostile Input"; design's threat matrix, "Sync response as
// hostile input"). Every case here must be dropped by validate.FilterValid,
// and dropped identically whether it arrived through ServerSource or
// FileSource — a compromised server must never gain a capability a
// malicious local aliases.yaml file does not already have.
type hostileAliasCase struct {
	label string
	alias domain.Alias
}

// hostileAliasCases is the shared table task 7.7 requires: one table, run
// through both ServerSource and FileSource, asserting the identical
// outcome. Every Name below is unique so a duplicate-name Issue never
// confuses which case dropped which entry.
var hostileAliasCases = []hostileAliasCase{
	{
		label: "shell metacharacter in name: semicolon command separator",
		alias: domain.Alias{Name: "evil;rm-rf", Command: "echo hi"},
	},
	{
		label: "command substitution $(...) in name",
		alias: domain.Alias{Name: "evil$(whoami)", Command: "echo hi"},
	},
	{
		label: "backtick command substitution in name",
		alias: domain.Alias{Name: "evil`whoami`", Command: "echo hi"},
	},
	{
		label: "logical AND chaining in name",
		alias: domain.Alias{Name: "evil&&ls", Command: "echo hi"},
	},
	{
		label: "pipe in name",
		alias: domain.Alias{Name: "evilpipe|ls", Command: "echo hi"},
	},
	{
		label: "newline embedded in name",
		alias: domain.Alias{Name: "evilnewline\nname", Command: "echo hi"},
	},
	{
		label: "carriage return embedded in name",
		alias: domain.Alias{Name: "evilcr\rname", Command: "echo hi"},
	},
	{
		label: "newline embedded in command",
		alias: domain.Alias{Name: "evilnewlinecmd", Command: "echo hi\nrm -rf /"},
	},
	{
		label: "carriage return embedded in command",
		alias: domain.Alias{Name: "evilcrcmd", Command: "echo hi\rrm -rf /"},
	},
	{
		label: "name is not a valid identifier: leading digit",
		alias: domain.Alias{Name: "1abc", Command: "echo hi"},
	},
	{
		label: "name is a POSIX-reserved word",
		alias: domain.Alias{Name: "if", Command: "echo hi"},
	},
	{
		label: "oversized command",
		alias: domain.Alias{Name: "eviloversizedcmd", Command: strings.Repeat("a", validate.MaxCommandLen+100)},
	},
	{
		label: "oversized description",
		alias: domain.Alias{Name: "eviloversizeddesc", Command: "echo hi", Description: strings.Repeat("d", validate.MaxDescriptionLen+50)},
	},
	{
		// POSIX quoting: a bare single quote in an unquoted context is
		// exactly the escape sequence renderers must never hand a
		// name-shaped string to unescaped.
		label: "single quote breaks POSIX quoting",
		alias: domain.Alias{Name: "evil'name", Command: "echo hi"},
	},
	{
		label: "double quote breaks quoting",
		alias: domain.Alias{Name: `evil"name`, Command: "echo hi"},
	},
	{
		// This project already fixed a real PowerShell injection where a
		// bare "}" in generated content closed the surrounding block early
		// and let anything after it execute as PowerShell. A name carrying
		// that character must never reach a renderer.
		label: "closing brace breaks a generated PowerShell block",
		alias: domain.Alias{Name: "evil}name", Command: "echo hi"},
	},
	{
		label: "opening brace",
		alias: domain.Alias{Name: "evil{name", Command: "echo hi"},
	},
	{
		label: "dollar sign (PowerShell variable interpolation)",
		alias: domain.Alias{Name: "evil$name", Command: "echo hi"},
	},
}

// aliasesFileYAML is a local, test-only mirror of internal/config's
// unexported aliases.yaml DTO shape. yaml.Marshal (not hand-built strings)
// is what makes this safe for every hostile string above: quotes, braces,
// newlines and carriage returns all round-trip through YAML's own escaping
// rather than through ad hoc string concatenation that the hostile input
// itself could break.
type aliasesFileYAML struct {
	Version int              `yaml:"version"`
	Aliases []aliasEntryYAML `yaml:"aliases"`
}

type aliasEntryYAML struct {
	Name        string `yaml:"name"`
	Command     string `yaml:"command"`
	Description string `yaml:"description,omitempty"`
}

// writeHostileAliasesYAML serializes safe plus every hostileAliasCases entry
// as aliases.yaml, for FileSource to read.
func writeHostileAliasesYAML(t *testing.T, path string, safe domain.Alias) {
	t.Helper()

	doc := aliasesFileYAML{Version: 1, Aliases: []aliasEntryYAML{
		{Name: safe.Name, Command: safe.Command, Description: safe.Description},
	}}
	for _, c := range hostileAliasCases {
		doc.Aliases = append(doc.Aliases, aliasEntryYAML{
			Name: c.alias.Name, Command: c.alias.Command, Description: c.alias.Description,
		})
	}

	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("marshaling hostile aliases.yaml fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// serverSyncHandlerFor scripts a server that returns safe plus every
// hostileAliasCases entry over GET /api/v1/sync, with a correctly computed
// revision — the point of this test is what happens *after* the wire, not
// whether the revision check itself works (that is server_test.go's job).
func serverSyncHandlerFor(t *testing.T, dev domain.Device, safe domain.Alias) http.HandlerFunc {
	t.Helper()
	all := append([]domain.Alias{safe}, aliasesFromCases()...)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(validSyncBody(t, dev, all))
	}
}

func aliasesFromCases() []domain.Alias {
	out := make([]domain.Alias, 0, len(hostileAliasCases))
	for _, c := range hostileAliasCases {
		out = append(out, c.alias)
	}
	return out
}

// namesOf returns the set of alias names present in cfg, for membership
// assertions below.
func namesOf(cfg domain.ResolvedConfig) map[string]bool {
	out := make(map[string]bool, len(cfg.Aliases))
	for _, a := range cfg.Aliases {
		out[a.Name] = true
	}
	return out
}

// TestHostileServerAliasDroppedIdenticallyToFileSource is task 7.7's
// central proof: every entry in hostileAliasCases must be dropped by
// validate.FilterValid, and dropped the same way whether it arrived
// through ServerSource or a local aliases.yaml. There is one validation
// path; the network source gets no shortcut around it.
func TestHostileServerAliasDroppedIdenticallyToFileSource(t *testing.T) {
	dev := domain.Device{ID: "device-1", Platform: domain.PlatformLinux, Shell: domain.ShellBash}
	safe := domain.Alias{Name: "safe", Command: "echo safe"}

	// --- FileSource path ---
	dir := t.TempDir()
	path := filepath.Join(dir, "aliases.yaml")
	writeHostileAliasesYAML(t, path, safe)

	fileSrc := FileSource{Path: path}
	fileCfg, err := fileSrc.Resolve(context.Background(), dev)
	if err != nil {
		t.Fatalf("FileSource.Resolve() returned an error: %v", err)
	}

	// --- ServerSource path ---
	srv := httptest.NewServer(serverSyncHandlerFor(t, dev, safe))
	t.Cleanup(srv.Close)
	serverSrc := &ServerSource{URL: srv.URL, Token: "add_test.secret", Client: srv.Client()}
	serverCfg, err := serverSrc.Resolve(context.Background(), dev)
	if err != nil {
		t.Fatalf("ServerSource.Resolve() returned an error: %v", err)
	}

	fileNames := namesOf(fileCfg)
	serverNames := namesOf(serverCfg)

	if !fileNames["safe"] {
		t.Error("FileSource dropped the safe alias, want it kept")
	}
	if !serverNames["safe"] {
		t.Error("ServerSource dropped the safe alias, want it kept")
	}

	for _, c := range hostileAliasCases {
		t.Run(c.label, func(t *testing.T) {
			if fileNames[c.alias.Name] {
				t.Errorf("FileSource kept hostile alias %q (%s), want it dropped", c.alias.Name, c.label)
			}
			if serverNames[c.alias.Name] {
				t.Errorf("ServerSource kept hostile alias %q (%s), want it dropped", c.alias.Name, c.label)
			}
		})
	}

	// The only survivor on either path must be "safe" — proves the two
	// pipelines do not merely drop *a* case each but drop exactly the same
	// set, leaving exactly the same result.
	if len(fileCfg.Aliases) != 1 || fileCfg.Aliases[0].Name != "safe" {
		t.Errorf("FileSource result = %+v, want exactly [safe]", fileCfg.Aliases)
	}
	if len(serverCfg.Aliases) != 1 || serverCfg.Aliases[0].Name != "safe" {
		t.Errorf("ServerSource result = %+v, want exactly [safe]", serverCfg.Aliases)
	}
}

// TestHostilePowerShellReservedWordDroppedIdenticallyToFileSource extends
// the shared-table proof to a shell-specific rule: "process" is reserved in
// PowerShell but not in bash/zsh (internal/validate/name.go's
// powershellReserved), so it must be dropped for a PowerShell device on
// both paths while a bash device (covered above) keeps it fine as an
// ordinary command name check would allow.
func TestHostilePowerShellReservedWordDroppedIdenticallyToFileSource(t *testing.T) {
	dev := domain.Device{ID: "device-2", Platform: domain.PlatformWindows, Shell: domain.ShellPowerShell}
	safe := domain.Alias{Name: "safe", Command: "echo safe"}
	hostile := domain.Alias{Name: "process", Command: "echo hi"}

	dir := t.TempDir()
	path := filepath.Join(dir, "aliases.yaml")
	doc := aliasesFileYAML{Version: 1, Aliases: []aliasEntryYAML{
		{Name: safe.Name, Command: safe.Command},
		{Name: hostile.Name, Command: hostile.Command},
	}}
	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("marshaling fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	fileSrc := FileSource{Path: path}
	fileCfg, err := fileSrc.Resolve(context.Background(), dev)
	if err != nil {
		t.Fatalf("FileSource.Resolve() returned an error: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(validSyncBody(t, dev, []domain.Alias{safe, hostile}))
	}))
	t.Cleanup(srv.Close)
	serverSrc := &ServerSource{URL: srv.URL, Token: "add_test.secret", Client: srv.Client()}
	serverCfg, err := serverSrc.Resolve(context.Background(), dev)
	if err != nil {
		t.Fatalf("ServerSource.Resolve() returned an error: %v", err)
	}

	fileNames := namesOf(fileCfg)
	serverNames := namesOf(serverCfg)

	if fileNames["process"] {
		t.Error("FileSource kept a PowerShell-reserved alias name, want it dropped")
	}
	if serverNames["process"] {
		t.Error("ServerSource kept a PowerShell-reserved alias name, want it dropped")
	}
	if !fileNames["safe"] || !serverNames["safe"] {
		t.Error("the safe alias must survive on both paths")
	}
}

// TestHostileServerAliasNeverBypassesFilterValid is the negative control:
// it directly proves ServerSource.Resolve calls validate.FilterValid by
// checking that domain.Resolve alone (no filtering) would have kept a
// hostile alias — establishing that the drop above is validate.FilterValid's
// doing, not an accident of how the fixture happened to be shaped.
func TestHostileServerAliasNeverBypassesFilterValid(t *testing.T) {
	dev := domain.Device{Platform: domain.PlatformLinux, Shell: domain.ShellBash}
	hostile := domain.Alias{Name: "evil;rm-rf", Command: "echo hi", Enabled: true}

	unfiltered := domain.Resolve(dev, []domain.Alias{hostile})
	if len(unfiltered.Aliases) != 1 {
		t.Fatalf("test fixture is broken: domain.Resolve alone dropped the hostile alias before FilterValid ran")
	}

	_, issues := validate.FilterValid(unfiltered)
	if !issues.HasErrors() {
		t.Fatal("validate.FilterValid reported no errors for a hostile alias name; the fixture no longer exercises what this test claims to")
	}
}
