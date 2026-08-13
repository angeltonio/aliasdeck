# Design: PowerShell, Windows and Git-hosted config — Milestone 3 (v0.2)

## Technical Approach

The v0.1 pipeline is unchanged. Three edges grow a third branch, and one new `ConfigSource` joins `FileSource`.

```text
config.yaml ─→ DetectPlatform(+windows) ─→ Device
                                             │
   FileSource ─┐                             ▼
   GitSource  ─┴─→ Resolve ─→ FilterValid ─→ Render(zsh|bash|powershell)
                      │                            │
                 ResolveInfo                 NativeBackend (.ps1)
                 (stale, ref)                      │
                      └────── state.json ←── AddBootstrap(shell-aware, EOL-preserving)
```

`internal/domain` and `internal/validate` are untouched: `ShellPowerShell`, `PlatformWindows`, `DefaultShellFor(windows)`, the PowerShell reserved words and case-insensitive duplicate detection already exist.

## Architecture Decisions

| # | Decision | Choice | Rejected | Rationale |
|---|---|---|---|---|
| 1 | PowerShell renderer | New `internal/renderers/powershell.go`, `powershellRenderer{}` (no shell field — one edition-agnostic renderer, §6.4); registry gains `ShellPowerShell`. Shares only the existing package helpers `writeHeader("#", …)` and `sanitizeComment`. | A `Renderer` base struct or generalising `posixRenderer` | The two renderers share a *header*, not a grammar. `Supported()` already sorts by `domain.AllShells` index, so `Render`, `For` and `doctor` need no branching at all. |
| 2 | Escaping | `quotePowerShell(s) = "'" + ReplaceAll(s, "'", "''") + "'"` | Backslash escaping | PowerShell has no backslash escape; doubling is the only single-quote mechanism (§6.3). |
| 3 | Shell-aware bootstrap | `BootstrapLine(sh domain.Shell, generatedPath, home string)`; `AddBootstrap` gains the same parameter. POSIX branch emits byte-identical output to today. Markers, `buildBlock`, `state.Bootstrap.Block` and `RemoveBootstrap` are unchanged — PowerShell comments are `#` too. | A second `AddBootstrapPS`; sniffing the shell from the file extension | The recorded-block mechanism `uninstall` depends on is marker- and byte-based, not grammar-based, so only the one line inside it varies. |
| 4 | `$HOME`-relative rewrite (**bug fix**, `bootstrap.go:30`) | Replace `CutPrefix` + `rel[0] == '/'` with `filepath.Rel(home, generatedPath)`, rejecting a result of `..` or `..`+separator, then emit `"$HOME/" + filepath.ToSlash(rel)`. | Branching on `os.PathSeparator` inside the prefix check | `filepath.Rel` is the separator-correct primitive. Forward slashes are emitted on every OS: PowerShell accepts `/`, and it keeps the line free of backslashes that would otherwise need escaping. |
| 5 | PowerShell path quoting | Own escaper for the double-quoted context: `` ` `` → ``` `` ```, `"` → `""`, `$` → `` `$ ``, applied to the path *after* the literal `$HOME` prefix is prepended. | `fmt.Sprintf("%q", …)` as the POSIX branch uses | `%q` is Go escaping: it would turn `\` into `\\`, which PowerShell reads as two literal backslashes. `$HOME` is our own literal, never user data, so it is exempt by construction. |
| 6 | Line endings | Generated `.ps1`: LF, unconditional (renderer writes `\n` explicitly, as posix does). rc file: `detectEOL(existing)` returns `\r\n` iff the file already contains one, else `\n`; `buildBlock(existing, line, eol)` uses it. | Writing `\r\n` on `GOOS == "windows"` | Determinism must not depend on the rendering machine. Removal still cuts the exact recorded `Block` bytes, so a CRLF profile round-trips byte-identically. |
| 7 | CRLF in the marker-scan fallback (**latent bug, activated by decision 4**) | `indexOfLine` must accept `\r\n` as a line terminator, and `removeMarkerScan` must consume `\r\n` for both the trailing newline and the blank separator. **This must land in the same change as CRLF preservation, not after it.** | Fixing it separately, or later | `indexOfLine` requires `content[end] == '\n'`, which a CRLF-terminated marker line fails. Measured: it finds an LF-terminated marker and does not find a CRLF-terminated one. It does not bite today only because AliasDeck writes LF markers even into a CRLF file — so preserving CRLF is precisely what would activate it, and separating the two leaves a window where a user-edited CRLF `$PROFILE` keeps the block forever. |
| 8 | `$PROFILE` resolution | New `internal/app/pwshprofile.go`: `resolvePowerShellProfile(env) (pwshProfile, error)` returning `{Path, Edition, Provenance, OtherPath, OtherExists}`. Precedence: `--rc-file` → `$ALIASDECK_PWSH_PROFILE` (test seam) → `LookPath("pwsh")` ⇒ Core → `LookPath("powershell")` ⇒ Desktop → Core default. `resolveRCPath` delegates; `status` prints edition + path + provenance; `doctor` warns when `OtherExists`. | Writing both profiles; probing by running `pwsh -c $PROFILE` | Never both (resolved decision). `LookPath` is already on `Env`, so detection is a fake in tests and never spawns a process. |
| 9 | Documents redirection | Base is `$HOME\Documents`; when it is absent and `$OneDrive`/`$OneDriveCommercial` names an existing `Documents`, use that and say so in the provenance. | Ignoring OneDrive | OneDrive Known Folder Move is the default on many managed Windows devices; bootstrapping a path PowerShell never reads is exactly the silent failure §6.4 warns about. |
| 10 | pwsh on macOS/Linux | Supported: Core profile `~/.config/powershell/Microsoft.PowerShell_profile.ps1`. | Erroring outside Windows | One extra branch, and it is what lets the real-`pwsh` `init`→`sync` integration test run on the macOS/Linux CI runners. **Scope extension beyond the proposal's literal wording — see Risks.** |
| 11 | `GitSource` placement and cache | `internal/source/git.go`. Cache at `<base>/cache/git/<sha256(url)[:12]>` — AliasDeck-owned, removed by `uninstall`, governed by `$ALIASDECK_HOME`. | Caching next to the user's own clones; a path segment derived from the URL text | A hashed segment cannot traverse, collide, or leak a credential-bearing URL onto disk as a directory name. |
| 12 | `GitSource` transport | `git -C <cache>` argv via an injected `Run func(ctx, dir string, args ...string) ([]byte, error)`, never a shell. Absent `.git` ⇒ `clone --quiet -- <url> <cache>`; otherwise `fetch --quiet --prune origin` then reset the worktree to the resolved ref. Read-only: never commit, push, or touch a user repository. | `go-git`; running in process cwd | Inherits the user's credentials, SSH agent and proxy (resolved decision). `-C` makes repository selection explicit rather than ambient; `Run` is the unit-test seam. |
| 13 | Ref resolution and offline | `source.ref` when set, else `refs/remotes/origin/HEAD` (`remote set-head origin --auto` when unset). Fetch failure with an existing checkout ⇒ resolve from the cache and mark stale; no checkout ⇒ hard error naming the source. | Failing closed always; falling back to a different source | The cached checkout is the *same* source's last successful content, not a second source, so §7.1 holds. Aliases are read at shell startup; an unreachable remote must not leave a machine without its commands. |
| 14 | Staleness reporting | Additive optional interface `ResolveReporter { LastResolve() ResolveInfo }`, type-asserted by `syncWithContext`; `state.State` gains `SourceStale bool` and `SourceFetchedAt time.Time` (`omitempty`). `Descriptor.Ref` becomes `<url>#<ref>@<short-sha>`. | Widening `ConfigSource.Resolve`'s signature | The §7 `ConfigSource` signature is verbatim in PROJECT.md and shared with `ServerSource` in M4. `FileSource` simply does not implement the optional interface. |
| 15 | Non-interactive git | `GIT_TERMINAL_PROMPT=0`, `GCM_INTERACTIVE=Never`, `GIT_SSH_COMMAND=ssh -o BatchMode=yes`; reject URLs starting with `-` or using the `ext::` transport. | Inheriting the ambient environment | A credential prompt would hang `sync` forever in CI; `ext::` is remote command execution by design. |

## Windows Path Handling

| Location | Change |
|---|---|
| `config.Base`, `state.Save`, `apply.writeFileAtomic` | None — already `filepath`-based. |
| `config.ExpandPath` | Recognise `~` + `os.PathSeparator` in addition to `~/`, so `~\dotfiles\aliases.yaml` expands. POSIX behaviour unchanged. |
| `config.DetectPlatform` | `case "windows": PlatformWindows`. |
| `apply.shellFileExt` | `ShellPowerShell → "ps1"`. |
| `apply.BootstrapLine` | Decisions 4 and 5. |
| `apply.indexOfLine` / `removeMarkerScan` | Decision 7. |
| `config.shellBasename`, `resolveRCPath` bash branch | None — `$SHELL` is normally unset on Windows, so detection falls through to `DefaultShellFor(windows)`. |

## Interfaces

```go
// internal/renderers/powershell.go — emitted per alias, verified form (§6.3)
// function <name> {
//     $__aliasdeck_cmd = '<quotePowerShell(command)>'
//     & ([scriptblock]::Create($__aliasdeck_cmd + ' @args')) @args
// }
// Both @args are load-bearing: the first makes the compiled command receive
// arguments, the second passes the caller's. Dropping either silently discards
// every argument.

// internal/apply
func BootstrapLine(sh domain.Shell, generatedPath, home string) string
func AddBootstrap(rcPath string, sh domain.Shell, generatedPath, home string) (block string, err error)

// internal/source
type ResolveInfo struct {
	Ref, Commit string
	FetchedAt   time.Time
	Stale       bool
}
type ResolveReporter interface{ LastResolve() ResolveInfo } // optional, additive

type GitSource struct {
	URL, Ref, Path, CacheDir string
	Run func(ctx context.Context, dir string, args ...string) ([]byte, error)
}
```

PowerShell bootstrap line:

```powershell
if (Test-Path -LiteralPath "$HOME/.config/aliasdeck/aliases.ps1") { . "$HOME/.config/aliasdeck/aliases.ps1" }
```

## Testing Strategy

| Layer | What | Approach |
|---|---|---|
| Golden | `powershell_basic`, `powershell_empty`, `powershell_awkward_commands` (`}`, `'`, `$`, backtick) | New `testdata/*.golden` via `make golden`; existing zsh/bash goldens untouched |
| Unit | `quotePowerShell` doubling; both `@args` present | Table-driven, plus a byte assertion on the emitted `@args` pair |
| Unit | `BootstrapLine` on Windows paths, `$HOME`-relative rewrite, path outside `$HOME`, `home == ""` | Table-driven with synthetic Windows-shaped paths |
| Unit | `detectEOL`; CRLF add/remove round-trip; CRLF marker-scan fallback | Extend `apply/roundtrip_test.go` (its CRLF case exists) |
| Unit | `$PROFILE` precedence, both editions, neither, OneDrive redirection, `doctor` other-edition warning | Fake `Env.LookPath` + `t.TempDir()` |
| Unit | `GitSource` clone vs fetch, ref default, offline-with-cache, offline-without-cache, hostile URL rejection | Injected `Run` recording argv; no network |
| Integration | Real `pwsh`: dot-source a rendered file with `}`-bearing payloads ⇒ no canary; and `git checkout` alias forwards its branch argument | New `powershell_integration_test.go`, no build tag, `shelltest.LookPath(t, "pwsh")` so `ALIASDECK_REQUIRE_SHELLS=1` turns a missing `pwsh` into a failure |
| Integration | `init` → `sync` → `uninstall` on a CRLF `$PROFILE` ⇒ byte-identical | `t.TempDir()` HOME |
| Unchanged | zsh/bash goldens and `shell_integration_test.go` | Never modified or weakened |

**Planned assertion inversions** (strengthening, not weakening — each replaces "unsupported" with a positive guarantee, and every unknown-shell case is retained):

- `renderers/posix_test.go:143` — `For(ShellPowerShell)` must now return a `powershellRenderer`; the `fish` case stays an error.
- `apply/native_test.go:41` — `OutputPath(ShellPowerShell)` must now return `aliases.ps1`.
- `app/misc_test.go:177` — `resolveRCPath(…, ShellPowerShell, PlatformMacOS, "")` must now return the Core profile path (decision 10).

## Threat Matrix

| Boundary | Applicability | Design response | Planned RED test |
|---|---|---|---|
| Documentation-like paths | N/A — no file-class execution decision; the generated `.ps1` is written by AliasDeck and executed only by the user's shell (§12.2) | — | — |
| Git repository selection | **Applicable** — `GitSource` runs `git` | Every invocation is `git -C <cacheDir>` with an argv slice; `--` before the URL; cwd is never authoritative; cache path is a hash, not user text | Relative/absolute/`..`-bearing `source.path` must resolve inside the cache; a URL starting with `-` or `ext::` must be refused before any exec |
| Commit state | N/A — v0.2 `GitSource` is read-only; it never stages or commits | — | — |
| Push state | N/A — no remote writes | — | — |
| PR commands | N/A — no PR automation | — | — |
| **Git subprocess environment** | **Applicable** | `GIT_TERMINAL_PROMPT=0`, `GCM_INTERACTIVE=Never`, `BatchMode=yes`; never `sh -c` | A source needing credentials must fail with an actionable message, not hang |
| **PowerShell rendering** | **Applicable** — output is executed by a shell | Single-quoted command compiled at call time via `scriptblock::Create`; `''` doubling; `guard` re-validates | A `}`-bearing command must execute nothing when dot-sourced in real `pwsh` |
| **`$PROFILE` mutation** | **Applicable** | Marker block, exact recorded bytes, `EvalSymlinks`, atomic write, EOL preserved | A CRLF profile must return byte-identical after `uninstall`; a profile containing marker-like text must not be corrupted |

## Migration / Rollout

No migration. `state.json` stays `version: 1`; the two new fields are `omitempty` and `state.Load` already tolerates anything it cannot parse. A v0.1 state file read by v0.2 yields `Stale=false`, which is correct for a `file` source. Rollback is reverting the merge.

## Open Questions

None blocking.
