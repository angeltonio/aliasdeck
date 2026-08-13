# Tasks: PowerShell, Windows and Git-hosted config — Milestone 3 (v0.2)

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~2600–3400 (authored additions+deletions; golden fixtures excluded from this count) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | 6 work units, see below — or `size:exception` mirroring the Milestone 2 precedent |
| Delivery strategy | ask-on-risk |
| Chain strategy | pending |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

Note: Milestone 2 was ~2800–3200 lines and shipped as one branch, work-unit commits, one PR. This change is comparable in size but crosses more independent seams (renderer, two OS-facing bug fixes, a new `ConfigSource`, CI, release). The orchestrator must ask whether to repeat the Milestone 2 precedent (`size:exception`, single PR) or split per the work units below.

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Schema + Windows platform detection foundations (Phases 1–2) | PR 1 | `go test ./internal/config/...` | N/A — no shell/process boundary yet | Revert `internal/config/*.go` additions; no downstream code depends on it yet |
| 2 | PowerShell renderer + goldens + real-`pwsh` test (Phase 3) | PR 2 | `go test ./internal/renderers/...` | `pwsh` dot-source injection test (`powershell_integration_test.go`) | Remove `powershell.go`, its goldens, registry entry; POSIX renderers untouched |
| 3 | Bootstrap defects A+B, EOL preservation, `.ps1` output (Phase 4) | PR 3 | `go test ./internal/apply/...` | CRLF round-trip integration case in `roundtrip_test.go` | Revert `bootstrap.go`/`native.go`; `AddBootstrap` callers revert together (same commit) |
| 4 | `$PROFILE` resolution, both editions + macOS/Linux Core (Phase 5) | PR 4 | `go test ./internal/app/...` | N/A — fake `Env.LookPath`, no real process spawned by design | Remove `pwshprofile.go`; `resolveRCPath` reverts to pre-change PowerShell error |
| 5 | `GitSource` + config schema + state staleness (Phases 1 git parts + 6) | PR 5 | `go test ./internal/source/... ./internal/state/...` | N/A — injected `Run` records argv, no network | Remove `git.go`, revert `resolveSource` git case; `file`/`server` sources unaffected |
| 6 | CLI reporting, CI matrix, release (Phases 7–9) | PR 6 | `go test ./internal/app/...` | Windows CI smoke test (`init`→`sync`→`uninstall` in real `pwsh`) | Revert workflow/goreleaser YAML; no Go behavior depends on it |

Base ordering if Feature Branch Chain is chosen: PR1 → PR2/PR3/PR4/PR5 (parallel, all base off PR1) → PR6 (bases off whichever lands last). If Stacked-to-main is chosen, land PR1 first, then 2–5 in any order, then PR6.

## Confirmed No-Change (verified against current source)

- `internal/domain`: `ShellPowerShell`, `PlatformWindows`, `DefaultShellFor(windows)` already exist (`internal/domain/shell.go`). No edit planned; flag loudly if any task below is found to require one.
- `internal/validate`: case-insensitive PowerShell reserved-word and duplicate-name checks already exist (`internal/validate/name.go:83-91`, `validate.go:180-186`, covered by `validate_test.go:41-43,138`). No edit planned.

## Phase 1: Open Decision — `source.git.path` — and Config Schema

- [x] 1.1 Decide `source.git.path`: OPTIONAL; omitted ⇒ `aliases.yaml` at the checkout root (mirrors `FileSource`'s existing path-omitted default). Record as design decision 16 in `design.md` and add scenarios to `specs/config-source/spec.md` (path omitted / path present, must resolve inside the cache).
- [x] 1.2 RED: `internal/config/device_test.go` — parse `source: {type: git, url, ref?, path?}`; unknown field under `source.git` rejected (standalone-config).
- [x] 1.3 GREEN: `internal/config/device.go` — add `Git struct{ URL, Ref, Path string }` to `Source`/`sourceDTO` (nested `git:` YAML key), strict `KnownFields` still rejects unknowns.

## Phase 2: Windows Platform Detection & Paths

- [x] 2.1 RED: `internal/config/detect_test.go` — `DetectPlatform` accepts `GOOS=="windows"` and `$ALIASDECK_PLATFORM=windows` (standalone-config).
- [x] 2.2 GREEN: `internal/config/detect.go` — add `case "windows": PlatformWindows`.
- [x] 2.3 RED: `internal/config/paths_test.go` — `ExpandPath` handles `~\dotfiles\aliases.yaml` (backslash after `~`).
- [x] 2.4 GREEN: `internal/config/paths.go` — `ExpandPath` recognizes `~` + `os.PathSeparator`, not only `~/`.

## Phase 3: PowerShell Renderer (golden files + real-`pwsh` test)

- [x] 3.1 RED: new `internal/renderers/powershell_test.go` — `quotePowerShell` doubling table test; byte assertion both `@args` are present (powershell-render, "Single-Quote Escaping" + "Arguments Forwarded via @args Twice").
- [x] 3.2 GREEN: new `internal/renderers/powershell.go` — `powershellRenderer{}`, `quotePowerShell`, `Render` (scriptblock::Create form, §6.3); register `domain.ShellPowerShell` in `registry`.
- [x] 3.3 Golden: add `testdata/powershell_basic.golden`, `powershell_empty.golden`, `powershell_awkward_commands.golden` (`}`,`'`,`$`,backtick) via `make golden`; inspect diff; rerun without `-update`.
- [x] 3.4 **Inversion, not weakening**: `renderers/posix_test.go:143` (`TestForUnsupportedShell`) — `For(ShellPowerShell)` must now succeed; `fish` stays an error.
- [x] 3.5 RED: new `internal/renderers/powershell_integration_test.go` (no build tag) — real `pwsh` via `shelltest.LookPath(t, "pwsh")`; `}`-bearing payloads execute nothing when dot-sourced; `git checkout <branch>` alias forwards its argument intact (powershell-render, both scenarios).
- [x] 3.6 GREEN: confirm 3.5 passes; add `Supported()` order coverage (zsh, bash, powershell per `domain.AllShells`).

## Phase 4: Windows Apply — Defects A+B, EOL, `.ps1` Output

- [x] 4.1 RED: `internal/apply/native_test.go` — `shellFileExt(ShellPowerShell)=="ps1"`; **inversion** of the current unsupported-shell case at line 41 (native-apply, "PowerShell Output File").
- [x] 4.2 GREEN: `internal/apply/native.go` — add `ShellPowerShell → "ps1"` to `shellFileExt`.
- [x] 4.3 RED (**Defect A**, `bootstrap.go:30`): `bootstrap_test.go` — Windows-shaped paths for `BootstrapLine`: under `$HOME`, outside `$HOME`, `home==""`.
- [x] 4.4 GREEN (**Defect A fix**, design decisions 4+5): `BootstrapLine(sh domain.Shell, generatedPath, home string)` — replace `CutPrefix`+`rel[0]=='/'` with `filepath.Rel(home, generatedPath)` (reject a `..`-bearing result), emit `"$HOME/"+filepath.ToSlash(rel)`; add PowerShell double-quoted path escaper (`` ` ``→```` `` ````, `"`→`""`, `$`→`` `$ ``) for the emitted line.
- [x] 4.5 GREEN: `AddBootstrap(rcPath string, sh domain.Shell, generatedPath, home string)` — POSIX branch byte-identical to today; PowerShell branch emits `if (Test-Path -LiteralPath "...") { . "..." }`. Update every call site (`internal/app/init.go:132,158,162`) to pass `dc.Device.Shell`; update all `bootstrap_test.go`/`roundtrip_test.go` call sites for the new signature.
- [x] 4.6 RED (**Defect B + CRLF, decisions 6+7 — one task, never split**): extend `roundtrip_test.go`'s existing CRLF case (line 32) plus new `bootstrap_test.go` cases for `detectEOL`, CRLF add/remove round-trip, and a marker-scan-fallback case (edit inside the block so exact-bytes removal misses) on a CRLF `$PROFILE`.
- [x] 4.7 GREEN (**same task as 4.6**): add `detectEOL(existing []byte) string` (`\r\n` iff already present, else `\n`); thread `eol` through `buildBlock`; fix `indexOfLine` (`bootstrap.go:237`) to accept `\r\n` as a line terminator at both start and end; fix `removeMarkerScan` to consume `\r\n` for the trailing newline and blank separator.
- [x] 4.8 Verify: `go test ./internal/apply/...` — the previously-latent CRLF marker-scan case now passes; POSIX byte-identical cases still pass unchanged.

## Phase 5: PowerShell `$PROFILE` Resolution

- [x] 5.1 RED: new `internal/app/pwshprofile_test.go` — precedence table: `--rc-file` → `$ALIASDECK_PWSH_PROFILE` → `LookPath("pwsh")`⇒Core → `LookPath("powershell")`⇒Desktop → Core default; fake `Env.LookPath` (standalone-config, "PowerShell Edition and $PROFILE Selection").
- [x] 5.2 RED: OneDrive redirection case — `$HOME\Documents` absent, `$OneDrive`/`$OneDriveCommercial` names an existing `Documents` (design decision 9).
- [x] 5.3 RED: macOS/Linux Core profile case, `~/.config/powershell/Microsoft.PowerShell_profile.ps1` — **inversion** of `app/misc_test.go:177`.
- [x] 5.4 GREEN: new `internal/app/pwshprofile.go` — `resolvePowerShellProfile(env) (pwshProfile, error)` returning `{Path, Edition, Provenance, OtherPath, OtherExists}`.
- [x] 5.5 GREEN: `internal/app/rcpath.go` — `resolveRCPath` delegates to `resolvePowerShellProfile` for `ShellPowerShell` on every platform; `--rc-file` still overrides.
- [x] 5.6 Update `app/misc_test.go:177` to expect the Core profile path instead of an error; label inversion in the test comment.

## Phase 6: GitSource (`internal/source`) + State Staleness

- [x] 6.1 RED (**threat-matrix: Git subprocess environment**): `internal/source/git_test.go` — injected `Run` recording argv; every invocation carries `GIT_TERMINAL_PROMPT=0`, `GCM_INTERACTIVE=Never`, `BatchMode=yes`; never `sh -c`.
- [x] 6.2 RED (**threat-matrix: Git repository selection**): clone vs fetch dispatch (absent `.git` ⇒ `clone --quiet -- <url> <cache>`; else `fetch --quiet --prune origin` + reset); ref default via `remote set-head origin --auto`; hostile URL (`-`-prefixed, `ext::`) rejected before any exec.
- [x] 6.3 RED: offline-with-cache (stale, no failure) and offline-without-cache (hard error, no partial state) (config-source, "GitSource Offline Behavior and Staleness").
- [x] 6.4 GREEN: new `internal/source/git.go` — `GitSource{URL, Ref, Path, CacheDir, Run}`, `Resolve`, `Descriptor`; cache at `<base>/cache/git/<sha256(url)[:12]>`.
- [x] 6.5 RED: `source.git.path` (Phase 1.1's decision) must resolve inside the cache; a `..`-bearing path must be rejected before reading `aliases.yaml`.
- [x] 6.6 GREEN: path-join + containment check ahead of the `os.ReadFile` in `git.go`.
- [x] 6.7 GREEN: optional `ResolveReporter{ LastResolve() ResolveInfo }`; `state.State` gains `SourceStale bool`, `SourceFetchedAt time.Time` (`omitempty`); `Descriptor.Ref` becomes `<url>#<ref>@<short-sha>`.
- [x] 6.8 GREEN: `internal/app/context.go` — `resolveSource` dispatches `config.SourceTypeGit` to `GitSource` instead of erroring; `syncWithContext` type-asserts `ResolveReporter` and records staleness into `state.State`.
- [x] 6.9 RED/GREEN: `edit` use case performs no git write when `source.type: git` (config-source, "GitSource Is Read-Only in v0.2").

## Phase 7: CLI Reporting — status/doctor

- [ ] 7.1 RED: `internal/app/status_test.go` — PowerShell edition + exact `$PROFILE` path bootstrapped; git ref + staleness fields (cli-commands).
- [ ] 7.2 GREEN: extend `StatusReport`/`Status()` (`internal/app/status.go`) with `PowerShellEdition`, `PowerShellProfilePath` (PowerShell devices only), `SourceRef`, `SourceStale`.
- [ ] 7.3 RED: `internal/app/doctor_test.go` — other-edition-profile-exists warning; stale-`GitSource` warning; both write nothing.
- [ ] 7.4 GREEN: extend `DoctorReport`/`Doctor()` (`internal/app/doctor.go`) with the two warnings above.

## Phase 8: CI Matrix & Release

- [ ] 8.1 `.github/workflows/ci.yml` — add `windows-latest` to the test matrix; install `pwsh` on `ubuntu-latest`/`macos-latest` (ships on `windows-latest`); keep `ALIASDECK_REQUIRE_SHELLS=1`.
- [ ] 8.2 `.github/workflows/ci.yml` — Windows-side smoke test step (`init`→config `aliases.yaml`→`sync`→`status`→`list`→`doctor`→`uninstall` in real `pwsh`, byte-identical `$PROFILE` restore), mirroring the existing zsh step; keep the zsh step unchanged.
- [ ] 8.3 **Prerequisite, same late-failure trap as the Homebrew tap**: create the `scoop-bucket` repository and its push token before touching `.goreleaser.yaml`'s Windows/Scoop config.
- [ ] 8.4 `.goreleaser.yaml` — add `windows`/`amd64`,`arm64` to `builds.goos`/`goarch` (six total artifacts, no cgo).
- [ ] 8.5 `.goreleaser.yaml` — add `scoop_buckets:` block referencing the verified bucket; document the prerequisite inline like the existing Homebrew-tap comment; fail loudly if the bucket/token is missing.
- [ ] 8.6 Confirm the existing `goreleaser-config` CI job (`goreleaser check`) validates the new Windows/Scoop block; no new job needed.

## Phase 9: Docs & Final Verification

- [ ] 9.1 `README.md` — update the PowerShell/Windows support status.
- [ ] 9.2 `docs/PROJECT.md` §16 — mark Milestone 3 items complete.
- [ ] 9.3 Run `make check` (`gofmt -l -w . && go vet ./... && go test ./...`) and `make cover`; confirm ≥70% on `renderers`, `apply`, `app`, `source`, `config`.
- [ ] 9.4 Confirm zsh/bash goldens and `shell_integration_test.go` are untouched and still green.

## Parallelization

- Phase 1 and Phase 2 have no dependency on each other; both must land before Phase 6 (schema) and before Phase 3/4/5 exercise Windows paths.
- Phase 3 (renderer) and Phase 6 (GitSource) share no code and can be implemented in parallel branches.
- Phase 4 changes `BootstrapLine`/`AddBootstrap` signatures; Phase 5 and Phase 7 call through them — Phase 4 must merge before Phase 5/7 land, even if developed in parallel.
- Phase 7 depends on Phase 5 (profile fields) and Phase 6 (git ref/staleness fields).
- Phase 8 depends on every prior phase being green; Phase 9 is last.
