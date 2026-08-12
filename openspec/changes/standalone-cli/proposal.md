# Proposal: Standalone CLI — Milestone 2 (v0.1)

## Intent

Milestone 1 shipped renderers and validation with no entry point; the README says the tool is unusable. Milestone 2 makes it installable: `init`, `sync`, aliases on disk for zsh and bash on macOS and Linux, zero server. Consistent with PROJECT.md §17 — no settled decision contradicted.

## Scope

### In Scope

- `internal/config` — `aliases.yaml` (§7.2) and `config.yaml` (§7.3) schemas, strict parsing, `enabled` defaults to `true`
- `internal/source` — `ConfigSource` interface, `FileSource` (read → filter → hash)
- `internal/apply` — `SyncBackend` interface, `NativeBackend`, atomic write (temp + rename), bootstrap line add/remove
- Sync state — revision, output hash, last sync time
- `cmd/aliasdeck` — `init`, `sync`, `status`, `list`, `doctor`, `edit`, `uninstall`
- Release — `.goreleaser.yaml`, Homebrew tap, install script

### Out of Scope

- PowerShell, Windows, Scoop, `GitSource` (M3)
- Server, API, database, web UI (M4+)
- `ChezmoiBackend` implementation — interface only (§11)
- Sync daemon, multi-source merging (§18)
- `aliasdeck config set`

## Capabilities

### New Capabilities

- `standalone-config`: file schemas, strict parsing, `enabled` default, device identity, platform/shell detection
- `config-source`: `ConfigSource` contract, `FileSource`, every source treated as hostile input
- `native-apply`: atomic write, bootstrap management, non-destructive reversible guarantees, `SyncBackend` seam
- `sync-state`: revision/hash/timestamp record, no-op skip when unchanged
- `cli-commands`: behaviour and output of the seven commands, including active-source reporting
- `release-distribution`: cross-compiled artifacts, tap formula, install script

### Modified Capabilities

- None — `openspec/specs/` is empty.

## Approach

One pipeline (§4.3), one package per stage: parse config → detect platform/shell → `FileSource.Resolve` → `validate.FilterValid` → `renderers.Render` → `NativeBackend.Apply` → record state. Cobra commands stay thin wrappers, keeping each stage table-driven testable with `t.TempDir()`. Sync is idempotent: unchanged revision plus matching on-disk hash means no write.

## Resolved Decisions

Settled before spec and design. Do not re-open these; treat them as inputs.

| Question | Decision | Rationale |
|----------|----------|-----------|
| Which shells does `sync` write? | **Only the device's detected shell.** | Writing files the user did not ask for contradicts §3.4. Single-shell keeps sync state to one hash and one bootstrap line. Multi-shell support is additive and non-breaking later. |
| Is there an `uninstall` command? | **Yes, first class.** | The non-destructive promise is the project's trust story, and it is only credible if removal is executable code with tests rather than a README paragraph. It is the command that makes the other six believable. |
| Meaning of top-level `profiles:` in `aliases.yaml` | **Declarative registry only.** An alias referencing an undeclared profile is a `doctor` warning, never a hard error. | A typo should degrade to a diagnostic, not refuse to sync the other forty aliases. |
| Bootstrap line on `init` | **Prompt before editing any rc file**, with `--no-bootstrap` for non-interactive installs. | Editing a user-owned file is the one destructive-adjacent act in v0.1 and must be consented to, not assumed. |
| `backend: chezmoi` in v0.1 | **Hard error** with an explicit "not implemented in v0.1" message. | Silently accepting a backend that does nothing would make a user believe their aliases were applied through Chezmoi when they were not. |

## Affected Areas

| Area | Impact |
|------|--------|
| `internal/config`, `source`, `apply`, `state` | New — pipeline stages |
| `cmd/aliasdeck/` | New — Cobra root, six commands |
| `.goreleaser.yaml`, `scripts/install.sh` | New — release tooling |
| `internal/domain`, `renderers`, `validate` | Unchanged — no golden or injection test edits expected |
| `README.md`, `go.mod` | Modified — status table, first runtime deps |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| `enabled` zero value disables every alias (`Alias.Enabled` is a plain `bool`; `AppliesTo` requires it) | High | Default `true` at parse; regression test with `enabled` omitted |
| Bootstrap edit damages a user-owned rc file | Medium | One sentinel-marked line, idempotent add, clean removal, fixture-rc tests |
| Interrupted sync leaves a truncated sourced file | Medium | Temp file in same directory, then rename |
| Shell or platform misdetected | Medium | Reported by `status`/`doctor`, overridable in `config.yaml` |
| Hostile `aliases.yaml` reaches disk | Medium | `validate.FilterValid` before render; renderer `guard` retained |

## Rollback Plan

Additive packages only, Milestone 1 untouched: revert the merge commit and `go test ./...` returns to baseline. On a machine, uninstall is deleting the generated file and removing one bootstrap line. No prior release exists, so a bad v0.1 is withdrawn by deleting the tag and the tap bump; nothing to migrate.

## Dependencies

- `spf13/cobra` (§9.2) plus one YAML parser — first runtime deps; parser choice belongs to design
- `goreleaser` in CI; a `homebrew-tap` repository must exist before release
- Real `bash`/`zsh` for the existing integration test

## Success Criteria

- [ ] `init` then `sync` produces a working generated file on clean macOS and Linux
- [ ] An entry omitting `enabled` renders
- [ ] `status` always names the active source; `doctor` reports each skipped alias with a reason
- [ ] A second `sync` with no upstream change performs no write
- [ ] `uninstall` leaves every user-owned file byte-identical to its pre-install state
- [ ] `make check` green, new packages ≥70% coverage
- [ ] Tagged v0.1 installs from the tap and the install script
