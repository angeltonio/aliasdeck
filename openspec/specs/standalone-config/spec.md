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

The system MUST parse `config.yaml` (`version`, `device`, `source`, `backend`) strictly, and MUST derive a stable device identity from `device.name` (or a generated fallback if omitted). When `source.type: git`, the system MUST require `source.git.url` and MUST reject unknown fields under `source.git`, identical in strictness to `source.type: file`. `source.git.ref` MAY be omitted.

#### Scenario: Valid config.yaml parses
- GIVEN a well-formed `config.yaml` matching §7.3
- WHEN parsed
- THEN it yields a `Device` with name, profiles, source, and backend populated

#### Scenario: Unknown backend value rejected at parse time
- GIVEN `backend: invalid-value`
- WHEN parsed
- THEN parsing fails; only `native` and `chezmoi` are accepted values

#### Scenario: Git source without a ref parses
- GIVEN `source: {type: git, url: <repo>}` with no `ref`
- WHEN parsed
- THEN parsing succeeds and ref resolution defers to the remote's default branch

#### Scenario: Git source missing url rejected
- GIVEN `source: {type: git}` with no `url`
- WHEN parsed
- THEN parsing fails naming the missing required field

### Requirement: Platform and Shell Auto-Detection

When `config.yaml` does not explicitly override platform or shell, the system MUST detect them from the runtime OS and `$SHELL`, and MUST make the detected values visible to `status`/`doctor`. On `windows`, the system MUST accept `runtime.GOOS == "windows"` as a valid platform and MUST default the shell to `powershell` without requiring a `$SHELL` variable, since Windows does not set one.

#### Scenario: Detection on a clean macOS/Linux install
- GIVEN no explicit platform/shell override in `config.yaml`
- WHEN `init` runs on macOS with `$SHELL=/bin/zsh`
- THEN the device is recorded with platform `macos` and shell `zsh`

#### Scenario: Unsupported shell detected
- GIVEN `$SHELL` points to an unsupported shell (e.g. fish)
- WHEN detection runs
- THEN the CLI reports the unsupported shell via `doctor`/`status` rather than guessing a supported one

#### Scenario: Detection on Windows defaults to PowerShell
- GIVEN no explicit override and no `$SHELL` variable
- WHEN `init` runs on Windows
- THEN the device is recorded with platform `windows` and shell `powershell`

### Requirement: Windows Config Base Directory

On `windows`, the system MUST resolve the base configuration directory to `~/.config/aliasdeck`, expanded against the user's home directory, in the same shape used on macOS and Linux. `$ALIASDECK_HOME`, when set, MUST override this on every platform. The system MUST NOT use `%APPDATA%` or `os.UserConfigDir()`.

#### Scenario: Default base directory on Windows
- GIVEN a Windows device with no `$ALIASDECK_HOME`
- WHEN the base directory is resolved
- THEN it resolves under the user's home directory at `.config/aliasdeck`, not `%APPDATA%`

#### Scenario: ALIASDECK_HOME override on Windows
- GIVEN `$ALIASDECK_HOME` set to a custom path on Windows
- WHEN the base directory is resolved
- THEN that path is used instead of the default

### Requirement: PowerShell Edition and $PROFILE Selection

The system MUST detect which PowerShell edition (5.1 Desktop or 7 Core) is running AliasDeck and MUST select only that edition's `$PROFILE` path as the bootstrap target, never both. `--rc-file` MUST override detection explicitly.

#### Scenario: PowerShell 7 (Core) detected
- GIVEN the detected edition is PowerShell 7
- WHEN `init` resolves the bootstrap target
- THEN it targets `~\Documents\PowerShell\Microsoft.PowerShell_profile.ps1` only

#### Scenario: Windows PowerShell 5.1 (Desktop) detected
- GIVEN the detected edition is Windows PowerShell 5.1
- WHEN `init` resolves the bootstrap target
- THEN it targets `~\Documents\WindowsPowerShell\Microsoft.PowerShell_profile.ps1` only

#### Scenario: --rc-file overrides detection
- GIVEN `--rc-file <path>` is passed
- WHEN `init` or `sync` resolve the bootstrap target
- THEN detection is bypassed and the given path is used
