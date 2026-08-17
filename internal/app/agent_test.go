package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentInstallWritesStablePlistAndLoadsOnlyTheWatcher(t *testing.T) {
	home := t.TempDir()
	var commands [][]string
	env := testAgentEnv(home, func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{name}, args...))
		return nil, nil
	})

	status, err := AgentInstall(context.Background(), env, AgentOptions{
		Executable:    "/Applications/Alias Deck/aliasdeck",
		AliasDeckHome: filepath.Join(home, "aliasdeck-dev"),
		Interval:      5 * time.Second,
	})
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
	for _, want := range []string{"com.aliasdeck.watch", "<string>/Applications/Alias Deck/aliasdeck</string>", "<string>watch</string><string>--interval</string><string>5s</string>", "<key>ALIASDECK_HOME</key><string>" + filepath.Join(home, "aliasdeck-dev") + "</string>", "<key>ProcessType</key><string>Background</string>", "<key>RunAtLoad</key><true/>", "<key>KeepAlive</key><true/>"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("plist missing %q:\n%s", want, data)
		}
	}
	if len(commands) != 2 || commands[0][2] != "gui/501/"+agentLabel || commands[1][1] != "bootstrap" {
		t.Fatalf("launchctl commands = %#v", commands)
	}
}

func TestAgentInstallDefaultsToThirtySecondsAndRejectsUnsafeInterval(t *testing.T) {
	home := t.TempDir()
	env := testAgentEnv(home, func(context.Context, string, ...string) ([]byte, error) { return nil, nil })
	if _, err := AgentInstall(context.Background(), env, AgentOptions{Executable: "/usr/local/bin/aliasdeck"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, "Library", "LaunchAgents", agentFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<string>watch</string><string>--interval</string><string>30s</string>") {
		t.Fatalf("default plist does not persist 30s: %s", data)
	}
	if _, err := AgentInstall(context.Background(), env, AgentOptions{Executable: "/usr/local/bin/aliasdeck", Interval: 500 * time.Millisecond}); err == nil {
		t.Fatal("AgentInstall accepted an interval below the safety minimum")
	}
}

func TestAgentInstallSurfacesBootstrapFailure(t *testing.T) {
	home := t.TempDir()
	var commands [][]string
	env := testAgentEnv(home, func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{name}, args...))
		if len(args) > 0 && args[0] == "bootstrap" {
			return nil, errors.New("bootstrap EIO")
		}
		return nil, nil
	})
	if _, err := AgentInstall(context.Background(), env, AgentOptions{Executable: "/usr/local/bin/aliasdeck"}); err == nil {
		t.Fatal("AgentInstall hid a bootstrap failure")
	}
	if len(commands) != 2 || commands[0][2] != "gui/501/"+agentLabel {
		t.Fatalf("launchctl commands = %#v, want exact-service bootout then bootstrap", commands)
	}
}

func TestAgentUninstallIfOwnedPreservesDifferentInstallation(t *testing.T) {
	home := t.TempDir()
	var commands [][]string
	env := testAgentEnv(home, func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{name}, args...))
		return nil, nil
	})
	path := filepath.Join(home, "Library", "LaunchAgents", agentFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	production := AgentOptions{Executable: "/usr/local/bin/aliasdeck"}
	if err := os.WriteFile(path, renderAgentPlist(production), 0o600); err != nil {
		t.Fatal(err)
	}

	_, removed, err := AgentUninstallIfOwned(context.Background(), env, AgentOptions{
		Executable:    filepath.Join(home, "dev", "aliasdeck"),
		AliasDeckHome: filepath.Join(home, "dev", "state"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("AgentUninstallIfOwned removed a different installation")
	}
	if len(commands) != 0 {
		t.Fatalf("launchctl commands = %#v, want none", commands)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("different installation plist was touched: %v", err)
	}
}

func TestAgentUninstallIfOwnedRemovesMatchingDevelopmentAgent(t *testing.T) {
	home := t.TempDir()
	var commands [][]string
	env := testAgentEnv(home, func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{name}, args...))
		return nil, nil
	})
	opts := AgentOptions{Executable: filepath.Join(home, "dev", "aliasdeck"), AliasDeckHome: filepath.Join(home, "dev", "state")}
	normalized, err := normalizeAgentOptions(opts)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "Library", "LaunchAgents", agentFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, renderAgentPlist(normalized), 0o600); err != nil {
		t.Fatal(err)
	}

	_, removed, err := AgentUninstallIfOwned(context.Background(), env, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("matching development agent was not removed")
	}
	if len(commands) != 1 || commands[0][1] != "bootout" {
		t.Fatalf("launchctl commands = %#v, want one bootout", commands)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("matching plist still exists: %v", err)
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
		MkdirAll:   os.MkdirAll, WriteFile: os.WriteFile, ReadFile: os.ReadFile, Remove: os.Remove, Stat: os.Stat,
	}
}
