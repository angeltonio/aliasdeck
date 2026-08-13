# Apply Progress: Self-hosted server — Milestone 4 (v0.3)

**Change**: `server`
**Mode**: Strict TDD (per `openspec/config.yaml`), applied where it fits — Phase 1 is decisions, skeleton and a mechanical guard, not new runtime behavior, so TDD is not applied ceremonially to `doc.go` files. Where behavior exists (the archtest guard), RED → GREEN evidence is below.
**Batch**: 1 of N (first apply batch; no prior apply-progress existed)
**Branch**: `feat/server-foundation` (no commit created — orchestrator owns commit boundaries)

## Phase 1: Open Decisions & Foundation Skeleton — COMPLETE (6/6)

- [x] 1.1 `login`/`logout` semantics — confirmed as proposed, recorded as `design.md` decision 17
- [x] 1.2 `goose` vs `golang-migrate` — confirmed as proposed, `design.md` decision 5 annotated
- [x] 1.3 `go.mod` deps added
- [x] 1.4 Skeleton packages created
- [x] 1.5 `internal/archtest/deps_test.go` — written, proven to fail and recover
- [x] 1.6 Binary-size stub — built, measured, not over budget

## 1.1 — `login`/`logout` semantics

**Decision**: confirmed as proposed. `login` authenticates the operator only, exchanging credentials for a `session` token stored in the credentials file (design decision 14), never in `config.yaml`. `logout` clears that local file only and never contacts the server — it succeeds even offline. Server-side "log out everywhere" (`RevokeSubject`) remains a distinct, explicit operator action, not something `logout` triggers implicitly.

**Recorded at**: `openspec/changes/server/design.md`, Architecture Decisions table, new row 17.

## 1.2 — `goose` vs `golang-migrate`

**Decision**: confirmed as proposed. `design.md` decision 5 already documented `pressly/goose/v3` in library mode with the newer-database refusal; that row is now annotated "Confirmed as proposed (Phase 1 open item 1.2)" and its rationale sentence made explicit: the refusal behavior is ours regardless of runner, so this is an ergonomics choice (Go library API vs. golang-migrate's source-driver model), not a correctness one.

**Recorded at**: `openspec/changes/server/design.md`, decision 5.

## 1.3 — `go.mod` dependencies

Added as direct requires:

| Module | Version | Purpose |
|---|---|---|
| `modernc.org/sqlite` | v1.56.0 | Pure-Go SQLite driver (mandatory per proposal — no cgo) |
| `github.com/pressly/goose/v3` | v3.27.3 | Migration runner (decision 5) |
| `golang.org/x/crypto` | v0.55.0 | `argon2` operator password KDF (decision 8) |

**Deviation from task wording, recorded as a design update**: `sqlc` was **not** added via `go get -tool` (the Go 1.24+ `tool` directive). That was tried first and reverted: `sqlc`'s own module graph (Postgres/MySQL parsers, wazero, gRPC/protobuf stack) is unrelated to what AliasDeck ships, and requiring it in the main `go.mod` forced Go's automatic directive bump from `go 1.25.7` to `go 1.26.0` — silently exceeding the asdf-pinned `1.25.11` toolchain the project targets. `sqlc` will instead be invoked via a version-pinned `go run github.com/sqlc-dev/sqlc/cmd/sqlc@<version>` from the Makefile/CI in Phase 2 (task 2.6), which gets the same reproducibility without widening `go.sum` or the module's minimum Go version. Recorded at `design.md` decision 6.

`chi` was **not** added. Per the proposal ("`chi` only if middleware ergonomics justify it") and decision 15 (router is a `[]route{method,pattern,handler}` slice the coverage test walks), there is no code yet that needs it; the decision is deferred to Phase 5 where the router is actually built.

The `go.mod` `go` directive moved from `1.25` to `1.25.7` — a transitive minimum-version requirement from one of the three added modules, not a toolchain change (the installed `go1.25.11` already satisfies it; no toolchain download occurred). Confirmed via `go version` before/after: both report `go1.25.11 darwin/arm64` — the directive bump is metadata, not an active toolchain switch.

## 1.4 — Skeleton packages

Created (`doc.go` only, no logic, matching the constraint):

- `internal/server/doc.go`
- `internal/api/doc.go`
- `internal/auth/doc.go`
- `internal/store/doc.go`
- `internal/sync/doc.go`

Each doc comment names its future responsibility, its "MUST NOT import internal/renderers" boundary where applicable, and the phase where behavior lands, so a reader hitting the package before Phase 2–6 knows why it is empty.

## 1.5 — `internal/archtest/deps_test.go` (the task that matters most)

Wrote `internal/archtest/doc.go` + `internal/archtest/deps_test.go` with two tests:

- `TestServerPackagesNeverImportRenderers` — walks `go list <mod>/internal/{server,api,auth,store,sync}/...`, then `go list -deps` on each resolved package, and fails if `internal/renderers` (or any subpackage of it) appears.
- `TestClientPackagesNeverImportServerPersistence` — same mechanism over `internal/{source,app}/...`, failing on `internal/store` or `modernc.org/sqlite`.

Both use a local `requireGo(t)` helper that mirrors `internal/shelltest.LookPath`'s skip/fail rule (skip when `go` is absent from `PATH`, fail instead when `ALIASDECK_REQUIRE_SHELLS` is set) using `shelltest.RequireEnv` as the shared env var — written locally rather than calling `shelltest.LookPath` directly so the failure message says "go" and "architecture guard" instead of a shell-binary-specific message.

### TDD Cycle Evidence

| Step | Action | Result |
|---|---|---|
| RED (proof of aliveness) | Appended `import _ "github.com/angeltonio/aliasdeck/internal/renderers"` to `internal/store/doc.go`; ran `go test ./internal/archtest/... -run TestServerPackagesNeverImportRenderers -v` | **FAIL** — `deps_test.go:109: github.com/angeltonio/aliasdeck/internal/store depends on github.com/angeltonio/aliasdeck/internal/renderers: the server transmits data, the client produces shell syntax (design decision 2) — no server package may import internal/renderers`. Sibling packages (`api`, `auth`, `server`, `sync`) still passed — only the mutated package failed. |
| Revert | Removed the injected import from `internal/store/doc.go` | File returned to its committed skeleton state (`git status --short` shows a clean untracked-file diff, no stray import) |
| GREEN (recovery) | Re-ran the same test | **PASS** — all five packages green |
| RED (second assertion, proof of aliveness) | Added `_ "github.com/angeltonio/aliasdeck/internal/store"` to `internal/source/source.go`'s import block | **FAIL** on both `internal/source` (direct) and `internal/app` (transitive, since `internal/app` imports `internal/source`) — `deps_test.go:120: ... depends on github.com/angeltonio/aliasdeck/internal/store: internal/source and internal/app must never depend on server persistence (design decision 2)` |
| Revert | `git checkout -- internal/source/source.go` | Restored to tracked committed content |
| GREEN (recovery) | Re-ran full `internal/archtest` suite | **PASS** — both tests, all subtests |
| Env-var contract | Built a standalone test binary (`go test -c`) and ran it with `PATH=/usr/bin:/bin` (no `go`) | **SKIP** — `go is not installed on this machine` |
| Env-var contract | Same, with `ALIASDECK_REQUIRE_SHELLS=1` | **FAIL** — `go is not installed but ALIASDECK_REQUIRE_SHELLS is set: this environment promised to run the architecture guard and must not skip it` |

Exact captured output for the primary (renderers) proof:

```
=== RUN   TestServerPackagesNeverImportRenderers/github.com/angeltonio/aliasdeck/internal/store
    deps_test.go:109: github.com/angeltonio/aliasdeck/internal/store depends on github.com/angeltonio/aliasdeck/internal/renderers: the server transmits data, the client produces shell syntax (design decision 2) — no server package may import internal/renderers
--- FAIL: TestServerPackagesNeverImportRenderers (0.10s)
    --- PASS: TestServerPackagesNeverImportRenderers/github.com/angeltonio/aliasdeck/internal/api (0.01s)
    --- PASS: TestServerPackagesNeverImportRenderers/github.com/angeltonio/aliasdeck/internal/auth (0.01s)
    --- PASS: TestServerPackagesNeverImportRenderers/github.com/angeltonio/aliasdeck/internal/server (0.01s)
    --- FAIL: TestServerPackagesNeverImportRenderers/github.com/angeltonio/aliasdeck/internal/store (0.03s)
    --- PASS: TestServerPackagesNeverImportRenderers/github.com/angeltonio/aliasdeck/internal/sync (0.01s)
FAIL

[after revert]
--- PASS: TestServerPackagesNeverImportRenderers (0.08s)
    --- PASS: .../internal/api, .../internal/auth, .../internal/server, .../internal/store, .../internal/sync
PASS
```

A guard nobody has seen fail is a guard nobody knows works. This one has been seen to fail twice — once per assertion — naming the exact offending package both times, and to recover cleanly both times.

## 1.6 — Binary-size stub and measurement

Created `cmd/aliasdeck/serve.go` (registered in `cmd/aliasdeck/root.go`): a `serve` subcommand that blank-imports `modernc.org/sqlite` and prints `not implemented`. This is the actual file design decision 1 names for Phase 4; Phase 4 will replace the stub body with real `server.Run` wiring rather than create a second file.

Measured with `CGO_ENABLED=0`, `-ldflags="-s -w"` (matching `.goreleaser.yaml`), all six release targets, comparing the pre-Phase-1 tree (baseline) against the tree with the stub + `modernc.org/sqlite` (stub):

| Target | Baseline | Stub | Delta |
|---|---|---|---|
| darwin/amd64 | 3,796,592 B (3.62 MiB) | 7,572,736 B (7.22 MiB) | +3,776,144 B (3.60 MiB) |
| darwin/arm64 | 3,632,594 B (3.46 MiB) | 7,404,722 B (7.06 MiB) | +3,772,128 B (3.60 MiB) |
| linux/amd64 | 3,752,120 B (3.58 MiB) | 7,454,904 B (7.11 MiB) | +3,702,784 B (3.53 MiB) |
| linux/arm64 | 3,604,664 B (3.44 MiB) | 7,274,680 B (6.94 MiB) | +3,670,016 B (3.50 MiB) |
| windows/amd64 | 3,937,792 B (3.76 MiB) | 7,697,920 B (7.34 MiB) | +3,760,128 B (3.59 MiB) |
| windows/arm64 | 3,661,824 B (3.49 MiB) | 7,367,168 B (7.03 MiB) | +3,705,344 B (3.53 MiB) |

**Largest artifact**: `windows/amd64` stub at 7,697,920 B (7.34 MiB / 7.70 MB decimal).

**Budget check**: 25 MB budget. Using the stricter MiB interpretation (25 × 1,048,576 = 26,214,400 B): headroom = 26,214,400 − 7,697,920 = **18,516,480 B (≈17.66 MiB, ~70.6% of budget still free)**. Using the decimal interpretation (25,000,000 B): headroom = **17,302,080 B (≈17.30 MB, ~69.2% free)**.

**Not over budget — no STOP triggered.** The `modernc.org/sqlite` driver costs ~3.5–3.6 MiB per target, well inside the room the proposal reserved.

**Finding worth flagging to the maintainer (not a blocker)**: the proposal's stated "~8 MB baseline" does not match the measured pre-Phase-1 baseline of ~3.4–3.9 MiB stripped (or ~5.4 MiB unstripped for linux/amd64, checked separately). The actual baseline is smaller than planned, which only *increases* the measured headroom above — this is a favorable correction to the plan's assumption, not a risk, but the 25 MB budget language in `docs/PROJECT.md`/CI (task 10.2) should be checked against these real numbers rather than the proposal's estimate when Phase 10 wires the CI gate.

## Verification (all commands run from repo root)

```
$ go build ./... && go vet ./...
(clean, no output)

$ make ci
go vet ./...
go test -race ./...
ok  	github.com/angeltonio/aliasdeck/cmd/aliasdeck	...
?   	github.com/angeltonio/aliasdeck/internal/api	[no test files]
ok  	github.com/angeltonio/aliasdeck/internal/app	...
ok  	github.com/angeltonio/aliasdeck/internal/apply	...
ok  	github.com/angeltonio/aliasdeck/internal/archtest	...
?   	github.com/angeltonio/aliasdeck/internal/auth	[no test files]
ok  	github.com/angeltonio/aliasdeck/internal/config	...
ok  	github.com/angeltonio/aliasdeck/internal/domain	...
ok  	github.com/angeltonio/aliasdeck/internal/renderers	...
?   	github.com/angeltonio/aliasdeck/internal/server	[no test files]
?   	github.com/angeltonio/aliasdeck/internal/shelltest	[no test files]
ok  	github.com/angeltonio/aliasdeck/internal/source	...
ok  	github.com/angeltonio/aliasdeck/internal/state	...
?   	github.com/angeltonio/aliasdeck/internal/store	[no test files]
?   	github.com/angeltonio/aliasdeck/internal/sync	[no test files]
ok  	github.com/angeltonio/aliasdeck/internal/validate	...
CI checks passed
```

Six-target `CGO_ENABLED=0` cross-compilation (proof, both baseline and stub trees):

```
CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -ldflags="-s -w" -o ... ./cmd/aliasdeck   # exit 0
CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -ldflags="-s -w" -o ... ./cmd/aliasdeck   # exit 0
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -ldflags="-s -w" -o ... ./cmd/aliasdeck   # exit 0
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -ldflags="-s -w" -o ... ./cmd/aliasdeck   # exit 0
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o ... ./cmd/aliasdeck   # exit 0
CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags="-s -w" -o ... ./cmd/aliasdeck   # exit 0
```

All six succeeded for both the pre-stub and stub trees, satisfying constraint 2 (pure-Go SQLite, `CGO_ENABLED=0` still cross-compiles) with the new dependency present.

## Work Unit Evidence

| Evidence | Value |
|---|---|
| Focused test command and exact result | `go test ./internal/archtest/...` → `ok github.com/angeltonio/aliasdeck/internal/archtest` (2 tests, 7 subtests, all pass); proven to fail on both assertions via temporary forbidden imports (see 1.5 evidence table) |
| Runtime harness command/scenario and exact result | N/A — Phase 1 has no runtime behavior (per the tasks artifact's own Work Unit table). Closest analog run: `./aliasdeck serve` stub invocation prints `not implemented` and exits 0 |
| Rollback boundary | Revert `internal/{server,api,auth,store,sync}/doc.go`, `internal/archtest/{doc,deps_test}.go`, `cmd/aliasdeck/serve.go`, the `root.go` one-line registration, and the `go.mod`/`go.sum` dependency additions. Nothing outside this set imports any of it yet — Phase 2+ is the first consumer. |

## Files Changed

| File | Action | What Was Done |
|---|---|---|
| `internal/server/doc.go` | Created | Skeleton package doc comment, no logic |
| `internal/api/doc.go` | Created | Skeleton package doc comment, no logic |
| `internal/auth/doc.go` | Created | Skeleton package doc comment, no logic |
| `internal/store/doc.go` | Created | Skeleton package doc comment, no logic |
| `internal/sync/doc.go` | Created | Skeleton package doc comment, no logic |
| `internal/archtest/doc.go` | Created | Package doc comment |
| `internal/archtest/deps_test.go` | Created | Import-graph boundary guard (design decision 2), proven to fail and recover |
| `cmd/aliasdeck/serve.go` | Created | `serve` stub: prints "not implemented", blank-imports `modernc.org/sqlite` for size measurement |
| `cmd/aliasdeck/root.go` | Modified | Registered `newServeCmd()` |
| `go.mod` / `go.sum` | Modified | Added `modernc.org/sqlite`, `github.com/pressly/goose/v3`, `golang.org/x/crypto` as direct requires; `go` directive `1.25` → `1.25.7` (transitive minimum, no toolchain change) |
| `openspec/changes/server/design.md` | Modified | Decision 17 added (login/logout); decisions 5 and 6 annotated with Phase 1 confirmations/deviation |
| `openspec/changes/server/tasks.md` | Modified | Phase 1 tasks 1.1–1.6 marked `[x]` with resolution notes |

## Deviations from Design

- `sqlc` is invoked via version-pinned `go run` rather than a `go.mod` `tool` directive — see 1.3 above and `design.md` decision 6. This is a mechanism choice, not a scope change; Phase 2 (2.6) still checks in generated code and CI still fails on drift.
- No other deviations. `internal/domain`, `internal/validate`, `internal/renderers` were not touched.

## Issues Found

- The proposal's "~8 MB baseline" does not match the measured ~3.4–3.9 MiB (stripped) pre-Phase-1 binary — flagged above as a favorable correction, relevant to Phase 10's CI budget wiring.

## Remaining Tasks

- [ ] Phase 5: Server API (`internal/api`)
- [ ] Phase 6: Server Sync (`internal/sync`, `internal/api/sync.go`)
- [ ] Phase 7: ServerSource & Credentials (`internal/source`, `internal/config`)
- [ ] Phase 8: CLI Wiring (`internal/app`, `cmd/aliasdeck`)
- [ ] Phase 9: Cross-Cutting Verification
- [ ] Phase 10: Release, CI & Docs

Phases 2–4 completed in intervening apply batches; their per-task detail is
recorded directly in `tasks.md` (this file's Phase 1 section predates that
convention). This file resumes with the Phase 4 bounded-review correction
pass below.

## Phase 4 — Bounded-Review Correction Pass (Second Pass)

**Scope**: a four-lens review of the already-`[x]`-complete Phase 4
(`internal/server`, `cmd/aliasdeck/serve.go`) plus its one permitted
excursion into `internal/auth/bootstrap.go` (CRITICAL 2's explicitly
correct home), found 2 CRITICAL, 7 WARNING, and 2 SUGGESTION findings. All
were reproducible or directly actionable from design.md/spec text; none
were re-litigated. `tasks.md` 3.6 and 4.2/4.5/4.6 now carry per-finding
"Post-review correction (bounded-review, second pass)" notes at their
original task locations; this section is the batch-level summary.

### CRITICAL 1 — default bind address

`defaultServeAddr` changed from `":8080"` (wildcard) to `"127.0.0.1:8080"`
(loopback). `--addr`'s help text now states the default and names what
widening it means. Recorded as `design.md` decision 21 and a new threat
matrix row ("Default network exposure").

### CRITICAL 2 — bootstrap password vs. a persistent log

`auth.Bootstrap` gained a `passwordFilePath string` parameter: empty
preserves the original console-print behavior byte-for-byte (every
pre-existing call site passes `""`); non-empty makes it write the password
atomically at `0600` (mirroring `internal/state.Save`'s existing
temp-file-then-rename pattern — the project's second use of that pattern,
not a third) and print only the file's path. Delivery was reordered to run
*before* the operator row is created, so a failed delivery leaves no
operator behind for a clean retry. The terminal probe itself
(`github.com/mattn/go-isatty`, promoted from indirect to a direct
dependency) lives in `cmd/aliasdeck/serve.go`'s new `isTerminalWriter` and
`bootstrapPasswordFilePath`, threaded through a new
`server.Config.BootstrapPasswordFile` field — `internal/auth` performs no
terminal detection itself. Recorded as `design.md` decision 22 and a new
threat matrix row ("Bootstrap password destination").

**Deviation from a strict reading of the constraint** ("change nothing
else in `internal/auth`"): `internal/auth/bootstrap_test.go` also required
edits — every existing call site needed the new parameter added (`""` to
preserve behavior), which is unavoidable once `Bootstrap`'s exported
signature changes; the package cannot compile otherwise. No other file in
`internal/auth` (`token.go`, `password.go`, `middleware.go`, `errors.go`,
`doc.go`, or their tests) was touched. Three new tests were added to
`bootstrap_test.go` for the file-delivery path, including the
delivery-before-create failure case.

### WARNING 3 — SIGTERM ambiguity during startup

`Run` now checks `ctx.Err()` immediately after `OpenStore` or `Bootstrap`
fails, returning `nil` when the caller itself cancelled instead of
wrapping the error. A genuine failure never flips `ctx.Err()` on its own,
so this cannot mask one. Two new tests
(`TestRunReturnsNilWhenCancelledDuringOpenStore`,
`TestRunReturnsNilWhenCancelledDuringBootstrap`) exercise the seam that
already existed for this; a `blockingBootstrapStore`/`blockingOperatorRepo`
pair was added so the Bootstrap case could be driven independently of the
OpenStore case.

### WARNING 4 — `defer st.Close()` unverified

`fakeStore.isClosed()` existed but was called from nowhere.
`TestRunClosesStoreAfterReturning` now calls it, asserting `false` before
shutdown and `true` after `Run` returns.

### WARNING 5 — health body/content-type unasserted

`TestRunHealthEndpointRequiresNoAuthentication` now asserts the exact body
(`{"status":"ok"}\n`) and `Content-Type` header, not just the status code.

### WARNING 6 — 10s drain default unpinned

New `internal/server/config_test.go`:
`TestConfigWithDefaultsAppliesEveryDefault` asserts every field
`withDefaults()` produces against a zero-value `Config`, including
`ShutdownTimeout == 10s`; two sibling tests cover the explicit-override and
non-mutation properties.

### WARNING 7 — no test for `cmd/aliasdeck/serve.go`

New `cmd/aliasdeck/serve_test.go`: flag wiring (`--addr`/`--db` defaults
and help text), `resolveServeDBPath`'s precedence and idempotency against a
pre-existing directory, and both new terminal-detection helpers. None
invokes `newServeCmd()`'s `RunE` — doing so would start `server.Run`, a
long-running process, which is exactly what this correction pass exists to
avoid per the host-safety incident named in its own prompt. Every
filesystem-touching case uses `t.TempDir()` via `ALIASDECK_HOME`, matching
`main_test.go`'s existing style.

### WARNING 8 — `MkdirAll` mode mismatch

`resolveServeDBPath`'s `os.MkdirAll(base, 0o700)` did not match `init`
(`internal/app/init.go`), `config.Write` (`internal/config/device.go`), or
`state.Save` (`internal/state/state.go`) — all three use `0o755` for this
same shared base directory — and the doc comment's claim that it mirrored
`init` was false (verified: `init` uses `0o755`, not `0o700`). Changed to
`0o755` and the comment now names the three call sites it matches instead
of asserting an unverified claim. The database file itself stays `0600`
regardless (decision 19, unaffected — this was purely a directory-mode
question). `config.yaml`/`aliases.yaml`, which already share this
directory, are unaffected by the change since `0o755` is what they already
assumed.

### WARNING 9 — `Bootstrap` call uncovered inside `Run`

`fakeStore` gained `emptyOperators`/`createErr` fields and
`newFakeStoreWithEmptyOperators`/`newFakeStoreWithOperatorCreateError`
constructors, plus a `createWasCalled()` accessor backed by an
`onCreate` callback threaded through `fakeOperatorRepo.Create`.
`TestRunBootstrapsOperatorOnEmptyStore` and
`TestRunWrapsBootstrapErrorFromOperatorCreate` now drive both branches and
assert `createWasCalled()` — not just that `Run` returned the expected
result, which a weaker version of these tests would not have caught (see
mutation evidence below).

### SUGGESTION 10 — bounds table values with no stated rationale

`server.go`'s bound-constants doc comment and `design.md`'s Bounded
Operations table row both now state plainly that the five `http.Server`
values are conventional defaults, not measurements — matching
`GitTimeout`'s house style of recording either a real measurement or an
honest absence of one, per the finding's own framing.

### SUGGESTION 11 — health route only in comments

Recorded as `design.md` decision 23: `/api/v1/health` is unauthenticated
by design, and Phase 5's route slice must not remove or re-guard it —
findable from the decision table the way decisions 9 and 15 already are,
rather than only from a doc comment.

### Mutation Evidence (findings 3–6, 9 — required by the correction prompt)

Every mutation below was applied to a temporary copy, confirmed to fail the
named test, then reverted and re-verified green.

| Finding | Mutation | Result |
|---|---|---|
| 3 | Removed both `if ctx.Err() != nil { return nil }` checks in `Run` | `TestRunReturnsNilWhenCancelledDuringOpenStore` and `TestRunReturnsNilWhenCancelledDuringBootstrap` both FAIL: `Run() cancelled during OpenStore = server: opening store: context canceled, want nil`; `Run() cancelled during Bootstrap = server: bootstrapping operator: auth: counting operators: context canceled, want nil` |
| 4 | Deleted `defer st.Close()` | `TestRunClosesStoreAfterReturning` FAILs: `Run() returned without closing the store — defer st.Close() must run` |
| 5 | Removed `handleHealth`'s `Content-Type` header and added `"buildInfo":"mutated"` to its body | `TestRunHealthEndpointRequiresNoAuthentication` FAILs on both assertions: `Content-Type = "text/plain; charset=utf-8", want "application/json; charset=utf-8"` and `body = ...buildInfo...want exactly {"status":"ok"}\n` |
| 6 | Changed `defaultShutdownTimeout` from `10 * time.Second` to `3 * time.Second` | `TestConfigWithDefaultsAppliesEveryDefault` FAILs: `withDefaults().ShutdownTimeout = 3s, want 10s` |
| 9 | Removed the `auth.Bootstrap(...)` call from `Run` entirely (kept a `_ = auth.Bootstrap` reference so the package still compiles) | `TestRunBootstrapsOperatorOnEmptyStore` FAILs: `Run() returned successfully but never called Operators().Create`; `TestRunWrapsBootstrapErrorFromOperatorCreate` FAILs: `Run() = server: listening: Listen must not be called when Bootstrap fails, want an error wrapping operator create failed` |

No mutation in this table failed to produce a failure — every one had
teeth.

### Verification

- `go test -count=1 ./...`: all packages `ok` (no failures, no skips beyond
  the pre-existing `[no test files]` packages).
- `gofmt -l .`: no output (nothing unformatted).
- `go vet ./...`: no output (clean).
- Six cross-compiles, `CGO_ENABLED=0`:

  | Target | Bytes | MiB |
  |---|---|---|
  | darwin/amd64 | 17,612,288 | 16.79 |
  | darwin/arm64 | 16,862,834 | 16.08 |
  | linux/amd64 | 17,261,974 | 16.46 |
  | linux/arm64 | 16,448,655 | 15.68 |
  | windows/amd64 | 17,755,648 | 16.93 |
  | windows/arm64 | 16,633,344 | 15.86 |

  All six well under the 25 MB CI budget; `github.com/mattn/go-isatty`
  moving from indirect to direct added no meaningful size (it was already
  linked transitively).
- No fixed port was bound anywhere in this batch — every listener in every
  test (new and pre-existing) uses `"127.0.0.1:0"` or an injected `Listen`
  closure; no long-running process was started; nothing this batch touched
  or stopped any process it did not itself start.

## Workload / PR Boundary

- Mode: Feature Branch Chain slice, continuing Phase 4's boundary (PR 4 per
  tasks.md's "Suggested Work Units")
- Current work unit: Phase 4 correction — `internal/server`,
  `cmd/aliasdeck/serve.go`, plus the one permitted `internal/auth/bootstrap.go`
  excursion
- Boundary: reverting this batch means reverting
  `internal/server/{server,config,handler,server_test,shutdown_test,fakestore_test,config_test}.go`,
  `cmd/aliasdeck/serve.go` + new `cmd/aliasdeck/serve_test.go`,
  `internal/auth/{bootstrap,bootstrap_test}.go`, and the `design.md`/`tasks.md`
  doc updates — nothing outside Phase 4's own files was touched, and nothing
  in Phases 5–10 exists yet to depend on any of it
- Estimated review budget impact: moderate — primarily test additions and
  doc comments; the only new production-code surface is `Bootstrap`'s new
  parameter, `Run`'s two `ctx.Err()` checks, and `serve.go`'s two new small
  helper functions

## Status (superseded by the Phase 5 batch below for current totals)

Phases 1–4 complete (30/30 tasks per `tasks.md`: 6+9+9+6), including this
bounded-review correction pass over Phase 4. Ready for `sdd-verify`, or for
the next apply batch to start Phase 5.

## Phase 5 (first half): Server API foundation — tasks 5.1–5.6

**Batch scope**: explicitly limited to tasks 5.1–5.6 (route slice,
`http.TimeoutHandler` bound, `http.MaxBytesReader` bound, and the one error
shape). Tasks 5.7–5.14 (CRUD/auth handlers, OpenAPI, coverage test, the
login concurrency semaphore) are deliberately untouched — a second apply
batch implements them.

### Files created

| File | What |
|---|---|
| `internal/api/router.go` | `route` struct, `routes()` (currently just `GET /api/v1/health`, `Public: true`), `validKinds`, `NewRouter`/`newRouter`, `validateRoute`, `handleHealth`, `handlerTimeout` (20s), `timeoutBody` |
| `internal/api/router_test.go` | Registration-failure tests (missing/unknown `RequiredKind`), well-formed-table acceptance, `RequireKind` actually guarding a synthetic route, decision 23's health-route test against the real `routes()`, and a mutation-shaped test proving the router applies no path-based special-casing |
| `internal/api/middleware.go` | `maxBodyBytes` (1<<20), `withMaxBytes` |
| `internal/api/middleware_test.go` | `infiniteReader`-based oversized-body rejection test (bounded, no hang), plus a within-limit GREEN-path test |
| `internal/api/errors.go` | `errorBody`/`errorFields`, error code constants, `writeError`, `writeStoreError` (`ErrNotFound`→404, `ErrConflict`→409, `ErrInvalidReference`→422) |
| `internal/api/errors_test.go` | Same-shape-across-endpoints test, per-sentinel status mapping (including a wrapped error and an unknown error), and an internal-error-text-never-leaks test |

### Why the router is not yet wired into `internal/server.Run`

Design decision 23 states Phase 5's route slice must include `GET
/api/v1/health` and never re-guard it — satisfied entirely within
`internal/api`'s own tests against its own `routes()` table. Actually
replacing `internal/server/handler.go`'s Phase 4 direct wiring with
`internal/api.NewRouter` was considered and deliberately deferred: doing so
now would either (a) wire a router whose only real route is health,
duplicating Phase 4's already-tested behavior for no functional gain, or
(b) require threading `store.Tokens()` and a real clock through
`server.Config` ahead of the CRUD/auth handlers that will actually need
them — surface not assigned to this batch (5.1–5.6) and explicitly reserved
for the second pass once 5.7–5.10 exist. `internal/server/{server,handler}.go`
were not modified in this batch.

### TDD Cycle Evidence

| Task | RED | GREEN | REFACTOR |
|---|---|---|---|
| 5.1/5.2 (router) | Wrote `router_test.go` against a non-existent `router.go`; `go test ./internal/api/...` failed to build: `undefined: writeStoreError`, `undefined: withMaxBytes`, and (once errors.go/middleware.go existed) `undefined: route`, `undefined: newRouter`, `undefined: NewRouter`, `undefined: healthMethod`, `undefined: healthPattern`, `undefined: handleHealth` | Wrote `router.go`; full `internal/api` suite green | None needed — first-pass implementation matched design decisions 15/23 directly |
| 5.3/5.4 (max-bytes) | `middleware_test.go` failed to build: `undefined: withMaxBytes` | Wrote `middleware.go`; suite green | None |
| 5.5/5.6 (error shape) | `errors_test.go` failed to build: `undefined: writeStoreError`, `undefined: codeNotFound`, etc. | Wrote `errors.go`; suite green | None |

### Mutation Evidence (the four required by the correction prompt)

Every mutation below was applied directly to the production file, confirmed
to fail the named test with `go test`, then reverted and the full package
re-verified green.

| # | Mutation | Verbatim result |
|---|---|---|
| 1 | Removed a route's required-kind declaration: `validateRoute` in `router.go` changed to `func validateRoute(r route) error { return nil }` (the `fmt` import was dropped to keep the mutated file compiling, then restored on revert) | `go test ./internal/api/... -run 'TestNewRouterFailsRegistrationForRouteMissingRequiredKindDeclaration\|TestNewRouterFailsRegistrationForUnknownRequiredKind\|TestNewRouterRefusesAHealthRouteMissingItsPublicDeclaration' -v`: `--- FAIL: TestNewRouterFailsRegistrationForRouteMissingRequiredKindDeclaration` (`router_test.go:45: newRouter(...) = nil error, want a non-nil error: a route declaring neither Public nor a valid RequiredKind must fail registration`), `--- FAIL: TestNewRouterFailsRegistrationForUnknownRequiredKind` (`router_test.go:66: newRouter(...) = nil error, want a non-nil error: an unrecognized RequiredKind must fail registration exactly like a missing one`); the third test still passed (it exercises the Public/RequiredKind conflict at the health path specifically, a different assertion) |
| 2 | Deleted the `MaxBytesReader` wrapper: `withMaxBytes` in `middleware.go` reduced to `next.ServeHTTP(w, r)`, dropping the `r.Body = http.MaxBytesReader(...)` line entirely | `go test ./internal/api/... -run TestMaxBytesMiddlewareRejectsOversizedBodyBeforeFullyReadingIt -v -timeout 15s`: `--- FAIL: TestMaxBytesMiddlewareRejectsOversizedBodyBeforeFullyReadingIt (2.00s)` — `middleware_test.go:58: handler did not return within the bound — the body read against an infinite source never terminated, meaning it was not size-limited at all (removing the MaxBytesReader wrapper reproduces exactly this hang)`. The test's own 2s bound turned an unbounded hang into a deterministic failure rather than an actual hang. |
| 3 | Changed `ErrInvalidReference`'s mapping from 422 to 409: `writeStoreError` in `errors.go`'s `ErrInvalidReference` case changed to `http.StatusConflict` | `go test ./internal/api/... -run TestWriteStoreErrorMapsEachSentinelToItsOwnStatus -v`: `--- FAIL: TestWriteStoreErrorMapsEachSentinelToItsOwnStatus` — `--- FAIL: TestWriteStoreErrorMapsEachSentinelToItsOwnStatus/invalid_reference` (`errors_test.go:88: status = 409, want 422`); `not_found`, `conflict`, `wrapped_not_found`, and `unknown_error` subtests still passed, isolating the failure to exactly the mutated branch |
| 4 | Re-guarded `/api/v1/health` behind a token kind: `routes()` in `router.go` changed to `{Method: healthMethod, Pattern: healthPattern, Handler: handleHealth, RequiredKind: store.TokenKindSession}` (dropping `Public: true`) | `go test ./internal/api/... -run TestHealthRouteIsReachableWithoutAuthentication -v`: `--- FAIL: TestHealthRouteIsReachableWithoutAuthentication` — `router_test.go:139: GET /api/v1/health without an Authorization header = 401, want 200 — decision 23 requires this route stay reachable without a token` |

No mutation in this table failed to produce a failure — every one had teeth,
and every one was reverted with the full `internal/api` suite (and
`go vet`/`gofmt`) re-verified clean before moving to the next.

### 5.14 — Login concurrency semaphore placement (decided, not implemented)

**Decision**: the semaphore lives in `internal/api` (`internal/api/auth.go`,
an unexported package-level `chan struct{}` the login handler acquires
around `auth.VerifyPassword`), not `internal/auth`.

**Reasoning**: the property being bounded is concurrent *reachability* of
an unauthenticated HTTP route — the same category `http.TimeoutHandler`
(this batch's own `router.go`) already bounds — and the design's own
Bounded Operations table ties the semaphore's overflow explicitly to that
handler's 20s timeout, a relationship that stays legible only if both live
in one package. `internal/auth`'s package doc describes pure identity/crypto
primitives with zero HTTP awareness; putting an HTTP-exposure-driven limiter
there would also silently throttle `auth.Bootstrap`'s own one-time,
single-threaded `HashPassword`/`VerifyPassword` calls at startup against a
pool sized for an adversarial `POST /login` — coupling two callers with
nothing in common but the function they call. Recorded as `design.md`
decision 24; `tasks.md` 5.14's text now points at it. Not implemented in
this batch — the semaphore itself lands with 5.9/5.10 in the second pass,
per the task's own stated sequencing.

### Verification (this batch)

```
$ gofmt -l .
(no output)

$ go vet ./...
(no output)

$ go test -count=1 ./...
ok  	github.com/angeltonio/aliasdeck/cmd/aliasdeck
ok  	github.com/angeltonio/aliasdeck/internal/api
ok  	github.com/angeltonio/aliasdeck/internal/app
ok  	github.com/angeltonio/aliasdeck/internal/apply
ok  	github.com/angeltonio/aliasdeck/internal/archtest
ok  	github.com/angeltonio/aliasdeck/internal/auth
ok  	github.com/angeltonio/aliasdeck/internal/config
ok  	github.com/angeltonio/aliasdeck/internal/domain
ok  	github.com/angeltonio/aliasdeck/internal/renderers
ok  	github.com/angeltonio/aliasdeck/internal/server
?   	github.com/angeltonio/aliasdeck/internal/shelltest	[no test files]
ok  	github.com/angeltonio/aliasdeck/internal/source
ok  	github.com/angeltonio/aliasdeck/internal/state
ok  	github.com/angeltonio/aliasdeck/internal/store
ok  	github.com/angeltonio/aliasdeck/internal/store/sqlitestore
?   	github.com/angeltonio/aliasdeck/internal/store/storetest	[no test files]
?   	github.com/angeltonio/aliasdeck/internal/sync	[no test files]
ok  	github.com/angeltonio/aliasdeck/internal/validate

$ go test -count=1 ./internal/archtest
ok  	github.com/angeltonio/aliasdeck/internal/archtest
```

`go list -deps github.com/angeltonio/aliasdeck/internal/api | grep renderers`
matched nothing — `internal/api` imports no part of `internal/renderers`.

Six-target `CGO_ENABLED=0` cross-compile (`-ldflags="-s -w"`, ephemeral
scratch output paths, no fixed ports, no long-running process started):

| Target | Bytes | MiB |
|---|---|---|
| darwin/amd64 | 12,047,264 | 11.49 |
| darwin/arm64 | 11,463,634 | 10.93 |
| linux/amd64 | 11,833,528 | 11.28 |
| linux/arm64 | 11,272,376 | 10.75 |
| windows/amd64 | 12,184,064 | 11.62 |
| windows/arm64 | 11,351,552 | 10.83 |

All six well under the 25 MB CI budget. No fixed port was bound anywhere in
this batch (no runtime harness applies — the router is exercised entirely
through `httptest.NewRecorder`/`httptest.NewRequest`, never a real
listener); no long-running process was started; nothing this batch touched
or stopped any process it did not itself start.

## Work Unit Evidence (Phase 5, 5.1–5.6)

| Evidence | Value |
|---|---|
| Focused test command and exact result | `go test -count=1 ./internal/api/...` → `ok github.com/angeltonio/aliasdeck/internal/api` (17 test functions, all pass) |
| Runtime harness command/scenario and exact result | N/A — this batch has no real listener or process boundary; `internal/api`'s router is exercised entirely via `httptest.NewRecorder`, which is itself the smallest-possible in-process harness proving the same HTTP-handler behavior a real listener would. The full `httptest.NewServer` CRUD round trip named in tasks.md's Work Unit table applies once 5.7–5.10's real handlers exist, not to this foundation-only batch. |
| Rollback boundary | Revert `internal/api/{router,router_test,middleware,middleware_test,errors,errors_test}.go` and the `design.md`/`tasks.md` doc edits (decision 24, 5.1–5.6/5.14 annotations). Nothing outside `internal/api` imports any of it yet — `internal/server` was not modified, so Phase 4's tests and wiring are entirely unaffected. |

## Workload / PR Boundary (Phase 5, 5.1–5.6)

- Mode: Feature Branch Chain slice, PR 5 per tasks.md's "Suggested Work
  Units" — this batch is the first half of that unit only
- Current work unit: Phase 5 foundation — router, middleware, error shape
  (tasks 5.1–5.6); CRUD/auth handlers, OpenAPI, and the coverage test
  (5.7–5.13) plus the login semaphore implementation (5.14) remain for the
  next batch before PR 5 is complete
- Boundary: see Rollback boundary above — this slice is self-contained and
  does not require 5.7–5.14 to be reverted independently
- Estimated review budget impact: low — six new files, ~470 lines total
  including tests and doc comments, well under the 400-line budget on its
  own; the remaining Phase 5 tasks (handlers, OpenAPI, coverage test) are
  the larger remaining share of PR 5's total estimated size

## Status (superseded by the Phase 5 second-half batch below for current totals)

Phases 1–4 complete (30/30 tasks). Phase 5: 6/14 tasks complete (5.1–5.6).
Remaining: 5.7–5.14, then Phases 6–10. Ready for the next apply batch to
continue Phase 5 (5.7 onward), or for `sdd-verify` to review this slice
first per the Feature Branch Chain boundary above.

## Phase 5 (second half): Server API — tasks 5.7–5.14, plus server wiring (5.15)

**Batch scope**: tasks 5.7–5.14 (CRUD handlers for aliases/profiles/devices,
auth routes, OpenAPI document + embed + bidirectional coverage test, the
login concurrency semaphore) plus one item outside the numbered task list
that the orchestrator explicitly instructed for this batch: wiring
`internal/api.NewRouter` into `internal/server.Run`, recorded as new task
5.15 in `tasks.md`. `internal/{domain,validate,renderers,store,auth}` were
read but not modified, per the standing constraint; `internal/api` and
`internal/server` do not import `internal/renderers` (verified — see below).

### Files created

| File | What |
|---|---|
| `internal/api/aliases.go` | `serverValidationShells`, `aliasResponse` (alias + `nameWarnings`), `nameWarnings`, `validateAliasWrite` (blocks on `validate.Command`/`validate.Description`, never on `validate.Name`), the five alias handlers |
| `internal/api/aliases_test.go` | Unauthenticated-rejected (all 5 routes), authenticated create+list round trip, blocking-command-rejected, and the name-warning-never-blocks test |
| `internal/api/profiles.go` | The five profile handlers (no per-field validation beyond store-level name uniqueness — profiles carry no shell-syntax fields) |
| `internal/api/profiles_test.go` | Unauthenticated-rejected, authenticated round trip, duplicate-name 409, and a dangling-`ProfileIDs`-reference 422 test (exercised through the alias endpoint, since only aliases/devices carry a `ProfileIDs` field to dangle) |
| `internal/api/devices.go` | `deviceTokenResponse`, list/get/update/delete, `handleDevicesRevoke` (revokes the device row **and** every device-kind token via `RevokeSubject`), `handleDevicesRotateToken` (revokes-then-mints) |
| `internal/api/devices_test.go` | Unauthenticated-rejected (6 routes including revoke/rotate), a `registerTestDevice` helper driving the real registration handler (not a store shortcut), and two token-invalidation tests that assert directly against the persisted token's `RevokedAt` field (no device-kind-gated route exists yet in this batch's scope — sync is Phase 6 — so the property is asserted at the point the future route would rely on) |
| `internal/api/auth.go` | `loginConcurrency`/`verifyPassword` seam, `handleLogin` (semaphore-bounded), `handleLogout`, `handleEnrollmentTokensCreate`, `handleDevicesRegister` (consumes a bearer enrollment token via `auth.ConsumeEnrollment`), a local `bearerToken` header parser |
| `internal/api/auth_test.go` | Missing-credential rejections, login success + wrong-password/unknown-username, logout-revokes-session, the replayed-enrollment-token threat-matrix test, enrollment-mint-then-register round trip, and the login concurrency proof |
| `internal/api/openapi.go` | `openapiPattern`, `//go:embed openapi.yaml` (co-located — `go:embed` cannot escape its package directory), `handleOpenAPISpec` |
| `internal/api/openapi.yaml` | Embedded copy, byte-identical to `docs/openapi.yaml` |
| `internal/api/openapi_coverage_test.go` | `registeredRoutes`/`documentedRoutes` (filtering YAML path-item keys to actual HTTP methods, excluding `parameters`), the bidirectional coverage test, the docs/embedded drift test, and a public-reachability test for the spec route itself |
| `internal/api/json.go` | `writeJSON`, `decodeJSON` — the two functions every new handler in this batch shares |
| `internal/api/fakestore_test.go` | An in-memory `store.Store` double (`fakeStore` + five repo types) driving every handler test through the real `NewRouter` chain, plus `newFakeStoreWithOperator`/`mintSessionFor`/`mintEnrollmentToken` test helpers that call real `auth` package functions (`HashPassword`, `Mint`) rather than synthetic stand-ins |
| `docs/openapi.yaml` | Hand-written OpenAPI 3.0.3 document covering all 22 routes this batch registers |

### Files modified

| File | What changed |
|---|---|
| `internal/api/router.go` | Added the `api` struct (`store`, `now`, `loginSem`); `routes()` became `(*api).routes()` returning all 22 routes (was 1); `NewRouter`'s signature changed from `(auth.TokenLookup, func() time.Time)` to `(store.Store, func() time.Time)` — it now derives the token lookup from `st.Tokens()` itself. `newRouter` (the lowercase, table-injectable core 5.1–5.6 already wrote) is **unchanged** — every existing test calling it directly still compiles and passes verbatim. |
| `internal/api/router_test.go` | One line: `TestHealthRouteIsReachableWithoutAuthentication` now calls `NewRouter(newFakeStore(), time.Now)` instead of `NewRouter(fakeTokenLookup{}, time.Now)`, tracking `NewRouter`'s new signature. `fakeTokenLookup` itself is untouched and still used by the tests calling `newRouter` directly. |
| `internal/api/errors.go` | Appended six new error code constants (`codeInvalidBody`, `codeInvalidCommand`, `codeInvalidDescription`, `codeInvalidRequest`, `codeInvalidCredentials`, `codeInvalidToken`). `writeError`/`writeStoreError` themselves — 5.5/5.6's own functions — were not touched. |
| `internal/server/server.go` | `Run` now builds its handler via `api.NewRouter(st, time.Now)` instead of the Phase 4 stub's `newHandler()`, wrapping any registration error (there is no code path that currently produces one against the real, valid `routes()` table, but `NewRouter`'s contract allows it). |
| `internal/server/handler.go` | **Deleted.** Its `healthPath`/`handleHealth`/`newHandler` are superseded by `internal/api`'s own `healthMethod`/`healthPattern`/`handleHealth` and `(*api).routes()`'s `Public: true` entry — decision 23's constraint moves with the code that now owns it. |

### Route table (22 routes; task 5.11's `docs/openapi.yaml` documents all 22)

`GET /api/v1/health` (Public) · `GET /api/v1/openapi.yaml` (Public) ·
`POST /api/v1/auth/login` (Public) · `POST /api/v1/auth/logout` (session) ·
`POST /api/v1/enrollment-tokens` (session) · `POST /api/v1/devices/register`
(Public) · `GET|POST /api/v1/aliases` + `GET|PUT|DELETE /api/v1/aliases/{id}`
(session) · `GET|POST /api/v1/profiles` + `GET|PUT|DELETE
/api/v1/profiles/{id}` (session) · `GET /api/v1/devices` + `GET|PUT|DELETE
/api/v1/devices/{id}` + `POST /api/v1/devices/{id}/revoke` + `POST
/api/v1/devices/{id}/token` (session). There is no `POST /api/v1/devices`:
`store.DeviceRepo` has no `Create` — a device is born only through the
enrollment exchange, matching design's Interfaces section verbatim.

### Design decision 16, applied precisely

`validateAliasWrite` calls exactly two functions: `validate.Command` and
`validate.Description`, both blocking (400) on error. It never calls
`validate.Name`. `nameWarnings` — a separate function, called unconditionally
after `validateAliasWrite` passes — runs `validate.Name` once per shell in
`serverValidationShells` (`zsh`, `bash`, `powershell`, deliberately duplicated
from `renderers.Supported()` rather than importing it, per decision 2) and
collects failures as informational strings on the response body, never as a
rejection reason. `TestAliasesCreateAcceptsNameWarningAndStoresIt` uses the
name `"process"` — a PowerShell reserved word (`internal/validate/name.go`'s
`powershellReserved`) that is an ordinary bash/zsh identifier — asserting
both a 201 response carrying a `powershell`-mentioning warning and that the
alias is actually retrievable afterward via `List`.

### Design decision 24, implemented

`loginConcurrency = 4` and `loginSem chan struct{}` are unexported,
package-level/per-`*api`-instance in `internal/api/auth.go`, exactly as
decided in the first half's batch. `handleLogin` acquires the semaphore
immediately before, and releases immediately after, its call to the
package-level `verifyPassword` seam (defaulting to `auth.VerifyPassword`).
No second timeout was added anywhere — the design's explicit instruction
that overflow "queue behind the semaphore, bounded in turn by the handler's
existing `http.TimeoutHandler` 20s bound... never a second, separate
timeout" was followed literally.

### Threat-matrix and spec scenarios covered directly

- **"A replayed enrollment token is refused end-to-end"** (threat matrix,
  token handling) — `TestReplayedEnrollmentTokenIsRefusedEndToEnd`: registers
  once, replays the identical consumed wire token, asserts the second call
  does not return 201, and asserts exactly one device exists afterward (not
  merely that the second call "looked like" a failure).
- **"A second register with an already-consumed token yields no second
  device token"** — the same test's final assertion (`len(devices) != 1`)
  is precisely this.
- **"Immediate Device Revocation"** (server-auth spec) —
  `TestDevicesRevokeInvalidatesItsToken` asserts the device's token's
  `RevokedAt` is set immediately after the revoke call returns.
- **"Device Token Rotation"** (server-auth spec) —
  `TestDevicesRotateTokenInvalidatesThePreviousToken` asserts the rotated-away
  token's `RevokedAt` is set and that the new wire value differs from the old.
- **"Unauthenticated request rejected"** / **"Authenticated CRUD succeeds"**
  (server-api spec) — every `*_test.go` file's own unauthenticated-rejection
  and round-trip tests.
- **"Route coverage test catches drift"** (server-api spec) —
  `TestOpenAPIDocumentsExactlyTheRegisteredRoutes`, both directions, one test.

### Mutation Evidence (the five required by the correction prompt)

Every mutation below was applied directly to the production file, confirmed
to fail the named test with `go test`, then reverted and the full package
re-verified green (`go build ./...` after every revert).

| # | Mutation | Verbatim result |
|---|---|---|
| 1 | Removed the authentication requirement from `GET /api/v1/aliases`: changed `RequiredKind: store.TokenKindSession` to `Public: true` in `router.go`'s `(*api).routes()` | `go test -count=1 -run 'TestAliasesEndpointsRejectUnauthenticatedRequests' ./internal/api/... -v`: `--- FAIL: TestAliasesEndpointsRejectUnauthenticatedRequests` — `--- FAIL: TestAliasesEndpointsRejectUnauthenticatedRequests/GET_/api/v1/aliases` (`aliases_test.go:68: GET /api/v1/aliases without auth = 200, want 401`); the other four subtests (POST, GET/PUT/DELETE by id) still passed, isolating the failure to exactly the mutated route |
| 2 | Made a `validate.Name` warning block the request: added a loop calling `validate.Name` over `serverValidationShells` at the top of `validateAliasWrite` in `aliases.go`, returning 400 on any failure | `go test -count=1 -run 'TestAliasesCreateAcceptsNameWarningAndStoresIt' ./internal/api/... -v`: `--- FAIL: TestAliasesCreateAcceptsNameWarningAndStoresIt` — `aliases_test.go:156: POST /api/v1/aliases with a name warning (no blocking issue) = 400, want 201, body={"error":{"code":"invalid_command","message":"name \"process\" is a reserved word in powershell"}}` |
| 3 | Removed the login semaphore: deleted the `a.loginSem <- struct{}{}` / `<-a.loginSem` lines around the `verifyPassword` call in `handleLogin` (`auth.go`) | `go test -count=1 -run 'TestLoginConcurrencySemaphoreBoundsConcurrentVerifyPasswordCalls' ./internal/api/... -v`: `--- FAIL: TestLoginConcurrencySemaphoreBoundsConcurrentVerifyPasswordCalls` — `auth_test.go:253: a 5th call entered verifyPassword while 4 were already held open — the login semaphore did not bound concurrency to 4`. (The un-drained stub connections then made `httptest.Server.Close` print its 5s-interval "blocked in Close" diagnostic before the test process's own teardown completed at ~20s — an expected, bounded consequence of `t.Fatalf` unwinding the test goroutine before `close(release)` runs, not an unbounded hang: the run still terminated and reported FAIL.) |
| 4 | Added a route without documenting it: inserted `{Method: http.MethodGet, Pattern: "/api/v1/undocumented", Handler: handleHealth, Public: true}` into `(*api).routes()` | `go test -count=1 -run 'TestOpenAPIDocumentsExactlyTheRegisteredRoutes' ./internal/api/... -v`: `--- FAIL: TestOpenAPIDocumentsExactlyTheRegisteredRoutes` — `openapi_coverage_test.go:94: route GET /api/v1/undocumented is registered but not documented in docs/openapi.yaml` |
| 5 | Deleted a route while leaving it documented: removed the `DELETE /api/v1/profiles/{id}` entry from `(*api).routes()` | `go test -count=1 -run 'TestOpenAPIDocumentsExactlyTheRegisteredRoutes' ./internal/api/... -v`: `--- FAIL: TestOpenAPIDocumentsExactlyTheRegisteredRoutes` — `openapi_coverage_test.go:102: docs/openapi.yaml documents DELETE /api/v1/profiles/{id} but no such route is registered` |

No mutation in this table failed to produce a failure — every one had teeth.

### Verification (this batch)

```
$ gofmt -l .
(no output)

$ go vet ./...
(no output)

$ go test -count=1 ./...
ok  	github.com/angeltonio/aliasdeck/cmd/aliasdeck
ok  	github.com/angeltonio/aliasdeck/internal/api
ok  	github.com/angeltonio/aliasdeck/internal/app
ok  	github.com/angeltonio/aliasdeck/internal/apply
ok  	github.com/angeltonio/aliasdeck/internal/archtest
ok  	github.com/angeltonio/aliasdeck/internal/auth
ok  	github.com/angeltonio/aliasdeck/internal/config
ok  	github.com/angeltonio/aliasdeck/internal/domain
ok  	github.com/angeltonio/aliasdeck/internal/renderers
ok  	github.com/angeltonio/aliasdeck/internal/server
?   	github.com/angeltonio/aliasdeck/internal/shelltest	[no test files]
ok  	github.com/angeltonio/aliasdeck/internal/source
ok  	github.com/angeltonio/aliasdeck/internal/state
ok  	github.com/angeltonio/aliasdeck/internal/store
ok  	github.com/angeltonio/aliasdeck/internal/store/sqlitestore
?   	github.com/angeltonio/aliasdeck/internal/store/storetest	[no test files]
?   	github.com/angeltonio/aliasdeck/internal/sync	[no test files]
ok  	github.com/angeltonio/aliasdeck/internal/validate

$ go test -count=1 ./internal/archtest
ok  	github.com/angeltonio/aliasdeck/internal/archtest

$ go test -count=1 -race ./internal/api/...
ok  	github.com/angeltonio/aliasdeck/internal/api
```

`internal/server.Run` now imports `internal/api` (composition root wiring
new for this batch); `TestServerPackagesNeverImportRenderers` (`internal/archtest`)
still passed with that new edge present, confirming `internal/server` →
`internal/api` does not introduce a path to `internal/renderers`.

Six-target `CGO_ENABLED=0` cross-compile (no `-ldflags`, ephemeral scratch
output paths under `/tmp`, no fixed ports bound, no long-running process
started or stopped):

| Target | Bytes | MiB |
|---|---|---|
| darwin/amd64 | 17,898,080 | 17.07 |
| darwin/arm64 | 17,134,354 | 16.34 |
| linux/amd64 | 17,545,708 | 16.73 |
| linux/arm64 | 16,712,693 | 15.94 |
| windows/amd64 | 18,036,736 | 17.20 |
| windows/arm64 | 16,888,832 | 16.11 |

All six well under the 25 MB CI budget (worst case, windows/amd64, has
~7.8 MB / ~31% headroom remaining). Sizes grew from the 5.1–5.6 batch's
~11–12 MB (that batch built `./cmd/aliasdeck` with `-ldflags="-s -w"`; this
run intentionally omitted strip flags to report an unstripped, worst-case
size against the budget per the orchestrator's instruction — the CI gate
itself, task 10.2, is unimplemented until Phase 10 and will decide its own
flags then).

### Deliberately left out of this batch

- **A device-kind-gated route to directly HTTP-test rotation/revocation.**
  No such route exists yet (sync, `GET /api/v1/sync`, is Phase 6 and out of
  this batch's scope) — `TestDevicesRotateTokenInvalidatesThePreviousToken`
  and `TestDevicesRevokeInvalidatesItsToken` instead assert directly against
  the persisted token's `RevokedAt` field via `auth.Parse` + `ByLookup`,
  which is the exact property the future sync route's `RequireKind` check
  will rely on. Flagged rather than papered over with a synthetic route that
  would not exist in production.
- **A `codeInvalidName` error constant.** Deliberately never introduced —
  design decision 16 means an alias name issue never reaches `writeError` at
  all, so a dedicated error code for it would be dead code by construction.
- **Login timing side-channel hardening** (calling `verifyPassword` with a
  dummy hash even when the username is unknown, to make the two failure
  paths cost the same wall time). Not requested by any task or spec
  scenario in this batch's scope; `handleLogin` returns "invalid
  credentials" identically for both cases at the response-body level, but
  an unknown username currently returns faster than a wrong password
  because it skips the semaphore/KDF call entirely. Noted here rather than
  silently shipped as if it were a considered non-issue.
- **Operator-facing enrollment-token listing/revocation endpoints** (e.g.
  `GET /api/v1/enrollment-tokens`, `DELETE /api/v1/enrollment-tokens/{id}`).
  Not named by 5.9/5.10's task text or any spec scenario; `TokenRepo.Revoke`
  exists and is exercised by `handleDevicesRevoke`/`handleDevicesRotateToken`,
  but no route currently exposes revoking an *unconsumed* enrollment token
  before it is used. Flagged as a plausible gap for a future task, not
  implemented speculatively here.

## Work Unit Evidence (Phase 5, 5.7–5.15)

| Evidence | Value |
|---|---|
| Focused test command and exact result | `go test -count=1 ./internal/api/...` → `ok github.com/angeltonio/aliasdeck/internal/api` (all test functions pass, including under `-race`) |
| Runtime harness command/scenario and exact result | `httptest.NewServer` wrapping the real `NewRouter` output, used by `TestLoginConcurrencySemaphoreBoundsConcurrentVerifyPasswordCalls` for genuine concurrent network requests (not `httptest.NewRecorder`, which cannot exercise real concurrency) — result: PASS, semaphore bounds concurrency to `loginConcurrency` as designed. `go test -count=1 ./internal/server/...` (real `Run`, ephemeral listener, `TestRunHealthEndpointRequiresNoAuthentication`) → PASS, confirming the new `api.NewRouter` wiring keeps the health route reachable unauthenticated. |
| Rollback boundary | Revert every file under "Files created"/"Files modified" above, plus `docs/openapi.yaml` and this batch's `tasks.md`/`design.md`-adjacent doc edits (task 5.15's entry). `internal/server/handler.go`'s deletion and `server.go`'s wiring edit are the only changes outside `internal/api`; reverting both together restores Phase 4's stub wiring exactly, and nothing else in the tree references either. |

## Workload / PR Boundary (Phase 5, 5.7–5.15)

- Mode: Feature Branch Chain slice, PR 5 per `tasks.md`'s "Suggested Work
  Units" — this batch completes Phase 5 entirely (5.1–5.15)
- Current work unit: Phase 5 — Server API (`internal/api`), now including
  the deferred `internal/server.Run` wiring
- Boundary: see Rollback boundary above
- Estimated review budget impact: High for this batch alone (13 new files,
  6 error-code constants, a route table, ~20 new tests) — consistent with
  `tasks.md`'s own forecast that PR 5 is one of the larger units in this
  milestone's Feature Branch Chain; the maintainer already has size:exception
  context via the chain strategy rather than a single-PR budget decision

## Status

Phases 1–4 complete (30/30). Phase 5 complete (15/15, including task 5.15
added for the `internal/server.Run` wiring). Remaining: Phases 6–10
(`internal/sync`, `ServerSource`/credentials, CLI wiring, cross-cutting
verification, release/CI/docs). Ready for `sdd-verify` on this slice, or for
the next apply batch to start Phase 6.
