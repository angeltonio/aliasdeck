# Server Sync Specification

## Purpose

Defines server-side resolution via `GET /api/v1/sync`, the neutral response contract, and the guarantee that no shell code ever leaves the server (PROJECT.md §6.1, §7, §12.2).

## Requirements

### Requirement: Server-Side Resolution Reuses domain.Resolve

`GET /api/v1/sync` MUST resolve the requesting device's aliases by loading them from the store and calling the same `domain.Resolve` function used by local resolution, filtered by platform, shell, and profile identically to standalone mode.

#### Scenario: Server resolution matches local resolution
- GIVEN identical alias data available both server-side and in an equivalent `aliases.yaml`
- WHEN each is resolved for the same device
- THEN both yield a `ResolvedConfig` with the same applicable aliases

### Requirement: Sync Response Contains No Shell Syntax or Server IDs

The sync response body MUST contain only neutral alias data — name, command, description and targeting — and MUST NOT contain rendered or shell-specific output, and MUST NOT include the server-side alias ID.

#### Scenario: Response has no shell syntax
- GIVEN any alias set
- WHEN `GET /api/v1/sync` responds
- THEN the response body contains no shell keywords, quoting, or escape sequences produced by a renderer

#### Scenario: Server alias ID absent
- GIVEN a sync response
- WHEN inspected
- THEN it contains no server-side alias identifier field

### Requirement: Identical Response Shape Across Shells and Platforms

The same aliases synced to devices with different platforms or shells MUST differ only in targeting (which aliases apply), never in shell-specific rendering, since rendering happens exclusively on the client.

#### Scenario: zsh and PowerShell devices receive equivalent neutral data
- GIVEN the same alias set on a device with shell `zsh` and a device with shell `powershell`
- WHEN each syncs
- THEN each response carries the same neutral alias fields for aliases applicable to both, and each client renders its own shell syntax locally

### Requirement: Client Owns Platform/Shell; Server Owns Profile Membership

On each sync, the server MUST update the device record's platform and shell from what the client reports, and MUST determine profile membership solely from server-side records, ignoring any client-supplied profile data.

#### Scenario: Client-reported shell change is recorded
- GIVEN a device that previously synced as `bash` and now reports `zsh`
- WHEN it syncs
- THEN the server updates its stored shell to `zsh`

#### Scenario: Client cannot alter its own profile membership
- GIVEN a sync request that includes a profile list
- WHEN the server resolves the device's aliases
- THEN it uses only server-stored profile membership, ignoring any client-supplied profile claim
