package app

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
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
	Executable string
}

// AgentInstall writes and loads a per-user LaunchAgent for foreground watch.
// It never edits shell startup files or attaches watch to an interactive
// terminal.
func AgentInstall(ctx context.Context, env Env, opts AgentOptions) (AgentStatus, error) {
	path, err := agentPath(env)
	if err != nil {
		return AgentStatus{}, err
	}
	if opts.Executable == "" {
		return AgentStatus{}, fmt.Errorf("resolving aliasdeck executable")
	}
	if env.MkdirAll == nil || env.WriteFile == nil || env.Remove == nil || env.Stat == nil {
		return AgentStatus{}, fmt.Errorf("filesystem is not configured")
	}
	if err := env.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return AgentStatus{}, fmt.Errorf("creating LaunchAgents directory: %w", err)
	}
	data := renderAgentPlist(opts.Executable)
	if err := env.WriteFile(path, data, 0o600); err != nil {
		return AgentStatus{}, fmt.Errorf("writing LaunchAgent plist: %w", err)
	}

	// bootout is deliberately best-effort: it makes install idempotent while
	// avoiding duplicate labels, and only affects this watcher service.
	_, _ = runCommand(ctx, env, "launchctl", "bootout", launchDomain(env), agentLabel)
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
	_, err = runCommand(ctx, env, "launchctl", "print", launchDomain(env)+"/"+agentLabel)
	status.Loaded = err == nil
	return status, nil
}

func AgentUninstall(ctx context.Context, env Env) (string, error) {
	path, err := agentPath(env)
	if err != nil {
		return "", err
	}
	_, _ = runCommand(ctx, env, "launchctl", "bootout", launchDomain(env), agentLabel)
	if err := env.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("removing LaunchAgent plist: %w", err)
	}
	return path, nil
}

func agentPath(env Env) (string, error) {
	home, err := env.HomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", agentFile), nil
}

func launchDomain(env Env) string { return fmt.Sprintf("gui/%d", env.UserID()) }

func runCommand(ctx context.Context, env Env, name string, args ...string) ([]byte, error) {
	if env.RunCommand == nil {
		return nil, fmt.Errorf("command runner is not configured")
	}
	return env.RunCommand(ctx, name, args...)
}

func renderAgentPlist(executable string) []byte {
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>watch</string></array>
<key>RunAtLoad</key><true/>
<key>KeepAlive</key><true/>
</dict></plist>
`, xmlEscape(agentLabel), xmlEscape(executable)))
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
