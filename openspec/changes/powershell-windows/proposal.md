# Proposal: PowerShell, Windows and Git-hosted config — Milestone 3 (v0.2)

## Intent

v0.1 ships zsh and bash on macOS and Linux. The README already advertises the PowerShell output form, and `renderers.For` already refuses `powershell` with `ErrUnsupportedShell` — so a Windows user today gets a binary that installs and then declines to work. Milestone 3 closes the gap PROJECT.md §16 names: all three operating systems, plus an `aliases.yaml` that can live in an existing dotfiles repository (§7). No settled decision in §17 is contradicted.

## Scope

### In Scope

- `internal/renderers` — PowerShell renderer using the verified `scriptblock::Create` form (§6.3), golden files, and a real-`pwsh` injection test matching the bash/zsh one
- Windows end to end — `windows` platform detection, path resolution, `$PROFILE` rc detection across both editions (§6.4), `.ps1` output, PowerShell bootstrap line, CRLF preservation
- `internal/source` — `GitSource` implementing `ConfigSource`; `source.type: git` becomes usable
- Release — `windows/amd64` and `windows/arm64` artifacts, Scoop bucket
- CI — a `windows-latest` job running the suite plus a binary smoke test in real PowerShell

### Out of Scope

- Server, API, database, web UI (M4+)
- `ChezmoiBackend` implementation — interface-only stub stands (§11)
- Fish, Nushell (§13)
- Background sync daemon, opportunistic auto-sync (§4.3, §18)
- Windows PowerShell 5.1 as a *test* target — 5.1 is supported at runtime via edition-aware `$PROFILE` detection, but CI verifies injection containment on `pwsh` 7

## Capabilities

> Base specs are not yet archived into `openspec/specs/`; they currently live in `openspec/changes/standalone-cli/specs/`. Deltas below target those capability names.

### New Capabilities

- `powershell-render`: the generated function form, single-quote escaping with `''` doubling, the two mandatory `@args`, determinism, and the real-`pwsh` containment guarantee
- `windows-platform`: platform detection, base-directory and path expansion on Windows, `$PROFILE` resolution per edition, CRLF handling, PowerShell bootstrap line
- `git-source`: repository-hosted `aliases.yaml` — checkout, ref selection, offline behaviour, and the "every source is hostile" contract

### Modified Capabilities

- `standalone-config`: `DetectPlatform` accepts `runtime.GOOS == "windows"`; `source.type: git` gains required fields
- `config-source`: `git` joins `file` as an implemented source type
- `native-apply`: `.ps1` extension for PowerShell; rc-file edits preserve the file's existing line endings
- `cli-commands`: `status`/`doctor` report the detected PowerShell edition and rc file
- `release-distribution`: Windows artifacts and Scoop manifest alongside the Homebrew cask

## Approach

The pipeline is unchanged — only its edges gain a third platform. The renderer is a new `Renderer` in the existing registry, so `renderers.Render` needs no branching. Windows support is concentrated where the code already asks the OS a question: `DetectPlatform`, `resolveRCPath`, `shellFileExt`, `BootstrapLine`, `config.Base`/`ExpandPath`. `GitSource` is a second `ConfigSource` whose output feeds the identical validate → render → apply → state path.

Escaping correctness is proven the same way it was for POSIX: golden files for the bytes, and a real interpreter for the assumption behind them. The `pwsh` test feeds the same class of injection payloads and asserts nothing executes at source time.

## Resolved Decisions

Settled before spec and design. Treat as inputs.

| Question | Decision | Rationale |
|----------|----------|-----------|
| Config base directory on Windows | **Keep `%USERPROFILE%\.config\aliasdeck`**, with `$ALIASDECK_HOME` as the escape hatch. Not `%APPDATA%`. | PROJECT.md §3.4 promises this path and `openspec/config.yaml` forbids `os.UserConfigDir()`. One path shape across all three OSes is also what makes a shared dotfiles repo work. Unidiomatic on Windows, and deliberately so. |
| Which `$PROFILE` does `init` bootstrap? | **The edition of the `pwsh`/`powershell` that AliasDeck detects**, reported by `status`; `--rc-file` overrides. Never both. | Writing two profiles doubles the surface that `uninstall` must restore. Bootstrapping the wrong one is the failure §6.4 warns about, so detection must be visible rather than silent. |
| Line endings in the generated `.ps1` | **LF, unconditionally.** | PowerShell reads LF fine on every supported edition, and byte-determinism (§17, "output has no timestamp") must not depend on the machine that rendered it. |
| Line endings in the user's `$PROFILE` | **Match whatever the file already uses.** | The non-destructive promise is byte-identical restoration (§3.4). A CRLF profile that comes back LF fails that, even though it still works. |
| Renderer branching on edition | **None.** | Both editions were verified to contain the corrected form. Only rc-file detection differs (§6.4). |
| `GitSource` transport | **Shell out to the user's `git` binary**, checkout cached under the base directory. | Inherits the credentials, SSH agent and proxy config the user already has. A vendored Git implementation would be a large dependency that then has to re-solve authentication. |
| `GitSource` when the network is down | **Use the last successful checkout and report staleness** via `status`/`doctor`; never fail `sync` silently. | Aliases are read at shell startup; an unreachable remote must not leave a machine without its commands. |

## Affected Areas

| Area | Impact |
|------|--------|
| `internal/renderers/powershell.go`, `testdata/` | New — renderer, goldens, `pwsh` injection test |
| `internal/source/git.go` | New — `GitSource` |
| `internal/config` (`paths.go`, `detect.go`, `device.go`) | Modified — Windows paths, `windows` GOOS, git source fields |
| `internal/apply` (`native.go`, `bootstrap.go`) | Modified — `.ps1`, PowerShell bootstrap line, line-ending preservation |
| `internal/app` (`rcpath.go`, `status.go`, `doctor.go`) | Modified — `$PROFILE` detection, edition reporting |
| `internal/domain`, `internal/validate` | Unchanged — `ShellPowerShell`, `PlatformWindows`, `DefaultShellFor(windows)`, reserved words and case-insensitive duplicates already exist |
| `.goreleaser.yaml`, `.github/workflows/ci.yml`, Scoop bucket | Modified/New — Windows builds, matrix entry, manifest |
| `README.md`, `docs/PROJECT.md` §16 | Modified — status table, roadmap |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| A future edit reverts the PowerShell renderer to the inlined form and reintroduces source-time execution | Medium | Golden file plus a real-`pwsh` injection test that must fail loudly; `ALIASDECK_REQUIRE_SHELLS=1` turns a missing `pwsh` in CI into a failure, never a skip |
| A dropped `@args` silently discards every argument — output looks correct and is not | Medium | An argument-forwarding assertion in the `pwsh` integration test, not only a golden diff |
| `uninstall` leaves a CRLF `$PROFILE` byte-different | Medium | Detect the file's dominant ending and match it; extend the existing round-trip test (`apply/roundtrip_test.go` already covers CRLF input) |
| `BootstrapLine` compares against `'/'` and would mis-handle a Windows path | High | Path handling reviewed with `filepath` semantics; covered by a Windows-path unit test |
| `$PROFILE` detection picks the edition the user does not launch | Medium | `status` names the file it bootstrapped; `--rc-file` overrides; `doctor` warns when the other edition's profile also exists |
| `GitSource` credential prompt blocks a non-interactive `sync` | Medium | Run `git` with prompting disabled and fail with an actionable message |
| Scoop bucket repository does not exist at release time | Medium | Same late-failure trap the Homebrew tap documented in `.goreleaser.yaml`; create and verify the bucket before tagging |

## Rollback Plan

Additive on every axis: a new renderer registry entry, a new `ConfigSource`, new switch branches for a platform that previously errored. Reverting the merge restores v0.1 behaviour exactly — Windows returns to failing at detection, `powershell` returns to `ErrUnsupportedShell`, and `go test ./...` returns to the v0.1 baseline. On a machine, `aliasdeck uninstall` removes the generated `.ps1` and the `$PROFILE` block. A bad v0.2 is withdrawn by deleting the tag and reverting the Scoop manifest bump; v0.1 users are unaffected because nothing in their config schema changed.

## Dependencies

- `pwsh` on the CI runners (`windows-latest` ships both editions; macOS/Linux runners need it installed or the test skips — which must be an explicit, visible skip)
- A `scoop-bucket` repository plus a token that can push to it, mirroring `GORELEASER_TAP_TOKEN`
- A real `git` binary on the device for `GitSource`

## Success Criteria

- [ ] A `}`-bearing command renders to a file that executes nothing when dot-sourced in real `pwsh`
- [ ] `aliasdeck sync` on Windows produces a `.ps1` whose aliases forward arguments (`git checkout` receives the branch name)
- [ ] Rendered bytes are identical on Windows, macOS and Linux for the same input
- [ ] `uninstall` leaves a CRLF `$PROFILE` byte-identical to its pre-install state
- [ ] `status` names the detected PowerShell edition and the exact `$PROFILE` bootstrapped
- [ ] `source.type: git` resolves, and `status` reports the ref and staleness
- [ ] CI is green on `windows-latest` with `ALIASDECK_REQUIRE_SHELLS=1`
- [ ] `make check` green; new and modified packages ≥70% coverage
- [ ] Tagged v0.2 installs from Scoop and from the Homebrew cask
