# Native Apply Specification

## Purpose

Defines atomic write, shell bootstrap line management, the non-destructive/reversible guarantee, and the `SyncBackend` seam that lets `NativeBackend` ship in v0.1 while `ChezmoiBackend` stays an interface-only stub (PROJECT.md §3.4, §11, §12.2).

## Requirements

### Requirement: Atomic Write

`NativeBackend.Apply` MUST write rendered output to a temporary file in the target directory, then rename it into place. An interrupted apply MUST NOT leave a truncated generated file that a shell would source.

#### Scenario: Successful apply
- GIVEN valid rendered output
- WHEN `Apply` runs
- THEN the generated file exists with the exact rendered bytes

#### Scenario: Interruption before rename
- GIVEN an apply interrupted before the rename step
- WHEN the shell later sources the generated file
- THEN it sees either the prior valid file or no file — never a partial write

### Requirement: Bootstrap Line Management

`init` MUST insert exactly one sentinel-marked bootstrap line into the device's rc file to source the generated file, and this operation MUST be idempotent.

#### Scenario: First bootstrap insertion
- GIVEN an rc file with no AliasDeck sentinel
- WHEN `init` bootstraps it
- THEN exactly one sentinel-marked line referencing the generated file is appended

#### Scenario: Repeated init does not duplicate
- GIVEN an rc file already bootstrapped
- WHEN `init` runs again
- THEN the rc file still contains exactly one sentinel-marked bootstrap line

### Requirement: Non-Destructive to User Files

`NativeBackend` MUST NOT modify, reorder, or delete any content in a user-owned file other than its own sentinel-marked block (§3.4).

#### Scenario: Existing rc content preserved
- GIVEN an rc file with existing user content
- WHEN bootstrap runs
- THEN all pre-existing lines remain unchanged and in order, with the sentinel block appended

### Requirement: Uninstall Restores Byte-Identical Files

`uninstall` MUST delete the generated file and remove exactly the sentinel-marked bootstrap block from the rc file, leaving every other byte of user-owned files unchanged.

#### Scenario: Full install/uninstall cycle
- GIVEN a device where `init` then `sync` have run
- WHEN `uninstall` runs
- THEN the rc file is byte-identical to its pre-install content and the generated file no longer exists

### Requirement: SyncBackend Seam

Apply MUST be dispatched through a `SyncBackend` interface selected by `config.yaml`'s `backend` field. `NativeBackend` is the only backend implemented in v0.1.

#### Scenario: Native backend selected
- GIVEN `backend: native`
- WHEN sync applies
- THEN `NativeBackend.Apply` handles the write

### Requirement: Chezmoi Backend Fails Explicitly

Selecting `backend: chezmoi` MUST fail sync with an explicit "not implemented in v0.1" error. It MUST NOT write a generated file or edit any rc file.

#### Scenario: Chezmoi backend configured
- GIVEN `config.yaml` with `backend: chezmoi`
- WHEN `sync` runs
- THEN it exits non-zero with an explicit not-implemented message, and no generated file or bootstrap edit occurs
