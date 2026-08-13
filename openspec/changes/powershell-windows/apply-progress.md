# Apply Progress: PowerShell, Windows and Git-hosted config — Milestone 3 (v0.2)

**Batches**: 1 (Phases 1–2) + 2 (Phase 3) + 3 (Phase 4) + 4 (Phase 5), per orchestrator scope
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

### Phase 4 (this batch): Windows Apply — Defects A+B, EOL, `.ps1` Output

- [x] 4.1 RED: `internal/apply/native_test.go` — added `{domain.ShellPowerShell, "ps1"}` to `TestNativeBackendOutputPath`'s table (fails: `no generated-file extension defined for shell "powershell"`); inverted `TestNativeBackendOutputPathUnsupportedShell` to use `domain.Shell("fish")` instead of `ShellPowerShell` (every real shell now has a defined extension).
- [x] 4.2 GREEN: `internal/apply/native.go` — `shellFileExt` gained `case domain.ShellPowerShell: return "ps1", nil`.
- [x] 4.3 RED: `internal/apply/bootstrap_test.go` — `BootstrapLine` gained a `sh domain.Shell` parameter (compile-error RED across the whole package, since every existing call site needed updating simultaneously — the expected shape of a signature-changing RED under Go's whole-package compilation). New cases: zsh (existing 3, unchanged expectations), a bash case proving byte-identical POSIX output, a `home==""` case, and four PowerShell cases (Windows-shaped path under `$HOME`, outside `$HOME`, `home==""`, and one exercising the double-quote escaper on a path containing `` ` ``, `"`, `$`). "Windows-shaped" paths are built with `filepath.Join` (not literal backslash strings) so the exact same test source exercises the real backslash-separator branch of `filepath.Rel` once this suite runs under `GOOS=windows` (Phase 8) — literal backslash strings would not, since `filepath.Rel` is OS-native and this CI host is not Windows; this limitation and its resolution are documented inline in the test file.
- [x] 4.4 GREEN (Defect A fix, design decisions 4+5): `BootstrapLine(sh domain.Shell, generatedPath, home string)` — replaced `strings.CutPrefix`+`rel[0]=='/'` with a new `relUnderHome` helper (`filepath.Rel`, reject a `..`-bearing result); POSIX branch (`homeRelativeDisplay`) emits `"$HOME/"+filepath.ToSlash(rel)`, byte-identical to the old output on every existing test case (verified: `filepath.Rel` on this build reproduces the exact same accept/reject decisions as the old `CutPrefix` check for "under home", "outside home", and the "user"/"user2" prefix-collision case). New `bootstrapLinePowerShell` + `escapePowerShellDoubleQuoted` (backtick→``` `` ```, `"`→`""`, `$`→`` `$ ``) implement the PowerShell form; the escaper is applied only to the relative remainder, never to the literal `"$HOME/"` prefix, so PowerShell still expands `$HOME` as its automatic variable instead of it being escaped to a literal `` `$HOME ``.
- [x] 4.5 GREEN: `AddBootstrap(rcPath string, sh domain.Shell, generatedPath, home string)` — threads `sh` into `BootstrapLine`; POSIX branch unchanged bytes. Updated `internal/app/init.go`'s three call sites (both `BootstrapLine` calls for the manual-line message, one `AddBootstrap` call) to pass `dc.Device.Shell`. Updated every `bootstrap_test.go`/`roundtrip_test.go` call site for the new signature.
- [x] 4.6 RED (Defect B + CRLF, decisions 6+7 — landed as one unit, per the non-negotiable ordering rule): extended `roundtrip_test.go`'s existing CRLF case with an assertion that the appended block's own line endings match the rc file's pre-existing convention (fails to compile: no such check existed, and more importantly the underlying mechanism didn't exist yet). Added to `bootstrap_test.go`: `TestDetectEOL` (5-case table: empty, LF-only, CRLF, one-CRLF-among-LF, no-trailing-newline), `TestBootstrapCRLFAddRemoveRoundTrip` (add/remove on a CRLF `$PROFILE`-shaped file, unedited), and `TestRemoveBootstrapMarkerScanFallbackOnCRLFProfile` — the critical proof: seeds a CRLF file, calls `AddBootstrap`, edits the sourcing line inside the block (forcing the exact-bytes match to miss), then asserts `RemoveBootstrap` still finds and removes the block via the marker-scan fallback and restores the surrounding CRLF content byte-for-byte. Observed failing to compile (`undefined: detectEOL`) before any production code existed.
- [x] 4.7 GREEN (same task as 4.6): added `detectEOL(existing []byte) string` (`"\r\n"` iff `existing` already contains one, else `"\n"`); threaded `eol` through `buildBlock(existing []byte, line, eol string)`, replacing every hardcoded `"\n"` append; fixed `indexOfLine`'s `atLineEnd` check to also accept a `\r`+`\n` pair (previously required `content[end] == '\n'` exactly, which a CRLF-terminated marker line fails on the `\r` byte); fixed `removeMarkerScan`'s blank-separator-line and trailing-newline consumption via a new shared `stripTrailingEOL(content []byte, pos int) (int, bool)` helper that accepts either terminator, generalizing the old bare-`\n`-only backward/forward checks.
- [x] 4.8 Verify: `go test ./internal/apply/...` — all tests pass, including the new CRLF cases and every pre-existing POSIX byte-identical assertion, unmodified. See "Adversarial validation" and "Verification" below for the full evidence.

### Adversarial validation of the CRLF marker-scan fallback (not a tracked task, done for the same reason Phase 3 did it — this is the security-critical, user-file-mutating phase, and the task prompt explicitly demanded proof the fix was genuinely load-bearing)

Before trusting `TestRemoveBootstrapMarkerScanFallbackOnCRLFProfile` as the regression guard the ordering rule exists for, `indexOfLine`'s `atLineEnd` check was temporarily reverted to its original LF-only form (`atLineEnd := end == len(content) || content[end] == '\n'`, dropping the added `\r`+`\n` branch) via a surgical single-hunk edit — not a full-file revert, since the `AddBootstrap`/`BootstrapLine` signature changes from earlier in this same batch had to stay in place for the package to compile — and the test was rerun. It failed exactly as the design document predicts: `RemoveBootstrapMarkerScanFallbackOnCRLFProfile` reported `RemoveBootstrap() must report a non-exact restore when it falls back to marker scanning` — meaning the buggy `indexOfLine` could not find the CRLF-terminated marker lines at all, `removeMarkerScan` returned `found=false`, and `RemoveBootstrap` silently reported `exact=true` (nothing removed) instead of finding and cutting the block. This is the literal, measured shape of the bug the design document and the task prompt describe: a user who edited inside a CRLF `$PROFILE`'s block would have kept that block forever, with no error and no indication anything was wrong. The correct `atLineEnd` check was then restored (`diff` against the pre-revert file was byte-identical afterward, confirming a clean restore) and the full suite was rerun green.

### Gotcha discovered mid-phase: gofmt mangles literal `''` inside doc-comment prose

Go's `gofmt` (confirmed on the actual `go1.25.11` binary, not a shim artifact) reformats top-level doc comments via `go/doc/comment` and treats two adjacent straight single quotes (`''`) appearing inline in prose (inside backticks, outside an indented code block) as a typewriter-style double-quote convention, silently rewriting them to a Unicode right-double-quotation-mark (U+201D). This is exactly the sequence this renderer's own doc comments needed to describe (PowerShell's `''` doubling), so `quotePowerShell`'s doc comment and two test doc comments were rewritten to describe the doubling in words instead of showing bare `''` inline in prose — the indented code-block example (`don't -> 'don''t'`) is unaffected because gofmt preserves preformatted blocks verbatim, and Go string literals in test tables are code, not comments, so they were never at risk. Caught by running `gofmt -l .` and inspecting the diff (`gofmt -d`) before trusting `-w`; worth flagging for any future PowerShell-adjacent Go doc comment in this codebase.

### Phase 5 (this batch): PowerShell `$PROFILE` Resolution

- [x] 5.1 RED: new `internal/app/pwshprofile_test.go` — `TestResolvePowerShellProfile` precedence table (both editions present, only Desktop, only Core, neither/Core default) against a fake `Env.LookPath` (`lookPathFake`, built from `newTestEnv`'s already-not-found default), and `$ALIASDECK_PWSH_PROFILE` overriding detection entirely. Observed failing to compile (`undefined: resolvePowerShellProfile`, `undefined: pwshEditionCore`, `undefined: pwshEditionDesktop`) before any production code existed.
- [x] 5.2 RED: same file — `TestResolvePowerShellProfile`'s two OneDrive sub-tests (redirection present: `$HOME\Documents` absent, `$OneDrive` names an existing `Documents`; redirection absent: `$OneDrive` set but names no existing `Documents`, falls back to the default) plus a dedicated `TestWindowsDocumentsDir` isolating design decision 9's pure path logic from LookPath/edition precedence. Same compile-error RED as 5.1 (one new file, one RED observation covers both tasks' new cases).
- [x] 5.3 RED (**inversion, not weakening**, of `app/misc_test.go:177`): before touching `misc_test.go`, the pre-change assertion (`resolveRCPath(..., ShellPowerShell, PlatformMacOS, "") must return an error`) was reproduced verbatim in a throwaway scratch test file and run against the already-landed 5.4/5.5 implementation; it failed with `resolveRCPath() must fail for a shell with no rc-file convention`, proving the new behavior genuinely changed what the old assertion checked. The scratch file was deleted immediately after this observation (see "RED evidence for the inversion" below for the exact command and output).
- [x] 5.4 GREEN: new `internal/app/pwshprofile.go` — `pwshEdition` (`Core`/`Desktop`), `pwshProfile{Path, Edition, Provenance, OtherPath, OtherExists}`, `resolvePowerShellProfile(env Env, platform domain.Platform) (pwshProfile, error)` implementing the precedence chain, `pwshProfilePaths` (Windows-vs-non-Windows split, decisions 9/10), `windowsDocumentsDir` (decision 9, pure function of `home`/`getenv` so it is testable on any host OS), `pathExists`/`isDir`/`withDocsProvenance` helpers. `TestResolvePowerShellProfile` and `TestWindowsDocumentsDir` pass.
- [x] 5.5 GREEN: `internal/app/rcpath.go` — `resolveRCPath` gained a `case domain.ShellPowerShell:` delegating to `resolvePowerShellProfile(env, platform)`, using the `platform domain.Platform` parameter `resolveRCPath` already receives (no new parameter threading needed elsewhere: `internal/app/init.go`'s one call site already passes `dc.Device.Platform`, itself resolved via `config.DetectPlatform`, which already carries Phase 2's `$ALIASDECK_PLATFORM` test seam). `--rc-file` is unaffected: it is checked and returned before the shell `switch` is ever reached, so it overrides PowerShell exactly like every other shell, with zero PowerShell-specific code.
- [x] 5.6 GREEN: `internal/app/misc_test.go:177`'s `"unsupported shell is an error"` sub-test replaced with three inverted/new sub-tests (`"powershell on macOS resolves the Core profile, not an error"`, `"powershell on Windows resolves under Documents"`, `"--rc-file overrides PowerShell detection too"`) plus a retained `"unsupported shell is an error"` sub-test now using `domain.Shell("fish")` (a shell AliasDeck does not model at all), since every *real* shell now has an rc-file convention — the same retained-but-inverted pattern Phase 3/4 used for `posix_test.go`/`native_test.go`. Test comment explicitly labels this an inversion and cites the design doc's "Planned assertion inversions" table.

**Deviation from design.md, corrected in-place** (see the "Deviations from Design" section below for the full rationale): design decision 8's row literally wrote `resolvePowerShellProfile(env) (pwshProfile, error)`, omitting a platform parameter. Implemented as `resolvePowerShellProfile(env Env, platform domain.Platform)` instead, reusing `resolveRCPath`'s existing `platform` parameter and Phase 2's `$ALIASDECK_PLATFORM` test seam. Without it, decision 9 (Windows Documents/OneDrive) and decision 10 (non-Windows `~/.config/powershell`) could not both be unit-tested on the same non-Windows CI host, since the two decisions apply to genuinely different platforms and a hard-coded `runtime.GOOS` read inside the function would make the Windows branch untestable without a real Windows machine. `design.md`'s decision-8 row was updated in the same commit-worthy change to reflect the implemented signature and explain why, rather than leaving the design doc silently wrong.

**RED evidence for the inversion (5.3), reproduced exactly**:

```
$ cat internal/app/scratch_inversion_check_test.go   # temporary, deleted right after
package app

import (
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

func TestScratchOldAssertionNowFails(t *testing.T) {
	te := newTestEnv(t)
	if _, err := resolveRCPath(te.Env, domain.ShellPowerShell, domain.PlatformMacOS, ""); err == nil {
		t.Error("resolveRCPath() must fail for a shell with no rc-file convention")
	}
}

$ go test ./internal/app/... -run TestScratchOldAssertionNowFails -v
=== RUN   TestScratchOldAssertionNowFails
    scratch_inversion_check_test.go:16: resolveRCPath() must fail for a shell with no rc-file convention
--- FAIL: TestScratchOldAssertionNowFails (0.00s)
FAIL
FAIL	github.com/angeltonio/aliasdeck/internal/app	0.373s
FAIL

$ rm internal/app/scratch_inversion_check_test.go
```

## TDD Cycle Evidence

| Task | RED (test written first, observed failing) | GREEN (implementation, observed passing) | REFACTOR |
|---|---|---|---|
| 1.2/1.3 | `go test ./internal/config/... -run TestParseDeviceConfigGitSource` — compile error: `cfg.Source.Git undefined` | Same command passes after adding `GitSourceConfig`/`gitSourceDTO` | None needed — matches existing `Source`/`sourceDTO` mirroring pattern |
| 2.1/2.2 | `go test ./internal/config/... -run TestDetectPlatformPrecedence -v` — `windows_maps_to_windows_via_runtime.GOOS` failed: `unsupported operating system "windows"` | Same command passes after adding the `case "windows"` branch | None needed — one-line addition matching existing darwin/linux cases |
| 2.3/2.4 | `go test ./internal/config/... -run TestExpandPath -v` — `tilde_backslash_prefix` failed: got `"~\\dotfiles\\aliases.yaml"`, want `"/home/user/dotfiles/aliases.yaml"` | Same command passes after adding `tildePrefixRest` and backslash-to-native-separator normalization | Factored the shared "~/" / "~\\" prefix-stripping into `tildePrefixRest` rather than inlining a second `HasPrefix` branch, to keep `ExpandPath` readable |
| 3.1/3.2 | `go test ./internal/renderers/... -run 'TestQuotePowerShell\|TestRenderArgsForwardedTwice\|TestPowerShellRendererShell' -v` — compile error: `undefined: quotePowerShell` (twice) | Same command passes after adding `powershell.go` (`powershellRenderer`, `quotePowerShell`, `Render`) and registering `domain.ShellPowerShell` in `renderer.go`'s `registry` | None needed — mirrors `posixRenderer`'s structure (guard → clone/sort → `writeHeader` → per-alias loop) with an unrelated grammar inside the loop |
| 3.4 | `go test ./internal/renderers/... -v` after 3.2's GREEN — `TestForUnsupportedShell` failed (`expected PowerShell to be unsupported`), `TestSupported` failed (`Supported() = [zsh bash powershell], want [zsh bash]`) | Both fixed by inverting the assertions in `posix_test.go` to the positive guarantee; full suite green after | None needed |
| 3.5/3.6 | New `powershell_integration_test.go` run against real `pwsh` (`PATH` prepended with the provided pwsh directory) — passed on first run because the renderer under test (3.2) was already correct; RED was instead demonstrated adversarially (see "Adversarial validation" note above): reverting `Render` to raw, unquoted interpolation made both integration sub-tests fail against real `pwsh` with a `ParserError` | Restoring the correct `Render` implementation returned the suite to green (`git diff internal/renderers/powershell.go` empty after revert) | None needed |
| 4.1/4.2 | `go test ./internal/apply/... -run TestNativeBackendOutputPath -v` — `TestNativeBackendOutputPath/powershell` failed: `OutputPath() returned an error: no generated-file extension defined for shell "powershell"` | Same command passes after adding `case domain.ShellPowerShell: return "ps1", nil` to `shellFileExt` | None needed — one-line addition matching the existing zsh/bash cases |
| 4.3/4.4/4.5 | `go test ./internal/apply/... -run TestBootstrapLine -v` (and the whole package) — compile errors: `too many arguments in call to BootstrapLine`/`AddBootstrap` across every existing call site, since the signature change (`sh domain.Shell` parameter) is package-wide by construction | Same commands pass after: `relUnderHome` (filepath.Rel-based, replacing the `strings.CutPrefix`+`rel[0]=='/'` check), `escapePowerShellDoubleQuoted`, `bootstrapLinePowerShell`, `homeRelativeDisplay`, the `BootstrapLine`/`AddBootstrap` signature changes, and the three `internal/app/init.go` call-site updates (`dc.Device.Shell`) | None needed — the POSIX branch is a straight extraction of the pre-existing logic into `homeRelativeDisplay`, verified byte-identical below |
| 4.6/4.7 | `go test ./internal/apply/... -run TestDetectEOL -v` — compile error: `undefined: detectEOL`; `roundtrip_test.go`'s extended CRLF assertion could not even be written against working code | Same commands pass after `detectEOL`, `eol`-threaded `buildBlock`, the CRLF-aware `indexOfLine.atLineEnd` check, and the new `stripTrailingEOL` helper feeding `removeMarkerScan`'s blank-line/trailing-newline logic | None needed — `stripTrailingEOL` is a single shared primitive replacing two separate bare-`\n` checks that would otherwise each need their own CRLF branch |
| 5.1/5.2 | `go test ./internal/app/... -run 'TestResolvePowerShellProfile\|TestWindowsDocumentsDir' -v` — compile errors: `undefined: resolvePowerShellProfile`, `undefined: pwshEditionCore`, `undefined: pwshEditionDesktop` (`windowsDocumentsDir` too, once the first three were fixed) | Same command passes after adding `pwshprofile.go` in full (all four LookPath precedence branches, `$ALIASDECK_PWSH_PROFILE`, `windowsDocumentsDir`, both platform branches) | None needed — `pwshProfilePaths` extracts the Windows-vs-non-Windows split so `resolvePowerShellProfile`'s four precedence branches stay free of platform conditionals |
| 5.3 | The old `app/misc_test.go:177` assertion reproduced verbatim in a throwaway scratch test and run with `go test ./internal/app/... -run TestScratchOldAssertionNowFails -v` **after** 5.4/5.5's GREEN — failed: `resolveRCPath() must fail for a shell with no rc-file convention` (see "RED evidence for the inversion" above) | `misc_test.go` updated to the three inverted/new sub-tests plus the retained `fish`-based unsupported-shell case; `go test ./internal/app/... -run TestResolveRCPath -v` passes | None needed |

**Note on 5.3's RED ordering**: unlike 5.1/5.2/5.4/5.5 (new behavior, test-first), 5.3 is an *inversion* of an existing assertion — the same pattern Phase 3 (`posix_test.go` `TestForUnsupportedShell`/`TestSupported`) and Phase 4 (`native_test.go` `TestNativeBackendOutputPathUnsupportedShell`) used: the RED evidence is the pre-existing assertion breaking once the new capability lands, not a new test written before code that doesn't exist yet. The scratch file made that breakage an explicit, observed, reproducible step instead of an inferred one.

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

### Unit 3 (Phase 4)

| Evidence | Value |
|---|---|
| Focused test command and exact result | `go test ./internal/apply/... -v` → all tests pass (see full output below); `go test ./internal/apply/... -cover` → 84.9% |
| Runtime integration harness | `TestRemoveBootstrapMarkerScanFallbackOnCRLFProfile` — not a spawned process, but the real filesystem round-trip this package exists to protect: seeds a CRLF `$PROFILE`-shaped file in `t.TempDir()`, runs `AddBootstrap`, hand-edits the sourcing line inside the block (forcing the exact-byte match to miss), then asserts `RemoveBootstrap` still finds it via the marker-scan fallback and restores the surrounding CRLF content byte-for-byte. This is the exact scenario the ordering rule in tasks.md 4.6/4.7 exists to prevent from regressing silently. Adversarially validated by reverting `indexOfLine`'s CRLF fix and observing this exact test fail (see "Adversarial validation" note above). |
| Rollback boundary | Revert `internal/apply/bootstrap.go`, `bootstrap_test.go`, `native.go`, `native_test.go`, `roundtrip_test.go`, and the three `apply.BootstrapLine`/`apply.AddBootstrap` call sites in `internal/app/init.go` (same commit, per the tasks artifact's own rollback note for this unit). No other package calls `apply.BootstrapLine`/`apply.AddBootstrap`/`shellFileExt` besides `internal/app/init.go` and `internal/apply/native.go` itself, so this reverts cleanly. |

### Unit 4 (Phase 5)

| Evidence | Value |
|---|---|
| Focused test command and exact result | `go test ./internal/app/... -v` → all tests pass, including `TestResolvePowerShellProfile` (8 sub-cases), `TestWindowsDocumentsDir` (3 sub-cases), and the updated `TestResolveRCPath` (10 sub-cases, 3 of them new/inverted for PowerShell); `go test ./internal/app/... -cover` → 81.8% |
| Runtime integration harness | N/A, as the tasks artifact's own forecast for this unit states: "fake `Env.LookPath`, no real process spawned by design." Confirmed: `resolvePowerShellProfile` never calls `exec.LookPath` directly — only through the injected `Env.LookPath` field, which is `lookPathFake` (never a real binary lookup) in every test in this batch. `windowsDocumentsDir` and `pathExists`/`isDir` touch only `t.TempDir()` paths, no real `$HOME`. |
| Rollback boundary | Remove `internal/app/pwshprofile.go` and `internal/app/pwshprofile_test.go`; revert the `case domain.ShellPowerShell:` branch added to `internal/app/rcpath.go`'s switch (restores the pre-change `"no rc file convention defined for shell..."` error for PowerShell); revert `internal/app/misc_test.go`'s three new/inverted sub-tests back to the single pre-change `"unsupported shell is an error"` assertion. No other package calls `resolvePowerShellProfile`/`pwshProfilePaths`/`windowsDocumentsDir` yet (Phase 7's `status`/`doctor` wiring has not landed), so this reverts cleanly. |

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
| `internal/apply/native.go` | Modified | `shellFileExt` gained `case domain.ShellPowerShell: return "ps1", nil` |
| `internal/apply/native_test.go` | Modified | Added the `ps1` case to `TestNativeBackendOutputPath`'s table; inverted `TestNativeBackendOutputPathUnsupportedShell` to use `domain.Shell("fish")` |
| `internal/apply/bootstrap.go` | Modified | `BootstrapLine`/`AddBootstrap` gained a `sh domain.Shell` parameter; added `relUnderHome` (Defect A fix, filepath.Rel-based), `homeRelativeDisplay` (POSIX branch), `bootstrapLinePowerShell`, `escapePowerShellDoubleQuoted` (Defect A's PowerShell counterpart + decision 5's escaper), `detectEOL`, `stripTrailingEOL` (decision 6/7); `buildBlock` gained an `eol` parameter; `indexOfLine`'s `atLineEnd` and `removeMarkerScan`'s blank-line/trailing-newline handling now accept `\r\n` (Defect B fix) |
| `internal/apply/bootstrap_test.go` | Modified | `TestBootstrapLine` extended with shell-parameterized cases (bash, `home==""`, four PowerShell cases); every `AddBootstrap`/`BootstrapLine` call site updated for the new signature; added `TestDetectEOL`, `TestBootstrapCRLFAddRemoveRoundTrip`, `TestRemoveBootstrapMarkerScanFallbackOnCRLFProfile` |
| `internal/apply/roundtrip_test.go` | Modified | Every `AddBootstrap` call site updated for the new signature; extended the CRLF case with a block-CRLF-usage assertion (design decision 6) |
| `internal/app/init.go` | Modified | Three call sites (`apply.BootstrapLine` ×2, `apply.AddBootstrap` ×1) now pass `dc.Device.Shell` |
| `openspec/changes/powershell-windows/tasks.md` | Modified | Marked 1.1–2.4 (batch 1), 3.1–3.6 (batch 2), 4.1–4.8 (batch 3), and 5.1–5.6 (batch 4) as `[x]` |
| `internal/app/pwshprofile.go` | Created | `pwshEdition`, `pwshProfile`, `resolvePowerShellProfile(env, platform)` (precedence chain, design decision 8), `pwshProfilePaths` (Windows/non-Windows split, decisions 9/10), `windowsDocumentsDir` (OneDrive redirection, decision 9), `pathExists`/`isDir`/`withDocsProvenance` helpers |
| `internal/app/pwshprofile_test.go` | Created | `TestResolvePowerShellProfile` (both-editions/only-Desktop/only-Core/neither, `$ALIASDECK_PWSH_PROFILE` override, OneDrive redirection present/absent, non-Windows Core path), `TestWindowsDocumentsDir`, `lookPathFake`/`mustWriteFile` test helpers |
| `internal/app/rcpath.go` | Modified | `resolveRCPath` gained `case domain.ShellPowerShell:` delegating to `resolvePowerShellProfile(env, platform)` |
| `internal/app/misc_test.go` | Modified | `TestResolveRCPath` gained 3 PowerShell sub-tests (macOS Core path — **inversion** of the old error assertion, Windows Documents path, `--rc-file` override); retained `"unsupported shell is an error"` now uses `domain.Shell("fish")` instead of `domain.ShellPowerShell` |
| `openspec/changes/powershell-windows/design.md` | Modified | Decision 8's row updated to the implemented `resolvePowerShellProfile(env, platform)` signature, with rationale for the added `platform` parameter |

## Deviations from Design

**One deviation for Phase 5, corrected in `design.md` itself (see the Phase 5 task list above for the full rationale)**: decision 8's row wrote `resolvePowerShellProfile(env) (pwshProfile, error)`, with no platform parameter. Implemented as `resolvePowerShellProfile(env Env, platform domain.Platform)`. Reason: decisions 9 (Windows `Documents`/OneDrive) and 10 (non-Windows `~/.config/powershell`) are genuinely different platform behaviors, not a filesystem-shape heuristic; reading `runtime.GOOS` directly inside the function (the only alternative that matches the literal no-platform-parameter signature) would make decision 9's Windows-only branch permanently untestable on this project's actual CI hosts (macOS/Linux) until Phase 8's Windows runner exists — which would leave 5.1/5.2's RED/GREEN cycle impossible to observe on this machine, violating Strict TDD's "observed failing" requirement. Threading the `platform domain.Platform` parameter `resolveRCPath` already receives (itself resolved via `config.DetectPlatform`, which already carries Phase 2's `$ALIASDECK_PLATFORM` test seam) avoids adding any new test seam and keeps every Windows-path test a pure, real, no-mocking-of-globals unit test. `internal/app/init.go` needed zero changes: its one `resolveRCPath` call site already passed `dc.Device.Platform`.

Everything else in Phase 5 matches design decisions 8, 9, and 10 as implemented: never both profiles chosen (verified by every `TestResolvePowerShellProfile` sub-case asserting exactly one `Edition`), `LookPath` never spawns a process (verified: `pwshprofile_test.go` never imports `os/exec`, only `Env.LookPath`, which is `lookPathFake` in every case), and the OneDrive fallback only activates when `$HOME\Documents` is absent, per decision 9's literal ordering.

None for Phase 4 — implementation matches design decisions 3, 4, 5, 6, and 7 verbatim, including the non-negotiable ordering rule (4.6/4.7 landed as one unit, verified adversarially — see above) and the constraint that the POSIX branch stay byte-identical (verified: `filepath.Rel`-based `relUnderHome` reproduces the exact same accept/reject decisions as the old `strings.CutPrefix` check on every pre-existing test case, and every pre-existing POSIX assertion in `bootstrap_test.go`/`roundtrip_test.go` passed unmodified).

One documented limitation, not a deviation: the "Windows-shaped paths" in `TestBootstrapLine`'s new PowerShell cases are built with `filepath.Join` rather than literal backslash strings, because `filepath.Rel` is OS-native and this CI host is not Windows — a literal backslash string would exercise the *wrong* (POSIX) branch of `filepath.Rel` on this host and could not prove the Windows-separator behavior either way. The same test source will exercise the true backslash-separator branch once Phase 8's Windows CI matrix runs this suite under `GOOS=windows`, with no code change required. This is called out inline in the test file's comments.

None for Phase 3 — implementation matches design decisions 1 and 2 (renderer shape, escaping), the interface shown in "Interfaces" (§ PowerShell function form), and the testing strategy table's golden/unit/integration rows verbatim.

Phases 1–2: None — implementation matches design decisions 16 and the "Windows Path Handling" table's `config.ExpandPath`/`config.DetectPlatform` rows.

One clarification worth flagging: design decision 16 and the "Windows Path Handling" table both describe `~\` recognition in terms of `os.PathSeparator`. Implemented instead as an explicit, always-recognized `~\` literal (in addition to `~/`), independent of the host OS's `os.PathSeparator`. This is a deliberate, narrower-than-literal-text but design-intent-preserving choice: `os.PathSeparator` is `/` on the macOS/Linux CI runners that run this suite today (Windows-in-CI is Phase 8, out of this batch's scope), so gating recognition on `os.PathSeparator` would make the Windows-shaped-path test un-exercisable until Phase 8 lands, and would only work when AliasDeck itself runs on Windows — not when a Windows-authored `config.yaml`/`source.path` is inspected or tested elsewhere. Recognizing the literal backslash unconditionally satisfies the task's literal acceptance criterion ("`ExpandPath` handles `~\dotfiles\aliases.yaml`") on every CI runner and matches the design rationale ("one path shape across three operating systems") more directly than a GOOS-gated check would. POSIX-authored paths (`~/...`) are unaffected.

## Issues Found

None for Phase 5.

Note for whoever reviews or extends this package next (Phase 7 wires `status`/`doctor`): `resolvePowerShellProfile`'s `pwshProfile.OtherPath`/`OtherExists` are already computed on every code path specifically so Phase 7's `doctor` "other-edition-profile-exists" warning (non-negotiable constraint 1) needs no new detection logic — just read the fields off a call to `resolvePowerShellProfile(env, dc.Device.Platform)`. Do not call `resolvePowerShellProfile` a second time with different inputs elsewhere; if `status`/`doctor` need the same resolution `init`/`sync` used, thread the already-resolved `pwshProfile` (or re-derive it identically) rather than risking two call sites disagreeing on which edition was chosen — that would recreate exactly the silent-mismatch failure mode this phase exists to prevent.

None for Phase 4.

Note for whoever reviews or extends this package next (Phase 5 calls `resolveRCPath`, which is a different function of the same name in `internal/app/rcpath.go`, not `internal/apply/bootstrap.go`'s — do not confuse the two when threading PowerShell's `$PROFILE` resolution through): `apply.BootstrapLine`/`apply.AddBootstrap` now require a `domain.Shell`. Any future caller must pass the actual detected shell, not assume POSIX — `dc.Device.Shell` is the pattern used in `internal/app/init.go`.

None for Phase 3.

Note carried from Phase 3 for whoever reviews or extends this package: Go's `gofmt` doc-comment reformatter mangles literal `''` written inline in comment prose (see the "Gotcha" note above). Any future doc comment describing PowerShell's quote-doubling — or any other language's `''`-shaped syntax — should either avoid the bare pair in inline prose or move the example into an indented code block, and `gofmt -d` should be inspected (not just `gofmt -l`) before trusting `-w` on renderer files.

## Remaining Tasks (not in this batch's scope)

- [ ] Phase 6: GitSource + State Staleness (6.1–6.9)
- [ ] Phase 7: CLI Reporting — status/doctor (7.1–7.4)
- [ ] Phase 8: CI Matrix & Release (8.1–8.6)
- [ ] Phase 9: Docs & Final Verification (9.1–9.4)

## Workload / PR Boundary

- Mode: `ask-on-risk` forecast in tasks.md remains unresolved for the overall change (chain strategy still `pending`); this batch executes Work Unit 4 ("`$PROFILE` resolution, both editions + macOS/Linux Core (Phase 5)") as an autonomous, revertible slice, following Units 1–3 (Phases 1–4, already landed in this working tree) and regardless of which chain strategy the orchestrator ultimately picks for the remaining units. The orchestrator explicitly scoped this batch to "Phase 5 only," so this executor proceeded without re-raising the unresolved chain-strategy decision, consistent with how Phases 3 and 4 each proceeded on their own explicitly-scoped batch.
- Current work unit: Unit 4 of 6 (per the Suggested Work Units table in `tasks.md`)
- Boundary: starts from Phase 4 complete (Unit 3); ends with Phase 5 fully green, isolated from Phases 6–9 (Phase 7's `status`/`doctor` wiring calls `resolvePowerShellProfile` and has not landed yet, so nothing downstream depends on this unit outside `internal/app/rcpath.go`'s one new `case` branch). Per the tasks artifact's own rollback note for this unit: remove `pwshprofile.go`/`pwshprofile_test.go` and revert `rcpath.go`'s new case and `misc_test.go`'s inverted sub-tests together as one commit; `resolveRCPath` reverts to the pre-change PowerShell error.
- Estimated review budget impact: `git diff --stat` for this unit (excluding new untracked files, which `--stat` does not show without `--no-index`): `internal/app/misc_test.go` +46/-1, `internal/app/rcpath.go` +9/0; new files `internal/app/pwshprofile.go` (178 lines) and `internal/app/pwshprofile_test.go` (268 lines). Total authored addition ≈501 lines — over the 400-line single-PR guard on its own, driven by the breadth of the precedence/OneDrive/non-Windows table-driven test coverage the task list explicitly requires (both editions, only each edition, neither, two overrides, OneDrive present/absent, non-Windows). Flagged here per the review workload guard; the orchestrator's overall delivery-strategy decision for this change remains `pending`/`ask-on-risk`, and this unit's own rollback boundary above keeps it independently revertible regardless of how that decision resolves.

- Mode: `ask-on-risk` forecast in tasks.md remains unresolved for the overall change (chain strategy still `pending`); this batch executes Work Unit 3 ("Bootstrap defects A+B, EOL preservation, `.ps1` output (Phase 4)") as an autonomous, revertible slice, following Units 1 and 2 (Phases 1–3, already landed in this working tree) and regardless of which chain strategy the orchestrator ultimately picks for the remaining units.
- Current work unit: Unit 3 of 6 (per the Suggested Work Units table in `tasks.md`)
- Boundary: starts from Phase 3 complete (Unit 2); ends with Phase 4 fully green, isolated from Phases 5–9 (Phase 5's `resolveRCPath` calls `apply.AddBootstrap`/`apply.BootstrapLine` and has not landed yet, so nothing downstream depends on this unit outside `internal/app/init.go`'s three already-updated call sites). Per the tasks artifact's own rollback note for this unit: revert `internal/apply/bootstrap.go`/`native.go` and their test files, plus `internal/app/init.go`'s three call-site updates, together as one commit.
- Estimated review budget impact: small on its own (2 modified source files in `internal/apply` — `bootstrap.go`, `native.go` — plus their tests, and 3 call-site lines in `internal/app/init.go`; `git diff --stat` for this unit: 6 files, 393 insertions, 46 deletions) — comfortably under the 400-line guard for this unit alone, consistent with the tasks artifact's per-unit forecast.

- Mode: `ask-on-risk` forecast in tasks.md remains unresolved for the overall change (chain strategy still `pending`); Phase 3's batch executed Work Unit 2 ("PowerShell renderer + goldens + real-`pwsh` test (Phase 3)") as an autonomous, revertible slice, following Unit 1 (Phases 1–2, already landed in commits `6cd3fbb`/`8100b0a` on this branch) and regardless of which chain strategy the orchestrator ultimately picks for the remaining units.
- Unit 2 boundary: starts from Phases 1–2 complete (Unit 1); ends with Phase 3 fully green, isolated from Phases 4–9 (no other package imports `powershellRenderer` or `quotePowerShell` yet), and with `posix.go`/`header.go`/every pre-existing golden file byte-identical to before that batch.
- Unit 2 estimated review budget impact: moderate on its own (2 new source files, 1 new test file, 3 new goldens, ~60 changed lines across 3 pre-existing test/registry files) — comfortably under the 400-line guard for that unit alone, consistent with the tasks artifact's per-unit forecast.

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

### Phase 4 (this batch)

```
$ go test ./internal/apply/... -v
--- PASS: TestWriteFileAtomicSuccess (0.02s)
--- PASS: TestWriteFileAtomicRefusesSymlinkDestination (0.00s)
--- PASS: TestWriteFileAtomicRefusesDirectoryDestination (0.00s)
--- PASS: TestWriteFileAtomicCleansUpTempFileOnRenameFailure (0.01s)
--- PASS: TestBootstrapLine (0.00s)
    --- PASS: TestBootstrapLine/zsh:_path_under_home_becomes_$HOME-relative (0.00s)
    --- PASS: TestBootstrapLine/zsh:_path_outside_home_is_used_verbatim (0.00s)
    --- PASS: TestBootstrapLine/zsh:_prefix_collision_is_not_mistaken_for_a_home-relative_path (0.00s)
    --- PASS: TestBootstrapLine/bash:_identical_POSIX_form_to_zsh (0.00s)
    --- PASS: TestBootstrapLine/zsh:_home_empty_uses_the_path_verbatim (0.00s)
    --- PASS: TestBootstrapLine/powershell:_Windows-shaped_path_under_$HOME_becomes_$HOME-relative_with_forward_slashes (0.00s)
    --- PASS: TestBootstrapLine/powershell:_Windows-shaped_path_outside_$HOME_is_used_verbatim (0.00s)
    --- PASS: TestBootstrapLine/powershell:_home_empty_uses_the_path_verbatim (0.00s)
    --- PASS: TestBootstrapLine/powershell:_backtick,_double-quote_and_dollar_in_the_relative_remainder_are_escaped (0.00s)
--- PASS: TestAddBootstrapFixtures (0.03s)
--- PASS: TestAddBootstrapIsIdempotent (0.01s)
--- PASS: TestAddBootstrapNoOpsOnManuallyCraftedPreExistingBlock (0.00s)
--- PASS: TestRemoveBootstrapExactByteRestore (0.01s)
--- PASS: TestRemoveBootstrapFallsBackWhenUserEditedInsideBlock (0.01s)
--- PASS: TestBootstrapSymlinkedRCStaysSymlink (0.01s)
--- PASS: TestRemoveBootstrapMarkerLikeTextInHostileRCNotCorrupted (0.00s)
--- PASS: TestDetectEOL (0.00s)
--- PASS: TestBootstrapCRLFAddRemoveRoundTrip (0.01s)
--- PASS: TestRemoveBootstrapMarkerScanFallbackOnCRLFProfile (0.01s)
--- PASS: TestNativeBackendOutputPath (0.00s)
    --- PASS: TestNativeBackendOutputPath/zsh (0.00s)
    --- PASS: TestNativeBackendOutputPath/bash (0.00s)
    --- PASS: TestNativeBackendOutputPath/powershell (0.00s)
--- PASS: TestNativeBackendOutputPathUnsupportedShell (0.00s)
--- PASS: TestNativeBackendApplyHappyPath (0.01s)
--- PASS: TestNativeBackendApplyNoPartialWriteOnInterruption (0.01s)
--- PASS: TestChezmoiBackendFailsExplicitly (0.00s)
--- PASS: TestBackendsSatisfySyncBackendInterface (0.00s)
--- PASS: TestBootstrapRoundTripOnRealisticRCFiles (0.10s)
    --- PASS: TestBootstrapRoundTripOnRealisticRCFiles/typical_zshrc (0.01s)
    --- PASS: TestBootstrapRoundTripOnRealisticRCFiles/no_trailing_newline (0.01s)
    --- PASS: TestBootstrapRoundTripOnRealisticRCFiles/empty_file (0.01s)
    --- PASS: TestBootstrapRoundTripOnRealisticRCFiles/only_newlines (0.01s)
    --- PASS: TestBootstrapRoundTripOnRealisticRCFiles/windows_line_endings (0.01s)
    --- PASS: TestBootstrapRoundTripOnRealisticRCFiles/text_resembling_our_marker (0.01s)
    --- PASS: TestBootstrapRoundTripOnRealisticRCFiles/trailing_whitespace_preserved (0.01s)
    --- PASS: TestBootstrapRoundTripOnRealisticRCFiles/unicode_content (0.01s)
--- PASS: TestBootstrapNeverTouchesUnrelatedFiles (0.01s)
PASS
ok  	github.com/angeltonio/aliasdeck/internal/apply	0.700s

$ go test ./... 
ok  	github.com/angeltonio/aliasdeck/cmd/aliasdeck	0.812s
ok  	github.com/angeltonio/aliasdeck/internal/app	2.700s
ok  	github.com/angeltonio/aliasdeck/internal/apply	0.388s
ok  	github.com/angeltonio/aliasdeck/internal/config	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/domain	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/renderers	(cached)
?   	github.com/angeltonio/aliasdeck/internal/shelltest	[no test files]
ok  	github.com/angeltonio/aliasdeck/internal/source	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/state	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/validate	(cached)

$ make ci
go vet ./...
go test -race ./...
ok  	github.com/angeltonio/aliasdeck/cmd/aliasdeck	1.428s
ok  	github.com/angeltonio/aliasdeck/internal/app	3.562s
ok  	github.com/angeltonio/aliasdeck/internal/apply	1.803s
ok  	github.com/angeltonio/aliasdeck/internal/config	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/domain	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/renderers	(cached)
?   	github.com/angeltonio/aliasdeck/internal/shelltest	[no test files]
ok  	github.com/angeltonio/aliasdeck/internal/source	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/state	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/validate	(cached)
CI checks passed

$ go test ./internal/apply/... -cover
ok  	github.com/angeltonio/aliasdeck/internal/apply	0.643s	coverage: 84.9% of statements

$ gofmt -l .
(no output — everything formatted)

$ git diff --stat internal/apply/ internal/app/init.go
 internal/app/init.go             |   6 +-
 internal/apply/bootstrap.go      | 180 ++++++++++++++++++++++++++-----
 internal/apply/bootstrap_test.go | 221 ++++++++++++++++++++++++++++++++++++---
 internal/apply/native.go         |   2 +
 internal/apply/native_test.go    |   8 +-
 internal/apply/roundtrip_test.go |  22 +++-
 6 files changed, 393 insertions(+), 46 deletions(-)
```

**Adversarial regression proof** (see "Adversarial validation" note above for the full narrative): with `indexOfLine`'s CRLF fix surgically reverted, `go test ./internal/apply/... -run TestRemoveBootstrapMarkerScanFallbackOnCRLFProfile -v` failed with:

```
--- FAIL: TestRemoveBootstrapMarkerScanFallbackOnCRLFProfile (0.01s)
    bootstrap_test.go:524: RemoveBootstrap() must report a non-exact restore when it falls back to marker scanning
FAIL
```

confirming the test genuinely catches the class of regression it exists to catch, before the fix was restored (`diff` against the pre-revert file was byte-identical) and the suite rerun green.

No git commit was created. Changes are left in the working tree per the orchestrator's delivery-strategy instructions.

### Phase 5 (this batch)

```
$ go test ./internal/app/... -v
... (full output; every pre-existing case plus the following new ones, all PASS)
--- PASS: TestResolveRCPath (0.00s)
    --- PASS: TestResolveRCPath/override_wins (0.00s)
    --- PASS: TestResolveRCPath/zsh_honors_ZDOTDIR (0.00s)
    --- PASS: TestResolveRCPath/zsh_falls_back_to_home (0.00s)
    --- PASS: TestResolveRCPath/bash_macOS_prefers_an_existing_.bash_profile (0.00s)
    --- PASS: TestResolveRCPath/bash_Linux_prefers_an_existing_.bashrc (0.00s)
    --- PASS: TestResolveRCPath/bash_with_neither_candidate_returns_the_platform_default_to_create (0.00s)
    --- PASS: TestResolveRCPath/powershell_on_macOS_resolves_the_Core_profile,_not_an_error (0.00s)
    --- PASS: TestResolveRCPath/powershell_on_Windows_resolves_under_Documents (0.00s)
    --- PASS: TestResolveRCPath/--rc-file_overrides_PowerShell_detection_too (0.00s)
    --- PASS: TestResolveRCPath/unsupported_shell_is_an_error (0.00s)
--- PASS: TestResolvePowerShellProfile (0.01s)
    --- PASS: TestResolvePowerShellProfile/both_editions_present_on_PATH_prefers_Core,_never_both (0.00s)
    --- PASS: TestResolvePowerShellProfile/only_Desktop_on_PATH (0.00s)
    --- PASS: TestResolvePowerShellProfile/only_Core_on_PATH (0.00s)
    --- PASS: TestResolvePowerShellProfile/neither_on_PATH_defaults_to_Core (0.00s)
    --- PASS: TestResolvePowerShellProfile/ALIASDECK_PWSH_PROFILE_overrides_detection_entirely (0.00s)
    --- PASS: TestResolvePowerShellProfile/OneDrive_redirection_present:_$HOME\Documents_absent,_$OneDrive_names_an_existing_Documents (0.00s)
    --- PASS: TestResolvePowerShellProfile/OneDrive_redirection_absent:_$OneDrive_set_but_names_no_existing_Documents,_falls_back_to_$HOME\Documents (0.00s)
    --- PASS: TestResolvePowerShellProfile/non-Windows_uses_the_Core_XDG-style_config_path,_never_Documents (0.00s)
--- PASS: TestWindowsDocumentsDir (0.00s)
    --- PASS: TestWindowsDocumentsDir/default_$HOME\Documents_exists (0.00s)
    --- PASS: TestWindowsDocumentsDir/absent,_no_OneDrive_vars_set,_falls_back_to_the_default_location_to_create (0.00s)
    --- PASS: TestWindowsDocumentsDir/absent,_$OneDriveCommercial_names_an_existing_Documents (0.00s)
PASS
ok  	github.com/angeltonio/aliasdeck/internal/app	2.073s

$ go test ./...
ok  	github.com/angeltonio/aliasdeck/cmd/aliasdeck	0.396s
ok  	github.com/angeltonio/aliasdeck/internal/app	2.174s
ok  	github.com/angeltonio/aliasdeck/internal/apply	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/config	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/domain	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/renderers	(cached)
?   	github.com/angeltonio/aliasdeck/internal/shelltest	[no test files]
ok  	github.com/angeltonio/aliasdeck/internal/source	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/state	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/validate	(cached)

$ make ci
go vet ./...
go test -race ./...
ok  	github.com/angeltonio/aliasdeck/cmd/aliasdeck	1.408s
ok  	github.com/angeltonio/aliasdeck/internal/app	3.399s
ok  	github.com/angeltonio/aliasdeck/internal/apply	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/config	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/domain	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/renderers	(cached)
?   	github.com/angeltonio/aliasdeck/internal/shelltest	[no test files]
ok  	github.com/angeltonio/aliasdeck/internal/source	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/state	(cached)
ok  	github.com/angeltonio/aliasdeck/internal/validate	(cached)
CI checks passed

$ go test ./internal/app/... -cover
ok  	github.com/angeltonio/aliasdeck/internal/app	2.082s	coverage: 81.8% of statements

$ gofmt -l .
(no output — everything formatted)

$ git status --porcelain internal/app/
 M internal/app/misc_test.go
 M internal/app/rcpath.go
?? internal/app/pwshprofile.go
?? internal/app/pwshprofile_test.go
```

**Proof the POSIX rc-path behaviour is unchanged**: every pre-existing `TestResolveRCPath` sub-test (`override_wins`, `zsh_honors_ZDOTDIR`, `zsh_falls_back_to_home`, `bash_macOS_prefers_an_existing_.bash_profile`, `bash_Linux_prefers_an_existing_.bashrc`, `bash_with_neither_candidate_returns_the_platform_default_to_create`) passed **without being edited** — their source lines were not touched by this batch (`git diff internal/app/misc_test.go` shows only additions after line 173, none inside those sub-tests). The one assertion that *was* touched is exactly the one the task explicitly ordered inverted: the old `"unsupported shell is an error"` sub-test, which used `domain.ShellPowerShell` as its example of an unsupported shell — a shell that is no longer unsupported, on any platform, as of this batch. It now uses `domain.Shell("fish")`, a shell AliasDeck does not model at all, to keep exercising the same error path with an input that is still genuinely wrong.

## Engram

`mem_save`/`mem_search`/`mem_update` tools were not bound in this session's tool set (consistent with prior batches — Phases 1–2, 3, and 4 all reported the same, and Phase 5 confirms it again: the tool list available to this executor contained only `Read`, `Edit`, `Write`, `Bash`, and `mcp__codegraph__codegraph_explore`, no `mem_*` tools). Progress was persisted only to this file (`openspec/changes/powershell-windows/apply-progress.md`) and to `tasks.md`. No Engram tool call was attempted or fabricated.
