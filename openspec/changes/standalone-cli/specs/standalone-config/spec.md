# Standalone Config Specification

## Purpose

Defines the `aliases.yaml` and `config.yaml` schemas, strict parsing rules, field defaulting, device identity, and platform/shell auto-detection that feed the standalone sync pipeline (PROJECT.md §7.2, §7.3).

## Requirements

### Requirement: aliases.yaml Strict Schema

The system MUST parse `aliases.yaml` (`version`, `profiles`, `aliases[]`) and MUST reject unknown top-level or alias-level fields as a parse error rather than ignoring them silently.

#### Scenario: Valid file parses
- GIVEN a well-formed `aliases.yaml` matching §7.2
- WHEN it is parsed
- THEN parsing succeeds and returns the declared aliases and profiles

#### Scenario: Unknown field rejected
- GIVEN an alias entry with an undeclared field (e.g. `commnad`)
- WHEN the file is parsed
- THEN parsing fails with an error naming the offending field and alias

### Requirement: `enabled` Defaults to True

The parse layer MUST set `Alias.Enabled = true` for any entry that omits the `enabled` key, before the value reaches `domain.AppliesTo`, since the Go zero value is `false`.

#### Scenario: Entry omitting `enabled` renders
- GIVEN an `aliases.yaml` entry with no `enabled` key (as in every §7.2 example)
- WHEN it is parsed and resolved
- THEN the alias is treated as enabled and is eligible for rendering

#### Scenario: Explicit `enabled: false` is honored
- GIVEN an entry with `enabled: false`
- WHEN it is parsed
- THEN the alias is excluded from the resolved config

### Requirement: Omitted Targeting Fields Mean "All" / "Always"

Omitted `platforms` or `shells` on an alias MUST mean "all supported"; omitted `profiles` MUST mean "always active", per §7.2.

#### Scenario: No platforms/shells declared
- GIVEN an alias with no `platforms` and no `shells` key
- WHEN resolved for any supported device
- THEN the alias applies regardless of the device's platform or shell

### Requirement: Profile References Degrade to a Warning

An alias referencing a profile not declared in the top-level `profiles:` list MUST NOT fail parsing or sync. It MUST be reported by `doctor` as a warning.

#### Scenario: Alias references undeclared profile
- GIVEN an alias with `profiles: [typo-profile]` not present in the top-level list
- WHEN the file is parsed and `doctor` runs
- THEN parsing succeeds, the alias is still evaluated normally, and `doctor` reports a warning naming the undeclared profile

### Requirement: config.yaml Strict Schema and Device Identity

The system MUST parse `config.yaml` (`version`, `device`, `source`, `backend`) strictly, and MUST derive a stable device identity from `device.name` (or a generated fallback if omitted).

#### Scenario: Valid config.yaml parses
- GIVEN a well-formed `config.yaml` matching §7.3
- WHEN parsed
- THEN it yields a `Device` with name, profiles, source, and backend populated

#### Scenario: Unknown backend value rejected at parse time
- GIVEN `backend: invalid-value`
- WHEN parsed
- THEN parsing fails; only `native` and `chezmoi` are accepted values

### Requirement: Platform and Shell Auto-Detection

When `config.yaml` does not explicitly override platform or shell, the system MUST detect them from the runtime OS and `$SHELL`, and MUST make the detected values visible to `status`/`doctor`.

#### Scenario: Detection on a clean macOS/Linux install
- GIVEN no explicit platform/shell override in `config.yaml`
- WHEN `init` runs on macOS with `$SHELL=/bin/zsh`
- THEN the device is recorded with platform `macos` and shell `zsh`

#### Scenario: Unsupported shell detected
- GIVEN `$SHELL` points to an unsupported shell (e.g. fish)
- WHEN detection runs
- THEN the CLI reports the unsupported shell via `doctor`/`status` rather than guessing a supported one
