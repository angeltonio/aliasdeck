package app

import (
	"fmt"
	"os"
	"runtime"

	"github.com/angeltonio/aliasdeck/internal/apply"
	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/source"
)

// Version is AliasDeck's client version, recorded in domain.Device and
// state.State so `status` can show which build last synced a device.
const Version = "0.1.0"

// Options carries the flags every command shares. --shell overrides
// detection the same way for every command (design, "Shell" precedence).
type Options struct {
	Shell string
}

// deviceContext bundles everything a command needs once config.yaml has
// been loaded: the resolved device identity, base directory, active
// source, and target backend.
type deviceContext struct {
	Base        string
	ConfigPath  string
	AliasesPath string
	DeviceCfg   config.DeviceFileConfig
	Device      domain.Device
	Source      source.ConfigSource
	SourceDesc  source.Descriptor
	Backend     apply.SyncBackend

	PlatformProvenance string
	ShellProvenance    string
}

// loadDeviceContext resolves config.yaml and everything derived from it.
// It returns ErrNotInitialized when config.yaml does not exist yet, and a
// ConfigError when it exists but fails to parse.
func loadDeviceContext(env Env, opts Options) (deviceContext, error) {
	cenv := env.ConfigEnv()
	base, err := config.Base(cenv)
	if err != nil {
		return deviceContext{}, fmt.Errorf("resolving base directory: %w", err)
	}

	configPath := config.ConfigFile(base)
	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			return deviceContext{}, ErrNotInitialized
		}
		return deviceContext{}, fmt.Errorf("checking %s: %w", configPath, err)
	}

	devCfg, err := config.Load(configPath)
	if err != nil {
		return deviceContext{}, ConfigError{Err: fmt.Errorf("loading config.yaml: %w", err)}
	}

	platformDet, err := config.DetectPlatform(devCfg.Device.Platform, env.Getenv, runtime.GOOS)
	if err != nil {
		return deviceContext{}, fmt.Errorf("detecting platform: %w", err)
	}

	shellDet, err := config.DetectShell(opts.Shell, devCfg.Device.Shell, env.Getenv, platformDet.Platform)
	if err != nil {
		return deviceContext{}, fmt.Errorf("detecting shell: %w", err)
	}

	dev := domain.Device{
		ID:            devCfg.Device.ID,
		Name:          devCfg.Device.Name,
		ProfileIDs:    devCfg.Device.ProfileIDs,
		Platform:      platformDet.Platform,
		Shell:         shellDet.Shell,
		ClientVersion: Version,
	}

	aliasesPath, src, desc, err := resolveSource(devCfg, cenv, base)
	if err != nil {
		return deviceContext{}, err
	}

	backend, err := resolveBackend(devCfg, base)
	if err != nil {
		return deviceContext{}, err
	}

	return deviceContext{
		Base:               base,
		ConfigPath:         configPath,
		AliasesPath:        aliasesPath,
		DeviceCfg:          devCfg,
		Device:             dev,
		Source:             src,
		SourceDesc:         desc,
		Backend:            backend,
		PlatformProvenance: platformDet.Provenance,
		ShellProvenance:    shellDet.Provenance,
	}, nil
}

// resolveSource builds the ConfigSource devCfg.Source declares.
//
// Only file sources are implemented in this milestone (git and server are
// Milestone 3+, PROJECT.md §7); selecting either today is an explicit
// error rather than a silent fallback to a file.
func resolveSource(devCfg config.DeviceFileConfig, cenv config.Env, base string) (path string, src source.ConfigSource, desc source.Descriptor, err error) {
	switch devCfg.Source.Type {
	case config.SourceTypeFile, "":
		path = devCfg.Source.Path
		if path == "" {
			path = config.AliasesFile(base)
		} else {
			path, err = config.ExpandPath(path, cenv)
			if err != nil {
				return "", nil, source.Descriptor{}, fmt.Errorf("expanding source.path: %w", err)
			}
		}
		fs := source.FileSource{Path: path}
		return path, fs, fs.Descriptor(), nil
	default:
		return "", nil, source.Descriptor{}, fmt.Errorf(
			"source type %q is not supported in this version of AliasDeck", devCfg.Source.Type)
	}
}

// resolveBackend builds the SyncBackend devCfg.Backend declares.
//
// ChezmoiBackend is returned without error here: design decision 9 makes
// selecting it valid, deferring the hard error to the moment it is asked
// to actually do something (OutputPath/Apply).
func resolveBackend(devCfg config.DeviceFileConfig, base string) (apply.SyncBackend, error) {
	switch devCfg.Backend {
	case config.BackendNative, "":
		return apply.NativeBackend{Base: base}, nil
	case config.BackendChezmoi:
		return apply.ChezmoiBackend{}, nil
	default:
		return nil, fmt.Errorf("backend %q is not supported", devCfg.Backend)
	}
}
