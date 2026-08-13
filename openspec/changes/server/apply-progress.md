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

- [ ] Phase 2: Server Persistence (`internal/store`)
- [ ] Phase 3: Server Auth (`internal/auth`)
- [ ] Phase 4: Server Runtime (`internal/server`, `cmd/aliasdeck/serve.go`)
- [ ] Phase 5: Server API (`internal/api`)
- [ ] Phase 6: Server Sync (`internal/sync`, `internal/api/sync.go`)
- [ ] Phase 7: ServerSource & Credentials (`internal/source`, `internal/config`)
- [ ] Phase 8: CLI Wiring (`internal/app`, `cmd/aliasdeck`)
- [ ] Phase 9: Cross-Cutting Verification
- [ ] Phase 10: Release, CI & Docs

## Workload / PR Boundary

- Mode: Feature Branch Chain slice (per tasks.md's "Suggested Work Units" — chain strategy itself is still `pending` at the orchestrator/delivery level; this batch stayed inside Unit 1's boundary regardless)
- Current work unit: Unit 1 — "Decisions confirmed + skeleton packages + archtest import-graph guard (Phase 1)"
- Boundary: starts from a clean `feat/server-foundation` (no prior commits) and ends with Phase 1 fully green; nothing outside this batch depends on it yet
- Estimated review budget impact: small — mostly `doc.go` files, one guard test file, one CLI stub, and `go.mod`/`go.sum`; well under the 400-line budget on its own

## Status

6/6 Phase 1 tasks complete. Ready for verify (or for the next apply batch to start Phase 2).
