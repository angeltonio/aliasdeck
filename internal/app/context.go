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
const Version = "0.5.3"

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
	id, err := loadDeviceIdentity(env, opts)
	if err != nil {
		return deviceContext{}, err
	}

	aliasesPath, src, desc, err := resolveSource(id.devCfg, env.ConfigEnv(), id.base)
	if err != nil {
		return deviceContext{}, err
	}

	backend, err := resolveBackend(id.devCfg, id.base)
	if err != nil {
		return deviceContext{}, err
	}

	return deviceContext{
		Base:               id.base,
		ConfigPath:         id.configPath,
		AliasesPath:        aliasesPath,
		DeviceCfg:          id.devCfg,
		Device:             id.device,
		Source:             src,
		SourceDesc:         desc,
		Backend:            backend,
		PlatformProvenance: id.platformProvenance,
		ShellProvenance:    id.shellProvenance,
	}, nil
}

// deviceIdentity is everything loadDeviceContext needs before it ever
// touches Source.Type: base directory, config.yaml's parsed content, and the
// detected device identity. register (task 8.4/8.5) needs exactly this and
// nothing more — resolveSource's server arm requires a device token that
// register itself is the one obtaining, so register cannot go through
// resolveSource (via loadDeviceContext) to reach its own device identity
// without risking exactly the chicken-and-egg failure that would create.
type deviceIdentity struct {
	base       string
	configPath string
	devCfg     config.DeviceFileConfig
	device     domain.Device

	platformProvenance string
	shellProvenance    string
}

// loadDeviceIdentity resolves config.yaml and this device's platform/shell,
// without resolving a ConfigSource or a SyncBackend. It returns
// ErrNotInitialized when config.yaml does not exist yet, and a ConfigError
// when it exists but fails to parse — identically to loadDeviceContext,
// which is loadDeviceIdentity plus resolveSource/resolveBackend.
func loadDeviceIdentity(env Env, opts Options) (deviceIdentity, error) {
	cenv := env.ConfigEnv()
	base, err := config.Base(cenv)
	if err != nil {
		return deviceIdentity{}, fmt.Errorf("resolving base directory: %w", err)
	}

	configPath := config.ConfigFile(base)
	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			return deviceIdentity{}, ErrNotInitialized
		}
		return deviceIdentity{}, fmt.Errorf("checking %s: %w", configPath, err)
	}

	devCfg, err := config.Load(configPath)
	if err != nil {
		return deviceIdentity{}, ConfigError{Err: fmt.Errorf("loading config.yaml: %w", err)}
	}

	platformDet, err := config.DetectPlatform(devCfg.Device.Platform, env.Getenv, runtime.GOOS)
	if err != nil {
		return deviceIdentity{}, fmt.Errorf("detecting platform: %w", err)
	}

	shellDet, err := config.DetectShell(opts.Shell, devCfg.Device.Shell, env.Getenv, platformDet.Platform)
	if err != nil {
		return deviceIdentity{}, fmt.Errorf("detecting shell: %w", err)
	}

	dev := domain.Device{
		ID:            devCfg.Device.ID,
		Name:          devCfg.Device.Name,
		ProfileIDs:    devCfg.Device.ProfileIDs,
		Platform:      platformDet.Platform,
		Shell:         shellDet.Shell,
		ClientVersion: Version,
	}

	return deviceIdentity{
		base:               base,
		configPath:         configPath,
		devCfg:             devCfg,
		device:             dev,
		platformProvenance: platformDet.Provenance,
		shellProvenance:    shellDet.Provenance,
	}, nil
}

// resolveSource builds the ConfigSource devCfg.Source declares.
//
// File, git and server sources are all implemented (PROJECT.md §7; design
// decisions 11-16, server-source spec). Every command that reaches a
// ConfigSource does so through here, once per invocation (loadDeviceContext
// calls this on every run) — that is what makes design decision 13's
// "checked on every sync, not only at login" property hold for a server
// source without this function having to special-case it:
// resolveServerSource fails fast on an insecure URL, and
// *source.ServerSource.Resolve re-checks the same guard internally on every
// call regardless.
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
	case config.SourceTypeGit:
		return resolveGitSource(devCfg.Source.Git, base)
	case config.SourceTypeServer:
		return resolveServerSource(devCfg.Source, base)
	default:
		return "", nil, source.Descriptor{}, fmt.Errorf(
			"source type %q is not supported in this version of AliasDeck", devCfg.Source.Type)
	}
}

// resolveServerSource builds a *source.ServerSource from config.yaml's
// source.url, the device's credentials file (design decision 14), and
// source.allowInsecureHTTP (design decision 13). It requires the device to
// already hold a device token — i.e. `aliasdeck register` to have already
// run — rather than building a source with an empty Token that would only
// fail once Resolve is actually called.
//
// ValidateServerURL is called here, fail-fast, exactly like
// resolveGitSource's own "before a checkout ever runs" guard for a missing
// git URL — but this is deliberately *not* the only place it runs:
// *source.ServerSource.Resolve calls the identical check again on every one
// of its own invocations (design decision 13), so a hand-edited config.yaml
// switching to an insecure URL after this function has already returned a
// source is still caught the next time that source is actually resolved,
// not just once at command-startup time.
func resolveServerSource(src config.Source, base string) (path string, cs source.ConfigSource, desc source.Descriptor, err error) {
	if src.URL == "" {
		return "", nil, source.Descriptor{}, fmt.Errorf("source.url is required for a server source")
	}
	if err := source.ValidateServerURL(src.URL, src.AllowInsecureHTTP); err != nil {
		return "", nil, source.Descriptor{}, err
	}

	creds, err := config.LoadCredentials(config.CredentialsFile(base))
	if err != nil {
		return "", nil, source.Descriptor{}, fmt.Errorf("loading credentials: %w", err)
	}
	if creds.DeviceToken == "" {
		return "", nil, source.Descriptor{}, fmt.Errorf(
			"no device token found for %q; run `aliasdeck register` first", src.URL)
	}

	ss := &source.ServerSource{
		URL:       src.URL,
		Token:     creds.DeviceToken,
		AllowHTTP: src.AllowInsecureHTTP,
	}
	// A server source has no local aliases file: every reader of the path
	// this function returns must go through Source.Resolve (or, for
	// diagnostics, source.UnfilteredResolver) instead of reading a file
	// directly at this path. Fixing every remaining direct os.ReadFile(dc.
	// AliasesPath) call site (edit, list) is task 8.10/8.11, not this one.
	return "", ss, ss.Descriptor(), nil
}

// resolveGitSource builds a *source.GitSource from config.yaml's
// source.git: block (design decisions 11-16). It fails fast — before a
// checkout ever runs — when the URL is missing or source.git.path would
// escape the checkout, rather than deferring either failure to sync time.
func resolveGitSource(g config.GitSourceConfig, base string) (path string, src source.ConfigSource, desc source.Descriptor, err error) {
	if g.URL == "" {
		return "", nil, source.Descriptor{}, fmt.Errorf("source.git.url is required for a git source")
	}

	cacheDir := source.GitCacheDir(base, g.URL)
	path, err = source.GitAliasesPath(cacheDir, g.Path)
	if err != nil {
		return "", nil, source.Descriptor{}, fmt.Errorf("resolving source.git.path: %w", err)
	}

	gs := &source.GitSource{
		URL:      g.URL,
		Ref:      g.Ref,
		Path:     g.Path,
		CacheDir: cacheDir,
		Run:      source.RunGit,
	}
	return path, gs, gs.Descriptor(), nil
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
