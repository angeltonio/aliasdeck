package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentInstallWritesStablePlistAndLoadsOnlyTheWatcher(t *testing.T) {
	home := t.TempDir()
	var commands [][]string
	env := testAgentEnv(home, func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{name}, args...))
		return nil, nil
	})

	status, err := AgentInstall(context.Background(), env, AgentOptions{Executable: "/Applications/Alias Deck/aliasdeck"})
	if err != nil {
		t.Fatalf("AgentInstall() error = %v", err)
	}
	if !status.Installed || !status.Loaded {
		t.Fatalf("status = %+v, want installed and loaded", status)
	}
	if got, want := status.Path, filepath.Join(home, "Library", "LaunchAgents", agentFile); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	data, err := os.ReadFile(status.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"com.aliasdeck.watch", "<string>/Applications/Alias Deck/aliasdeck</string>", "<string>watch</string>", "<key>RunAtLoad</key><true/>", "<key>KeepAlive</key><true/>"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("plist missing %q:\n%s", want, data)
		}
	}
	if len(commands) != 2 || commands[0][3] != agentLabel || commands[1][1] != "bootstrap" {
		t.Fatalf("launchctl commands = %#v", commands)
	}
}

func TestAgentStatusAndUninstallHandleNotLoadedAgent(t *testing.T) {
	home := t.TempDir()
	env := testAgentEnv(home, func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "print" {
			return nil, errors.New("service not found")
		}
		return nil, nil
	})
	path := filepath.Join(home, "Library", "LaunchAgents", agentFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("plist"), 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := AgentStatusFor(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Installed || status.Loaded {
		t.Fatalf("status = %+v, want installed but not loaded", status)
	}
	if _, err := AgentUninstall(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("plist still exists, stat error = %v", err)
	}
}

func testAgentEnv(home string, runner func(context.Context, string, ...string) ([]byte, error)) Env {
	return Env{
		HomeDir:    func() (string, error) { return home, nil },
		UserID:     func() int { return 501 },
		RunCommand: runner,
		MkdirAll:   os.MkdirAll, WriteFile: os.WriteFile, Remove: os.Remove, Stat: os.Stat,
	}
}
