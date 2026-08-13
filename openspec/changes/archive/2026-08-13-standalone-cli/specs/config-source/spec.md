# Config Source Specification

## Purpose

Defines the `ConfigSource` contract and the `FileSource` implementation (PROJECT.md §7), and pins that every source — local or remote — is treated as hostile input (§12.1).

## Requirements

### Requirement: ConfigSource Contract

Every `ConfigSource` implementation MUST return a `ResolvedConfig` already filtered by the device's platform, shell, and profiles, or a non-nil error. It MUST NOT execute, interpret, or shell-out to any content it reads.

#### Scenario: Successful resolve
- GIVEN a valid source and a device
- WHEN `Resolve(ctx, device)` is called
- THEN it returns a `ResolvedConfig` containing only aliases applicable to that device

#### Scenario: Resolve error is not partially applied
- GIVEN a source that fails (missing file, malformed YAML)
- WHEN `Resolve` is called
- THEN it returns an error and no partial `ResolvedConfig` is used downstream

### Requirement: FileSource Reads a Single Local Path

`FileSource` MUST read the local `aliases.yaml` at the path declared in `config.yaml`'s `source.path`, and MUST NOT read or merge any other file.

#### Scenario: Configured path is used
- GIVEN `source: {type: file, path: ~/dotfiles/aliases.yaml}`
- WHEN `sync` runs
- THEN `FileSource` reads exactly that path

### Requirement: Every Source Is Hostile Input

`FileSource` output MUST pass through `validate.FilterValid` before reaching `renderers.Render`, identically to any remote source (§12.1). A local file receives no lesser scrutiny than a Git or server source would.

#### Scenario: Invalid alias name filtered
- GIVEN a local `aliases.yaml` containing an alias name with a shell metacharacter
- WHEN resolved
- THEN the entry is dropped by `validate.FilterValid` before rendering, not written to disk

#### Scenario: Oversized payload bounded
- GIVEN an alias command exceeding the validated size bound
- WHEN resolved
- THEN the entry is rejected by validation, not truncated or passed through

### Requirement: Exactly One Source Per Device

A device MUST resolve through exactly one `ConfigSource`, as declared by `config.yaml`'s `source.type`. The system MUST NOT merge or fall back across multiple sources (§7.1).

#### Scenario: No automatic fallback
- GIVEN `source.type: file` configured
- WHEN the configured file is temporarily unavailable
- THEN sync fails with an error naming the unavailable source; no other source is attempted
