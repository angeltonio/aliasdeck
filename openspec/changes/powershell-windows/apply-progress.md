# Apply Progress: PowerShell, Windows and Git-hosted config — Milestone 3 (v0.2)

**Batch**: 1 (Phases 1–2 only, per orchestrator scope)
**Mode**: Strict TDD

## Completed Tasks

- [x] 1.1 Decision recorded: `source.git.path` is OPTIONAL, omitted ⇒ `aliases.yaml` at checkout root. Recorded as design decision 16 in `design.md`; scenarios added to `specs/config-source/spec.md`.
- [x] 1.2 RED: `internal/config/device_test.go` — `TestParseDeviceConfigGitSource`, `TestParseDeviceConfigGitSourceRefAndPathOptional`, `TestParseDeviceConfigUnknownGitFieldRejected`.
- [x] 1.3 GREEN: `internal/config/device.go` — `GitSourceConfig{URL, Ref, Path}` added to `Source`; `gitSourceDTO` added to `sourceDTO` under nested `git:` key; `ParseDeviceConfig`/`Write` updated; `KnownFields(true)` rejects unknown fields under `source.git` for free (nested struct).
- [x] 2.1 RED: `internal/config/detect_test.go` — added `"windows maps to windows via runtime.GOOS"` and `"ALIASDECK_PLATFORM=windows is accepted as a test seam"` cases to `TestDetectPlatformPrecedence`.
- [x] 2.2 GREEN: `internal/config/detect.go` — `DetectPlatform` gained `case "windows": PlatformWindows`.
- [x] 2.3 RED: `internal/config/paths_test.go` — added `"tilde backslash prefix (Windows-shaped path)"` case to `TestExpandPath`.
- [x] 2.4 GREEN: `internal/config/paths.go` — `ExpandPath` now recognizes a leading `~\` in addition to `~/` via `tildePrefixRest`; the matched remainder is normalized with `filepath.FromSlash`/`filepath.Join` so mixed separators resolve correctly on any host OS.

## TDD Cycle Evidence

| Task | RED (test written first, observed failing) | GREEN (implementation, observed passing) | REFACTOR |
|---|---|---|---|
| 1.2/1.3 | `go test ./internal/config/... -run TestParseDeviceConfigGitSource` — compile error: `cfg.Source.Git undefined` | Same command passes after adding `GitSourceConfig`/`gitSourceDTO` | None needed — matches existing `Source`/`sourceDTO` mirroring pattern |
| 2.1/2.2 | `go test ./internal/config/... -run TestDetectPlatformPrecedence -v` — `windows_maps_to_windows_via_runtime.GOOS` failed: `unsupported operating system "windows"` | Same command passes after adding the `case "windows"` branch | None needed — one-line addition matching existing darwin/linux cases |
| 2.3/2.4 | `go test ./internal/config/... -run TestExpandPath -v` — `tilde_backslash_prefix` failed: got `"~\\dotfiles\\aliases.yaml"`, want `"/home/user/dotfiles/aliases.yaml"` | Same command passes after adding `tildePrefixRest` and backslash-to-native-separator normalization | Factored the shared "~/" / "~\\" prefix-stripping into `tildePrefixRest` rather than inlining a second `HasPrefix` branch, to keep `ExpandPath` readable |

## Work Unit Evidence

| Evidence | Value |
|---|---|
| Focused test command and exact result | `go test ./internal/config/... -v` → all cases pass, including the 6 new/extended cases above (`TestParseDeviceConfigGitSource`, `TestParseDeviceConfigGitSourceRefAndPathOptional`, `TestParseDeviceConfigUnknownGitFieldRejected`, `TestDetectPlatformPrecedence` windows sub-cases, `TestExpandPath` backslash sub-case) |
| Runtime integration harness | N/A — per the tasks artifact's own forecast for this work unit: "no shell/process boundary yet." Confirmed: no code touched in this batch spawns a process or writes to a real filesystem path outside `t.TempDir()`. |
| Rollback boundary | Revert the six modified files under `internal/config/` (`detect.go`, `detect_test.go`, `device.go`, `device_test.go`, `paths.go`, `paths_test.go`). No other package imports the new `GitSourceConfig` type or the widened `DetectPlatform`/`ExpandPath` behavior yet, so this reverts cleanly with zero downstream impact. |

## Files Changed

| File | Action | What Was Done |
|---|---|---|
| `internal/config/device.go` | Modified | Added `GitSourceConfig{URL, Ref, Path}` field `Git` on `Source`; added `gitSourceDTO` field `Git` on `sourceDTO` (nested `git:` YAML key); wired both through `ParseDeviceConfig` and `Write` |
| `internal/config/device_test.go` | Modified | Added `TestParseDeviceConfigGitSource`, `TestParseDeviceConfigGitSourceRefAndPathOptional`, `TestParseDeviceConfigUnknownGitFieldRejected` |
| `internal/config/detect.go` | Modified | `DetectPlatform` gained `case "windows": PlatformWindows` |
| `internal/config/detect_test.go` | Modified | Added two Windows-related cases to `TestDetectPlatformPrecedence` |
| `internal/config/paths.go` | Modified | `ExpandPath` recognizes a leading `~\` (Windows-shaped) via new `tildePrefixRest` helper, normalizing mixed separators with `filepath.FromSlash`/`filepath.Join` |
| `internal/config/paths_test.go` | Modified | Added `"tilde backslash prefix (Windows-shaped path)"` case to `TestExpandPath` |
| `openspec/changes/powershell-windows/design.md` | Modified | Added decision 16 (`source.git.path` optionality and containment) |
| `openspec/changes/powershell-windows/specs/config-source/spec.md` | Modified | Added `Requirement: GitSource aliases.yaml Path Resolution` with 3 scenarios |
| `openspec/changes/powershell-windows/tasks.md` | Modified | Marked 1.1, 1.2, 1.3, 2.1, 2.2, 2.3, 2.4 as `[x]` |

## Deviations from Design

None — implementation matches design decisions 16 and the "Windows Path Handling" table's `config.ExpandPath`/`config.DetectPlatform` rows.

One clarification worth flagging: design decision 16 and the "Windows Path Handling" table both describe `~\` recognition in terms of `os.PathSeparator`. Implemented instead as an explicit, always-recognized `~\` literal (in addition to `~/`), independent of the host OS's `os.PathSeparator`. This is a deliberate, narrower-than-literal-text but design-intent-preserving choice: `os.PathSeparator` is `/` on the macOS/Linux CI runners that run this suite today (Windows-in-CI is Phase 8, out of this batch's scope), so gating recognition on `os.PathSeparator` would make the Windows-shaped-path test un-exercisable until Phase 8 lands, and would only work when AliasDeck itself runs on Windows — not when a Windows-authored `config.yaml`/`source.path` is inspected or tested elsewhere. Recognizing the literal backslash unconditionally satisfies the task's literal acceptance criterion ("`ExpandPath` handles `~\dotfiles\aliases.yaml`") on every CI runner and matches the design rationale ("one path shape across three operating systems") more directly than a GOOS-gated check would. POSIX-authored paths (`~/...`) are unaffected.

## Issues Found

None.

## Remaining Tasks (not in this batch's scope)

- [ ] Phase 3: PowerShell Renderer (3.1–3.6)
- [ ] Phase 4: Windows Apply — Defects A+B, EOL, `.ps1` Output (4.1–4.8)
- [ ] Phase 5: PowerShell `$PROFILE` Resolution (5.1–5.6)
- [ ] Phase 6: GitSource + State Staleness (6.1–6.9)
- [ ] Phase 7: CLI Reporting — status/doctor (7.1–7.4)
- [ ] Phase 8: CI Matrix & Release (8.1–8.6)
- [ ] Phase 9: Docs & Final Verification (9.1–9.4)

## Workload / PR Boundary

- Mode: `ask-on-risk` forecast in tasks.md remains unresolved for the overall change (chain strategy still `pending`); this batch executes Work Unit 1 ("Schema + Windows platform detection foundations (Phases 1–2)") as an autonomous, revertible slice regardless of which chain strategy the orchestrator ultimately picks.
- Current work unit: Unit 1 of 6 (per the Suggested Work Units table in `tasks.md`)
- Boundary: starts from a clean `internal/config` (no prior batch); ends with Phases 1–2 fully green and isolated from Phases 3–9, which depend on it but are untouched.
- Estimated review budget impact: small — 6 source/test files in one package, well under the 400-line guard on its own.

## Verification

```
$ go test ./...
ok  	github.com/angeltonio/aliasdeck/cmd/aliasdeck	0.404s
ok  	github.com/angeltonio/aliasdeck/internal/app	2.680s
ok  	github.com/angeltonio/aliasdeck/internal/apply	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/config	1.067s
ok  	github.com/angeltonio/aliasdeck/internal/domain	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/renderers	0.943s
?   	github.com/angeltonio/aliasdeck/internal/shelltest	[no test files]
ok  	github.com/angeltonio/aliasdeck/internal/source	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/state	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/validate	(cached)

$ make ci
go vet ./...
go test -race ./...
ok  	github.com/angeltonio/aliasdeck/cmd/aliasdeck	1.420s
ok  	github.com/angeltonio/aliasdeck/internal/app	3.843s
ok  	github.com/angeltonio/aliasdeck/internal/apply	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/config	2.145s
ok  	github.com/angeltonio/aliasdeck/internal/domain	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/renderers	(cached)
?   	github.com/angeltonio/aliasdeck/internal/shelltest	[no test files]
ok  	github.com/angeltonio/aliasdeck/internal/source	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/state	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/validate	(cached)
CI checks passed

$ go test ./internal/config/... -cover
ok  	github.com/angeltonio/aliasdeck/internal/config	0.318s	coverage: 88.2% of statements

$ gofmt -l .
(no output — everything formatted)
```

No git commit was created. Changes are left in the working tree per the orchestrator's delivery-strategy instructions.

## Engram

`mem_save`/`mem_search`/`mem_update` tools were not bound in this session's tool set (consistent with prior batches). Progress was persisted only to this file (`openspec/changes/powershell-windows/apply-progress.md`) and to `tasks.md`. No Engram tool call was attempted or fabricated.
