# Design: Standalone CLI — Milestone 2 (v0.1)

## Technical Approach

One pipeline (§4.3), one package per stage, Cobra reduced to flag parsing and output.

```text
config.yaml  ─→ config.Load ─→ Device (detect, then config override)
aliases.yaml ─→ config.ParseAliases ─→ source.FileSource.Resolve ─→ domain.Resolve
                                                  │
                       validate.FilterValid ─→ renderers.Render ─→ apply.NativeBackend
                              │                                          │
                         validate.Issues                      atomic write + bootstrap
                              ↓                                          ↓
                           doctor                              state.Save (state.json)
```

`internal/app` holds one function per command returning a report struct; `cmd/aliasdeck` prints reports and maps typed errors to exit codes. Rendering is called only from the client and only from `internal/renderers`; nothing upstream of `Render` produces shell text, so the §6.1 boundary is preserved by construction. `internal/domain`, `internal/validate`, `internal/renderers` are read-only for this milestone.

## Architecture Decisions

| # | Decision | Choice | Rejected | Rationale |
|---|---|---|---|---|
| 1 | Package boundaries | `internal/config` (paths, both schemas, detection), `internal/source`, `internal/apply`, `internal/state`, `internal/app`, `cmd/aliasdeck` | Logic inside Cobra `RunE`; a single `internal/cli` | Every stage is a pure-ish function over an injected `Env{Stdin,Stdout,Stderr,Getenv,HomeDir,Now,LookPath}`, so all seven commands are table-driven testable under `t.TempDir()`. `internal/state` and `internal/app` extend §10 additively |
| 2 | `enabled` default | Parse-layer DTO with `Enabled *bool`; `Enabled: dto.Enabled == nil \|\| *dto.Enabled` | Change `domain.Alias.Enabled` to `*bool`; default inside `AppliesTo` | Keeps `domain` and the golden files untouched. The DTO is needed anyway: YAML `profiles:` maps to `ProfileIDs`, and `ID` is derived from `Name` (§5) |
| 3 | Strict parsing | `yaml.Decoder` with `KnownFields(true)`, `version` must equal `1`, 1 MiB input cap | Lenient decode | A typo (`comand:`) must fail loudly, not silently drop an alias |
| 4 | Sync state | `<base>/state.json`, JSON, mode `0600` | YAML; state inside the generated file | Machine-owned, never hand-edited; `encoding/json` needs no dependency. A missing or corrupt file degrades to empty state, never a fatal error |
| 5 | No-op skip | Always render (deterministic, cheap), skip the write when `sha256(rendered) == sha256(on-disk)`; `state.Revision` is the reported "source unchanged" signal | Trusting `Revision` alone | Hash-of-disk also catches manual tampering and a deleted output file. Revision alone would report "in sync" for a file that no longer exists |
| 6 | Bootstrap line | One marker-delimited block appended to the rc file; exact block bytes stored in state and removed with a single `bytes.Replace` | Marker-only removal; rewriting the whole rc file | Byte-identical restoration is provable, not heuristic. Marker scan is the documented fallback when the user edited inside the block, and it warns that exactness is no longer guaranteed |
| 7 | rc file writes | `filepath.EvalSymlinks` on the rc path, then atomic write onto the resolved target | Rename over `~/.zshrc` directly | A dotfiles-managed `~/.zshrc` is usually a symlink; renaming over it would replace the link |
| 8 | Editor invocation | `exec.Command(bin, args..., path)` after splitting `$EDITOR` on whitespace | `sh -c "$EDITOR file"` | Never hand an environment variable to a shell. `code -w` still works; quoted paths inside `$EDITOR` are a documented limitation |
| 9 | `backend: chezmoi` | Interface ships (§11 signature unchanged, plus additive `Name()` / `OutputPath()`); the value is a hard error | Silent no-op | Per the proposal's resolved decision |
| 10 | YAML library | `go.yaml.in/yaml/v3 v3.0.5` | `gopkg.in/yaml.v3` (archived April 2025, frozen at v3.0.1) | Maintained drop-in from the YAML organization, still receiving security fixes |

**Dependency note**: `spf13/cobra v1.10.2` and `go.yaml.in/yaml/v3 v3.0.5` are the project's first runtime dependencies. PROJECT.md §9 and `openspec/config.yaml` (`context: "stdlib only, no external deps yet"`) become stale in this milestone and MUST be updated as part of it.

## Paths, Detection, Exit Codes

**Base directory**: `$ALIASDECK_HOME` → `$XDG_CONFIG_HOME/aliasdeck` → `~/.config/aliasdeck`. `os.UserConfigDir` is deliberately not used: on macOS it returns `~/Library/Application Support`, contradicting §3.4.

| Path | Mode | Owner |
|---|---|---|
| `<base>/config.yaml` | 0600 | user |
| `<base>/aliases.yaml` | 0600 | user (default source only) |
| `<base>/aliases.{zsh,bash}` | 0644 | AliasDeck (regenerated) |
| `<base>/state.json` | 0600 | AliasDeck |

**Platform**: `config.yaml device.platform` → `$ALIASDECK_PLATFORM` (test seam) → `runtime.GOOS` map (`darwin→macos`, `linux→linux`); anything else is an error.

**Shell**: `--shell` → `config.yaml device.shell` → `$ALIASDECK_SHELL` → `$SHELL` basename (login `-` prefix stripped) → `domain.DefaultShellFor(platform)`. The result carries a `Provenance` string that `status` and `doctor` print, so a wrong guess is visible rather than mysterious. A detected shell with no renderer fails with `renderers.Supported()` listed. Only the detected shell is written (resolved decision).

**rc file**: zsh → `$ZDOTDIR/.zshrc` else `~/.zshrc`. bash → first existing of (`~/.bash_profile`, `~/.bashrc`) on macOS, (`~/.bashrc`, `~/.bash_profile`) on Linux, else the platform default. `--rc-file` overrides.

| Exit | Meaning |
|---|---|
| 0 | Success, including a no-op skip and a `doctor` run with warnings only |
| 1 | Runtime failure: I/O, render refused, unsupported shell, `backend: chezmoi` |
| 2 | Usage error (Cobra default; `SilenceUsage`/`SilenceErrors` on) |
| 3 | Invalid configuration: parse failure, or `doctor`/`edit` found `SeverityError` |
| 4 | Not initialized (`config.yaml` absent) — message names `aliasdeck init` |

`doctor` prints `Issue.String()` per finding, sorted by alias then field, with counts; it exits 3 only when `issues.HasErrors()`. Aliases referencing an undeclared `profiles:` entry are `SeverityWarning`, produced by `config.ProfileWarnings` rather than by `validate` (which stays untouched).

## Interfaces

```go
// internal/source — §7 signature verbatim.
type ConfigSource interface {
	Resolve(ctx context.Context, dev domain.Device) (domain.ResolvedConfig, error)
}
type Descriptor struct{ Type, Ref string } // status always names the active source (§7.1)

// internal/apply — §11 Apply signature unchanged; the other methods are additive.
type SyncBackend interface {
	Name() string
	OutputPath(dev domain.Device) (string, error)
	Apply(ctx context.Context, cfg domain.ResolvedConfig, rendered string) error
}

// internal/state
type State struct {
	Version       int             `json:"version"` // 1
	Revision      string          `json:"revision"`
	OutputPath    string          `json:"outputPath"`
	OutputHash    string          `json:"outputHash"` // sha256 hex of rendered bytes
	AliasCount    int             `json:"aliasCount"`
	Platform      domain.Platform `json:"platform"`
	Shell         domain.Shell    `json:"shell"`
	SourceType    string          `json:"sourceType"`
	SourceRef     string          `json:"sourceRef"`
	LastSyncAt    time.Time       `json:"lastSyncAt"`
	ClientVersion string          `json:"clientVersion"`
	Bootstrap     *Bootstrap      `json:"bootstrap,omitempty"`
}
type Bootstrap struct {
	RCPath  string    `json:"rcPath"`
	Block   string    `json:"block"` // exact appended bytes, leading padding included
	RCHash  string    `json:"rcHash"`
	AddedAt time.Time `json:"addedAt"`
}
```

**Bootstrap block.** Markers are the whole contract:

```sh
# >>> aliasdeck >>>
[ -f "$HOME/.config/aliasdeck/aliases.zsh" ] && . "$HOME/.config/aliasdeck/aliases.zsh"
# <<< aliasdeck <<<
```

`.` is used instead of `source` (POSIX, valid in both shells, survives an `sh`-mode rc). The path is written `$HOME`-relative when the base dir is under `$HOME`. Appended bytes are `padding + separator + begin + "\n" + line + "\n" + end + "\n"`, where `padding` is `"\n"` only when the file lacked a trailing newline and `separator` is `"\n"` only when the file is non-empty; that whole string is what `state.Bootstrap.Block` holds, so removal is one exact cut. Add is idempotent: presence of `beginMarker` means no-op.

**Atomic write.** `MkdirAll(dir, 0755)` → `os.CreateTemp(dir, ".<name>.*.tmp")` in the *same* directory (same filesystem, so `Rename` is atomic) → `Chmod` → write → `Sync` → `Close` → `Rename`. A deferred `Remove(tmp)` covers every failure before the rename, so a partial write is never visible and never sourceable (§12.2). Refuse to write when the destination exists and is a symlink or a directory. Order per sync: generated file → bootstrap → state. Bootstrap failure leaves an unsourced generated file and no state, so the next `sync` retries; state failure exits 1 with a message stating the aliases are already live.

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/config/paths.go` | Create | Base dir, file paths, `~` and `$HOME` expansion |
| `internal/config/device.go` | Create | `config.yaml` schema, strict parse, `Load`, `Write` |
| `internal/config/aliases.go` | Create | `aliases.yaml` DTO, `enabled` default, `ProfileWarnings` |
| `internal/config/detect.go` | Create | Platform/shell/rc detection with provenance |
| `internal/source/source.go`, `file.go` | Create | `ConfigSource`, `Descriptor`, `FileSource` |
| `internal/apply/backend.go`, `native.go`, `atomic.go`, `bootstrap.go` | Create | `SyncBackend`, `NativeBackend`, atomic write, block add/remove |
| `internal/state/state.go` | Create | `State`, `Load` (tolerant), `Save` (atomic, 0600) |
| `internal/app/*.go` | Create | One use case per command, returning report structs |
| `cmd/aliasdeck/main.go`, `root.go`, `{init,sync,status,list,doctor,edit,uninstall}.go` | Create | Cobra wiring, `Env` injection, error→exit-code mapping |
| `.goreleaser.yaml`, `scripts/install.sh` | Create | Cross-compiled release, tap formula, install script |
| `go.mod`, `go.sum` | Modify | First runtime dependencies |
| `openspec/config.yaml`, `docs/PROJECT.md` §9, `README.md` | Modify | Retract "stdlib only"; update status table |

## Testing Strategy

| Layer | What | Approach |
|---|---|---|
| Unit | `enabled` omitted / `true` / `false`; unknown field; wrong `version`; oversize input | Table-driven, `t.Run` |
| Unit | Detection precedence incl. `config.yaml` override and unsupported shell | Table-driven over a fake `Env` |
| Unit | Atomic write: permissions, temp cleanup on failure, symlink/directory refusal | `t.TempDir()`, injected failures |
| Unit | Bootstrap add idempotence; removal restoring bytes exactly | rc fixtures with and without a trailing newline, empty file, pre-existing block, user-edited block |
| Unit | State round-trip; corrupt/missing JSON tolerated | `t.TempDir()` |
| Integration | `init` → `sync` → second `sync` (no write) → `uninstall` (byte-identical rc) | `t.TempDir()` HOME, filesystem assertions |
| Integration | Generated file sourced by real `bash` and `zsh` after a full sync | New test, skipped under `testing.Short()` |
| Unchanged | Renderer golden files and the existing real-shell injection test | Never modified or weakened |

New packages target ≥70% coverage; `make check` must stay green.

## Threat Matrix

| Boundary | Applicability | Design response | Planned RED test |
|---|---|---|---|
| Documentation-like paths | N/A — no file-class execution decision; the one generated file is written by AliasDeck and executed only by the user's shell (§12.2) | — | — |
| Git repository selection | N/A — `GitSource` is Milestone 3 | — | — |
| Commit state | N/A — the CLI runs no VCS commands | — | — |
| Push state | N/A — no remote operations | — | — |
| PR commands | N/A — no PR automation | — | — |
| **Editor subprocess** (`edit`) | Applicable | `exec.Command` with a split argv, never `sh -c`; missing editor is an error, not a fallback shell | `$EDITOR="x; rm -rf ."` must not execute `rm`; `code -w` must pass through |
| **rc file mutation** | Applicable | Marker block, stored exact bytes, symlink resolution, atomic write | Hostile rc containing the marker text must not be corrupted; symlinked rc must stay a symlink |
| **Hostile `aliases.yaml`** | Applicable | `FilterValid` before `Render`; renderer `guard` retained; every source treated as hostile (§12.1) | Injection fixture is skipped and reported, never written |
| **Output path** | Applicable | Refuse symlink or directory destinations; refuse a path outside the resolved base dir | Symlinked output path must fail closed |

## Migration / Rollout

No migration: `state.json` carries `version: 1` from the start and absence is a valid state. Rollback is reverting the merge commit; Milestone 1 packages are untouched.

## Open Questions

None blocking. Two deferred items, both out of scope by the proposal: `aliasdeck config set` (source switching, §7.1) and multi-shell output.
