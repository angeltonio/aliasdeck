package app

import (
	"context"
	"fmt"
	"os"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/validate"
)

// DoctorReport is doctor's read-only diagnosis: every issue validation
// would drop and every undeclared-profile reference — none of which
// FileSource.Resolve exposes, since it already discards validate.Issues
// once it has filtered by them (cli-commands spec, "doctor Diagnoses
// Without Writing").
type DoctorReport struct {
	Device             domain.Device
	AliasesPath        string
	PlatformProvenance string
	ShellProvenance    string
	Issues             validate.Issues
	ProfileWarnings    []string
}

// Doctor performs its own independent read-and-validate pass over the
// active aliases.yaml: the same domain.Resolve → validate.Config sequence
// FileSource.Resolve runs internally, except the issues are returned
// instead of discarded. It never calls Source.Resolve and never writes
// anything.
func Doctor(_ context.Context, env Env, opts Options) (DoctorReport, error) {
	dc, err := loadDeviceContext(env, opts)
	if err != nil {
		return DoctorReport{}, err
	}

	data, err := os.ReadFile(dc.AliasesPath)
	if err != nil {
		return DoctorReport{}, fmt.Errorf("reading %s: %w", dc.AliasesPath, err)
	}
	doc, err := config.ParseAliases(data)
	if err != nil {
		return DoctorReport{}, ConfigError{Err: fmt.Errorf("parsing %s: %w", dc.AliasesPath, err)}
	}

	resolved := domain.Resolve(dc.Device, doc.Aliases)
	issues := validate.Config(resolved)
	warnings := config.ProfileWarnings(doc.Profiles, doc.Aliases)

	return DoctorReport{
		Device:             dc.Device,
		AliasesPath:        dc.AliasesPath,
		PlatformProvenance: dc.PlatformProvenance,
		ShellProvenance:    dc.ShellProvenance,
		Issues:             issues,
		ProfileWarnings:    warnings,
	}, nil
}
