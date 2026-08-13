# Delta for Config Source

## MODIFIED Requirements

### Requirement: Exactly One Source Per Device

A device MUST resolve through exactly one `ConfigSource`, as declared by `config.yaml`'s `source.type` (`file`, `git`, or `server`). The system MUST NOT merge or fall back across multiple sources (§7.1), and `status` MUST always name the single active source and its type.
(Previously: only `file` was an implemented alternative; the no-fallback rule did not yet cover `git`.)

#### Scenario: No automatic fallback
- GIVEN `source.type: file` configured
- WHEN the configured file is temporarily unavailable
- THEN sync fails with an error naming the unavailable source; no other source is attempted

#### Scenario: GitSource is the sole active source
- GIVEN `source.type: git` configured
- WHEN `sync` or `status` run
- THEN `status` names `GitSource` and its repository, and no other source is merged or attempted on failure

### Requirement: Every Source Is Hostile Input

`FileSource` and `GitSource` output MUST pass through `validate.FilterValid` before reaching `renderers.Render`, identically regardless of origin (§12.1). A local file receives no lesser scrutiny than a Git or server source would. Name validation MUST match reserved words case-insensitively when the target shell is `powershell`, and duplicate-name detection MUST treat names as case-insensitive for `powershell` while remaining case-sensitive for POSIX shells.
(Previously: hostile-input coverage only exercised `FileSource`; case sensitivity was not part of the pinned contract.)

#### Scenario: Invalid alias name filtered
- GIVEN a local `aliases.yaml` containing an alias name with a shell metacharacter
- WHEN resolved
- THEN the entry is dropped by `validate.FilterValid` before rendering, not written to disk

#### Scenario: Oversized payload bounded
- GIVEN an alias command exceeding the validated size bound
- WHEN resolved
- THEN the entry is rejected by validation, not truncated or passed through

#### Scenario: Git-sourced hostile entry filtered identically
- GIVEN an `aliases.yaml` checked out via `GitSource` containing the same metacharacter-bearing name
- WHEN resolved
- THEN the entry is dropped by the same `validate.FilterValid` path used for `FileSource`

#### Scenario: PowerShell name collision case-insensitively
- GIVEN aliases named `dps` and `DPS` on a device whose shell is `powershell`
- WHEN validated
- THEN duplicate detection reports a collision, and on a POSIX-shell device the same two names coexist without conflict

#### Scenario: PowerShell reserved word rejected case-insensitively
- GIVEN an alias named `Function` (mixed case) on a `powershell` device
- WHEN validated
- THEN it is rejected as a reserved word, matched case-insensitively

## ADDED Requirements

### Requirement: GitSource Implements ConfigSource

`GitSource` MUST implement `ConfigSource` by shelling out to the user's `git` binary to obtain a checkout of the configured repository under the base directory, and MUST resolve `aliases.yaml` from that checkout through the same filter-by-device pipeline as `FileSource`. `source.git.ref`, when omitted, MUST resolve to the remote's default branch.

#### Scenario: Resolve via cached checkout
- GIVEN `source.type: git` with a reachable remote
- WHEN `sync` runs
- THEN `git` clones or fetches into a cached checkout and `aliases.yaml` is resolved from it, filtered by platform, shell and profiles

#### Scenario: Ref defaults to the remote's default branch
- GIVEN `source.git.ref` is omitted
- WHEN resolved
- THEN the remote's default branch is used

### Requirement: GitSource Offline Behavior and Staleness

When the configured remote is unreachable, `GitSource` MUST resolve using the last successful checkout instead of failing `sync`, and MUST report that checkout as stale via `status` and `doctor` rather than presenting it as current. If no prior successful checkout exists, `sync` MUST fail with an actionable error naming the source.

#### Scenario: Unreachable remote with a prior checkout
- GIVEN a prior successful checkout and an unreachable remote
- WHEN `sync` runs
- THEN it succeeds using the cached checkout, and `status`/`doctor` report staleness

#### Scenario: Unreachable remote with no prior checkout
- GIVEN no prior successful checkout and an unreachable remote
- WHEN `sync` runs
- THEN it fails with an actionable error naming the source; no partial state is recorded

### Requirement: GitSource aliases.yaml Path Resolution

`source.git.path` MUST be OPTIONAL. When omitted, `GitSource` MUST resolve `aliases.yaml` at the checkout root, mirroring `FileSource`'s existing path-omitted default. When present, the resolved path MUST lie inside the checkout; a path that would escape it MUST be rejected before the file is read.

#### Scenario: Path omitted resolves aliases.yaml at the checkout root
- GIVEN `source: {type: git, url: <repo>}` with no `path`
- WHEN resolved
- THEN `aliases.yaml` at the checkout root is read

#### Scenario: Path present resolves relative to the checkout root
- GIVEN `source: {type: git, url: <repo>, path: config/aliases.yaml}`
- WHEN resolved
- THEN `<checkout>/config/aliases.yaml` is read

#### Scenario: Path escaping the checkout is rejected
- GIVEN `source: {type: git, url: <repo>, path: ../../etc/passwd}`
- WHEN resolved
- THEN resolution fails before any file is read; no content outside the checkout is ever accessed

### Requirement: GitSource Is Read-Only in v0.2

`edit` MUST NOT commit, push, or otherwise write to the Git remote when `source.type: git` is configured.

#### Scenario: Editing a git-sourced file performs no git write
- GIVEN `source.type: git`
- WHEN `aliasdeck edit` opens the checked-out `aliases.yaml` and the editor exits
- THEN AliasDeck performs no commit or push
