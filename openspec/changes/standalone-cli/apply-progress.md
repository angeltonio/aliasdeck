# Apply Progress: standalone-cli

## Batch 1 — Phase 1: Config Foundation (tasks 1.1–1.9)

**Mode**: Strict TDD
**Branch**: `feat/standalone-cli`
**Status**: 9/9 Phase 1 tasks complete.

### Completed Tasks

- [x] 1.1 `go.mod`/`go.sum`: added `go.yaml.in/yaml/v3 v3.0.5` (direct dep, actually imported). `github.com/spf13/cobra v1.10.2` was added via `go get` but removed by `go mod tidy` because nothing in the tree imports it yet — see Deviations.
- [x] 1.2 RED `internal/config/paths_test.go`
- [x] 1.3 GREEN `internal/config/paths.go` — `Env`, `OSEnv()`, `Base()`, `ConfigFile`/`AliasesFile`/`StateFile`, `ExpandPath`
- [x] 1.4 RED `internal/config/aliases_test.go`
- [x] 1.5 GREEN `internal/config/aliases.go` — `ParseAliases`, `AliasesDocument`, `ProfileWarnings`
- [x] 1.6 RED `internal/config/device_test.go`
- [x] 1.7 GREEN `internal/config/device.go` — `ParseDeviceConfig`, `DeviceFileConfig`, `Load`/`Write`, `Backend`/`SourceType` enums
- [x] 1.8 RED `internal/config/detect_test.go`
- [x] 1.9 GREEN `internal/config/detect.go` — `DetectPlatform`, `DetectShell`, `PlatformDetection`/`ShellDetection`

### Files Changed (Batch 1)

| File | Action | What Was Done |
|------|--------|----------------|
| `go.mod`, `go.sum` | Modified | Added `go.yaml.in/yaml/v3 v3.0.5` as a direct dependency |
| `internal/config/paths.go` | Created | `Env` abstraction (`Getenv`, `HomeDir`), `OSEnv()`, `Base()` with `$ALIASDECK_HOME → $XDG_CONFIG_HOME/aliasdeck → ~/.config/aliasdeck` precedence, per-file path helpers, `ExpandPath` for `~`/`$HOME` |
| `internal/config/paths_test.go` | Created | Table-driven precedence tests, tilde expansion, `os.UserConfigDir` regression test, `HomeDir` error propagation, `OSEnv` wiring test |
| `internal/config/aliases.go` | Created | `ParseAliases` (strict `KnownFields(true)`, 1 MiB cap, `version` check), `aliasDTO`→`domain.Alias` mapping (`Enabled *bool` default, `profiles:`→`ProfileIDs`, `ID` from `Name`), `ProfileWarnings` |
| `internal/config/aliases_test.go` | Created | Valid-file parse (matches PROJECT.md §7.2 example), `enabled` default table, unknown-field rejection (alias-level and top-level), wrong-version rejection, oversize rejection, `ProfileWarnings` table |
| `internal/config/device.go` | Created | `ParseDeviceConfig` (strict parse, `Backend`/`SourceType` enums, unknown-backend rejection), `Load` (read + parse + stable fallback identity generation + persist), `Write` (0600) |
| `internal/config/device_test.go` | Created | Valid-file parse (matches PROJECT.md §7.3 example), unknown-backend rejection, unknown-field rejection, fallback-identity stability across two `Load` calls, `Write`→`Load` round-trip with mode assertion |
| `internal/config/detect.go` | Created | `DetectPlatform` (config override → `$ALIASDECK_PLATFORM` → `runtime.GOOS` map), `DetectShell` (flag → config → `$ALIASDECK_SHELL` → `$SHELL` basename with login-dash stripped → `domain.DefaultShellFor`), both carrying `Provenance` |
| `internal/config/detect_test.go` | Created | Full precedence tables for both platform and shell detection, including the unsupported-shell-is-an-error scenario |
| `openspec/changes/standalone-cli/tasks.md` | Modified | Marked 1.1–1.9 `[x]` |

### TDD Cycle Evidence (Batch 1)

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| 1.2/1.3 | `internal/config/paths_test.go` | Unit | N/A (new) | ✅ Written (compile failure: `undefined: Env/Base/...`) | ✅ Passed (`go test ./internal/config/...`) | ✅ 3 `Base` precedence cases + tilde + UserConfigDir regression + HomeDir error + per-file paths + `ExpandPath` table | ✅ Added `OSEnv` real-behavior test after initial pass to close a coverage gap; no logic changes needed |
| 1.4/1.5 | `internal/config/aliases_test.go` | Unit | ✅ ran `go test ./internal/config/...` before adding (only paths_test present, 12/12 passing) | ✅ Written (compile failure: `undefined: ParseAliases/ProfileWarnings`) | ✅ Passed | ✅ 3 `enabled` cases, 2 unknown-field cases (alias-level + top-level, tightened top-level case to avoid a trivial substring overlap), version + oversize cases, 2 `ProfileWarnings` cases | ➖ None needed — code was already minimal and clean |
| 1.6/1.7 | `internal/config/device_test.go` | Unit | ✅ ran full config suite before adding (23/23 passing) | ✅ Written (compile failure: `undefined: ParseDeviceConfig/Load/Write/...`) | ✅ Passed | ✅ Valid file, unknown backend, unknown field, fallback-identity stability, write→load round-trip with mode check | ➖ None needed |
| 1.8/1.9 | `internal/config/detect_test.go` | Unit | ✅ ran full config suite before adding (28/28 passing) | ✅ Written (compile failure: `undefined: DetectPlatform/DetectShell`) | ✅ Passed | ✅ 6 platform-precedence cases, 7 shell-precedence cases (including unsupported-shell-is-an-error) | ➖ None needed |

### Deviations from Design (Batch 1)

1. **`github.com/spf13/cobra` is not currently in `go.mod`.** Deferred to the batch that writes `cmd/aliasdeck/main.go`, per the constraint that `go mod tidy` strips unimported requirements.
2. **`config.yaml` `source.type` is not validated against the `file|git|server` enum at parse time.** Deferred to `internal/source` (this batch), which actually acts on it — see Batch 2 below.
3. **Device identity fallback generation** originally used random bytes plus a hostname-derived label (`sanitizeHostname`), per the codebase as read at the start of this batch.

None of the Batch 1 work touches `internal/domain`, `internal/validate`, or `internal/renderers`.

---

## Batch 2 — Phase 2: Source, Apply, State (tasks 2.1–2.10)

**Mode**: Strict TDD
**Branch**: `feat/standalone-cli`
**Status**: 10/10 Phase 2 tasks complete. Ready for the next apply batch (Phase 3).

### Completed Tasks

- [x] 2.1 RED `internal/source/file_test.go`: configured-path-only, resolve error not partially applied, hostile alias name/oversized command filtered (threat matrix "Hostile aliases.yaml")
- [x] 2.2 GREEN `internal/source/source.go`, `file.go`: `ConfigSource`, `Descriptor`, `FileSource.Resolve` (reads → `config.ParseAliases` → `domain.Resolve` → `validate.FilterValid`, discarding `Issues` since the interface returns only `(ResolvedConfig, error)`; `doctor`, in Phase 3, performs its own independent read+validate pass for diagnostics)
- [x] 2.3 RED `internal/apply/atomic_test.go`: success + mode, symlink refusal, directory refusal, temp-file cleanup on a forced rename failure (threat matrix "Output path")
- [x] 2.4 GREEN `internal/apply/atomic.go`: `writeFileAtomic` — MkdirAll → Lstat refusal check → CreateTemp (same dir) → Chmod → Write → Sync → Close → Rename, with a deferred `Remove(tmp)` and a package-level `osRename` seam for deterministic failure-injection tests
- [x] 2.5 RED `internal/apply/bootstrap_test.go`: rc fixtures (trailing newline / no trailing newline / empty file / file absent), idempotent add, manually-crafted pre-existing block no-op, exact-byte removal, user-edited-block fallback removal, symlinked rc preserved through both add and remove, three hostile-marker-text-not-corrupted cases (threat matrix "rc file mutation")
- [x] 2.6 GREEN `internal/apply/bootstrap.go`: `BootstrapLine` (POSIX `[ -f ... ] && . ...` guard, `$HOME`-relative when applicable), `AddBootstrap`/`RemoveBootstrap`, `buildBlock` (exact padding+separator+markers per design decision 6), `removeMarkerScan`+`indexOfLine` (whole-line-only marker matching, consumes the separator blank line too when present so the common fallback case is also byte-identical), `resolveRCPath` using `filepath.EvalSymlinks`
- [x] 2.7 RED `internal/apply/native_test.go`: `NativeBackend` output-path mapping (zsh/bash + unsupported-shell error), `Apply` happy path, no-partial-write on a forced rename interruption (prior valid content preserved, no leftover temp file), `ChezmoiBackend` hard error on both `OutputPath` and `Apply`, interface-satisfaction check
- [x] 2.8 GREEN `internal/apply/backend.go`, `native.go`: `SyncBackend{Name,OutputPath,Apply}`, `NativeBackend{Base}` (writes `<Base>/aliases.<ext>` via `writeFileAtomic`), `ChezmoiBackend` — every method returns `errChezmoiNotImplemented` (`backend "chezmoi" is not implemented in v0.1`)
- [x] 2.9 RED `internal/state/state_test.go`: round-trip (with and without `Bootstrap`), file mode 0600, missing-file tolerance, corrupt-JSON tolerance, overwrite leaves no leftover temp file, `Save` directory-creation failure, `Save` temp-file-creation failure (read-only dir), `Load` propagates a genuine (non-`IsNotExist`) read error
- [x] 2.10 GREEN `internal/state/state.go`: `State`, `Bootstrap`, `Load` (tolerant of missing/corrupt, propagates other read errors), `Save` (atomic temp+rename, 0600)

### Files Changed (Batch 2)

| File | Action | What Was Done |
|------|--------|----------------|
| `internal/source/source.go` | Created | `ConfigSource` interface, `Descriptor` struct |
| `internal/source/file.go` | Created | `FileSource{Path}`: `Descriptor()`, `Resolve()` — read → `config.ParseAliases` → `domain.Resolve` → `validate.FilterValid` |
| `internal/source/file_test.go` | Created | Configured-path-only test (with a decoy file proving no merge), resolve-error-not-partially-applied table (missing file, malformed YAML), hostile-input-filtered test (invalid name + oversized command), `Descriptor()` test |
| `internal/apply/atomic.go` | Created | `writeFileAtomic`, `refuseUnsafeDestination`, `writeSyncClose`, package-level `osRename` seam |
| `internal/apply/atomic_test.go` | Created | Success+mode+no-leftover-tmp, symlink refusal (target untouched), directory refusal, forced-rename-failure cleanup |
| `internal/apply/bootstrap.go` | Created | `beginMarker`/`endMarker`, `BootstrapLine`, `AddBootstrap`, `RemoveBootstrap`, `resolveRCPath`, `buildBlock`, `removeMarkerScan`, `indexOfLine` |
| `internal/apply/bootstrap_test.go` | Created | `BootstrapLine` table (home-relative, outside-home, prefix-collision guard), `AddBootstrap` fixture table (4 rc shapes), idempotence test, manually-crafted-block no-op test, exact-byte-restore round trip, user-edited-block fallback test, symlinked-rc-stays-symlink test (add + remove), 3 hostile-marker-not-corrupted cases |
| `internal/apply/native.go` | Created | `NativeBackend{Base}` (`Name`, `OutputPath`, `Apply`), `shellFileExt`, `ChezmoiBackend` + `errChezmoiNotImplemented` |
| `internal/apply/native_test.go` | Created | `OutputPath` table (zsh/bash/unsupported), `Apply` happy path, no-partial-write-on-interruption, `ChezmoiBackend` hard-error test, `SyncBackend` interface-satisfaction check |
| `internal/apply/backend.go` | Created | `SyncBackend` interface |
| `internal/state/state.go` | Created | `State`, `Bootstrap`, `Load`, `Save`, `writeSyncCloseState` |
| `internal/state/state_test.go` | Created | Round-trip (with/without `Bootstrap`), mode-0600 check, missing-file tolerance, corrupt-JSON tolerance, overwrite-no-leftover-tmp, `Save` MkdirAll-failure test, `Save` CreateTemp-failure test (read-only dir, skipped as root), `Load` non-`IsNotExist` read-error propagation test (skipped as root) |
| `openspec/changes/standalone-cli/tasks.md` | Modified | Marked 2.1–2.10 `[x]` |

### TDD Cycle Evidence (Batch 2)

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| 2.1/2.2 | `internal/source/file_test.go` | Unit | ✅ ran `go test ./...` before adding (Phase 1 suite green) | ✅ Written first; confirmed compile failure `undefined: FileSource` (4 occurrences) before any production code existed | ✅ Passed on first implementation | ✅ configured-path-only + decoy file, 2-case error table, hostile-name + oversized-command in one fixture, `Descriptor()` case | ➖ None needed |
| 2.3/2.4 | `internal/apply/atomic_test.go` | Unit | ✅ ran `go test ./internal/apply/...` before adding (package did not exist yet) | ✅ Written first; confirmed compile failure `undefined: writeFileAtomic`/`osRename` (7 occurrences) | ✅ Passed on first implementation | ✅ success+mode+MkdirAll-of-nested-dir, symlink refusal, directory refusal, forced-rename-failure cleanup via the `osRename` seam | ➖ None needed |
| 2.5/2.6 | `internal/apply/bootstrap_test.go` | Unit | ✅ ran `go test ./internal/apply/...` before adding (atomic tests green, 4/4) | ✅ Written first; confirmed compile failure `undefined: BootstrapLine/AddBootstrap/beginMarker/...` (10+ occurrences) | ⚠️ First implementation failed 1/12 (`TestRemoveBootstrapFallsBackWhenUserEditedInsideBlock`: fallback left one leftover separator newline) | ✅ 4 rc-shape fixtures × assertions, idempotence, manually-crafted-block no-op, exact restore, user-edited fallback, symlink preserved on both add and remove, 3 hostile-marker-text cases | ✅ Extended `removeMarkerScan` to also consume the immediately-preceding blank separator line when present, making the common fallback case byte-identical too, not only the exact-block-match case — re-ran, 12/12 passing |
| 2.7/2.8 | `internal/apply/native_test.go` | Unit | ✅ ran `go test ./internal/apply/...` before adding (bootstrap tests green, 12/12) | ✅ Written first; confirmed compile failure `undefined: NativeBackend/ChezmoiBackend/SyncBackend` (9 occurrences) | ✅ Passed on first implementation | ✅ 2-shell output-path table + unsupported-shell case, happy-path apply, forced-interruption-preserves-prior-content, chezmoi hard-error on both methods, interface-satisfaction assertion | ➖ None needed |
| 2.9/2.10 | `internal/state/state_test.go` | Unit | ✅ ran `go test ./...` before adding (Phase 1 + Phase 2 source/apply all green) | ✅ Written first; confirmed compile failure `undefined: State/Bootstrap/Save/Load` (10+ occurrences) | ✅ Passed on first implementation, but package coverage was 64.9% (below the 70% floor) | ✅ round trip ×2 (with/without Bootstrap pointer), mode check, missing/corrupt tolerance, overwrite-no-leftover-tmp | ✅ Added 3 more tests (MkdirAll failure via a file-blocking-a-directory path; CreateTemp failure via a read-only dir; `Load` propagating a genuine permission-denied read error) purely to close the coverage gap — no production logic changed, coverage rose to 73.0% |

### Design Interpretation Notes (not deviations — the design text was ambiguous on one point)

The design's pipeline diagram shows `source.FileSource.Resolve ─→ domain.Resolve` feeding into a branch that also shows `validate.FilterValid ─→ renderers.Render ─→ apply.NativeBackend`. Task 2.2's own wording ("`FileSource.Resolve` → `validate.FilterValid` → `renderers.Render`") could be read either as "these three calls all happen inside `file.go`" or as "this is the pipeline `file.go`'s output eventually feeds." Since `ConfigSource.Resolve`'s signature (per the design's own Interfaces section) returns only `(domain.ResolvedConfig, error)` — no `validate.Issues` — and the architecture decision table states rendering happens "only from the client and only from `internal/renderers`," I implemented `FileSource.Resolve` to call `domain.Resolve` then `validate.FilterValid` internally (discarding `Issues`), and left `renderers.Render` for the sync use case (Phase 3) to call on the already-filtered result. This satisfies the config-source spec's literal requirement ("`FileSource` output MUST pass through `validate.FilterValid` before reaching `renderers.Render`") by construction: whatever `Resolve` returns is already filtered, so nothing unfiltered can ever reach a renderer through this source. `doctor`'s hostile-entry diagnostics (Phase 3, task 3.5) will need their own independent read+validate pass to surface the dropped issues, exactly as `ProfileWarnings` already works independently of `ParseAliases`'s own return value.

### Work Unit Evidence (Batch 2)

| Evidence | Value |
|---|---|
| Focused test command and exact result | `go test ./internal/source/... ./internal/apply/... ./internal/state/... -v` → all PASS, 0 failures (33 top-level test functions, many with `t.Run` subtests) |
| Runtime harness command/scenario and exact result | N/A for this batch — no CLI or use-case boundary exists yet (Phase 3). The closest runtime-adjacent proof is the atomic-write and bootstrap tests exercising real filesystem operations (`t.TempDir()`, real symlinks, real permission bits) rather than mocks |
| Rollback boundary | Delete `internal/source/`, `internal/apply/`, `internal/state/`; revert the `[x]` marks for 2.1–2.10 in `tasks.md`. `go.mod`/`go.sum` are untouched by this batch (verified via `git diff --stat go.mod go.sum` — no output). No other package imports these three, so this is a clean, isolated revert |

### Test Summary (Batch 2)

- **Total tests written**: 15 top-level test functions across 3 packages (many with `t.Run` subtests); 45+ individual subtest/table cases
- **Total tests passing**: all (`go test ./internal/source/... ./internal/apply/... ./internal/state/... -v` — zero failures)
- **Layers used**: Unit (15), Integration (0), E2E (0) — matches design's "Layer: Unit" for every Phase 2 item; no CLI or use-case boundary exists yet
- **Approval tests**: None — no refactoring of existing behavior, only new files
- **Coverage per package**:
  - `internal/source`: **100.0%** of statements
  - `internal/apply`: **82.5%** of statements
  - `internal/state`: **73.0%** of statements
  - All three clear the ≥70% floor from `openspec/config.yaml`

### Deviations from Design (Batch 2)

None that change behavior. See "Design Interpretation Notes" above for the one place the design text needed a resolved reading rather than a literal one, and the note on `internal/state/state.go` duplicating a small atomic-write helper (`writeSyncCloseState`) rather than importing `internal/apply`'s `writeFileAtomic` — the design's File Changes table lists `internal/state` and `internal/apply` as independent, dependency-free packages, and reusing `apply`'s helper would introduce a `state → apply` import that nothing in the pipeline diagram calls for.

### Issues Found (Batch 2)

None. `internal/domain`, `internal/validate`, and `internal/renderers` remain completely untouched — confirmed by `git status --short` (only `internal/apply/`, `internal/source/`, `internal/state/` appear as new, untracked directories) and by their coverage numbers staying exactly at 70.4%/89.1%/87.7%, identical to the Batch 1 baseline.

### Remaining Tasks

- [ ] Phase 3: App Use Cases & CLI Wiring (tasks 3.1–3.12)
- [ ] Phase 4: Milestone-1-Adjacent Verification (tasks 4.1–4.4)
- [ ] Phase 5: Release Tooling (tasks 5.1–5.3)
- [ ] Phase 6: Docs & Config Sync (tasks 6.1–6.5)

### Workload / PR Boundary

- Mode: stacked-to-main (per tasks.md "Suggested Work Units"; chain strategy still `pending` in the forecast table as of this batch — the orchestrator resolved delivery for this batch's execution but the tasks.md forecast header text has not been rewritten)
- Current work unit: Unit 3 — "`internal/source` (`ConfigSource`, `FileSource`)" plus Unit 4 — "`internal/apply` (atomic write, bootstrap, `NativeBackend`)" plus Unit 5 — "`internal/state` (state.json round-trip)". All three landed together in this batch since Phase 2 is internally small and each sub-package is independently testable and revertable (see Rollback boundary above); no cross-package coupling was introduced beyond the documented `source → config`, `source → validate`, `apply → domain` imports the design already specifies.
- Boundary: starts from the Batch 1 tree (Phase 1 `internal/config` complete, `internal/domain`/`internal/validate`/`internal/renderers` untouched); ends with `internal/source`, `internal/apply`, `internal/state` fully implemented and tested, `go.mod`/`go.sum` still untouched, no `cmd/` or `internal/app` created.
- Estimated review budget impact: 3 new packages, ~9 new files (6 production + a few more test files), no existing code modified outside `tasks.md`/`apply-progress.md`. New code only; well within a single PR's budget for this unit, consistent with the tasks.md estimate of PR 3/4/5 being independently mergeable.

### Status

19/19 tasks complete across Phases 1–2 (9/9 + 10/10). Ready for the next apply batch (Phase 3: `internal/app` use cases + `cmd/aliasdeck` Cobra wiring — this is the batch that must re-add `github.com/spf13/cobra` to `go.mod`, per Batch 1's Deviation #1) or `sdd-verify` if the orchestrator wants to verify Phases 1–2 together first.
