# Apply Progress: standalone-cli

## Batch 1 — Phase 1: Config Foundation (tasks 1.1–1.9)

**Mode**: Strict TDD
**Branch**: `feat/standalone-cli`
**Status**: 9/9 Phase 1 tasks complete. Ready for the next apply batch (Phase 2).

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

### Files Changed

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

### TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| 1.2/1.3 | `internal/config/paths_test.go` | Unit | N/A (new) | ✅ Written (compile failure: `undefined: Env/Base/...`) | ✅ Passed (`go test ./internal/config/...`) | ✅ 3 `Base` precedence cases + tilde + UserConfigDir regression + HomeDir error + per-file paths + `ExpandPath` table | ✅ Added `OSEnv` real-behavior test after initial pass to close a coverage gap; no logic changes needed |
| 1.4/1.5 | `internal/config/aliases_test.go` | Unit | ✅ ran `go test ./internal/config/...` before adding (only paths_test present, 12/12 passing) | ✅ Written (compile failure: `undefined: ParseAliases/ProfileWarnings`) | ✅ Passed | ✅ 3 `enabled` cases, 2 unknown-field cases (alias-level + top-level, tightened top-level case to avoid a trivial substring overlap), version + oversize cases, 2 `ProfileWarnings` cases | ➖ None needed — code was already minimal and clean |
| 1.6/1.7 | `internal/config/device_test.go` | Unit | ✅ ran full config suite before adding (23/23 passing) | ✅ Written (compile failure: `undefined: ParseDeviceConfig/Load/Write/...`) | ✅ Passed | ✅ Valid file, unknown backend, unknown field, fallback-identity stability, write→load round-trip with mode check | ➖ None needed |
| 1.8/1.9 | `internal/config/detect_test.go` | Unit | ✅ ran full config suite before adding (28/28 passing) | ✅ Written (compile failure: `undefined: DetectPlatform/DetectShell`) | ✅ Passed | ✅ 6 platform-precedence cases, 7 shell-precedence cases (including unsupported-shell-is-an-error) | ➖ None needed |

### Test Summary

- **Total tests written**: 20 top-level test functions (many with `t.Run` subtests); 60+ individual subtest cases
- **Total tests passing**: all (`go test ./internal/config/... -v` — zero failures)
- **Layers used**: Unit (20), Integration (0), E2E (0) — matches design's "Layer: Unit" for every Phase 1 item
- **Approval tests** (refactoring): None — no refactoring tasks in this batch, only new files
- **Pure functions created**: `Base`, `ExpandPath`, `ConfigFile`/`AliasesFile`/`StateFile`, `ParseAliases`, `ProfileWarnings`, `parsePlatforms`/`parseShells`, `ParseDeviceConfig`, `DetectPlatform`, `DetectShell`, `shellBasename`, `parseShellDetection` — every parse/detect function takes explicit inputs (`Env`, `getenv func`, `goos string`) instead of reading global state, so all of them are unit-testable without touching the real OS or `$HOME`

### Work Unit Evidence

| Evidence | Value |
|---|---|
| Focused test command and exact result | `go test ./internal/config/...` → `ok github.com/angeltonio/aliasdeck/internal/config 0.2xx s coverage: 88.2% of statements` |
| Runtime harness command/scenario and exact result | N/A — no CLI or filesystem-integration boundary exists yet in this batch; `Load`/`Write` are exercised through `t.TempDir()` unit tests, not a runtime harness |
| Rollback boundary | Delete `internal/config/{paths,aliases,device,detect}*.go`; revert `go.mod`/`go.sum` to the pre-batch state (drops `go.yaml.in/yaml/v3`); revert the `[x]` marks in `tasks.md`. No other package references `internal/config` yet, so this is a clean, isolated revert |

### Deviations from Design

1. **`github.com/spf13/cobra` is not currently in `go.mod`.** Task 1.1 says to add both `cobra` and `go.yaml.in/yaml/v3`, then run `go mod tidy`. `go mod tidy` removes any module requirement nothing in the tree imports, and Phase 1 (`internal/config`) has no reason to import Cobra — only `cmd/aliasdeck` (Phase 3, Unit 6) does. Adding it now and running `tidy` strips it again; adding it and skipping `tidy` leaves a dependency-graph entry the tool itself would flag as stale. I kept `go.yaml.in/yaml/v3` (genuinely imported by `aliases.go`/`device.go` in this same batch) and deferred `cobra` to the batch that actually writes `cmd/aliasdeck/main.go` and imports it, at which point `go get github.com/spf13/cobra@v1.10.2` needs to be re-run. This is a tooling-driven adjustment, not a design disagreement — flagging it so the next apply batch (or whichever batch lands `cmd/aliasdeck`) knows to re-add it.
2. **`config.yaml` `source.type` is not validated against the `file|git|server` enum at parse time.** The spec's explicit "strict schema" scenario only tests the `backend` enum; `internal/source` (Phase 2) is the package that actually acts on `source.type`, so semantic validation of that field is deferred there rather than duplicated here without a test demanding it (strict TDD: no untested production logic).
3. **Device identity fallback generation uses random bytes, not a hostname derivation.** The design text only says "a generated fallback if omitted" without specifying the algorithm, and Phase 1's task list (1.6) only names two RED scenarios (valid file, unknown backend). I added a third test (fallback identity is generated and stable across reloads) to satisfy the spec's "MUST derive a stable device identity ... or a generated fallback if omitted" requirement text, using `crypto/rand` + hex encoding rather than `os.Hostname()` so two devices sharing a hostname never collide. This is additive, not a scope reduction — happy to switch to a hostname-based scheme if that's the intended behavior.

None of these touch `internal/domain`, `internal/validate`, or `internal/renderers` — all three packages are untouched, confirmed by `git status` and by their coverage numbers staying exactly at 70.4%/89.1%/87.7% (unchanged from the cached baseline in `openspec/config.yaml`).

### Issues Found

None.

### Remaining Tasks

- [ ] Phase 2: Source, Apply, State (tasks 2.1–2.10)
- [ ] Phase 3: App Use Cases & CLI Wiring (tasks 3.1–3.12)
- [ ] Phase 4: Milestone-1-Adjacent Verification (tasks 4.1–4.4)
- [ ] Phase 5: Release Tooling (tasks 5.1–5.3)
- [ ] Phase 6: Docs & Config Sync (tasks 6.1–6.5)

### Workload / PR Boundary

- Mode: stacked-to-main (per tasks.md "Suggested Work Units", Chain strategy resolved by the orchestrator)
- Current work unit: Unit 1 — "go.mod deps + internal/config paths/device/detect (Phase 1.1-1.9 minus 1.4/1.5)" plus Unit 2 — "aliases.yaml DTO + enabled default + profile warnings (1.4-1.5)". Both units landed together in this batch since they share one package and one PR-sized diff.
- Boundary: starts from a clean Milestone-1-only tree, ends with `internal/config` fully implemented and tested; no other package touched.
- Estimated review budget impact: new files only, no existing code modified outside `go.mod`/`go.sum`/`tasks.md`. Well under the 400-line single-PR budget for this unit alone.

### Status

9/9 Phase 1 tasks complete. Ready for next apply batch (Phase 2) or `sdd-verify` if the orchestrator wants to verify Phase 1 in isolation before continuing.
