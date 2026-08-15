package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/angeltonio/aliasdeck/internal/apply"
	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/state"
)

// defaultAliasesYAML is what `init` writes when it creates a fresh
// aliases.yaml: a valid, empty document rather than a pre-populated
// example, so a new user never has to explain (or delete) an alias they
// never asked for.
const defaultAliasesYAML = `version: 1

profiles: []

aliases: []
`

// InitOptions configures Init.
type InitOptions struct {
	// Source, when set, points config.yaml's source.path at an existing
	// aliases.yaml (e.g. inside a dotfiles repository) instead of creating
	// a new one (PROJECT.md §15.1, "Pointing at an existing dotfiles
	// repository instead").
	Source string

	// NoBootstrap skips the rc-file edit entirely, without prompting
	// (cli-commands spec, "Non-interactive install").
	NoBootstrap bool

	// AssumeYes consents to the rc-file edit without asking.
	//
	// It is the counterpart to NoBootstrap and exists because a prompt is
	// unanswerable outside a terminal. Without it, an install script, a
	// container build or a CI job could only ever decline the bootstrap, so
	// the one step that makes aliases actually load would be unreachable
	// exactly where automation needs it.
	AssumeYes bool

	// SkipInitialSync creates the local configuration without contacting the
	// configured source. This is useful when registration must happen before
	// the first authenticated sync.
	SkipInitialSync bool

	// Shell overrides shell detection for this run (--shell).
	Shell string

	// RCFile overrides rc-file detection for this run (--rc-file).
	RCFile string

	// Confirm asks the user a yes/no question. Defaults to prompting over
	// Env's Stdin/Stdout when nil.
	Confirm func(question string) (bool, error)
}

// InitReport summarizes what one `init` run did.
type InitReport struct {
	Base           string
	ConfigCreated  bool
	AliasesCreated bool
	Device         domain.Device
	Sync           SyncReport

	BootstrapPrompted      bool
	BootstrapAdded         bool
	BootstrapSkippedReason string // "--no-bootstrap", "declined", or ""
	RCPath                 string
	ManualBootstrapLine    string
}

// Init creates config.yaml and aliases.yaml when absent, detects this
// device's platform and shell, runs an initial sync, and — unless
// NoBootstrap is set — prompts for consent before adding AliasDeck's
// sourcing line to the shell rc file (cli-commands spec, "init Creates
// Config and Prompts Before Bootstrap").
func Init(ctx context.Context, env Env, opts InitOptions) (InitReport, error) {
	cenv := env.ConfigEnv()
	base, err := config.Base(cenv)
	if err != nil {
		return InitReport{}, fmt.Errorf("resolving base directory: %w", err)
	}
	report := InitReport{Base: base}

	configPath := config.ConfigFile(base)
	aliasesPath := config.AliasesFile(base)

	if opts.Source == "" {
		created, err := createIfAbsent(aliasesPath, []byte(defaultAliasesYAML), 0o600)
		if err != nil {
			return report, fmt.Errorf("creating aliases.yaml: %w", err)
		}
		report.AliasesCreated = created
	}

	if _, err := os.Stat(configPath); err != nil {
		if !os.IsNotExist(err) {
			return report, fmt.Errorf("checking %s: %w", configPath, err)
		}
		devCfg := config.DeviceFileConfig{
			Version: 1,
			Source:  config.Source{Type: config.SourceTypeFile, Path: opts.Source},
			Backend: config.BackendNative,
		}
		if err := config.Write(configPath, devCfg); err != nil {
			return report, fmt.Errorf("writing config.yaml: %w", err)
		}
		report.ConfigCreated = true
	}

	dc, err := loadDeviceContext(env, Options{Shell: opts.Shell})
	if err != nil {
		return report, err
	}
	report.Device = dc.Device

	if opts.SkipInitialSync {
		outputPath, err := dc.Backend.OutputPath(dc.Device)
		if err != nil {
			return report, fmt.Errorf("resolving generated output path: %w", err)
		}
		report.Sync = SyncReport{Device: dc.Device, Source: dc.SourceDesc, Backend: dc.Backend.Name(), OutputPath: outputPath}
	} else {
		syncReport, err := syncWithContext(ctx, env, dc)
		if err != nil {
			return report, fmt.Errorf("initial sync: %w", err)
		}
		report.Sync = syncReport
	}
	syncReport := report.Sync

	home, err := env.HomeDir()
	if err != nil {
		return report, fmt.Errorf("resolving home directory: %w", err)
	}

	if opts.NoBootstrap {
		report.BootstrapSkippedReason = "--no-bootstrap"
		report.ManualBootstrapLine = apply.BootstrapLine(dc.Device.Shell, syncReport.OutputPath, home)
		return report, nil
	}

	rcPath, err := resolveRCPath(env, dc.Device.Shell, dc.Device.Platform, opts.RCFile)
	if err != nil {
		return report, err
	}
	report.RCPath = rcPath
	report.BootstrapPrompted = true

	ok := opts.AssumeYes
	if !ok {
		confirm := opts.Confirm
		if confirm == nil {
			confirm = func(q string) (bool, error) { return promptYesNo(env, q) }
		}

		var err error
		ok, err = confirm(fmt.Sprintf("Add AliasDeck's bootstrap line to %s?", rcPath))
		if err != nil {
			return report, fmt.Errorf("reading bootstrap confirmation: %w", err)
		}
	}
	if !ok {
		report.BootstrapSkippedReason = "declined"
		report.ManualBootstrapLine = apply.BootstrapLine(dc.Device.Shell, syncReport.OutputPath, home)
		return report, nil
	}

	block, err := apply.AddBootstrap(rcPath, dc.Device.Shell, syncReport.OutputPath, home)
	if err != nil {
		return report, fmt.Errorf("adding bootstrap line to %s: %w", rcPath, err)
	}
	report.BootstrapAdded = true

	if block != "" {
		if err := recordBootstrap(config.StateFile(base), rcPath, block, env.Now()); err != nil {
			// The rc file has already been modified at this point. Returning a
			// bare state error would tell the user the command failed while
			// leaving them unaware that their shell configuration was changed,
			// and `uninstall` cannot remove a block it has no record of. Say
			// both things, and say where the block is.
			return report, fmt.Errorf(
				"%w\n\n%s was modified and the bootstrap line was added, but it could not be "+
					"recorded, so `aliasdeck uninstall` will not remove it. Delete this block by hand:\n%s",
				err, rcPath, block)
		}
	}

	return report, nil
}

// createIfAbsent writes data to path at mode when path does not already
// exist. It reports whether it created the file.
func createIfAbsent(path string, data []byte, mode os.FileMode) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("checking %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("creating directory for %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return true, nil
}

// recordBootstrap persists the exact bytes AddBootstrap appended into
// state.Bootstrap, so uninstall can restore them byte-for-byte later
// (design decision 6).
func recordBootstrap(statePath, rcPath, block string, now time.Time) error {
	st, err := state.Load(statePath)
	if err != nil {
		return fmt.Errorf("loading state before recording bootstrap: %w", err)
	}

	rcData, err := os.ReadFile(rcPath)
	if err != nil {
		return fmt.Errorf("reading %s after bootstrap: %w", rcPath, err)
	}

	st.Bootstrap = &state.Bootstrap{
		RCPath:  rcPath,
		Block:   block,
		RCHash:  hashBytes(rcData),
		AddedAt: now,
	}
	if err := state.Save(statePath, st); err != nil {
		return fmt.Errorf("saving state after recording bootstrap: %w", err)
	}
	return nil
}
