package config

import (
	"bytes"
	"fmt"

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
	Profiles []string   `yaml:"profiles"`
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
	Description string   `yaml:"description"`
	Enabled     *bool    `yaml:"enabled"`
	Tags        []string `yaml:"tags"`
	Platforms   []string `yaml:"platforms"`
	Shells      []string `yaml:"shells"`
	Profiles    []string `yaml:"profiles"`
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
