package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"go.yaml.in/yaml/v3"
)

// maxAliasesFileSize caps aliases.yaml at 1 MiB. Every source is treated as
// hostile (PROJECT.md §12.1): an unbounded YAML decode is a trivial resource
// exhaustion vector, so the size is rejected before a single byte is parsed.
const maxAliasesFileSize = 1 << 20

// AliasesDocument is the parsed, strictly-typed content of aliases.yaml
// (PROJECT.md §7.2): the declared aliases plus the top-level profile list
// doctor needs to spot references to a profile nobody declared.
type AliasesDocument struct {
	Profiles []string
	Aliases  []domain.Alias
}

// aliasesFileDTO mirrors aliases.yaml's on-disk shape exactly, so
// yaml.Decoder's KnownFields(true) can reject a typo'd field before it is
// ever translated into a domain.Alias.
type aliasesFileDTO struct {
	Version  int        `yaml:"version"`
	Profiles []string   `yaml:"profiles,omitempty"`
	Aliases  []aliasDTO `yaml:"aliases"`
}

// aliasDTO is the parse-layer counterpart to domain.Alias.
//
// It exists because the YAML shape and the domain shape diverge in ways the
// domain type should not know about: `enabled` needs a pointer to distinguish
// "omitted" from "false" (domain.Alias.Enabled's zero value is already
// false), and `profiles:` is renamed to ProfileIDs on the way in (design D2).
type aliasDTO struct {
	Name        string   `yaml:"name"`
	Command     string   `yaml:"command"`
	Description string   `yaml:"description,omitempty"`
	Enabled     *bool    `yaml:"enabled,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
	Platforms   []string `yaml:"platforms,omitempty"`
	Shells      []string `yaml:"shells,omitempty"`
	Profiles    []string `yaml:"profiles,omitempty"`
}

// ParseAliases decodes and strictly validates the bytes of an aliases.yaml
// file: unknown fields, a version other than 1, and oversized input are all
// parse errors rather than silently accepted or ignored data.
func ParseAliases(data []byte) (AliasesDocument, error) {
	if len(data) > maxAliasesFileSize {
		return AliasesDocument{}, fmt.Errorf(
			"aliases.yaml is %d bytes, exceeds the %d byte limit", len(data), maxAliasesFileSize)
	}

	var dto aliasesFileDTO
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&dto); err != nil {
		return AliasesDocument{}, fmt.Errorf("parsing aliases.yaml: %w", err)
	}

	if dto.Version != 1 {
		return AliasesDocument{}, fmt.Errorf(
			"aliases.yaml version %d is not supported, expected 1", dto.Version)
	}

	aliases := make([]domain.Alias, 0, len(dto.Aliases))
	for _, a := range dto.Aliases {
		alias, err := a.toDomain()
		if err != nil {
			return AliasesDocument{}, err
		}
		aliases = append(aliases, alias)
	}

	return AliasesDocument{Profiles: dto.Profiles, Aliases: aliases}, nil
}

// toDomain converts a parsed alias entry into domain.Alias, applying the
// `enabled` default (design D2) and deriving ID from Name.
func (dto aliasDTO) toDomain() (domain.Alias, error) {
	platforms, err := parsePlatforms(dto.Platforms)
	if err != nil {
		return domain.Alias{}, fmt.Errorf("alias %q: %w", dto.Name, err)
	}
	shells, err := parseShells(dto.Shells)
	if err != nil {
		return domain.Alias{}, fmt.Errorf("alias %q: %w", dto.Name, err)
	}

	return domain.Alias{
		ID:          dto.Name,
		Name:        dto.Name,
		Command:     dto.Command,
		Description: dto.Description,
		Enabled:     dto.Enabled == nil || *dto.Enabled,
		Tags:        dto.Tags,
		Platforms:   platforms,
		Shells:      shells,
		ProfileIDs:  dto.Profiles,
	}, nil
}

func parsePlatforms(values []string) ([]domain.Platform, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]domain.Platform, 0, len(values))
	for _, v := range values {
		p, err := domain.ParsePlatform(v)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func parseShells(values []string) ([]domain.Shell, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]domain.Shell, 0, len(values))
	for _, v := range values {
		s, err := domain.ParseShell(v)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// ProfileWarnings reports aliases that reference a profile not present in
// declaredProfiles. Per the spec, this is a doctor-level warning: it never
// fails parsing or blocks sync, it only surfaces a likely typo.
func ProfileWarnings(declaredProfiles []string, aliases []domain.Alias) []string {
	declared := make(map[string]struct{}, len(declaredProfiles))
	for _, p := range declaredProfiles {
		declared[p] = struct{}{}
	}

	var warnings []string
	for _, a := range aliases {
		for _, p := range a.ProfileIDs {
			if _, ok := declared[p]; !ok {
				warnings = append(warnings, fmt.Sprintf(
					"alias %q references undeclared profile %q", a.Name, p))
			}
		}
	}
	return warnings
}

// MarshalAliases renders a document back to aliases.yaml bytes.
//
// It goes through the same DTO ParseAliases decodes, which is what keeps the
// two honest: a field that gains a name here cannot fail to be readable
// there, because there is only one spelling of the shape. The round trip is
// covered by a test rather than left to that argument.
//
// Every optional field is omitempty, so a marshalled file looks like one a
// person would have written — `enabled: null` and `tags: []` on forty
// entries is noise nobody asked for, and this file is meant to stay
// hand-editable.
func MarshalAliases(doc AliasesDocument) ([]byte, error) {
	dto := aliasesFileDTO{Version: 1, Profiles: doc.Profiles}
	for _, a := range doc.Aliases {
		dto.Aliases = append(dto.Aliases, fromDomain(a))
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(dto); err != nil {
		return nil, fmt.Errorf("marshaling aliases.yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("marshaling aliases.yaml: %w", err)
	}
	return buf.Bytes(), nil
}

// fromDomain is toDomain's inverse. Enabled becomes a pointer again only when
// it is false: an omitted `enabled` already means true (design D2), so
// writing it out would add a line that says nothing.
func fromDomain(a domain.Alias) aliasDTO {
	dto := aliasDTO{
		Name:        a.Name,
		Command:     a.Command,
		Description: a.Description,
		Tags:        a.Tags,
		Profiles:    a.ProfileIDs,
	}
	if !a.Enabled {
		disabled := false
		dto.Enabled = &disabled
	}
	for _, p := range a.Platforms {
		dto.Platforms = append(dto.Platforms, string(p))
	}
	for _, s := range a.Shells {
		dto.Shells = append(dto.Shells, string(s))
	}
	return dto
}

// WriteAliases marshals doc and replaces the file at path atomically.
//
// Atomic for the reason design decision 33 gives everywhere else in this
// package: a plain os.WriteFile truncates before it writes, so an interrupted
// write leaves an empty or half-written file rather than a stale one. This is
// the file holding every alias a user has; losing it to a full disk would be
// the worst bug this project could ship.
//
// An existing file keeps its permissions. aliases.yaml holds no secrets, and
// a user who deliberately made theirs group-readable should not find it
// silently tightened by an unrelated command. A new file is created at 0600
// and left for the user to loosen.
func WriteAliases(path string, doc AliasesDocument) error {
	data, err := MarshalAliases(doc)
	if err != nil {
		return err
	}

	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".aliases.*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := writeSyncCloseMode(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", tmpPath, path, err)
	}
	return nil
}
