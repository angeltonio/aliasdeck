package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/watchconfig"
)

const (
	agentLabel = "com.aliasdeck.watch"
	agentFile  = "com.aliasdeck.watch.plist"
	taskName   = "AliasDeck Watch"
)

// AgentStatus describes the user-level background watcher without requiring
// callers to know launchctl or schtasks output formats.
type AgentStatus struct {
	Path      string
	Installed bool
	Loaded    bool
}

// AgentOptions controls the executable recorded in the background watcher.
type AgentOptions struct {
	Executable    string
	AliasDeckHome string
	Interval      time.Duration
}

// AgentInstall writes and loads a per-user background supervisor for
// foreground watch.
// It never edits shell startup files or attaches watch to an interactive
// terminal.
func AgentInstall(ctx context.Context, env Env, opts AgentOptions) (AgentStatus, error) {
	return agentInstallForOS(ctx, env, opts, runtime.GOOS)
}

func agentInstallForOS(ctx context.Context, env Env, opts AgentOptions, goos string) (AgentStatus, error) {
	if goos == "windows" {
		return agentInstallTask(ctx, env, opts)
	}
	return agentInstallLaunchAgent(ctx, env, opts)
}

func agentInstallLaunchAgent(ctx context.Context, env Env, opts AgentOptions) (AgentStatus, error) {
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
	return agentStatusForOS(ctx, env, runtime.GOOS)
}

func agentStatusForOS(ctx context.Context, env Env, goos string) (AgentStatus, error) {
	if goos == "windows" {
		return agentTaskStatus(ctx, env)
	}
	return agentLaunchStatus(ctx, env)
}

func agentLaunchStatus(ctx context.Context, env Env) (AgentStatus, error) {
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
	return agentUninstallForOS(ctx, env, runtime.GOOS)
}

func agentUninstallForOS(ctx context.Context, env Env, goos string) (string, error) {
	if goos == "windows" {
		return agentUninstallTask(ctx, env)
	}
	return agentUninstallLaunchAgent(ctx, env)
}

func agentUninstallLaunchAgent(ctx context.Context, env Env) (string, error) {
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
	return agentUninstallIfOwnedForOS(ctx, env, opts, runtime.GOOS)
}

func agentUninstallIfOwnedForOS(ctx context.Context, env Env, opts AgentOptions, goos string) (string, bool, error) {
	if goos == "windows" {
		return agentUninstallTaskIfOwned(ctx, env, opts)
	}
	return agentUninstallLaunchAgentIfOwned(ctx, env, opts)
}

func agentUninstallLaunchAgentIfOwned(ctx context.Context, env Env, opts AgentOptions) (string, bool, error) {
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
	removed, err := agentUninstallLaunchAgent(ctx, env)
	return removed, err == nil, err
}

func agentInstallTask(ctx context.Context, env Env, opts AgentOptions) (AgentStatus, error) {
	opts, err := normalizeAgentOptionsForOS(opts, "windows")
	if err != nil {
		return AgentStatus{}, fmt.Errorf("resolving aliasdeck executable")
	}
	if env.MkdirAll == nil || env.WriteFile == nil || env.Remove == nil {
		return AgentStatus{}, fmt.Errorf("filesystem is not configured")
	}
	xmlPath, err := agentTaskXMLPath(env)
	if err != nil {
		return AgentStatus{}, err
	}
	if err := env.MkdirAll(filepath.Dir(xmlPath), 0o700); err != nil {
		return AgentStatus{}, fmt.Errorf("creating scheduled task staging directory: %w", err)
	}
	if err := env.WriteFile(xmlPath, renderAgentTaskXML(opts), 0o600); err != nil {
		return AgentStatus{}, fmt.Errorf("writing scheduled task XML: %w", err)
	}
	defer func() { _ = env.Remove(xmlPath) }()
	if _, err := runCommand(ctx, env, "schtasks", "/Create", "/TN", taskName, "/XML", xmlPath, "/F"); err != nil {
		return AgentStatus{}, fmt.Errorf("creating scheduled task: %w", err)
	}
	if _, err := runCommand(ctx, env, "schtasks", "/Run", "/TN", taskName); err != nil {
		return AgentStatus{}, fmt.Errorf("starting scheduled task: %w", err)
	}
	return AgentStatus{Path: taskName, Installed: true, Loaded: true}, nil
}

func agentTaskStatus(ctx context.Context, env Env) (AgentStatus, error) {
	status := AgentStatus{Path: taskName}
	if _, err := runCommand(ctx, env, "schtasks", "/Query", "/TN", taskName, "/XML"); err != nil {
		return status, nil
	}
	status.Installed = true
	out, err := runCommand(ctx, env, "schtasks", "/Query", "/TN", taskName, "/FO", "LIST", "/V")
	if err != nil {
		return status, nil
	}
	status.Loaded = taskStatusRunning(out)
	return status, nil
}

func agentUninstallTask(ctx context.Context, env Env) (string, error) {
	_, _ = runCommand(ctx, env, "schtasks", "/End", "/TN", taskName)
	if _, err := runCommand(ctx, env, "schtasks", "/Query", "/TN", taskName, "/XML"); err != nil {
		return taskName, nil
	}
	if _, err := runCommand(ctx, env, "schtasks", "/Delete", "/TN", taskName, "/F"); err != nil {
		return "", fmt.Errorf("removing scheduled task: %w", err)
	}
	return taskName, nil
}

func agentUninstallTaskIfOwned(ctx context.Context, env Env, opts AgentOptions) (string, bool, error) {
	opts, err := normalizeAgentOptionsForOS(opts, "windows")
	if err != nil {
		return "", false, fmt.Errorf("resolving aliasdeck executable")
	}
	data, err := runCommand(ctx, env, "schtasks", "/Query", "/TN", taskName, "/XML")
	if err != nil {
		return taskName, false, nil
	}
	if !taskXMLMatches(data, opts) {
		return taskName, false, nil
	}
	removed, err := agentUninstallTask(ctx, env)
	return removed, err == nil, err
}

func agentTaskXMLPath(env Env) (string, error) {
	base, err := config.Base(env.ConfigEnv())
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "agent-task.xml"), nil
}

func renderAgentTaskXML(opts AgentOptions) []byte {
	command, arguments := agentTaskAction(opts)
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>Keep AliasDeck aliases synchronized.</Description></RegistrationInfo>
  <Triggers><LogonTrigger><Enabled>true</Enabled></LogonTrigger></Triggers>
  <Principals><Principal id="Author"><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
  <Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><ExecutionTimeLimit>PT0S</ExecutionTimeLimit></Settings>
  <Actions Context="Author"><Exec><Command>%s</Command><Arguments>%s</Arguments></Exec></Actions>
</Task>
`, xmlEscape(command), xmlEscape(arguments)))
}

func taskXMLMatches(data []byte, opts AgentOptions) bool {
	command, arguments := agentTaskAction(opts)
	text := string(data)
	return strings.Contains(text, "<Command>"+xmlEscape(command)+"</Command>") &&
		strings.Contains(text, "<Arguments>"+xmlEscape(arguments)+"</Arguments>")
}

func taskStatusRunning(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(line, "status:") && strings.Contains(line, "running") {
			return true
		}
	}
	return false
}

func agentTaskAction(opts AgentOptions) (command, arguments string) {
	if opts.AliasDeckHome == "" {
		return opts.Executable, "watch --interval " + opts.Interval.String()
	}
	script := "$env:ALIASDECK_HOME = '" + psSingleQuote(opts.AliasDeckHome) + "'\n" +
		"& '" + psSingleQuote(opts.Executable) + "' watch --interval '" + opts.Interval.String() + "'"
	return "powershell.exe", "-NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand " + psEncodedCommand(script)
}

func psSingleQuote(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func psEncodedCommand(script string) string {
	encoded := utf16.Encode([]rune(script))
	bytes := make([]byte, 0, len(encoded)*2)
	for _, v := range encoded {
		bytes = append(bytes, byte(v), byte(v>>8))
	}
	return base64.StdEncoding.EncodeToString(bytes)
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
	return normalizeAgentOptionsForOS(opts, runtime.GOOS)
}

func normalizeAgentOptionsForOS(opts AgentOptions, goos string) (AgentOptions, error) {
	if opts.Executable == "" {
		return AgentOptions{}, fmt.Errorf("empty executable")
	}
	if goos == "windows" && isWindowsAbs(opts.Executable) {
		opts.Executable = cleanWindowsPath(opts.Executable)
	} else {
		abs, err := filepath.Abs(opts.Executable)
		if err != nil {
			return AgentOptions{}, err
		}
		opts.Executable = filepath.Clean(abs)
	}
	// Keep a stable package-manager symlink (for example,
	// /opt/homebrew/bin/aliasdeck) in the LaunchAgent. Resolving it to a
	// versioned Caskroom target makes the agent break on the next upgrade.
	if opts.AliasDeckHome != "" {
		if goos == "windows" && isWindowsAbs(opts.AliasDeckHome) {
			opts.AliasDeckHome = cleanWindowsPath(opts.AliasDeckHome)
		} else {
			opts.AliasDeckHome = filepath.Clean(opts.AliasDeckHome)
		}
	}
	if opts.Interval == 0 {
		opts.Interval = watchconfig.DefaultInterval
	}
	if err := watchconfig.Validate(opts.Interval); err != nil {
		return AgentOptions{}, err
	}
	return opts, nil
}

func isWindowsAbs(path string) bool {
	return len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/') ||
		strings.HasPrefix(path, `\\`)
}

func cleanWindowsPath(path string) string {
	return strings.ReplaceAll(path, "/", `\`)
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
