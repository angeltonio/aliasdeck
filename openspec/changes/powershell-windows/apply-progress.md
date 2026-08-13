# Apply Progress: PowerShell, Windows and Git-hosted config — Milestone 3 (v0.2)

**Batches**: 1 (Phases 1–2) + 2 (Phase 3), per orchestrator scope
**Mode**: Strict TDD

## Completed Tasks

- [x] 1.1 Decision recorded: `source.git.path` is OPTIONAL, omitted ⇒ `aliases.yaml` at checkout root. Recorded as design decision 16 in `design.md`; scenarios added to `specs/config-source/spec.md`.
- [x] 1.2 RED: `internal/config/device_test.go` — `TestParseDeviceConfigGitSource`, `TestParseDeviceConfigGitSourceRefAndPathOptional`, `TestParseDeviceConfigUnknownGitFieldRejected`.
- [x] 1.3 GREEN: `internal/config/device.go` — `GitSourceConfig{URL, Ref, Path}` added to `Source`; `gitSourceDTO` added to `sourceDTO` under nested `git:` key; `ParseDeviceConfig`/`Write` updated; `KnownFields(true)` rejects unknown fields under `source.git` for free (nested struct).
- [x] 2.1 RED: `internal/config/detect_test.go` — added `"windows maps to windows via runtime.GOOS"` and `"ALIASDECK_PLATFORM=windows is accepted as a test seam"` cases to `TestDetectPlatformPrecedence`.
- [x] 2.2 GREEN: `internal/config/detect.go` — `DetectPlatform` gained `case "windows": PlatformWindows`.
- [x] 2.3 RED: `internal/config/paths_test.go` — added `"tilde backslash prefix (Windows-shaped path)"` case to `TestExpandPath`.
- [x] 2.4 GREEN: `internal/config/paths.go` — `ExpandPath` now recognizes a leading `~\` in addition to `~/` via `tildePrefixRest`; the matched remainder is normalized with `filepath.FromSlash`/`filepath.Join` so mixed separators resolve correctly on any host OS.
- [x] 3.1 RED: new `internal/renderers/powershell_test.go` — `TestQuotePowerShell` doubling table test, `TestQuotePowerShellNeutralizesBreakout`, and `TestRenderArgsForwardedTwice` (byte assertion that `@args` appears exactly twice, at the two load-bearing positions). Observed failing to compile (`undefined: quotePowerShell`) before any production code existed.
- [x] 3.2 GREEN: new `internal/renderers/powershell.go` — `powershellRenderer{}` (no per-edition field, per design decision 1), `quotePowerShell` (single-quote doubling, design decision 2), `Render` emitting the verified `$__aliasdeck_cmd` + `[scriptblock]::Create(... + ' @args')` form (§6.3); `internal/renderers/renderer.go` registry gained `domain.ShellPowerShell: powershellRenderer{}`.
- [x] 3.3 Golden: added `testdata/powershell_basic.golden`, `powershell_empty.golden`, `powershell_awkward_commands.golden` (covering `}`, `'`, `$`, and backtick) via `go test ./internal/renderers -update`; diff reviewed (only the three new files appeared — `git diff --stat internal/renderers/testdata/` is empty, confirming zero byte changes to the four pre-existing zsh/bash goldens); reran without `-update` and it passed.
- [x] 3.4 Inversion (not weakening) applied to `internal/renderers/posix_test.go`: `TestForUnsupportedShell` now asserts `For(ShellPowerShell)` succeeds (the `fish` case is still an error); `TestSupported` now asserts `[]domain.Shell{ShellZsh, ShellBash, ShellPowerShell}` in that order. Both were observed failing (RED) immediately after 3.2's GREEN, then fixed.
- [x] 3.5 RED: new `internal/renderers/powershell_integration_test.go` (no build tag, unlike the `!windows`-tagged POSIX integration test — pwsh is the same interpreter on every target OS) — `TestGeneratedFileIsInertInRealPowerShell`, using `shelltest.LookPath(t, "pwsh")`. Covers five hostile payloads (`}`, `;`, `$(...)`, an embedded `'`, and a `''`-escape-confusion attempt) plus a benign `showargs` alias proving two arguments — one containing a space — arrive intact.
- [x] 3.6 GREEN: confirmed 3.5 passes against real `pwsh` 7.6.4 (verified below); `Supported()` order coverage folded into 3.4's `TestSupported` update rather than a separate test, since it is the same assertion the inversion already required.

### Adversarial validation of the real-`pwsh` test (not a tracked task, done because this is the security-critical phase)

Before trusting `TestGeneratedFileIsInertInRealPowerShell` as a regression guard, `Render` was temporarily changed to emit raw, unquoted code directly inside the function block (`function <name> { <command> @args }` — exactly the vulnerable form design decision 1 warns against) and the test was rerun. It failed hard: the crafted `}`-bearing payload broke PowerShell's own parser (`ParserError`, "the splatting operator '@' cannot be used to reference variables in an expression") and the argument-forwarding sub-test failed too (`showargs` was undefined, since the malformed file never finished sourcing). The correct implementation was then restored (`git diff internal/renderers/powershell.go` was empty afterward, confirming a clean revert) and the full suite was rerun green. This is stronger evidence than "the test passed" alone — it shows the test actively catches the class of regression it exists to catch.

### Gotcha discovered mid-phase: gofmt mangles literal `''` inside doc-comment prose

Go's `gofmt` (confirmed on the actual `go1.25.11` binary, not a shim artifact) reformats top-level doc comments via `go/doc/comment` and treats two adjacent straight single quotes (`''`) appearing inline in prose (inside backticks, outside an indented code block) as a typewriter-style double-quote convention, silently rewriting them to a Unicode right-double-quotation-mark (U+201D). This is exactly the sequence this renderer's own doc comments needed to describe (PowerShell's `''` doubling), so `quotePowerShell`'s doc comment and two test doc comments were rewritten to describe the doubling in words instead of showing bare `''` inline in prose — the indented code-block example (`don't -> 'don''t'`) is unaffected because gofmt preserves preformatted blocks verbatim, and Go string literals in test tables are code, not comments, so they were never at risk. Caught by running `gofmt -l .` and inspecting the diff (`gofmt -d`) before trusting `-w`; worth flagging for any future PowerShell-adjacent Go doc comment in this codebase.

## TDD Cycle Evidence

| Task | RED (test written first, observed failing) | GREEN (implementation, observed passing) | REFACTOR |
|---|---|---|---|
| 1.2/1.3 | `go test ./internal/config/... -run TestParseDeviceConfigGitSource` — compile error: `cfg.Source.Git undefined` | Same command passes after adding `GitSourceConfig`/`gitSourceDTO` | None needed — matches existing `Source`/`sourceDTO` mirroring pattern |
| 2.1/2.2 | `go test ./internal/config/... -run TestDetectPlatformPrecedence -v` — `windows_maps_to_windows_via_runtime.GOOS` failed: `unsupported operating system "windows"` | Same command passes after adding the `case "windows"` branch | None needed — one-line addition matching existing darwin/linux cases |
| 2.3/2.4 | `go test ./internal/config/... -run TestExpandPath -v` — `tilde_backslash_prefix` failed: got `"~\\dotfiles\\aliases.yaml"`, want `"/home/user/dotfiles/aliases.yaml"` | Same command passes after adding `tildePrefixRest` and backslash-to-native-separator normalization | Factored the shared "~/" / "~\\" prefix-stripping into `tildePrefixRest` rather than inlining a second `HasPrefix` branch, to keep `ExpandPath` readable |
| 3.1/3.2 | `go test ./internal/renderers/... -run 'TestQuotePowerShell\|TestRenderArgsForwardedTwice\|TestPowerShellRendererShell' -v` — compile error: `undefined: quotePowerShell` (twice) | Same command passes after adding `powershell.go` (`powershellRenderer`, `quotePowerShell`, `Render`) and registering `domain.ShellPowerShell` in `renderer.go`'s `registry` | None needed — mirrors `posixRenderer`'s structure (guard → clone/sort → `writeHeader` → per-alias loop) with an unrelated grammar inside the loop |
| 3.4 | `go test ./internal/renderers/... -v` after 3.2's GREEN — `TestForUnsupportedShell` failed (`expected PowerShell to be unsupported`), `TestSupported` failed (`Supported() = [zsh bash powershell], want [zsh bash]`) | Both fixed by inverting the assertions in `posix_test.go` to the positive guarantee; full suite green after | None needed |
| 3.5/3.6 | New `powershell_integration_test.go` run against real `pwsh` (`PATH` prepended with the provided pwsh directory) — passed on first run because the renderer under test (3.2) was already correct; RED was instead demonstrated adversarially (see "Adversarial validation" note above): reverting `Render` to raw, unquoted interpolation made both integration sub-tests fail against real `pwsh` with a `ParserError` | Restoring the correct `Render` implementation returned the suite to green (`git diff internal/renderers/powershell.go` empty after revert) | None needed |

## Work Unit Evidence

### Unit 1 (Phases 1–2)

| Evidence | Value |
|---|---|
| Focused test command and exact result | `go test ./internal/config/... -v` → all cases pass, including the 6 new/extended cases above (`TestParseDeviceConfigGitSource`, `TestParseDeviceConfigGitSourceRefAndPathOptional`, `TestParseDeviceConfigUnknownGitFieldRejected`, `TestDetectPlatformPrecedence` windows sub-cases, `TestExpandPath` backslash sub-case) |
| Runtime integration harness | N/A — per the tasks artifact's own forecast for this work unit: "no shell/process boundary yet." Confirmed: no code touched in this batch spawns a process or writes to a real filesystem path outside `t.TempDir()`. |
| Rollback boundary | Revert the six modified files under `internal/config/` (`detect.go`, `detect_test.go`, `device.go`, `device_test.go`, `paths.go`, `paths_test.go`). No other package imports the new `GitSourceConfig` type or the widened `DetectPlatform`/`ExpandPath` behavior yet, so this reverts cleanly with zero downstream impact. |

### Unit 2 (Phase 3)

| Evidence | Value |
|---|---|
| Focused test command and exact result | `go test ./internal/renderers/... -v` → all 15 top-level tests pass (7 pre-existing + `TestQuotePowerShell`, `TestQuotePowerShellNeutralizesBreakout`, `TestRenderArgsForwardedTwice`, `TestPowerShellRendererShell`, `TestGeneratedFileIsInertInRealPowerShell`, plus the inverted `TestForUnsupportedShell`/`TestSupported`); `internal/renderers` coverage 92.0% (`go test ./internal/renderers/... -cover`) |
| Runtime integration harness | `TestGeneratedFileIsInertInRealPowerShell` — dot-sources a rendered file containing five hostile payloads in real `pwsh` 7.6.4 and asserts no canary file is created and no output is produced (definition-only load); a second sub-test calls a benign alias with two arguments (one containing a space) and asserts both arrive intact. Actually executed, not skipped, when `pwsh`'s directory is prepended to `PATH` — verbose output captured below. |
| Rollback boundary | Remove `internal/renderers/powershell.go`, `powershell_test.go`, `powershell_integration_test.go`, the three `testdata/powershell_*.golden` files, the one added map entry in `renderer.go`'s `registry`, and revert the two inverted assertions in `posix_test.go` plus the three new cases in `golden_test.go`. `posix.go` and `header.go` are untouched, so zsh/bash rendering is unaffected by the revert. |

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
| `internal/renderers/powershell.go` | Created | `powershellRenderer{}`, `quotePowerShell`, `Render` — the compile-at-call-time `$__aliasdeck_cmd` + `[scriptblock]::Create` form, double-`@args` splatting |
| `internal/renderers/powershell_test.go` | Created | `TestQuotePowerShell` (doubling table), `TestQuotePowerShellNeutralizesBreakout`, `TestRenderArgsForwardedTwice` (byte assertion), `TestPowerShellRendererShell` |
| `internal/renderers/powershell_integration_test.go` | Created | `TestGeneratedFileIsInertInRealPowerShell` — real-`pwsh` dot-source containment proof + argument-forwarding proof; no build tag |
| `internal/renderers/renderer.go` | Modified | Registered `domain.ShellPowerShell: powershellRenderer{}` in `registry` |
| `internal/renderers/posix_test.go` | Modified | Inverted `TestForUnsupportedShell` (PowerShell now supported, `fish` still not) and `TestSupported` (now `[zsh, bash, powershell]`) |
| `internal/renderers/golden_test.go` | Modified | Added `powershell_basic`, `powershell_empty`, `powershell_awkward_commands` cases to `TestGolden` |
| `internal/renderers/testdata/powershell_basic.golden` | Created | Golden output for the standard fixture rendered as PowerShell |
| `internal/renderers/testdata/powershell_empty.golden` | Created | Golden output for zero aliases |
| `internal/renderers/testdata/powershell_awkward_commands.golden` | Created | Golden output covering `}`, `'`, `$`, and backtick in commands |
| `openspec/changes/powershell-windows/tasks.md` | Modified | Marked 1.1–2.4 (batch 1) and 3.1–3.6 (batch 2) as `[x]` |

## Deviations from Design

None for Phase 3 — implementation matches design decisions 1 and 2 (renderer shape, escaping), the interface shown in "Interfaces" (§ PowerShell function form), and the testing strategy table's golden/unit/integration rows verbatim.

Phases 1–2: None — implementation matches design decisions 16 and the "Windows Path Handling" table's `config.ExpandPath`/`config.DetectPlatform` rows.

One clarification worth flagging: design decision 16 and the "Windows Path Handling" table both describe `~\` recognition in terms of `os.PathSeparator`. Implemented instead as an explicit, always-recognized `~\` literal (in addition to `~/`), independent of the host OS's `os.PathSeparator`. This is a deliberate, narrower-than-literal-text but design-intent-preserving choice: `os.PathSeparator` is `/` on the macOS/Linux CI runners that run this suite today (Windows-in-CI is Phase 8, out of this batch's scope), so gating recognition on `os.PathSeparator` would make the Windows-shaped-path test un-exercisable until Phase 8 lands, and would only work when AliasDeck itself runs on Windows — not when a Windows-authored `config.yaml`/`source.path` is inspected or tested elsewhere. Recognizing the literal backslash unconditionally satisfies the task's literal acceptance criterion ("`ExpandPath` handles `~\dotfiles\aliases.yaml`") on every CI runner and matches the design rationale ("one path shape across three operating systems") more directly than a GOOS-gated check would. POSIX-authored paths (`~/...`) are unaffected.

## Issues Found

None for Phase 3.

Note carried from Phase 3 for whoever reviews or extends this package: Go's `gofmt` doc-comment reformatter mangles literal `''` written inline in comment prose (see the "Gotcha" note above). Any future doc comment describing PowerShell's quote-doubling — or any other language's `''`-shaped syntax — should either avoid the bare pair in inline prose or move the example into an indented code block, and `gofmt -d` should be inspected (not just `gofmt -l`) before trusting `-w` on renderer files.

## Remaining Tasks (not in this batch's scope)

- [ ] Phase 4: Windows Apply — Defects A+B, EOL, `.ps1` Output (4.1–4.8)
- [ ] Phase 5: PowerShell `$PROFILE` Resolution (5.1–5.6)
- [ ] Phase 6: GitSource + State Staleness (6.1–6.9)
- [ ] Phase 7: CLI Reporting — status/doctor (7.1–7.4)
- [ ] Phase 8: CI Matrix & Release (8.1–8.6)
- [ ] Phase 9: Docs & Final Verification (9.1–9.4)

## Workload / PR Boundary

- Mode: `ask-on-risk` forecast in tasks.md remains unresolved for the overall change (chain strategy still `pending`); this batch executes Work Unit 2 ("PowerShell renderer + goldens + real-`pwsh` test (Phase 3)") as an autonomous, revertible slice, following Unit 1 (Phases 1–2, already landed in commits `6cd3fbb`/`8100b0a` on this branch) and regardless of which chain strategy the orchestrator ultimately picks for the remaining units.
- Current work unit: Unit 2 of 6 (per the Suggested Work Units table in `tasks.md`)
- Boundary: starts from Phases 1–2 complete (Unit 1); ends with Phase 3 fully green, isolated from Phases 4–9 (no other package imports `powershellRenderer` or `quotePowerShell` yet), and with `posix.go`/`header.go`/every pre-existing golden file byte-identical to before this batch.
- Estimated review budget impact: moderate on its own (2 new source files, 1 new test file, 3 new goldens, ~60 changed lines across 3 pre-existing test/registry files) — comfortably under the 400-line guard for this unit alone, consistent with the tasks artifact's per-unit forecast.

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

### Phase 3 (this batch)

```
$ export PATH="<scratchpad>/pwsh:$PATH"   # the provided real pwsh, not on PATH by default
$ go test ./internal/renderers/... -v
...
--- PASS: TestGolden (0.00s)
--- PASS: TestQuotePOSIX (0.00s)
--- PASS: TestQuotePOSIXNeutralizesBreakout (0.00s)
--- PASS: TestRenderIsDeterministic (0.00s)
--- PASS: TestRenderRejectsInvalidConfig (0.00s)
--- PASS: TestRenderRejectsCommentInjection (0.00s)
--- PASS: TestForUnsupportedShell (0.00s)
--- PASS: TestPosixRendererShell (0.00s)
--- PASS: TestSupported (0.00s)
--- PASS: TestGeneratedFileIsInertInRealPowerShell (0.83s)
    --- PASS: TestGeneratedFileIsInertInRealPowerShell/hostile_commands_execute_nothing_at_source_time (0.55s)
    --- PASS: TestGeneratedFileIsInertInRealPowerShell/arguments_forward_intact_through_both_@args (0.28s)
--- PASS: TestQuotePowerShell (0.00s)
--- PASS: TestQuotePowerShellNeutralizesBreakout (0.00s)
--- PASS: TestRenderArgsForwardedTwice (0.00s)
--- PASS: TestPowerShellRendererShell (0.00s)
--- PASS: TestGeneratedFileIsInertInRealShells (0.04s)
    --- PASS: TestGeneratedFileIsInertInRealShells/bash (0.01s)
    --- PASS: TestGeneratedFileIsInertInRealShells/zsh (0.04s)
PASS
ok  	github.com/angeltonio/aliasdeck/internal/renderers	1.254s

$ go test ./...
ok  	github.com/angeltonio/aliasdeck/cmd/aliasdeck	0.432s
ok  	github.com/angeltonio/aliasdeck/internal/app	2.615s
ok  	github.com/angeltonio/aliasdeck/internal/apply	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/config	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/domain	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/renderers	0.957s
?   	github.com/angeltonio/aliasdeck/internal/shelltest	[no test files]
ok  	github.com/angeltonio/aliasdeck/internal/source	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/state	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/validate	(cached)

$ make ci
go vet ./...
go test -race ./...
ok  	github.com/angeltonio/aliasdeck/cmd/aliasdeck	1.469s
ok  	github.com/angeltonio/aliasdeck/internal/app	3.850s
ok  	github.com/angeltonio/aliasdeck/internal/apply	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/config	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/domain	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/renderers	2.837s
?   	github.com/angeltonio/aliasdeck/internal/shelltest	[no test files]
ok  	github.com/angeltonio/aliasdeck/internal/source	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/state	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/validate	(cached)
CI checks passed

$ go test ./internal/renderers/... -cover
ok  	github.com/angeltonio/aliasdeck/internal/renderers	0.740s	coverage: 92.0% of statements

$ gofmt -l .
(no output — everything formatted, after fixing the gofmt/smart-quote gotcha above)

$ git diff --stat internal/renderers/testdata/
(empty — only new files were added: powershell_basic.golden, powershell_empty.golden, powershell_awkward_commands.golden)
```

No git commit was created. Changes are left in the working tree per the orchestrator's delivery-strategy instructions.

## Engram

`mem_save`/`mem_search`/`mem_update` tools were not bound in this session's tool set (consistent with prior batches — Phases 1–2 reported the same). Progress was persisted only to this file (`openspec/changes/powershell-windows/apply-progress.md`) and to `tasks.md`. No Engram tool call was attempted or fabricated.
