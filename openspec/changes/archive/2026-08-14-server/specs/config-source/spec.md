# Delta for Config Source

## MODIFIED Requirements

### Requirement: Exactly One Source Per Device

A device MUST resolve through exactly one `ConfigSource`, as declared by `config.yaml`'s `source.type` (`file`, `git`, or `server`). The system MUST NOT merge or fall back across multiple sources (§7.1), and `status` MUST always name the single active source and its type.
(Previously: `server` was accepted by the schema enum but rejected at resolution with "not supported in this version"; it is now a resolvable third arm, equal in strictness to `file` and `git`.)

#### Scenario: No automatic fallback
- GIVEN `source.type: file` configured
- WHEN the configured file is temporarily unavailable
- THEN sync fails with an error naming the unavailable source; no other source is attempted

#### Scenario: GitSource is the sole active source
- GIVEN `source.type: git` configured
- WHEN `sync` or `status` run
- THEN `status` names `GitSource` and its repository, and no other source is merged or attempted on failure

#### Scenario: ServerSource is the sole active source
- GIVEN `source.type: server` configured
- WHEN `sync` or `status` run
- THEN `status` names `ServerSource` and its URL, and no other source is merged or attempted on failure

### Requirement: Every Source Is Hostile Input

`FileSource`, `GitSource`, and `ServerSource` output MUST pass through `validate.FilterValid` before reaching `renderers.Render`, identically regardless of origin (§12.1). A sync response receives no lesser scrutiny than a local file. Name validation MUST match reserved words case-insensitively when the target shell is `powershell`, and duplicate-name detection MUST treat names as case-insensitive for `powershell` while remaining case-sensitive for POSIX shells.
(Previously: this requirement covered only `FileSource` and `GitSource`; `ServerSource` output is now bound by the identical rule.)

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

#### Scenario: Server-sourced hostile entry filtered identically
- GIVEN a sync response from `ServerSource` containing the same metacharacter-bearing name
- WHEN resolved
- THEN the entry is dropped by the same `validate.FilterValid` path used for `FileSource` and `GitSource`

#### Scenario: PowerShell name collision case-insensitively
- GIVEN aliases named `dps` and `DPS` on a device whose shell is `powershell`
- WHEN validated
- THEN duplicate detection reports a collision, and on a POSIX-shell device the same two names coexist without conflict

#### Scenario: PowerShell reserved word rejected case-insensitively
- GIVEN an alias named `Function` (mixed case) on a `powershell` device
- WHEN validated
- THEN it is rejected as a reserved word, matched case-insensitively
