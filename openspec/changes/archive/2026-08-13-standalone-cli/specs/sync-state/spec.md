# Sync State Specification

## Purpose

Defines the local sync-state record (revision, output hash, timestamp) and the no-op skip that keeps repeated `sync` runs idempotent and deterministic (PROJECT.md §4.3, §12.2).

## Requirements

### Requirement: Sync State Is Recorded After Apply

After a successful `NativeBackend.Apply`, the system MUST persist the resolved revision, a hash of the rendered output, and the sync timestamp to local state.

#### Scenario: Successful sync updates state
- GIVEN a sync that applies successfully
- WHEN it completes
- THEN local state records the new revision, output hash, and timestamp

### Requirement: No-Op Skip When Unchanged

`sync` MUST skip writing the generated file when both (a) the newly resolved revision equals the stored revision and (b) the hash of the on-disk generated file equals the stored hash.

#### Scenario: Second sync with no upstream change
- GIVEN a device already synced with no change to `aliases.yaml`
- WHEN `sync` runs again
- THEN no write occurs and the command reports the device is up to date

#### Scenario: On-disk file manually altered
- GIVEN the generated file was hand-edited after a prior sync
- WHEN `sync` runs with an unchanged upstream revision
- THEN the on-disk hash mismatch forces a rewrite even though the revision is unchanged

### Requirement: Rendered Output Is Deterministic

Rendered output MUST NOT embed timestamps or other non-deterministic content, so that an unchanged `ResolvedConfig` always produces an identical hash across runs.

#### Scenario: Identical input yields identical hash
- GIVEN the same resolved config rendered twice
- WHEN the two outputs are hashed
- THEN the hashes are equal
