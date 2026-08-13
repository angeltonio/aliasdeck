# Delta for CLI Commands

## MODIFIED Requirements

### Requirement: status Always Reports the Active Source

`status` MUST report the active `ConfigSource` type and location, device identity, last sync time, and whether the generated file is current, on every invocation. On Windows, `status` MUST additionally report the detected PowerShell edition and the exact `$PROFILE` path bootstrapped. When the active source is `GitSource`, `status` MUST report the resolved ref and whether the checkout is stale.
(Previously: reported source/device/timestamp only; no edition or git ref/staleness fields.)

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

### Requirement: doctor Diagnoses Without Writing

`doctor` MUST report every alias skipped by validation with its reason, every undeclared-profile reference, and any platform/shell detection issue, and MUST NOT write to disk. On a device where both PowerShell editions' profiles exist but only one is bootstrapped, `doctor` MUST warn that the other edition's profile exists and is not bootstrapped. When the active source is `GitSource` and its checkout is stale, `doctor` MUST report the staleness.
(Previously: covered validation and profile-reference warnings only; no PowerShell edition or git-staleness diagnostics.)

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
