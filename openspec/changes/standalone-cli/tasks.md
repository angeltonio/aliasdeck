# Tasks: Standalone CLI — Milestone 2 (v0.1)

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~2800-3200 (6 new packages + 7 commands + release tooling + docs) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | Stacked PRs to main, 7 units (see below), landed in dependency order |
| Delivery strategy | ask-on-risk |
| Chain strategy | pending — orchestrator must ask before apply |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

**Why stacked-to-main, not feature-branch-chain**: every package is additive; Milestone 1 (`domain`/`validate`/`renderers`) is never touched. Each unit below leaves `go build ./...` and `go test ./...` green on its own, so each can merge to `main` independently rather than waiting behind a tracker branch.

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | `go.mod` deps + `internal/config` paths/device/detect (Phase 1.1-1.9 minus 1.4/1.5) | PR 1 | `go test ./internal/config/...` | N/A — no CLI yet | Delete `internal/config/{paths,device,detect}*.go`; revert `go.mod` |
| 2 | `aliases.yaml` DTO + `enabled` default + profile warnings (1.4-1.5) | PR 2 | `go test ./internal/config/...` | N/A — parser only | Delete `internal/config/aliases*.go` |
| 3 | `internal/source` (`ConfigSource`, `FileSource`) | PR 3 | `go test ./internal/source/...` | N/A — no writer yet | Delete `internal/source/` |
| 4 | `internal/apply` (atomic write, bootstrap, `NativeBackend`) | PR 4 | `go test ./internal/apply/...` | Bootstrap fixtures with real rc-file bytes | Delete `internal/apply/` |
| 5 | `internal/state` (state.json round-trip) | PR 5 | `go test ./internal/state/...` | N/A — no CLI writes state yet | Delete `internal/state/` |
| 6 | `internal/app` (7 use cases) + `cmd/aliasdeck` (Cobra wiring) | PR 6 | `go test ./internal/app/... ./cmd/...` | `init && sync && sync && uninstall` on `t.TempDir()` HOME, real bash/zsh sourcing test | Delete `internal/app/`, `cmd/aliasdeck/` |
| 7 | Release tooling + docs sync (`.goreleaser.yaml`, `install.sh`, PROJECT.md §9/§11, README, `openspec/config.yaml`, renderers `Shell()` coverage) | PR 7 | `make check` | `curl ... install.sh` against a local artifact dir | Revert doc/tooling files individually; no code dependency |

Unit 4 and Unit 6 are the largest (~530 and ~730 lines respectively); split further into `apply-write` / `apply-bootstrap` or `app-core(sync,init)` / `app-diagnostics(status,list,doctor,edit,uninstall)` if a single PR still feels heavy in review.

## Phase 1: Config Foundation

- [ ] 1.1 `go.mod`/`go.sum`: add `github.com/spf13/cobra v1.10.2`, `go.yaml.in/yaml/v3 v3.0.5` (design D10); `go mod tidy`
- [ ] 1.2 RED `internal/config/paths_test.go`: `$ALIASDECK_HOME` → `$XDG_CONFIG_HOME/aliasdeck` → `~/.config/aliasdeck`; assert `os.UserConfigDir` is never called
- [ ] 1.3 GREEN `internal/config/paths.go`: `Base()`, per-file paths, `~`/`$HOME` expansion
- [ ] 1.4 RED `internal/config/aliases_test.go`: `enabled` omitted/`true`/`false`, unknown field rejected, wrong `version`, >1MiB input, undeclared `profiles:` entry (SC reqs 1-4; proposal risk #1)
- [ ] 1.5 GREEN `internal/config/aliases.go`: DTO with `Enabled *bool`, `profiles:`→`ProfileIDs`, `ID` from `Name`, `KnownFields(true)`, 1MiB cap, `ProfileWarnings`
- [ ] 1.6 RED `internal/config/device_test.go`: valid `config.yaml`, unknown `backend` value rejected
- [ ] 1.7 GREEN `internal/config/device.go`: `Device`/`config.yaml` schema, `Load`/`Write`, backend enum (SC req 5)
- [ ] 1.8 RED `internal/config/detect_test.go`: platform/shell precedence incl. `config.yaml` override, unsupported shell, `Provenance` string
- [ ] 1.9 GREEN `internal/config/detect.go`: platform/shell/rc detection (SC req 6; design §Paths)

## Phase 2: Source, Apply, State

- [ ] 2.1 RED `internal/source/file_test.go`: configured path only (no merge/fallback), resolve error not partially applied, hostile alias name/oversized command filtered before render (CS reqs 1-4; threat matrix "Hostile aliases.yaml")
- [ ] 2.2 GREEN `internal/source/source.go`, `file.go`: `ConfigSource`, `Descriptor`, `FileSource.Resolve` → `validate.FilterValid` → `renderers.Render`
- [ ] 2.3 RED `internal/apply/atomic_test.go`: temp+rename, mode, symlink/directory destination refused, temp cleanup on failure (NA req 1; threat matrix "Output path")
- [ ] 2.4 GREEN `internal/apply/atomic.go`: atomic write helper
- [ ] 2.5 RED `internal/apply/bootstrap_test.go`: rc fixtures (trailing-newline/none/empty/pre-existing block/user-edited block), idempotent add, exact-byte removal, symlinked rc preserved, marker text inside hostile rc not corrupted (NA reqs 2-4; threat matrix "rc file mutation")
- [ ] 2.6 GREEN `internal/apply/bootstrap.go`: marker block add/remove, `filepath.EvalSymlinks`
- [ ] 2.7 RED `internal/apply/native_test.go`: `NativeBackend.Apply` happy path, `backend: chezmoi` hard error, no partial writes
- [ ] 2.8 GREEN `internal/apply/backend.go`, `native.go`: `SyncBackend{Name,OutputPath,Apply}`, `NativeBackend`, `ChezmoiBackend` stub (NA reqs 5-6)
- [ ] 2.9 RED `internal/state/state_test.go`: round-trip, corrupt/missing JSON tolerated
- [ ] 2.10 GREEN `internal/state/state.go`: `State`, `Bootstrap`, `Load`/`Save` at `0600` (SS req 1)

## Phase 3: App Use Cases & CLI Wiring

- [ ] 3.1 RED `internal/app/sync_test.go`: full pipeline order, no-op skip on matching revision+hash, forced rewrite on disk-hash mismatch, deterministic render hash (SS reqs 2-3)
- [ ] 3.2 GREEN `internal/app/sync.go`: resolve→validate→render→apply→state, `Env` injection
- [ ] 3.3 RED `internal/app/init_test.go`: creates both config files, prompts before bootstrap, `--no-bootstrap` skip
- [ ] 3.4 GREEN `internal/app/init.go`
- [ ] 3.5 RED `internal/app/{status,list,doctor}_test.go`: active-source reporting, device-scoped listing, hostile-entry + undeclared-profile diagnostics, `doctor` writes nothing
- [ ] 3.6 GREEN `internal/app/status.go`, `list.go`, `doctor.go`
- [ ] 3.7 RED `internal/app/edit_test.go`: `$EDITOR="x; rm -rf ."` must not execute, `code -w` passes through, no sync side effect (threat matrix "Editor subprocess")
- [ ] 3.8 GREEN `internal/app/edit.go`: `exec.Command` with split argv, never `sh -c`
- [ ] 3.9 RED `internal/app/uninstall_test.go`: byte-identical rc restore, `--yes` vs interactive prompt
- [ ] 3.10 GREEN `internal/app/uninstall.go`
- [ ] 3.11 GREEN `cmd/aliasdeck/main.go`, `root.go`, `{init,sync,status,list,doctor,edit,uninstall}.go`: Cobra wiring, exit-code map (exit 0-4)
- [ ] 3.12 Integration `internal/app`: `init`→`sync`→second `sync` (no write)→`uninstall` (byte-identical rc) on `t.TempDir()` HOME

## Phase 4: Milestone-1-Adjacent Verification (no production edits)

- [ ] 4.1 Add table case to `internal/renderers/posix_test.go` covering `posixRenderer.Shell()` (0% coverage today) — test-only, no golden/production change
- [ ] 4.2 Run `make golden`; confirm zero diff in `internal/renderers/testdata` (golden files stay untouched, per design)
- [ ] 4.3 Run existing real bash/zsh injection test unmodified; add a new integration test (skipped under `-short`) asserting the Milestone-2 generated file sources cleanly in real `bash` and `zsh`
- [ ] 4.4 Decide and document CRLF rc-file handling scope for `uninstall`: add a CRLF fixture case to 2.5; either pass byte-identical or add an explicit "LF-only in v0.1" note plus a `doctor` warning

## Phase 5: Release Tooling

- [ ] 5.1 Create `.goreleaser.yaml`: darwin/linux, amd64/arm64, `CGO_ENABLED=0`, homebrew-tap target (RD reqs 1-2)
- [ ] 5.2 Create `scripts/install.sh`: OS/arch detection, download+install, clean non-zero exit on unsupported platform (RD req 3)
- [ ] 5.3 Confirm `homebrew-tap` repository prerequisite is documented (proposal Dependencies) before tagging

## Phase 6: Docs & Config Sync

- [ ] 6.1 `docs/PROJECT.md` §11: add `Name() string` and `OutputPath(dev domain.Device) (string, error)` to the `SyncBackend` code block (design D9)
- [ ] 6.2 `docs/PROJECT.md` §9: retract "stdlib only"; list `cobra` and `go.yaml.in/yaml/v3` as first runtime deps
- [ ] 6.3 `openspec/config.yaml` `context`: replace "stdlib only, no external deps yet" with the actual dependency list
- [ ] 6.4 `README.md`: replace "not usable yet" status banner/table with real `init`/`sync` usage
- [ ] 6.5 Run `make check` and `make cover`: confirm new packages ≥70% coverage, Milestone 1 coverage unchanged

## Parallelization Notes

- Phase 1: 1.2-1.3 first; 1.4-1.5 and 1.6-1.7 can run in parallel once 1.3 lands; 1.8-1.9 depends on both.
- Phase 2: `source` (2.1-2.2), `apply` (2.3-2.8), `state` (2.9-2.10) are mutually independent once Phase 1 lands — parallelizable across PR 3/4/5.
- Phase 3 depends on all of Phase 2 landing first; internally, 3.1-3.10 (per-command RED/GREEN pairs) can be split across contributors, 3.11 waits on all of them.
- Phase 4 is independent of Phases 1-3 and can start immediately (touches only `internal/renderers`).
- Phase 5 and 6 depend only on Phase 3 being functionally complete (need real commands/deps to document/package) and can run in parallel with each other.
