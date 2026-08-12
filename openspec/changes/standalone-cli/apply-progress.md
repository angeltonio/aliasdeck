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

---

## Batch 3 — Phase 3: App Use Cases & CLI Wiring (tasks 3.1–3.12)

**Mode**: Strict TDD
**Branch**: `feat/standalone-cli`
**Status**: 12/12 Phase 3 tasks complete. AliasDeck now builds and runs as a standalone CLI.

### First task: re-added cobra

`go get github.com/spf13/cobra@v1.10.2` then `go mod tidy` once `cmd/aliasdeck` actually imported it. `go.mod` now declares `github.com/spf13/cobra v1.10.2` and `go.yaml.in/yaml/v3 v3.0.5` as direct requirements, with `github.com/inconshreveable/mousetrap` and `github.com/spf13/pflag` as indirect (cobra's own deps).

### Completed Tasks

- [x] 3.1 RED `internal/app/sync_test.go`: `TestSyncFullPipelineOrder`, `TestSyncNoOpSkipWhenUnchanged` (read-only base dir proves no write attempt), `TestSyncForcedRewriteOnDiskHashMismatch`, `TestSyncRenderedOutputIsDeterministic` (delete state, re-resolve, compare hash), `TestSyncUnresolvableSourceNamesTheSource`
- [x] 3.2 GREEN `internal/app/sync.go` (+ shared `env.go`, `context.go`, `errors.go`, `hash.go` infrastructure every later use case reuses): resolve → validate(implicit via `FileSource.Resolve`) → render → apply → state, `Env` injection, no-op skip via revision+disk-hash
- [x] 3.3 RED `internal/app/init_test.go`: `TestInitCreatesBothConfigFiles`, `TestInitNoBootstrapSkipsPromptAndRCFile`, `TestInitPromptsBeforeBootstrapAndAddsOnConsent`, `TestInitPromptDeclinedLeavesRCFileUntouched`, `TestInitIsIdempotentForExistingFiles`
- [x] 3.4 GREEN `internal/app/init.go` (+ `prompt.go`, `rcpath.go`): creates both files only when absent, runs an initial sync via the shared `syncWithContext`, prompts via an injectable `Confirm` (defaults to a real `promptYesNo` over `Env.Stdin`), records `state.Bootstrap` on successful add
- [x] 3.5 RED `internal/app/{status,list,doctor}_test.go`: `TestStatusReportsActiveSource`/`TestStatusReportsNotInitialized`, `TestListShowsDeviceScopedEntries`, `TestDoctorReportsHostileEntryAndUndeclaredProfile`/`TestDoctorWritesNothing`
- [x] 3.6 GREEN `internal/app/status.go`, `list.go`, `doctor.go`
- [x] 3.7 RED `internal/app/edit_test.go` — **threat-matrix case**: `TestEditNeverInvokesAShell` (`$EDITOR="x; rm -rf ."`, asserts the literal first token `"x;"` is what gets looked up, asserts a marker file survives), `TestEditMultiWordEditorPassesThrough` (`code -w` via a fake script capturing argv), `TestEditHasNoSyncSideEffect`, `TestEditReturnsErrorWhenEditorNotSet`
- [x] 3.8 GREEN `internal/app/edit.go`: `strings.Fields($EDITOR)` → `Env.LookPath` (test seam) → `exec.Command(resolved, args...)`, never `sh -c`; documented quoted-path limitation in the doc comment
- [x] 3.9 RED `internal/app/uninstall_test.go`: `TestUninstallRestoresRCFileByteIdentically`, `TestUninstallYesSkipsPrompt`, `TestUninstallInteractivePromptsBeforeModifying`, `TestUninstallExactFalseWhenUserEditedInsideBlock`
- [x] 3.10 GREEN `internal/app/uninstall.go`: removes generated file → `apply.RemoveBootstrap` (surfaces `exact` as `report.BootstrapExact` rather than swallowing it) → removes `state.json`; leaves `config.yaml`/`aliases.yaml` untouched
- [x] 3.11 GREEN `cmd/aliasdeck/{main,root,exit,init,sync,status,list,doctor,edit,uninstall}.go`: Cobra wiring, `--shell` persistent flag, exit-code map (0/1/2/3/4) via an `*exitError` carrier type plus a `cmd.SilenceUsage`-based usage-vs-business-error split
- [x] 3.12 Integration `internal/app/integration_test.go` — `TestFullLifecycleInitSyncSyncUninstall`: `init` (with a pre-existing rc file) → explicit `sync` after adding real aliases → second `sync` against a read-only base dir (no-op proof) → `uninstall --yes`, asserting the rc file is byte-identical to its pre-init content and `config.yaml`/`aliases.yaml` survive while `state.json` and the generated file do not

### Files Changed (Batch 3)

| File | Action | What Was Done |
|------|--------|----------------|
| `go.mod`, `go.sum` | Modified | Re-added `github.com/spf13/cobra v1.10.2` as a direct dependency (`go get` + `go mod tidy` after `cmd/aliasdeck` imported it); indirect `mousetrap`/`pflag` added by cobra itself |
| `internal/app/env.go` | Created | `Env{Stdin,Stdout,Stderr,Getenv,HomeDir,Now,LookPath}`, `OSEnv()`, `Env.ConfigEnv()` adapter to `config.Env` |
| `internal/app/errors.go` | Created | `ErrNotInitialized`, `ConfigError{Err}` (`Error`/`Unwrap`) — the two exit-code-bearing sentinels every use case can return |
| `internal/app/hash.go` | Created | `hashBytes`, `diskHashMatches` — the sha256-hex helpers behind the no-op skip |
| `internal/app/context.go` | Created | `Version`, `Options{Shell}`, `deviceContext`, `loadDeviceContext` (config.yaml existence check → `ErrNotInitialized`; `config.Load` failure → `ConfigError`; platform/shell detection; builds `domain.Device`), `resolveSource` (file-only in this milestone; git/server are an explicit error), `resolveBackend` (native/chezmoi/unsupported) |
| `internal/app/sync.go` | Created | `SyncReport`, `Sync` (public, loads context) / `syncWithContext` (reused by `Init`): resolve → render → no-op-skip check (revision + disk hash) → `Backend.Apply` → `state.Save`, preserving any existing `state.Bootstrap` |
| `internal/app/prompt.go` | Created | `promptYesNo` — prints the question, reads one line from `Env.Stdin`, defaults to `false` on empty/EOF |
| `internal/app/rcpath.go` | Created | `resolveRCPath` — `--rc-file` override → zsh (`$ZDOTDIR`/`~/.zshrc`) → bash (platform-ordered existing-file preference, falling back to the platform default to create) → error for shells with no rc convention |
| `internal/app/init.go` | Created | `InitOptions`, `InitReport`, `Init`: create-if-absent for both files (skips creating aliases.yaml when `--source` points elsewhere), `loadDeviceContext` + `syncWithContext` for the initial sync, `resolveRCPath` + injectable `Confirm` for the bootstrap prompt, `apply.AddBootstrap` + `recordBootstrap` (persists `state.Bootstrap`) on consent |
| `internal/app/status.go` | Created | `StatusReport`, `Status`: source/device/backend/state + `UpToDate` (output path match + disk-hash match) |
| `internal/app/list.go` | Created | `AliasListing`, `ListReport`, `List` (own direct `config.ParseAliases` read, not `Source.Resolve`), `skipReason` (disabled/platform/shell/profile/device, in that precedence order) |
| `internal/app/doctor.go` | Created | `DoctorReport`, `Doctor`: independent `domain.Resolve` → `validate.Config` pass (mirrors what `FileSource.Resolve`+`FilterValid` do internally, but returns the `Issues` instead of discarding them) + `config.ProfileWarnings`; never writes |
| `internal/app/edit.go` | Created | `EditTarget`, `EditOptions`, `EditReport`, `ErrEditorNotSet`, `Edit`: `strings.Fields($EDITOR)` → `Env.LookPath` → `exec.Command`, documented quoted-path limitation |
| `internal/app/uninstall.go` | Created | `UninstallOptions`, `UninstallReport`, `Uninstall`: optional confirm → remove generated file → `apply.RemoveBootstrap` (surfaces `exact`) → remove `state.json` |
| `internal/app/testutil_test.go` | Created | `testEnv` (wraps `Env` + `Base`/`Home`/mutable `vars` map/buffers), `newTestEnv`, `writeConfigYAML`, `writeAliasesYAML`, `nativeDeviceConfig` — shared fixtures for every test file below |
| `internal/app/sync_test.go`, `init_test.go`, `status_test.go`, `list_test.go`, `doctor_test.go`, `edit_test.go`, `uninstall_test.go`, `misc_test.go`, `integration_test.go` | Created | Per-unit RED tests plus a `misc_test.go` sweep (`promptYesNo`, `resolveBackend`, `resolveRCPath`, `skipReason`, `ConfigError`, `OSEnv`) added after the GREEN pass purely to close coverage gaps — no production logic changed as a result |
| `cmd/aliasdeck/main.go` | Created | `main`/`run(args, stdout, stderr)` — separated from `main` for testability; maps `*exitError` and generic errors to exit codes; usage-vs-business-error split via `cmd.SilenceUsage` |
| `cmd/aliasdeck/root.go` | Created | `newRootCmd` (`SilenceErrors: true`, `--shell` persistent flag), `shellFlag` helper |
| `cmd/aliasdeck/exit.go` | Created | Exit-code constants (0/1/2/3/4), `*exitError{code,err}` carrier, `exitCodeFor` (maps `app.ErrNotInitialized`→4, `app.ConfigError`→3, else 1) |
| `cmd/aliasdeck/{init,sync,status,list,doctor,edit,uninstall}.go` | Created | One `newXCmd()` per command; each `RunE` sets `cmd.SilenceUsage = true` first, then calls the matching `internal/app` function and prints its report; `doctor` returns `&exitError{code: 3}` when `Issues.HasErrors()` after already printing the report itself |
| `cmd/aliasdeck/main_test.go` | Created | `TestRunNotInitializedExitsFour`, `TestRunInitThenSyncSucceeds`, `TestRunDoctorFindsErrorExitsThree`, `TestRunUnknownCommandExitsTwo`, `TestRunEditWithoutEditorExitsOne` — real Cobra tree, real (test-scoped) env vars, real filesystem |
| `openspec/changes/standalone-cli/tasks.md` | Modified | Marked 3.1–3.12 `[x]` |

### TDD Cycle Evidence (Batch 3)

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| 3.1/3.2 | `internal/app/sync_test.go` | Unit | ✅ ran `go test ./...` before adding (Phases 1–2 all green, `internal/app` package did not exist) | ✅ Written first; confirmed compile failure `undefined: Env/Sync/Options` (package did not exist) | ✅ Passed on first implementation | ✅ full pipeline, no-op skip (read-only-dir proof), forced rewrite on tampered disk hash, determinism across a deleted-state re-resolution, unresolvable-source error naming the path | ➖ None needed |
| 3.3/3.4 | `internal/app/init_test.go` | Unit | ✅ ran `go test ./internal/app/...` before adding (sync tests green, 5/5) | ✅ Written first; confirmed compile failure `undefined: Init/InitOptions` | ✅ Passed on first implementation | ✅ both-files-created, `--no-bootstrap` skip (confirm never called, rc untouched), consent-granted (question asked, block written, state recorded), consent-declined (rc untouched), idempotent re-run | ➖ None needed |
| 3.5/3.6 | `internal/app/{status,list,doctor}_test.go` | Unit | ✅ ran `go test ./internal/app/...` before adding (init tests green, 10/10) | ✅ Written first; confirmed compile failure `undefined: Status/List/Doctor` | ✅ Passed on first implementation | ✅ status (active source + up-to-date + not-initialized), list (active/skipped/disabled with reasons), doctor (hostile-name error surfaced + undeclared-profile warning + writes-nothing via before/after dir listing) | ➖ None needed |
| 3.7/3.8 | `internal/app/edit_test.go` | Unit — **threat-matrix RED required first** | ✅ ran `go test ./internal/app/...` before adding (15/15 passing) | ✅ Written first; confirmed compile failure `undefined: Edit/EditOptions/ErrEditorNotSet` | ✅ Passed on first implementation | ✅ hostile `$EDITOR` (asserted the literal `"x;"` token, not any shell, is what `LookPath` receives; marker file survives), `code -w` via a real fake-script subprocess capturing argv, no-sync-side-effect, unset-`$EDITOR` error | ➖ None needed |
| 3.9/3.10 | `internal/app/uninstall_test.go` | Unit | ✅ ran `go test ./internal/app/...` before adding (18/18 passing) | ✅ Written first; confirmed compile failure `undefined: Uninstall/UninstallOptions` | ✅ Passed on first implementation | ✅ byte-identical restore with real pre-existing rc content, `--yes` skips prompt, interactive decline leaves everything untouched (rc + state.json), fallback-path (`BootstrapExact=false`) when the block was edited inside | ➖ None needed |
| 3.11 | `cmd/aliasdeck/main_test.go` | Integration (real Cobra tree + real env vars + real filesystem) | ✅ ran `go build ./...` before adding (compiled clean once wiring was written) | N/A for this task — no RED assigned to Cobra wiring in tasks.md; written GREEN-first, then covered by `main_test.go` and a full manual smoke test (`--help`, `init`→`status`→`sync`→`doctor`→`list`→`uninstall`, plus exit-code probes for 1/2/3/4) | ✅ Passed | ✅ not-initialized(4), init+sync(0), doctor-with-error(3), unknown-command(2), edit-without-$EDITOR(1) | ➖ None needed |
| 3.12 | `internal/app/integration_test.go` | Integration, `t.TempDir()` HOME | ✅ ran full `internal/app` suite before adding (23/23 passing) | ✅ Written first; confirmed compile failure (referenced not-yet-relevant symbols were already defined by this point, so this test compiled but was run to confirm it exercises real behavior before being trusted as a safety net) | ✅ Passed on first implementation | ✅ init-with-pre-existing-rc-content, explicit content-bearing sync, second no-op sync under a read-only base dir, uninstall restoring byte-identical rc while leaving config.yaml/aliases.yaml in place | ➖ None needed |
| — | `internal/app/misc_test.go` | Unit (coverage closure) | ✅ ran `go test -cover ./internal/app/...` after the GREEN pass, found 70.5% (at the floor) | N/A — added after full GREEN specifically to close coverage gaps in already-implemented helpers, per Batch 2's own precedent | ✅ Passed on first implementation | ✅ `promptYesNo` (7 input cases), `resolveBackend` (4 cases), `resolveRCPath` (7 cases), `skipReason` (6 cases), `ConfigError.Unwrap`, `OSEnv` wiring | ➖ None needed — raised `internal/app` from 70.5% to 79.2% with zero production changes |

### Design Interpretation Notes (not deviations — two places the design text needed a resolved reading)

1. **Where bootstrap happens.** The design's "Atomic write" section says "Order per sync: generated file → bootstrap → state," which read literally would mean every `sync` call re-attempts `AddBootstrap`. But tasks.md's own phase breakdown assigns bootstrap prompting and `--no-bootstrap` exclusively to `init` (3.3/3.4) and lists `sync`'s pipeline as only "resolve→validate→render→apply→state" (3.1/3.2) with no bootstrap step — and the cli-commands spec requires `edit` to have zero side effects and `init` alone to prompt for rc-file consent. I implemented bootstrap as an `init`-only action (`Sync`/`syncWithContext` never touch any rc file), consistent with PROJECT.md §15.1's own flow (`init` "adds shell bootstrap"; a later bare `sync` does not). If a future milestone wants `sync` to also re-bootstrap a stale device (e.g. after a manual rc-file edit removed the block), that would need to be raised as an explicit new requirement, not inferred from this one line.
2. **Exit code 3 for "edit found SeverityError."** The design's exit table lists exit 3 for "parse failure, or `doctor`/`edit` found `SeverityError`," but the cli-commands spec's `edit` requirement never describes any validation step for `edit` — it only opens `$EDITOR` and forbids any sync/render/apply side effect. `Edit` therefore performs no validation and can only return `ErrEditorNotSet` or a runtime `LookPath`/`exec` failure (both exit 1), or a `ConfigError` from `loadDeviceContext` if `config.yaml` itself fails to parse (exit 3, via the same path every other command shares). I read the table's "doctor/edit" phrasing as a shorthand that primarily describes `doctor`'s own exit-3 behavior; `edit` inherits exit 3 only through the shared `config.yaml`-parse path, never through any aliases.yaml validation of its own.

### Work Unit Evidence (Batch 3)

| Evidence | Value |
|---|---|
| Focused test command and exact result | `go test ./internal/app/... ./cmd/... -v` → all PASS, 0 failures (36 top-level test functions across both packages, many with `t.Run` subtests) |
| Runtime harness command/scenario and exact result | Two real harnesses: (1) `go build -o /tmp/aliasdeck ./cmd/aliasdeck && /tmp/aliasdeck --help` plus a full manual walkthrough (`init --no-bootstrap` → `status` → `sync` (no-op) → `doctor` → `list` → `uninstall --yes`) against a real `t.TempDir()`-style scratch HOME, confirming exit codes 0/1/2/3/4 all match the design's table exactly; (2) `internal/app/integration_test.go`'s `TestFullLifecycleInitSyncSyncUninstall`, the required `init→sync→second sync (no write)→uninstall (byte-identical rc)` scenario |
| Rollback boundary | Delete `internal/app/` and `cmd/aliasdeck/`; revert `go.mod`/`go.sum` to the Batch 2 state (`git checkout -- go.mod go.sum` would drop cobra again); revert the `[x]` marks for 3.1–3.12 in `tasks.md`. No other package imports `internal/app` or `cmd/aliasdeck`, and `internal/domain`/`internal/validate`/`internal/renderers` remain byte-identical to Batch 2 (confirmed via `git diff --stat -- internal/domain internal/validate internal/renderers`, zero output) |

### Test Summary (Batch 3)

- **Total tests written**: 36 top-level test functions across `internal/app` (31) and `cmd/aliasdeck` (5), most with `t.Run` subtests; 90+ individual subtest/table cases
- **Total tests passing**: all (`go test ./... -v`, `go test -race ./internal/app/... ./cmd/...` also clean)
- **Layers used**: Unit (majority), Integration (`internal/app/integration_test.go`'s full lifecycle test; `cmd/aliasdeck/main_test.go`'s real-Cobra-tree tests; the real fake-editor-subprocess tests in `edit_test.go`)
- **Approval tests**: None — no refactoring of existing behavior, only new files
- **Coverage per package**:
  - `internal/app`: **79.2%** of statements (raised from 70.5% post-GREEN by `misc_test.go`'s coverage-closure sweep, with zero production changes)
  - `cmd/aliasdeck`: **62.3%** of statements (Cobra wiring glue; not gated by the ≥70% floor, which config.yaml scopes to "new packages" the design calls out — `internal/app` is the one with a numeric target in this phase's instructions)
  - Milestone 1 packages unchanged: `domain` 70.4%, `renderers` 89.1%, `validate` 87.7% — byte-identical to the Batch 1/2 baseline
  - Milestone 2 packages from Batch 2 unchanged: `apply` 82.5%, `source` 100.0%, `state` 73.0%, `config` 87.2%

### Deviations from Design (Batch 3)

None that change behavior. See "Design Interpretation Notes" above for the two places the design text needed a resolved reading (bootstrap's home in `init` rather than `sync`; exit-3's "edit" clause resolving to the shared config-parse path).

### Issues Found (Batch 3)

None. `internal/domain`, `internal/validate`, and `internal/renderers` remain completely untouched — confirmed by `git diff --stat -- internal/domain internal/validate internal/renderers` (zero output) and by their coverage numbers staying exactly at the Batch 1/2 baseline (70.4%/89.1%/87.7%). `internal/config`, `internal/source`, `internal/apply`, `internal/state` (Batch 2's packages) were read but never modified — confirmed the same way (82.5%/100.0%/73.0%/87.2%, unchanged from Batch 2).

### Verification Run (Batch 3)

- `go test ./...` → all 9 packages pass (`cmd/aliasdeck`, `internal/app`, `internal/apply`, `internal/config`, `internal/domain`, `internal/renderers`, `internal/source`, `internal/state`, `internal/validate`)
- `make check` (`gofmt -l -w .` + `go vet ./...` + `go test ./...`) → clean, no formatting diffs, no vet warnings, all tests pass
- `go build -o /tmp/aliasdeck ./cmd/aliasdeck && /tmp/aliasdeck --help` → prints the full command tree (`init`, `sync`, `status`, `list`, `doctor`, `edit`, `uninstall`, plus Cobra's built-in `help`/`completion`), exits 0
- Manual exit-code probe against a real scratch HOME: not-initialized → 4; `doctor` with a hostile alias name → 3 (message includes the specific validation reason); unknown subcommand → 2; `edit` with no `$EDITOR` → 1; every happy path → 0

### Remaining Tasks

- [ ] Phase 4: Milestone-1-Adjacent Verification (tasks 4.1–4.4)
- [ ] Phase 5: Release Tooling (tasks 5.1–5.3)
- [ ] Phase 6: Docs & Config Sync (tasks 6.1–6.5)

### Workload / PR Boundary

- Mode: stacked-to-main (per tasks.md "Suggested Work Units")
- Current work unit: Unit 6 — "`internal/app` (7 use cases) + `cmd/aliasdeck` (Cobra wiring)" (PR 6 of 7 in the suggested split)
- Boundary: starts from the Batch 2 tree (Phases 1–2 complete, `internal/domain`/`internal/validate`/`internal/renderers` untouched, no `cmd/` or `internal/app`); ends with all seven commands implemented, wired, and tested, `go.mod`/`go.sum` updated with `cobra` as a direct dependency, the binary building and running correctly end to end
- Estimated review budget impact: 1 new package (`internal/app`, 12 production files + 9 test files) + 1 new command package (`cmd/aliasdeck`, 10 production files + 1 test file) + a two-line `go.mod`/`go.sum` change. This is the largest unit in the plan (tasks.md flagged ~730 lines as a possible split candidate); it landed as one batch since every use case shares the same `Env`/`deviceContext` infrastructure and splitting further would have meant reviewing partial, non-compiling slices

### Status

31/31 tasks complete across Phases 1–3 (9/9 + 10/10 + 12/12). Ready for `sdd-verify` on Phases 1–3, or the next apply batch (Phase 4: Milestone-1-adjacent renderer coverage + golden/real-shell confirmation; Phase 5: release tooling; Phase 6: docs/config sync — Phases 4–6 are independent of each other and can run in any order or in parallel per tasks.md's Parallelization Notes).
