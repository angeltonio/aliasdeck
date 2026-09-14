package app

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/importers"
	"github.com/angeltonio/aliasdeck/internal/validate"
)

// maxRCFileSize caps the startup file Import will read, for the reason
// internal/config caps aliases.yaml: every input is treated as hostile, and
// an unbounded read of a path the user named is a trivial way to exhaust
// memory.
const maxRCFileSize = 1 << 20

// ImportOptions configures Import.
type ImportOptions struct {
	Options
	// From is the startup file to read. Empty means the rc file this
	// device's shell would use.
	From string
	// Write performs the merge. When false — the default — Import reads,
	// reports, and leaves aliases.yaml untouched.
	Write bool
}

// ImportNote pairs an imported alias with a caveat about what changed in
// translation.
type ImportNote struct {
	Name string
	Line int
	Note string
}

// ImportConflict is an alias whose name is already taken in aliases.yaml by a
// different command. Conflicts are reported and never resolved: the file is
// the user's, and quietly replacing a line in it is how an import tool
// destroys work.
type ImportConflict struct {
	Name     string
	Line     int
	Existing string
	Incoming string
}

// ImportReport is everything Import found and what it did about it.
type ImportReport struct {
	SourcePath  string
	AliasesPath string
	// New is what would be (or was) appended.
	New []domain.Alias
	// Unchanged names are already in aliases.yaml with the same command, so
	// re-running an import is a no-op rather than a duplicate.
	Unchanged []string
	Conflicts []ImportConflict
	Notes     []ImportNote
	Skipped   []importers.Skipped
	Written   bool
}

// ErrImportUnderServerSource mirrors ErrEditAliasesUnderServerSource: a
// server-backed device has no local aliases.yaml to import into. Writing one
// would produce a file that looks authoritative and that nothing ever reads.
var ErrImportUnderServerSource = errors.New(
	"aliases live on the server for this device; import into the server through its web UI or API, not `aliasdeck import`")

// ErrImportShellUnsupported is returned when no explicit file was named and
// the device's own shell has no format this importer can parse.
var ErrImportShellUnsupported = errors.New(
	"only POSIX shell startup files (zsh, bash) can be imported; name one explicitly with --from")

// Import reads alias definitions out of a shell startup file and merges them
// into aliases.yaml.
//
// The file is parsed, never sourced. See internal/importers for why that is
// a design constraint rather than an implementation detail.
//
// Without Write this is a pure read: it reports what it found, what it could
// not translate, and what would collide, and changes nothing. That is the
// default because the input is a file full of shell written over years and
// the output is the file holding every alias the user owns — the first run
// should tell you what is about to happen, not tell you what already did.
func Import(_ context.Context, env Env, opts ImportOptions) (ImportReport, error) {
	dc, err := loadDeviceContext(env, opts.Options)
	if err != nil {
		return ImportReport{}, err
	}
	if dc.SourceDesc.Type == "server" {
		return ImportReport{}, ErrImportUnderServerSource
	}

	// PowerShell profiles define aliases with Set-Alias and functions, which
	// this parser does not read. An explicit --from is still honoured: the
	// user has named the file and knows what is in it.
	if opts.From == "" && dc.Device.Shell == domain.ShellPowerShell {
		return ImportReport{}, ErrImportShellUnsupported
	}

	// resolveRCPath is reused rather than reimplemented so --from gets the
	// same ~ expansion --rc-file already has, and so there is one answer to
	// "which file is this device's rc file" instead of two that can drift.
	from, err := resolveRCPath(env, dc.Device.Shell, dc.Device.Platform, opts.From)
	if err != nil {
		return ImportReport{}, err
	}

	report := ImportReport{SourcePath: from, AliasesPath: dc.AliasesPath}

	rc, err := readCapped(from, maxRCFileSize)
	if err != nil {
		return report, err
	}

	existing, err := loadAliasesForImport(dc.AliasesPath)
	if err != nil {
		return report, err
	}

	byName := make(map[string]domain.Alias, len(existing.Aliases))
	for _, a := range existing.Aliases {
		byName[a.Name] = a
	}

	parsed := importers.ParsePOSIX(rc)
	report.Skipped = parsed.Skipped

	// A name defined twice in one startup file is not ambiguous: the shell
	// takes the last one. Matching that is the only reading that imports
	// what the user's shell actually has.
	winner := make(map[string]int, len(parsed.Found))
	for i, f := range parsed.Found {
		if prev, dup := winner[f.Name]; dup {
			p := parsed.Found[prev]
			report.Skipped = append(report.Skipped, importers.Skipped{
				Line:   p.Line,
				Text:   "alias " + p.Name,
				Reason: fmt.Sprintf("redefined at line %d; the shell would use that one", f.Line),
			})
		}
		winner[f.Name] = i
	}

	for i, f := range parsed.Found {
		if winner[f.Name] != i {
			continue
		}

		if err := validate.Name(f.Name, dc.Device.Shell); err != nil {
			report.Skipped = append(report.Skipped, importers.Skipped{
				Line: f.Line, Text: "alias " + f.Name, Reason: err.Error()})
			continue
		}
		if err := validate.Command(f.Command); err != nil {
			report.Skipped = append(report.Skipped, importers.Skipped{
				Line: f.Line, Text: "alias " + f.Name, Reason: err.Error()})
			continue
		}

		if prior, taken := byName[f.Name]; taken {
			if prior.Command == f.Command {
				report.Unchanged = append(report.Unchanged, f.Name)
			} else {
				report.Conflicts = append(report.Conflicts, ImportConflict{
					Name: f.Name, Line: f.Line,
					Existing: prior.Command, Incoming: f.Command,
				})
			}
			continue
		}

		// Claim the name so a second startup file in the same run, or a
		// later alias in this one, sees it as taken.
		imported := domain.Alias{
			ID:      f.Name,
			Name:    f.Name,
			Command: f.Command,
			Enabled: true,
		}
		byName[f.Name] = imported
		report.New = append(report.New, imported)

		if f.Note != "" {
			report.Notes = append(report.Notes, ImportNote{Name: f.Name, Line: f.Line, Note: f.Note})
		}
	}

	if total := len(existing.Aliases) + len(report.New); total > validate.MaxAliases {
		return report, fmt.Errorf(
			"importing %d aliases would bring aliases.yaml to %d, over the %d limit",
			len(report.New), total, validate.MaxAliases)
	}

	if !opts.Write || len(report.New) == 0 {
		return report, nil
	}

	existing.Aliases = append(existing.Aliases, report.New...)
	if err := config.WriteAliases(dc.AliasesPath, existing); err != nil {
		return report, err
	}
	report.Written = true
	return report, nil
}

// readCapped reads path, refusing anything over limit before the bytes are
// held rather than after.
func readCapped(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("%s is %d bytes, over the %d byte limit", path, info.Size(), limit)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return data, nil
}

// loadAliasesForImport parses aliases.yaml, treating a missing file as an
// empty document. Missing is a real state on a device whose source directory
// exists but has never been written to; refusing there would send the user
// to create an empty file by hand for no reason.
func loadAliasesForImport(path string) (config.AliasesDocument, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return config.AliasesDocument{}, nil
	}
	if err != nil {
		return config.AliasesDocument{}, fmt.Errorf("reading %s: %w", path, err)
	}
	return config.ParseAliases(data)
}
