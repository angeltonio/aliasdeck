package app

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/angeltonio/aliasdeck/internal/watchconfig"
)

const (
	agentLabel = "com.aliasdeck.watch"
	agentFile  = "com.aliasdeck.watch.plist"
)

// AgentStatus describes the user-level LaunchAgent without requiring callers
// to know launchctl's output format.
type AgentStatus struct {
	Path      string
	Installed bool
	Loaded    bool
}

// AgentOptions controls the executable recorded in the LaunchAgent plist.
type AgentOptions struct {
	Executable    string
	AliasDeckHome string
	Interval      time.Duration
}

// AgentInstall writes and loads a per-user LaunchAgent for foreground watch.
// It never edits shell startup files or attaches watch to an interactive
// terminal.
func AgentInstall(ctx context.Context, env Env, opts AgentOptions) (AgentStatus, error) {
	path, err := agentPath(env)
	if err != nil {
		return AgentStatus{}, err
	}
	opts, err = normalizeAgentOptions(opts)
	if err != nil {
		return AgentStatus{}, fmt.Errorf("resolving aliasdeck executable")
	}
	if env.MkdirAll == nil || env.WriteFile == nil || env.Remove == nil || env.Stat == nil {
		return AgentStatus{}, fmt.Errorf("filesystem is not configured")
	}
	if err := env.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return AgentStatus{}, fmt.Errorf("creating LaunchAgents directory: %w", err)
	}
	data := renderAgentPlist(opts)
	if err := env.WriteFile(path, data, 0o600); err != nil {
		return AgentStatus{}, fmt.Errorf("writing LaunchAgent plist: %w", err)
	}

	// bootout is deliberately best-effort: it makes install idempotent while
	// avoiding duplicate labels, and only affects this watcher service.
	_, _ = runCommand(ctx, env, "launchctl", "bootout", launchService(env))
	if _, err := runCommand(ctx, env, "launchctl", "bootstrap", launchDomain(env), path); err != nil {
		return AgentStatus{}, fmt.Errorf("loading LaunchAgent: %w", err)
	}
	return AgentStatus{Path: path, Installed: true, Loaded: true}, nil
}

func AgentStatusFor(ctx context.Context, env Env) (AgentStatus, error) {
	path, err := agentPath(env)
	if err != nil {
		return AgentStatus{}, err
	}
	status := AgentStatus{Path: path}
	if _, err := env.Stat(path); err == nil {
		status.Installed = true
	} else if !os.IsNotExist(err) {
		return AgentStatus{}, fmt.Errorf("checking LaunchAgent plist: %w", err)
	}
	if !status.Installed {
		return status, nil
	}
	_, err = runCommand(ctx, env, "launchctl", "print", launchService(env))
	status.Loaded = err == nil
	return status, nil
}

func AgentUninstall(ctx context.Context, env Env) (string, error) {
	path, err := agentPath(env)
	if err != nil {
		return "", err
	}
	_, _ = runCommand(ctx, env, "launchctl", "bootout", launchService(env))
	if err := env.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("removing LaunchAgent plist: %w", err)
	}
	return path, nil
}

// AgentUninstallIfOwned removes the watcher only when its plist exactly
// matches opts. Development reset uses this guard so it cannot remove a
// separately installed AliasDeck watcher, let alone an unrelated service.
func AgentUninstallIfOwned(ctx context.Context, env Env, opts AgentOptions) (string, bool, error) {
	path, err := agentPath(env)
	if err != nil {
		return "", false, err
	}
	opts, err = normalizeAgentOptions(opts)
	if err != nil {
		return "", false, fmt.Errorf("resolving aliasdeck executable")
	}
	if env.ReadFile == nil {
		return "", false, fmt.Errorf("filesystem is not configured")
	}
	data, err := env.ReadFile(path)
	if os.IsNotExist(err) {
		return path, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading LaunchAgent plist: %w", err)
	}
	if !bytes.Equal(data, renderAgentPlist(opts)) {
		return path, false, nil
	}
	removed, err := AgentUninstall(ctx, env)
	return removed, err == nil, err
}

func agentPath(env Env) (string, error) {
	home, err := env.HomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", agentFile), nil
}

func launchDomain(env Env) string { return fmt.Sprintf("gui/%d", env.UserID()) }

func launchService(env Env) string { return launchDomain(env) + "/" + agentLabel }

func runCommand(ctx context.Context, env Env, name string, args ...string) ([]byte, error) {
	if env.RunCommand == nil {
		return nil, fmt.Errorf("command runner is not configured")
	}
	return env.RunCommand(ctx, name, args...)
}

func normalizeAgentOptions(opts AgentOptions) (AgentOptions, error) {
	if opts.Executable == "" {
		return AgentOptions{}, fmt.Errorf("empty executable")
	}
	abs, err := filepath.Abs(opts.Executable)
	if err != nil {
		return AgentOptions{}, err
	}
	// Keep a stable package-manager symlink (for example,
	// /opt/homebrew/bin/aliasdeck) in the LaunchAgent. Resolving it to a
	// versioned Caskroom target makes the agent break on the next upgrade.
	opts.Executable = filepath.Clean(abs)
	if opts.AliasDeckHome != "" {
		opts.AliasDeckHome = filepath.Clean(opts.AliasDeckHome)
	}
	if opts.Interval == 0 {
		opts.Interval = watchconfig.DefaultInterval
	}
	if err := watchconfig.Validate(opts.Interval); err != nil {
		return AgentOptions{}, err
	}
	return opts, nil
}

func renderAgentPlist(opts AgentOptions) []byte {
	environment := ""
	if opts.AliasDeckHome != "" {
		environment = fmt.Sprintf("<key>EnvironmentVariables</key><dict><key>ALIASDECK_HOME</key><string>%s</string></dict>\n", xmlEscape(opts.AliasDeckHome))
	}
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>watch</string><string>--interval</string><string>%s</string></array>
%s<key>ProcessType</key><string>Background</string>
<key>RunAtLoad</key><true/>
<key>KeepAlive</key><true/>
</dict></plist>
`, xmlEscape(agentLabel), xmlEscape(opts.Executable), xmlEscape(opts.Interval.String()), environment))
}

func xmlEscape(value string) string {
	var out []byte
	enc := xml.NewEncoder(&byteBuffer{buf: &out})
	_ = enc.EncodeToken(xml.CharData(value))
	_ = enc.Flush()
	return string(out)
}

type byteBuffer struct{ buf *[]byte }

func (b *byteBuffer) Write(p []byte) (int, error) { *b.buf = append(*b.buf, p...); return len(p), nil }
