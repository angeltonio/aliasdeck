package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/config"
)

// fixedNow is the deterministic clock used across internal/app tests, so a
// timestamp field never makes an assertion flaky.
var fixedNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// testEnv bundles an Env with the paths and buffers a test needs to seed
// fixtures and inspect output. base is AliasDeck's config directory
// ($ALIASDECK_HOME); home is a fake $HOME that rc-file tests write under.
// Neither ever points at the real machine.
type testEnv struct {
	Env

	Base   string
	Home   string
	vars   map[string]string
	stdout *bytes.Buffer
	stderr *bytes.Buffer
	stdin  *bytes.Buffer
}

// newTestEnv returns a testEnv rooted entirely inside a fresh t.TempDir().
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	root := t.TempDir()
	base := filepath.Join(root, "config")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("creating fake home: %v", err)
	}

	te := &testEnv{
		Base:   base,
		Home:   home,
		vars:   map[string]string{"ALIASDECK_HOME": base},
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
		stdin:  &bytes.Buffer{},
	}

	te.Env = Env{
		Stdin:   te.stdin,
		Stdout:  te.stdout,
		Stderr:  te.stderr,
		Getenv:  func(key string) string { return te.vars[key] },
		HomeDir: func() (string, error) { return te.Home, nil },
		Now:     func() time.Time { return fixedNow },
		LookPath: func(file string) (string, error) {
			return "", fmt.Errorf("%s: executable file not found in $PATH", file)
		},
	}
	return te
}

// setenv sets a fake environment variable this Env's Getenv will report.
func (te *testEnv) setenv(key, value string) { te.vars[key] = value }

// setStdin replaces stdin's content, e.g. to feed a prompt answer.
func (te *testEnv) setStdin(content string) {
	te.stdin.Reset()
	te.stdin.WriteString(content)
}

// writeConfigYAML persists cfg as this test's config.yaml, creating Base if
// needed.
func writeConfigYAML(t *testing.T, base string, cfg config.DeviceFileConfig) {
	t.Helper()
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("creating base dir: %v", err)
	}
	if err := config.Write(config.ConfigFile(base), cfg); err != nil {
		t.Fatalf("writing config.yaml: %v", err)
	}
}

// writeAliasesYAML persists raw YAML content as this test's aliases.yaml.
func writeAliasesYAML(t *testing.T, base, content string) {
	t.Helper()
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("creating base dir: %v", err)
	}
	if err := os.WriteFile(config.AliasesFile(base), []byte(content), 0o600); err != nil {
		t.Fatalf("writing aliases.yaml: %v", err)
	}
}

// nativeDeviceConfig is a minimal, valid config.yaml body: native backend,
// file source, device name set so no fallback-identity generation kicks in
// mid-test.
func nativeDeviceConfig(name string) config.DeviceFileConfig {
	return config.DeviceFileConfig{
		Version: 1,
		Device:  config.DeviceConfig{Name: name, ID: name},
		Source:  config.Source{Type: config.SourceTypeFile},
		Backend: config.BackendNative,
	}
}
