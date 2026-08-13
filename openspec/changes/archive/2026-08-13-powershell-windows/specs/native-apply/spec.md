# Delta for Native Apply

## MODIFIED Requirements

### Requirement: Bootstrap Line Management

`init` MUST insert exactly one sentinel-marked bootstrap line into the device's rc file to source the generated file, and this operation MUST be idempotent. On Windows, the bootstrap line MUST dot-source the generated `.ps1` file and MUST be written into only the detected PowerShell edition's `$PROFILE` (§6.4), never both.
(Previously: bootstrap targeted a single POSIX rc file; no edition-specific target existed.)

#### Scenario: First bootstrap insertion
- GIVEN an rc file with no AliasDeck sentinel
- WHEN `init` bootstraps it
- THEN exactly one sentinel-marked line referencing the generated file is appended

#### Scenario: Repeated init does not duplicate
- GIVEN an rc file already bootstrapped
- WHEN `init` runs again
- THEN the rc file still contains exactly one sentinel-marked bootstrap line

#### Scenario: PowerShell bootstrap targets one edition's $PROFILE
- GIVEN a Windows device with a detected PowerShell edition
- WHEN `init` bootstraps it
- THEN exactly one sentinel-marked dot-source line referencing the generated `.ps1` is appended to that edition's `$PROFILE` only

### Requirement: Uninstall Restores Byte-Identical Files

`uninstall` MUST delete the generated file and remove exactly the sentinel-marked bootstrap block from the rc file, leaving every other byte of user-owned files unchanged, including the file's original line-ending convention (LF or CRLF).
(Previously: byte-identical restoration was pinned for POSIX rc files; CRLF `$PROFILE` restoration was untested.)

#### Scenario: Full install/uninstall cycle
- GIVEN a device where `init` then `sync` have run
- WHEN `uninstall` runs
- THEN the rc file is byte-identical to its pre-install content and the generated file no longer exists

#### Scenario: CRLF $PROFILE restored byte-identically
- GIVEN a Windows `$PROFILE` using CRLF line endings before install
- WHEN `init` bootstraps it and `uninstall` later runs
- THEN `$PROFILE` is byte-identical to its pre-install content, including every CRLF

## ADDED Requirements

### Requirement: PowerShell Output File and Line Endings

The generated PowerShell file MUST use the `.ps1` extension and MUST be written with LF line endings unconditionally, regardless of the host platform's native convention, so that rendering the same `ResolvedConfig` produces byte-identical output on Windows, macOS and Linux.

#### Scenario: Cross-platform byte-identical output
- GIVEN the same resolved config
- WHEN rendered on Windows and on macOS
- THEN the two `.ps1` outputs are byte-identical and LF-terminated

#### Scenario: Unchanged config performs no write
- GIVEN a device already synced with no change to `aliases.yaml`
- WHEN `sync` runs again
- THEN no write occurs, consistent with the existing no-op skip
