# Verification Report: standalone-cli (Milestone 2, v0.1)

**Change**: `standalone-cli`
**Branch**: `feat/standalone-cli`
**Mode**: full spec-driven verification (proposal + 6 spec files + design + tasks + apply-progress all present)
**Strict TDD**: active (`openspec/config.yaml` → `strict_tdd: true`)
**Verdict**: **PASS WITH WARNINGS** — 1 CRITICAL, 5 WARNING, 4 SUGGESTION

---

## 1. Artifact Completeness

| Artifact | Present | Notes |
|---|---|---|
| `proposal.md` | Yes | 5 resolved decisions, 7 success criteria |
| `specs/standalone-config/spec.md` | Yes | 6 requirements, 10 scenarios |
| `specs/config-source/spec.md` | Yes | 4 requirements, 6 scenarios |
| `specs/native-apply/spec.md` | Yes | 6 requirements, 8 scenarios |
| `specs/sync-state/spec.md` | Yes | 3 requirements, 4 scenarios |
| `specs/cli-commands/spec.md` | Yes | 8 requirements, 13 scenarios |
| `specs/release-distribution/spec.md` | Yes | 3 requirements, 4 scenarios |
| `design.md` | Yes | 10 architecture decisions, threat matrix |
| `tasks.md` | Yes | 44/44 marked `[x]` |
| `apply-progress.md` | Yes | 4 batches, matches task state |

**Counted totals: 30 requirements, 45 Given/When/Then scenarios.** (The verification request cited 54 scenarios; the actual on-disk count is 45. Requirement count of 30 matches.)

**Task completion**: 44/44 checked. Spot-checked against code — every task's named artifact exists on disk with the described content. No unchecked task blocks verification.

---

## 2. Command Evidence

| Command | Exit | Result |
|---|---|---|
| `make check` (`gofmt -l -w .` + `go vet ./...` + `go test ./...`) | 0 | Clean. No formatting diff, no vet finding, 9/9 packages pass |
| `go test ./... -count=1 -v` | 0 | 116 top-level tests pass, **0 failures, 0 skips** (real bash/zsh integration tests actually ran) |
| `make cover` | 0 | See table below |
| `go build -o /tmp/aliasdeck-verify ./cmd/aliasdeck` | 0 | Binary builds |
| `git diff --stat main -- internal/domain internal/validate internal/renderers/{posix,header,renderer}.go internal/renderers/testdata` | 0 | **Empty output — Milestone 1 production code and golden files are byte-unchanged** |
| `make golden` then `git diff --stat internal/renderers/testdata` | 0 | Empty — goldens regenerate identically |
| `shellcheck -s sh scripts/install.sh` | 0 | Clean |
| `git status --short` after all runs | — | Clean working tree |

### Coverage (`go test -cover ./...`)

| Package | Coverage | ≥70% floor |
|---|---|---|
| `internal/source` | 100.0% | PASS |
| `internal/renderers` | 90.6% | PASS (M1, +1.5pp from task 4.1 test-only addition) |
| `internal/config` | 87.2% | PASS |
| `internal/validate` | 87.7% | PASS (M1, unchanged) |
| `internal/apply` | 82.5% | PASS |
| `internal/app` | 79.7% | PASS |
| `internal/state` | 73.0% | PASS |
| `internal/domain` | 70.4% | PASS (M1, unchanged) |
| **`cmd/aliasdeck`** | **62.7%** | **FAIL — see WARNING-1** |

---

## 3. Spec Compliance Matrix

Status legend: **PASS** = requirement satisfied, cited evidence + passing covering test. **PASS (RUNTIME-ONLY)** = behavior proven by exercising the built binary, no automated covering test. **PARTIAL** / **UNVERIFIED** / **FAIL** as named.

### 3.1 standalone-config (6 requirements / 10 scenarios)

| Requirement | Status | Evidence |
|---|---|---|
| aliases.yaml Strict Schema | PASS | `config.ParseAliases` (`internal/config/aliases.go:53`) uses `yaml.Decoder` + `KnownFields(true)` (:61) and a 1 MiB cap (:54). Tests: `TestParseAliasesValidFile`, `TestParseAliasesUnknownFieldRejected`, `TestParseAliasesOversizeRejected`, `TestParseAliasesWrongVersionRejected` |
| **`enabled` Defaults to True** | PASS | `aliasDTO.Enabled *bool` (`aliases.go:43`) → `Enabled: dto.Enabled == nil \|\| *dto.Enabled` (`aliases.go:100`). Default is applied in the parse layer, before `domain.AppliesTo` ever sees it, exactly as the spec demands. Test: `TestParseAliasesEnabledDefault`. **Runtime proof**: an `aliases.yaml` entry `gs` with no `enabled` key rendered as `alias gs='git status'`; a sibling entry with `enabled: false` did not |
| Omitted Targeting Fields Mean All/Always | PASS | `parsePlatforms`/`parseShells` return `nil` for empty input (`aliases.go:108`,`:123`); `domain.Alias.TargetsPlatform`/`TargetsShell` treat nil as "all". Test: `TestAliasTargetingDefaultsToEverywhere`. Runtime: `gs` (no `platforms`/`shells`) rendered on macos/zsh |
| Profile References Degrade to a Warning | PASS | `config.ProfileWarnings` (`aliases.go:141`) is independent of `ParseAliases`' return value; `Doctor` (`internal/app/doctor.go:49`) calls it and the CLI prints it under "profile warning(s)" without affecting `Issues.HasErrors()`. Tests: `TestProfileWarningsUndeclaredProfile`, `TestDoctorReportsHostileEntryAndUndeclaredProfile`. Runtime: `doctor` printed `alias "typoprof" references undeclared profile "nonexistent-profile"` and **exited 0**; `sync` still applied the other aliases |
| config.yaml Strict Schema and Device Identity | PASS | `ParseDeviceConfig` (`device.go:105`) + `Backend.Valid()` (:31) reject an unknown backend at parse time; `Load` (:155) generates and persists a stable fallback identity via `generateDeviceName` (:228). Tests: `TestParseDeviceConfigUnknownBackendRejected`, `TestLoadGeneratesStableFallbackIdentityWhenNameOmitted`. **Note**: this persistence is the mechanism behind CRITICAL-1 |
| Platform and Shell Auto-Detection | PASS | `config.DetectPlatform`/`DetectShell` (`detect.go:32`,`:68`) with documented precedence and a `Provenance` string surfaced by `status` (`cmd/aliasdeck/status.go:27-28`) and `doctor` (`cmd/aliasdeck/doctor.go:43-44`). Tests: `TestDetectPlatformPrecedence`, `TestDetectShellPrecedence`. Runtime: `status` printed `Platform: macos ($ALIASDECK_PLATFORM)` / `Shell: zsh ($ALIASDECK_SHELL)`; with `ALIASDECK_SHELL=fish` both `status` and `doctor` failed with `detecting shell: $ALIASDECK_SHELL: unknown shell "fish"` — named, never guessed (see SUGGESTION-1) |

### 3.2 config-source (4 requirements / 6 scenarios)

| Requirement | Status | Evidence |
|---|---|---|
| ConfigSource Contract | PASS | `source.ConfigSource` (`internal/source/source.go`) returns `(domain.ResolvedConfig, error)`; `FileSource.Resolve` (`file.go:37`) returns a zero-value config on every error path (:40, :45) so nothing partial escapes. No `exec`/`sh` anywhere in `internal/source`. Tests: `TestFileSourceResolveReadsConfiguredPathOnly`, `TestFileSourceResolveErrorNotPartiallyApplied` |
| FileSource Reads a Single Local Path | PASS | `FileSource{Path}` reads exactly `s.Path` (`file.go:38`); `resolveSource` (`internal/app/context.go:112`) expands `source.path` or defaults to `<base>/aliases.yaml`, with no second candidate anywhere. **Runtime proof**: with `source.path` pointed at `$S/dotfiles/aliases.yaml` and a *decoy* `aliases.yaml` in the base dir declaring alias `DECOY`, `sync` emitted only `alias real='echo real'` and `status` named the dotfiles path |
| **Every Source Is Hostile Input** | PASS | `validate.FilterValid` is called inside `Resolve` (`file.go:49`), so nothing unfiltered can structurally reach `renderers.Render`. Test: `TestFileSourceResolveFiltersHostileInput`. **Runtime proof**: an `aliases.yaml` containing `evil; rm -rf /` (metacharacter name), a newline-injection command, and a 9 KB command produced a generated file containing only `alias good='echo ok'`; `doctor` listed all three with specific reasons and exited 3 |
| **Exactly One Source Per Device** | PASS | `resolveSource` has one `case` for file and a `default` that errors (`context.go:126`) — no merge, no chain. **Runtime proof**: deleting the configured source file made `sync` exit 1 with `resolving <path>: reading <path>: no such file or directory` and it did **not** fall back to the decoy in the base dir. `source.type: git` exits 1 with `source type "git" is not supported in this version of AliasDeck`. `status` names the active source on every invocation (`Source: file (<path>)`) |

### 3.3 native-apply (6 requirements / 8 scenarios)

| Requirement | Status | Evidence |
|---|---|---|
| **Atomic Write** | PASS | `writeFileAtomic` (`internal/apply/atomic.go:30`): `MkdirAll` → `refuseUnsafeDestination` → `os.CreateTemp` *in the same directory* (:40) → `defer os.Remove(tmpPath)` (:45) → `Chmod`/`Write`/`Sync`/`Close` (:82) → `osRename` (:51). The temp file is a dotfile (`.aliases.zsh.*.tmp`) and is removed on every pre-rename failure, so no truncated file is ever left where a shell would source it. Tests: `TestWriteFileAtomicSuccess`, `TestWriteFileAtomicCleansUpTempFileOnRenameFailure`, `TestNativeBackendApplyNoPartialWriteOnInterruption` (forces `osRename` failure and asserts the *prior valid content* survives with no leftover temp) |
| Bootstrap Line Management | PASS | `AddBootstrap` (`bootstrap.go:51`) no-ops when `beginMarker` is already present (:62). Tests: `TestAddBootstrapIsIdempotent`, `TestAddBootstrapNoOpsOnManuallyCraftedPreExistingBlock`, `TestAddBootstrapFixtures` (4 rc shapes). **Runtime proof**: `init --yes` twice against the same rc file left exactly 1 `# >>> aliasdeck >>>` marker |
| Non-Destructive to User Files | PASS | `buildBlock` (`bootstrap.go:163`) only *appends*; `indexOfLine` (:225) matches markers whole-line-only so marker-like text inside unrelated lines is never touched. Tests: `TestRemoveBootstrapMarkerLikeTextInHostileRCNotCorrupted`, `TestBootstrapNeverTouchesUnrelatedFiles`, `TestBootstrapSymlinkedRCStaysSymlink`. **Runtime proof**: pre-existing rc lines survived unchanged and in order |
| **Uninstall Restores Byte-Identical Files** | PASS | `state.Bootstrap.Block` stores the exact appended bytes (`init.go:213`); `RemoveBootstrap` (`bootstrap.go:99`) cuts that exact span with one index+splice and reports `exact`; `uninstall` surfaces `exact=false` as an explicit warning (`cmd/aliasdeck/uninstall.go:40`). Tests: `TestUninstallRestoresRCFileByteIdentically`, `TestRemoveBootstrapExactByteRestore`, `TestBootstrapRoundTripOnRealisticRCFiles` (8 fixtures incl. CRLF), `TestFullLifecycleInitSyncSyncUninstall`. **Runtime proof**: on the hardest fixture (an rc file with **no trailing newline**, so padding is involved), `cmp` reported the post-uninstall file byte-identical to a pre-init copy; `config.yaml`/`aliases.yaml` survived, `aliases.zsh` and `state.json` were removed |
| SyncBackend Seam | PASS | `apply.SyncBackend{Name,OutputPath,Apply}` (`backend.go`); `resolveBackend` (`context.go:137`) dispatches on `config.yaml`'s `backend`. Tests: `TestBackendsSatisfySyncBackendInterface`, `TestNativeBackendApplyHappyPath` |
| **Chezmoi Backend Fails Explicitly** | PASS | Every `ChezmoiBackend` method returns `errChezmoiNotImplemented` = `backend "chezmoi" is not implemented in v0.1` (`native.go:59,71,75`). Test: `TestChezmoiBackendFailsExplicitly`. **Runtime proof**: `sync` with `backend: chezmoi` printed exactly that message and exited **1**; the base dir afterwards contained only `aliases.yaml` and `config.yaml` — **no generated file, no `state.json`, no rc edit** |

### 3.4 sync-state (3 requirements / 4 scenarios)

| Requirement | Status | Evidence |
|---|---|---|
| Sync State Is Recorded After Apply | PASS | `syncWithContext` builds and saves `state.State` after `Backend.Apply` (`internal/app/sync.go:86-107`), recording `Revision`, `OutputHash` (sha256 hex of rendered bytes), `LastSyncAt`, and preserving `prevState.Bootstrap` (:102). `state.Save` is itself atomic at 0600 (`state.go:78`). Tests: `TestSyncFullPipelineOrder` (asserts `st.Revision`), `TestStateRoundTrip`, `TestStateSaveSetsFileMode0600`. Runtime: `state.json` present at `-rw-------` after `init`/`sync` |
| **No-Op Skip When Unchanged** | PASS | `if prevState.Revision == cfg.Revision && diskHashMatches(outputPath, prevState.OutputHash)` (`sync.go:81`) — **both** conditions, exactly as the spec words it. `diskHashMatches` returns false on any read failure or empty want (`hash.go:20`), so a deleted or tampered file forces a rewrite. Tests: `TestSyncNoOpSkipWhenUnchanged` (proves it by making the base dir read-only — a write attempt would error), `TestSyncForcedRewriteOnDiskHashMismatch`. **Runtime proof**: a second `sync` printed `Up to date: 1 alias(es), no write needed` and the generated file's **inode and nanosecond mtime were byte-for-byte unchanged** |
| Rendered Output Is Deterministic | PASS | `writeHeader` (`internal/renderers/header.go:21`) deliberately emits no timestamp (documented at :15-20); `ResolvedConfig.GeneratedAt` is recorded in state, never rendered. `domain.ComputeRevision` (`resolved.go:67`) hashes only output-affecting fields over a sorted alias list. Tests: `TestSyncRenderedOutputIsDeterministic`, `TestRenderIsDeterministic`, `TestRevisionTracksRenderedContent`, `TestGolden` |

### 3.5 cli-commands (8 requirements / 13 scenarios)

| Requirement | Status | Evidence |
|---|---|---|
| **Exit Code Convention** | PASS | `cmd/aliasdeck/exit.go` defines 0/1/2/3/4; `exitCodeFor` (:44) maps `app.ErrNotInitialized`→4, `app.ConfigError`→3, else 1; `run` (`main.go:45`) returns 2 when `cmd.SilenceUsage` is still false (Cobra flag/arg validation). Tests: `TestRunNotInitializedExitsFour`, `TestRunUnknownCommandExitsTwo`, `TestRunDoctorFindsErrorExitsThree`, `TestRunEditWithoutEditorExitsOne`. **Runtime probe of all five**: happy paths → 0; `backend: chezmoi` → 1; `edit` with no `$EDITOR` → 1; unknown subcommand → 2; unknown flag → 2; unknown field in `config.yaml` → 3; `doctor` with a `SeverityError` → 3; `status` with no `config.yaml` → 4. **Every code matches the design table.** All errors go to stderr (`main.go:34,39`) |
| **init Creates Config and Prompts Before Bootstrap** | PARTIAL | Creation: `createIfAbsent` for both files (`init.go:91`, :98-111). Prompt: `Init` calls `Confirm`/`promptYesNo` *before* `apply.AddBootstrap` (`init.go:143-165`) — tests `TestInitPromptsBeforeBootstrapAndAddsOnConsent`, `TestInitPromptDeclinedLeavesRCFileUntouched`. `--no-bootstrap`: `init.go:130` returns before any rc resolution — test `TestInitNoBootstrapSkipsPromptAndRCFile`; runtime confirmed rc untouched. Non-terminal stdin: `isInteractive` (`prompt.go:45`) returns false for a non-char-device `*os.File` and `promptYesNo` returns `(false, nil)` **without printing or reading** — test `TestPromptYesNoDoesNotBlockOnNonTerminalStdin`; **runtime proof**: `init` fed from a FIFO that was held open but never written returned in **0.058 s** with exit 0, printed `Bootstrap line not added (declined)` plus the manual line, and left the rc file untouched. **`--yes`/`-f` is the gap**: wired (`cmd/aliasdeck/init.go:43`, `init.go:143`) and **runtime-verified** (`init --yes --rc-file … </dev/null` added the block with no prompt output, exit 0), but **no automated test references `AssumeYes`** — see CRITICAL-1's sibling WARNING-2 |
| sync Runs the Full Pipeline | PASS | `syncWithContext` order: `Source.Resolve` (which internally does parse → `domain.Resolve` → `FilterValid`) → `renderers.Render` → `Backend.Apply` → `state.Save` (`sync.go:45-107`). Prints the alias count (`cmd/aliasdeck/sync.go:26-29`). Tests: `TestSyncFullPipelineOrder`, `TestSyncUnresolvableSourceNamesTheSource`. Runtime: `Applied 1 alias(es) to …`, exit 0; missing source → exit 1 naming the path |
| status Always Reports the Active Source | PASS | `Status` (`status.go:29`) always returns `Source` (descriptor), `Device`, `State.LastSyncAt`, and `UpToDate` computed from output-path match **and** disk-hash match (:42). The CLI prints all of them unconditionally. Test: `TestStatusReportsActiveSource`, `TestStatusReportsNotInitialized`. Runtime output includes Device / Platform+provenance / Shell+provenance / Source / Backend / Last sync / Status |
| list Shows Resolved Aliases | PASS | `List` (`list.go:33`) reads the source directly (not `Resolve`, which already drops non-matching entries) and annotates each with `Active` + `skipReason` covering disabled/platform/shell/profile/device in precedence order (:65). Tests: `TestListShowsDeviceScopedEntries`, `TestSkipReasonCoversEveryTargetingDimension`. Runtime on macos/zsh: `gs` active; `gp (disabled)`, `onlybash (not targeted at shell "zsh")`, `onlylinux (not targeted at platform "macos")`, `typoprof (no matching profile)` all shown as skipped with reasons |
| **doctor Diagnoses Without Writing** | **FAIL** | Diagnosis half is correct: `Doctor` (`doctor.go:32`) runs its own `domain.Resolve` → `validate.Config` pass, returns `Issues` plus `ProfileWarnings`, and the CLI prints each `Issue.String()` sorted by alias then field (`cmd/aliasdeck/doctor.go:49`), exiting 3 only on `HasErrors()`. **But the "MUST NOT write to disk" half is violated** — see CRITICAL-1 |
| edit Opens $EDITOR Without Side Effects | PASS | `Edit` (`edit.go:52`) does `strings.Fields($EDITOR)` → `env.LookPath(bin)` → `exec.Command(resolved, args…)`. **There is no `sh -c` anywhere in the package.** No sync/render/apply is invoked. Tests: `TestEditNeverInvokesAShell`, `TestEditMultiWordEditorPassesThrough`, `TestEditHasNoSyncSideEffect`, `TestEditReturnsErrorWhenEditorNotSet`. **Runtime proof**: with `EDITOR="x; rm -rf ."` run from inside a directory holding a canary file, the command exited 1 with `editor "x;" from $EDITOR is not an executable on PATH` and **the canary survived**; with `EDITOR="fakeeditor -w"` the fake editor received exactly `ARG1=[-w] ARG2=[<path>]` — argv only, never a shell string |
| uninstall Confirms and Restores | PASS | `Uninstall` (`uninstall.go:49`) prompts unless `opts.Yes` (:55-68), returning `Cancelled` on decline before touching anything. `--yes`/`-f` at `cmd/aliasdeck/uninstall.go:51`. Tests: `TestUninstallInteractivePromptsBeforeModifying`, `TestUninstallYesSkipsPrompt`, `TestUninstallRestoresRCFileByteIdentically`, `TestUninstallExactFalseWhenUserEditedInsideBlock`. Runtime: `uninstall --yes` removed the generated file and the block, left the rc byte-identical, and preserved `config.yaml`/`aliases.yaml` |

### 3.6 release-distribution (3 requirements / 4 scenarios)

| Requirement | Status | Evidence |
|---|---|---|
| Cross-Compiled Release Artifacts | **UNVERIFIED** | `.goreleaser.yaml` declares `goos: [darwin, linux] × goarch: [amd64, arm64]` with `CGO_ENABLED=0` and `-s -w` — configuration is correct by inspection. But `goreleaser` is not installed here, there is **no `.github/` directory and therefore no release pipeline**, and no tag exists. The scenario ("WHEN the release pipeline runs THEN four static binaries are produced") cannot be executed |
| Homebrew Tap Formula | **UNVERIFIED** | `brews:` block targets `angeltonio/homebrew-tap` with `directory: Formula`, install/test stanzas present. The tap repository does not exist yet (documented as a blocking comment at `.goreleaser.yaml:3-18` and in `proposal.md` Dependencies). Not executable |
| Install Script | PARTIAL | `scripts/install.sh` exists, is POSIX `sh`, `set -eu`, `shellcheck -s sh` clean. **Runtime proof of the failure path**: with a faked `uname` returning `SunOS`/`sparc` it exited **1** with `unsupported operating system: SunOS …` and performed no install. Archive naming is consistent: goreleaser produces `aliasdeck_{{.Version}}_{{.Os}}_{{.Arch}}.tar.gz` and install.sh requests `${BIN_NAME}_${version#v}_${os}_${arch}.tar.gz` — these match. The **success** path ("Supported platform") cannot be verified: no published release exists to download |

---

## 4. Proposal Success Criteria

| # | Criterion | Status |
|---|---|---|
| 1 | `init` then `sync` produces a working generated file on clean macOS and Linux | **PARTIAL** — verified end-to-end on macOS with the built binary; the generated file was also sourced cleanly by **real `bash` and real `zsh`** (`TestSyncedFileSourcesCleanlyInRealShells`, ran, did not skip). Linux is covered only by `ALIASDECK_PLATFORM=linux` unit tables, not by an actual Linux runtime — no Linux host or CI job exists |
| 2 | An entry omitting `enabled` renders | **MET** — runtime-proven |
| 3 | `status` always names the active source; `doctor` reports each skipped alias with a reason | **MET** — runtime-proven for both |
| 4 | A second `sync` with no upstream change performs no write | **MET** — inode + nanosecond mtime unchanged |
| 5 | `uninstall` leaves every user-owned file byte-identical to its pre-install state | **MET for the rc file and the two config files under the normal flow.** Qualified by CRITICAL-1: a `config.yaml` that omits `device.name` is rewritten by the *first* read-only command, so its pre-install bytes are not preserved by any command that touches it |
| 6 | `make check` green, new packages ≥70% coverage | **PARTIAL** — `make check` green; 7 of 8 new packages ≥70%, `cmd/aliasdeck` at 62.7% (WARNING-1) |
| 7 | Tagged v0.1 installs from the tap and the install script | **NOT MET / OUT OF REACH** — no tag, no tap repository, no CI. Honestly disclosed in `README.md` and `.goreleaser.yaml`. This criterion is a release-time gate, not an implementation gate |

---

## 5. Design Coherence

| Design decision | Implemented as designed | Note |
|---|---|---|
| D1 package boundaries + `Env{Stdin,Stdout,Stderr,Getenv,HomeDir,Now,LookPath}` | Yes | `internal/app/env.go:22` matches the design string exactly |
| D2 `enabled` via DTO `*bool` | Yes | `aliases.go:43,100`; `domain.Alias` untouched |
| D3 strict parsing (`KnownFields`, version==1, 1 MiB) | Yes | `aliases.go:54,61,66`; `device.go:106,113,118` |
| D4 `state.json`, JSON, 0600, tolerant load | Yes | `state.go:58,78,107` |
| D5 no-op skip = revision AND disk hash | Yes | `sync.go:81` |
| D6 marker block, exact bytes in state, single cut | Yes | `bootstrap.go:163,113-122`; fallback marker scan reports `exact=false` and is surfaced to the user |
| D7 `filepath.EvalSymlinks` on rc path | Yes | `bootstrap.go:143` |
| D8 `exec.Command` split argv, never `sh -c` | Yes | `edit.go:68-77` |
| D9 chezmoi hard error | Yes | `native.go:59` |
| D10 `go.yaml.in/yaml/v3` (not `gopkg.in/yaml.v3`) | Yes | `go.mod` declares `go.yaml.in/yaml/v3 v3.0.5` and `github.com/spf13/cobra v1.10.2` |
| Threat matrix "Output path": refuse symlink/dir destination | Yes | `atomic.go:61`. **Runtime proof**: `sync` onto a symlinked `aliases.zsh` exited 1 with `destination is a symlink` and the symlink target's content (`PRECIOUS`) was untouched |
| Threat matrix "Output path": *"refuse a path outside the resolved base dir"* | **NOT implemented** | See SUGGESTION-2 |
| Design: "Order per sync: generated file → bootstrap → state" | Deviation, documented | Bootstrap is `init`-only; `sync` never touches an rc file. Rationale recorded in `apply-progress.md` "Design Interpretation Notes 1" and is consistent with the cli-commands spec, which assigns rc consent to `init` alone. Accepted |
| Design exit table: "exit 3 for `doctor`/**`edit`** found SeverityError" | Deviation, documented | `edit` performs no validation; it reaches exit 3 only through the shared `config.yaml` parse path. Rationale in "Design Interpretation Notes 2". The cli-commands spec never gives `edit` a validation step, so the spec is not broken. Accepted |

---

## 6. Issues

### CRITICAL

**CRITICAL-1 — `doctor` writes to disk, violating an explicit MUST.**

- **Spec violated**: `cli-commands` → "doctor Diagnoses Without Writing": *"…and MUST NOT write to disk"*, and its scenario "Hostile entry reported": *"…and writes nothing"*.
- **Mechanism**: `Doctor` → `loadDeviceContext` (`internal/app/context.go:59`) → `config.Load` (`internal/config/device.go:155`). When `cfg.Device.Name == ""`, `Load` generates a fallback identity and **persists it by calling `Write(path, cfg)`** (`device.go:166-177`). `Write` re-serializes the whole DTO, so the user's `config.yaml` is rewritten.
- **Reproduction (performed)**: hand-written `config.yaml` with `version`, `source.type: file`, `backend: native` and **no `device:` block**. sha256 before `doctor` = `9e3203389a10a8ff…`; after `doctor` = `5dff8e6e2cb0eb72…`. Size grew 50 → 162 bytes, and the file gained `device.name`, `profiles: []`, `platform: ""`, `shell: ""`, `source.path: ""`, `source.url: ""`. `doctor` printed a normal report and exited 0 while doing this.
- **Why the suite does not catch it**: `TestDoctorWritesNothing` (`internal/app/doctor_test.go:55`) seeds its fixture with `nativeDeviceConfig("test-device")`, whose own comment says *"device name set so no fallback-identity generation kicks in mid-test"* — the exact failing precondition is excluded. The assertion then only compares **file counts** in the base dir (`:82`), not content, so even a rewrite in place would pass.
- **Blast radius**: `status`, `list`, and `edit` share `loadDeviceContext` and therefore share the write. Only `doctor` has a spec that forbids it, but rewriting a user-authored `config.yaml` (dropping comments and formatting, adding empty scalars) on a read-only command is also in tension with PROJECT.md §3.4's non-destructive promise and with Success Criterion 5.
- **Suggested direction (not applied — verification does not fix)**: split identity resolution from persistence — e.g. `config.Load` returns the generated name without writing, and only the mutating commands (`init`, `sync`) call an explicit `config.EnsureIdentity`. Then strengthen `TestDoctorWritesNothing` to hash every file in the base dir before and after, and add a case whose `config.yaml` omits `device.name`.

### WARNING

**WARNING-1 — `cmd/aliasdeck` coverage 62.7% is below the 70% floor.**
`openspec/config.yaml` sets `verify.coverage_threshold: 70` with no package exemption, and `proposal.md` Success Criterion 6 says "new packages ≥70% coverage". `cmd/aliasdeck` is a new package created by this change. `apply-progress.md` (Batch 3) asserts the floor is scoped to packages "the design calls out" — the design's own line is *"New packages target ≥70% coverage"*, which does not carve out Cobra wiring. Either raise the coverage or record an explicit, agreed exemption in `openspec/config.yaml`; do not leave the contradiction implicit.

**WARNING-2 — The `--yes` spec scenario has no covering test.**
`cli-commands` scenario "Non-interactive install with explicit consent" is a first-class scenario added in Batch 4. No test in the repository references `InitOptions.AssumeYes` (`rg AssumeYes internal/app cmd` matches only production code). Under Strict TDD this is the one behavior in `init` that can edit a user-owned rc file with **no prompt at all**, so it is precisely the path that should be pinned. The behavior itself is correct — I verified it at runtime — but a regression here would be silent.

**WARNING-3 — `edit --config` has no covering test.**
`cli-commands` → "edit Opens $EDITOR Without Side Effects" explicitly covers *"or `config.yaml` with an explicit flag"*. `EditTargetConfig` is referenced only from `cmd/aliasdeck/edit.go:22`; no test exercises the branch at `internal/app/edit.go:59-61`. Runtime-verified working (the fake editor received the `config.yaml` path), but untested.

**WARNING-4 — the whole `release-distribution` capability is unverifiable in this environment.**
3 requirements / 4 scenarios; only the install script's *failure* path could actually be executed. There is no `.github/` directory, so nothing invokes `goreleaser` on a tag; `goreleaser` is not installed, so not even `goreleaser check` could validate the config; and `angeltonio/homebrew-tap` does not exist. Tasks 5.1–5.3 asked only for the artifacts and those exist and read correctly, so this is not a task failure — but the capability must not be recorded as verified.

**WARNING-5 — Linux is not exercised at runtime.**
Success Criterion 1 names macOS *and* Linux. Platform handling is table-tested via the `ALIASDECK_PLATFORM` seam, and the Linux-specific bash rc ordering is covered by `TestResolveRCPath`, but no Linux process ever ran this binary. This is an environment limitation, stated rather than glossed.

### SUGGESTION

**SUGGESTION-1 — an unsupported shell is a hard error, not a `doctor` diagnosis.**
`standalone-config` scenario "Unsupported shell detected" says the CLI *"reports the unsupported shell via `doctor`/`status`"*. Today `ALIASDECK_SHELL=fish` makes `loadDeviceContext` fail, so `doctor` exits 1 with `detecting shell: $ALIASDECK_SHELL: unknown shell "fish"` before printing any report. It does name the shell and never guesses, so I read the requirement as satisfied — but a user whose `$SHELL` is fish gets a failure from the command whose entire job is to diagnose. Consider letting `doctor` degrade to a report with the detection failure as an issue.

**SUGGESTION-2 — the threat matrix's "refuse a path outside the resolved base dir" is not implemented.**
`design.md` Threat Matrix, "Output path" row, lists two defenses. `refuseUnsafeDestination` (`atomic.go:61`) implements the symlink/directory refusal but there is no containment check against `NativeBackend.Base`. It is currently unreachable — `OutputPath` always joins onto `Base` — so this is latent, not exploitable. Worth either implementing or striking from the design so a future backend does not inherit an assumption that was never enforced.

**SUGGESTION-3 — `InitReport.BootstrapPrompted` is set to `true` even when the prompt was skipped for a non-terminal stdin** (`init.go:141` runs before the `isInteractive` check inside `promptYesNo`). It is informational only and nothing consumes it today, but the field name now asserts something untrue in the exact scenario the spec was amended to describe.

**SUGGESTION-4 — `doctor` validates only device-applicable aliases.**
`Doctor` runs `validate.Config(domain.Resolve(dc.Device, doc.Aliases))` (`doctor.go:47-48`), so a hostile entry targeted at another platform is never reported on this device. The spec's wording ("every alias skipped by validation") is arguably satisfied — an alias filtered out by targeting was not skipped *by validation* — but a user debugging a shared `aliases.yaml` across machines will see different diagnoses per machine. Worth an explicit decision rather than an accident.

---

## 7. Verdict

**PASS WITH WARNINGS.**

The implementation is substantially faithful to its specifications. Of the 30 requirements, 29 are satisfied with cited code and passing tests, and I confirmed the load-bearing ones by exercising the built binary rather than by reading: `enabled` defaults to true at the parse layer; a second sync leaves the inode and nanosecond mtime untouched; exactly one source resolves with a decoy present and no fallback when it disappears; uninstall restores a no-trailing-newline rc file byte-identically; a symlinked output path is refused with the target intact; a hostile `aliases.yaml` reaches disk as nothing but the one valid alias; `$EDITOR="x; rm -rf ."` leaves the canary alive; `init` from a never-delivering pipe returns in 58 ms; `backend: chezmoi` fails with the exact not-implemented message and writes nothing; and all five exit codes match the design table. Milestone 1 production code and golden files are byte-unchanged.

One requirement fails outright: `doctor` writes to disk when `config.yaml` omits `device.name`, and the test named after that requirement is fixtured around the failing precondition. That is exactly the class of gap this phase exists to find — the requirement everyone assumed was obviously covered.

`release-distribution` is not verified, and should not be recorded as such: no CI, no tap, no tag, no `goreleaser`.
