# Delta for CLI Commands

## ADDED Requirements

### Requirement: register Consumes a Single-Use Enrollment Token

`aliasdeck register` MUST exchange an operator-issued enrollment token for a device token, store the device token separately from `config.yaml` at `0600` where the OS supports it, and set `config.yaml`'s `source.type` to `server` with the given URL. It MUST NOT require or accept the operator password.

#### Scenario: Successful registration configures ServerSource
- GIVEN a valid unused enrollment token and a server URL
- WHEN `aliasdeck register` runs
- THEN a device token is stored separately from `config.yaml`, and `config.yaml`'s `source.type` becomes `server`

#### Scenario: Invalid or consumed token leaves config unchanged
- GIVEN an invalid or already-consumed enrollment token
- WHEN `aliasdeck register` runs
- THEN it exits non-zero, no device token is stored, and `config.yaml` is unchanged

### Requirement: login Authenticates the Operator

`aliasdeck login` MUST authenticate against the server using the operator password and MUST store the resulting session token separately from `config.yaml` and from any device token.

#### Scenario: Successful operator login stores a session
- GIVEN a running server and correct operator password
- WHEN `aliasdeck login` runs
- THEN it exits `0` and a session token is stored outside `config.yaml`

#### Scenario: Incorrect password rejected
- GIVEN an incorrect operator password
- WHEN `aliasdeck login` runs
- THEN it exits non-zero and no session token is stored

### Requirement: logout Clears the Locally Stored Session

`aliasdeck logout` MUST remove the locally stored operator session and MUST succeed even when the server is unreachable, since it only removes local state.

#### Scenario: logout removes local session
- GIVEN a stored operator session
- WHEN `aliasdeck logout` runs
- THEN the local session is removed and exits `0`

#### Scenario: logout succeeds without server reachability
- GIVEN a stored operator session and an unreachable server
- WHEN `aliasdeck logout` runs
- THEN it still removes the local session and exits `0`

## MODIFIED Requirements

### Requirement: status Always Reports the Active Source

`status` MUST report the active `ConfigSource` type and location, device identity, last sync time, and whether the generated file is current, on every invocation. On Windows, `status` MUST additionally report the detected PowerShell edition and the exact `$PROFILE` path bootstrapped. When the active source is `GitSource`, `status` MUST report the resolved ref and whether the checkout is stale. When the active source is `ServerSource`, `status` MUST report the server URL and MUST NOT display the device token.
(Previously: reporting covered `file` and `git` sources only; `server` now reports its URL with the token withheld.)

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

#### Scenario: status reports server URL without the token
- GIVEN `source.type: server`
- WHEN `status` runs
- THEN it reports `ServerSource` and its URL, and the device token value never appears in the output

### Requirement: edit Opens $EDITOR Without Side Effects

`edit` MUST open `aliases.yaml` (or `config.yaml` with an explicit flag) in `$EDITOR` and MUST NOT trigger a sync automatically afterward. When the active source is `server`, editing aliases has no local file to open; `edit` MUST fail with an explicit error pointing at the API instead of opening any file. `edit config` (the local `config.yaml`) remains permitted under a server source.
(Previously: this requirement assumed a local `aliases.yaml` always existed; it now names the explicit-error behavior under a server source.)

#### Scenario: Edit and return
- GIVEN `$EDITOR` is set and `source.type: file`
- WHEN `aliasdeck edit` runs and the editor exits
- THEN no sync, render, or apply occurs as a side effect

#### Scenario: Editing aliases under a server source is refused
- GIVEN `source.type: server`
- WHEN `aliasdeck edit` runs
- THEN it fails with an explicit error pointing at the API, and no file is opened

#### Scenario: Editing local config.yaml remains available under a server source
- GIVEN `source.type: server`
- WHEN `aliasdeck edit --config` runs
- THEN the local `config.yaml` opens in `$EDITOR` normally
