# CLI Commands Specification

## Purpose

Defines the behavior, output, and exit codes of the seven `aliasdeck` commands: `init`, `sync`, `status`, `list`, `doctor`, `edit`, `uninstall` (PROJECT.md §4.1, §15.1).

## Requirements

### Requirement: Exit Code Convention

Every command MUST exit `0` on success and a non-zero code on any failure (parse error, validation failure, I/O error, unresolvable source).

#### Scenario: Failure surfaces a non-zero exit
- GIVEN any command encountering an error
- WHEN it terminates
- THEN the process exit code is non-zero and the error is printed to stderr

### Requirement: init Creates Config and Prompts Before Bootstrap

`init` MUST create `config.yaml` and `aliases.yaml` when absent, detect platform/shell, and MUST prompt for consent before editing any rc file. `--no-bootstrap` MUST skip that edit entirely. `--yes`/`-f` MUST consent to the edit without prompting. Independently of any flag, the prompt itself MUST be skipped — defaulting to declined, the same outcome as an unanswered interactive prompt — whenever stdin is not a terminal, since a pipe that never delivers a line would otherwise block forever under `curl … | sh`, a container build, or CI.

#### Scenario: First run on a clean device
- GIVEN no existing AliasDeck config
- WHEN `aliasdeck init` runs interactively
- THEN both config files are created and the user is prompted before the rc file is edited

#### Scenario: Non-interactive install, no consent given
- GIVEN `aliasdeck init --no-bootstrap`, or `aliasdeck init` with stdin not attached to a terminal and no `--yes`
- WHEN it runs
- THEN config files are created, no rc file is touched, and the command prints how to add the bootstrap line manually

#### Scenario: Non-interactive install with explicit consent
- GIVEN `aliasdeck init --yes`
- WHEN it runs
- THEN config files are created and the bootstrap line is added without ever printing or waiting on a prompt

### Requirement: sync Runs the Full Pipeline

`sync` MUST run resolve → validate → render → apply → record-state in order, and exit non-zero if the active source cannot be resolved.

#### Scenario: Successful sync
- GIVEN a valid config and reachable source
- WHEN `sync` runs
- THEN it exits `0` and prints the count of aliases applied

#### Scenario: Unresolvable source
- GIVEN a source that cannot be read (missing file, unreachable path)
- WHEN `sync` runs
- THEN it exits non-zero with an actionable error naming the source

### Requirement: status Always Reports the Active Source

`status` MUST report the active `ConfigSource` type and location, device identity, last sync time, and whether the generated file is current, on every invocation. On Windows, `status` MUST additionally report the detected PowerShell edition and the exact `$PROFILE` path bootstrapped. When the active source is `GitSource`, `status` MUST report the resolved ref and whether the checkout is stale.

#### Scenario: status after a successful sync
- GIVEN a device that has synced
- WHEN `status` runs
- THEN it prints the source, device identity, last sync timestamp, and up-to-date state

#### Scenario: status reports PowerShell edition and profile
- GIVEN a Windows device that has synced
- WHEN `status` runs
- THEN it additionally names the detected PowerShell edition and the exact `$PROFILE` path bootstrapped

#### Scenario: status reports git ref and staleness
- GIVEN `source.type: git`
- WHEN `status` runs
- THEN it reports the resolved ref and, if the checkout is stale, says so explicitly

### Requirement: list Shows Resolved Aliases

`list` MUST print the aliases that apply to the current device after platform/shell/profile filtering, distinguishing enabled entries from skipped ones.

#### Scenario: Device-scoped listing
- GIVEN an `aliases.yaml` with entries targeting multiple platforms
- WHEN `list` runs on a macOS/zsh device
- THEN only aliases applicable to macOS/zsh are shown as active

### Requirement: doctor Diagnoses Without Writing

`doctor` MUST report every alias skipped by validation with its reason, every undeclared-profile reference, and any platform/shell detection issue, and MUST NOT write to disk. On a device where both PowerShell editions' profiles exist but only one is bootstrapped, `doctor` MUST warn that the other edition's profile exists and is not bootstrapped. When the active source is `GitSource` and its checkout is stale, `doctor` MUST report the staleness.

#### Scenario: Hostile entry reported
- GIVEN an `aliases.yaml` entry that fails `validate.FilterValid`
- WHEN `doctor` runs
- THEN it lists the entry with the specific validation reason and writes nothing

#### Scenario: Undeclared profile reported
- GIVEN an alias referencing a profile absent from the top-level list
- WHEN `doctor` runs
- THEN it reports a warning, not an error

#### Scenario: Other-edition profile warning
- GIVEN both PowerShell 5.1 and PowerShell 7 profiles exist and only one was bootstrapped
- WHEN `doctor` runs
- THEN it warns that the other edition's profile exists and was not bootstrapped, and writes nothing

#### Scenario: Stale GitSource checkout reported
- GIVEN `source.type: git` and the cached checkout is stale
- WHEN `doctor` runs
- THEN it reports the staleness and writes nothing

### Requirement: edit Opens $EDITOR Without Side Effects

`edit` MUST open `aliases.yaml` (or `config.yaml` with an explicit flag) in `$EDITOR` and MUST NOT trigger a sync automatically afterward.

#### Scenario: Edit and return
- GIVEN `$EDITOR` is set
- WHEN `aliasdeck edit` runs and the editor exits
- THEN no sync, render, or apply occurs as a side effect

### Requirement: uninstall Confirms and Restores

`uninstall` MUST prompt for confirmation unless a `--yes`/`-f` flag is passed, then remove the generated file and the bootstrap sentinel block, leaving all other user files byte-identical to before install.

#### Scenario: Interactive uninstall
- GIVEN a synced device
- WHEN `aliasdeck uninstall` runs without `--yes`
- THEN the user is prompted before any file is modified

#### Scenario: Non-interactive uninstall
- GIVEN `aliasdeck uninstall --yes`
- WHEN it runs
- THEN it removes the generated file and bootstrap line without prompting, and the rc file is otherwise unchanged
