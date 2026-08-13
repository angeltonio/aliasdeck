# Tasks: Self-hosted server — Milestone 4 (v0.3)

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~7,000–9,500 (authored additions+deletions; sqlc-generated store code and `docs/openapi.yaml` prose excluded from this authored count, both still included in complete diff/snapshot identity) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | 9 work units, see below |
| Delivery strategy | ask-on-risk |
| Chain strategy | pending |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

This milestone is materially larger than Milestone 2 (~2800–3200) or Milestone 3 (~2600–3400): it adds five entirely new packages (`server`, `api`, `auth`, `store`+`sqlitestore`+`storetest`+migrations, `sync`), a new client source, a credentials subsystem, four new CLI commands, an OpenAPI contract with a coverage test, and CI/release changes — none of which existed before. Repeating the M2/M3 single-branch precedent is not recommended here; the natural slicing (store → auth → api → sync → ServerSource) is largely sequential, which favors a **Feature Branch Chain** over Stacked-to-main, but the orchestrator must ask before apply per `ask-on-risk`.

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Decisions confirmed + skeleton packages + archtest import-graph guard (Phase 1) | PR 1 | `go test ./internal/archtest/...` | N/A — guard has no runtime behavior | Revert empty skeleton packages + `deps_test.go`; nothing else depends on it yet |
| 2 | Store interfaces, migrations, sqlc, SQLite backend, conformance suite (Phase 2) | PR 2 | `go test ./internal/store/...` | `t.TempDir()` SQLite migration integration test | Revert `internal/store/*`; nothing outside it imports yet |
| 3 | Tokens, password KDF, bootstrap, auth middleware (Phase 3) | PR 3 | `go test ./internal/auth/...` | N/A — pure unit tests, injected `now func()` | Revert `internal/auth/*`; only unbuilt `internal/api` would consume it |
| 4 | `internal/server` composition root + `aliasdeck serve` (Phase 4) | PR 4 | `go test ./internal/server/...` | In-process start/shutdown test, ephemeral listener | Revert `internal/server/*` + `cmd/aliasdeck/serve.go`; root command registration reverts together |
| 5 | API router, middleware, CRUD handlers, error shape, OpenAPI (Phase 5) | PR 5 | `go test ./internal/api/...` | `httptest.NewServer` CRUD round trip | Revert `internal/api/*`; `server.Run` router wiring reverts in the same commit |
| 6 | Sync resolution + `GET /api/v1/sync` (Phase 6) | PR 6 | `go test ./internal/sync/... ./internal/api/...` | `httptest` sync handler + byte-identity fixture | Revert `internal/sync/*` and `internal/api/sync.go`; other routes unaffected |
| 7 | `ServerSource`, URL guard, credentials file, `Doctor`/`UnfilteredResolver` (Phase 7) | PR 7 | `go test ./internal/source/... ./internal/config/...` | `httptest.NewServer` scripted `ServerSource` integration | Revert `internal/source/{server,url}.go`, `internal/config/credentials.go`; `FileSource`/`GitSource` unaffected |
| 8 | `login`/`register`/`logout`, server-aware `status`/`doctor`/`edit`/`uninstall` (Phase 8) | PR 8 | `go test ./internal/app/...` | Full in-process serve→login→register→sync integration test | Revert `internal/app/{login,register,logout}.go` and server-aware branches; `resolveSource`'s server arm reverts with it |
| 9 | Byte-identity/full verification, archtest sweep, 25 MB CI gate, docs (Phases 9–10) | PR 9 | `make check && make cover` | CI six-target build + size measurement (N/A locally) | Revert CI/goreleaser/docs changes; no Go behavior depends on them |

Base ordering if Feature Branch Chain is chosen: PR1 → PR2 → PR3 → PR4 → PR5 → PR6 → PR7 → PR8 → PR9, each based off the immediately preceding PR branch. PR7's URL/credentials half (7.1–7.4) has no server-side dependency and MAY be developed off PR1 in parallel, then rebased onto PR6 before its ServerSource half (7.5–7.9) lands. If Stacked-to-main is chosen, land in the same order directly to main — each layer's tests only need earlier layers, never later ones.

## Confirmed No-Change (per design.md and orchestrator instructions)

- `internal/domain`, `internal/validate`, `internal/renderers`: no edits planned. Flag loudly if any task below is found to require one — the PowerShell case-insensitive name/duplicate scenarios in the `config-source` delta already exist from Milestone 3 (`internal/validate/name.go:83-91`, `validate.go:180-186`); this milestone only extends the *rule* to cover `ServerSource` output, not the validation code itself.

## Phase 1: Open Decisions & Foundation Skeleton

- [x] 1.1 **Open item**: confirm `login`/`logout` semantics before Phase 8 — `login` authenticates the operator only (session stored outside `config.yaml`), `logout` clears the local session only and never contacts the server. Record the decision (or a correction) in `design.md` before Phase 8 starts. **Confirmed as proposed** — recorded as design decision 17.
- [x] 1.2 **Open item**: confirm `goose` vs `golang-migrate` before Phase 2 — record the choice in `design.md`; note that the refusal-to-run-against-a-newer-database behavior is ours regardless of runner, so this choice does not gate correctness, only ergonomics. **Confirmed as proposed** — decision 5 annotated.
- [x] 1.3 Add go.mod deps per 1.2: `modernc.org/sqlite`, the chosen migration runner, `golang.org/x/crypto` (argon2), `sqlc` as a build-time tool; `chi` only if middleware ergonomics justify it. Added `modernc.org/sqlite`, `github.com/pressly/goose/v3`, `golang.org/x/crypto` as direct `go.mod` requires. `sqlc` is invoked via version-pinned `go run` (not a `go.mod` `tool` directive — see design decision 6's note on why). `chi` deferred to Phase 5, not added.
- [x] 1.4 Create empty skeleton packages `internal/{server,api,auth,store,sync}/doc.go` (no logic) so the import graph exists to test.
- [x] 1.5 Write `internal/archtest/deps_test.go` **now** (design decision 2): `go list -deps` assertions — no `internal/{server,api,auth,store,sync}` package depends on `internal/renderers`; `internal/source`/`internal/app` never depend on `internal/store` or `modernc.org/sqlite`; skip when `go` is absent from PATH. Must stay green from this commit on — every later phase is graded against it. Failure and recovery proven twice (both assertions) — see apply-progress.
- [x] 1.6 **Open item**: build a minimal `aliasdeck serve` stub (prints "not implemented", imports `modernc.org/sqlite` only); measure binary size vs the ~8 MB baseline and the 25 MB budget; record the delta. If already over budget at this stub stage, escalate to the maintainer before continuing. **Not over budget** — see apply-progress for measured baseline/stub/delta/headroom.

## Phase 2: Server Persistence (`internal/store`)

- [x] 2.1 Write `internal/store/{store,errors}.go` — `Store`, `AliasRepo`, `DeviceRepo`, `ProfileRepo`, `TokenRepo`, `OperatorRepo`; `ErrNotFound`/`ErrConflict`; no driver type in any signature.
- [x] 2.2 RED: `internal/store/storetest/conformance.go` — `Run(t, newStore)`: CRUD fidelity, `ErrNotFound`/`ErrConflict`, cascade behavior, deterministic list ordering, cancelled-ctx writes nothing, two concurrent `ConsumeEnrollment` calls yield exactly one device. Also includes the 2.8 SQL-metacharacter round-trip case. Unconsumed until 2.7 wires a real backend — see apply-progress for how its RED→GREEN transition was actually exercised.
- [x] 2.3 RED: `internal/store/migrate_test.go` — empty-DB migration, idempotent re-run, refusal to start when the DB schema is newer than the binary (threat matrix: migration execution), plus a transactional-rollback-on-failure case (spec scenario, not separately listed as a task but required by server-persistence spec.md). `t.TempDir()` database files, no sleep.
- [x] 2.4 GREEN: `internal/store/migrations/0001_init.sql` — `schema_migrations` (goose-managed version table, custom name via `goose.WithTableName`), `operators`, `profiles`, `aliases`, `alias_profiles`, `alias_devices`, `devices`, `device_profiles`, `tokens`.
- [x] 2.5 GREEN: `internal/store/migrate.go` — runner over `embed.FS` via `goose.NewProvider` (library mode, decision 5), one transaction per file (goose default), `context.WithTimeout(ctx, 30s)` around the whole run (bounded op), newer-DB refusal via `ErrSchemaNewer`.
- [x] 2.6 Write `internal/store/sqlitestore/query.sql` + `sqlc.yaml`; ran `sqlc generate` via `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.29.0 generate`; generated code checked in (CI drift check wiring is task 10.3, out of this batch's scope).
- [x] 2.7 GREEN: `internal/store/sqlitestore/*.go` — one `*sql.DB`, `SetMaxOpenConns(1)`, `journal_mode=WAL`/`busy_timeout=5000`/`foreign_keys=on` via DSN pragmas (bounded op); `ctx` first param on every method; wired `storetest.Run(t, newStore)` in `sqlitestore_test.go`.
- [x] 2.8 RED+GREEN: threat matrix (SQL construction) — `testAliasNameWithSQLMetacharactersRoundTrips` in `storetest/conformance.go`: an alias name containing `'; DROP TABLE` round-trips as literal text through parameterized queries. Proven to fail against a deliberately concatenated (injection-vulnerable) write path — see apply-progress.
- [x] 2.9 Verify: `go test ./internal/store/...` green; `deps_test.go` (1.5) still green (including a deliberately-broken-store proof, not just the passing run).

## Phase 3: Server Auth (`internal/auth`)

- [ ] 3.1 RED: `internal/auth/token_test.go` — mint/parse/verify, injected `now func()`; wrong secret, expired, revoked, wrong kind for the route.
- [ ] 3.2 GREEN: `internal/auth/token.go` — wire form `ad<k>_<lookup>.<secret>`; `lookup` UNIQUE-indexed plain text, `secret` sha256-hashed, `subtle.ConstantTimeCompare` (threat matrix: token handling).
- [ ] 3.3 RED: `internal/auth/password_test.go` — argon2id hash/verify round trip, wrong password rejected.
- [ ] 3.4 GREEN: `internal/auth/password.go` — `golang.org/x/crypto/argon2` for the operator password only.
- [ ] 3.5 RED: `internal/auth/bootstrap_test.go` — first start on an empty DB creates one operator and prints a generated password once, never logged; `ALIASDECK_ADMIN_PASSWORD` honored; subsequent starts don't reprint (bounded op: zero stdin reads).
- [ ] 3.6 GREEN: `internal/auth/bootstrap.go` — `crypto/rand` password generation, zero stdin reads.
- [ ] 3.7 RED: `internal/auth/middleware_test.go` — per-route required token kind enforced; a device token on an operator route is refused (threat matrix: HTTP routing); expired/revoked tokens rejected.
- [ ] 3.8 GREEN: `internal/auth/middleware.go`.
- [ ] 3.9 RED+GREEN: threat matrix (token handling) — a replayed enrollment token is refused end-to-end; a second `register` with an already-consumed token yields no second device token.

## Phase 4: Server Runtime (`internal/server`, `cmd/aliasdeck/serve.go`)

- [ ] 4.1 RED: `internal/server/server_test.go` — `Run(ctx)` migrates before accepting connections; refuses on a newer schema; health endpoint requires no auth; shutdown drains an in-flight request within bound then exits.
- [ ] 4.2 GREEN: `internal/server/{server,config}.go` — `http.Server{ReadHeaderTimeout:5s, ReadTimeout:15s, WriteTimeout:30s, IdleTimeout:60s, MaxHeaderBytes:64<<10}` (bounded op); SIGINT/SIGTERM → `srv.Shutdown(ctx)` 10s drain, then `srv.Close()` unconditionally (bounded op).
- [ ] 4.3 RED: zero-stdin-prompt test — `serve` with stdin closed (as under systemd) completes startup.
- [ ] 4.4 GREEN: confirm via bootstrap wiring from Phase 3; no interactive read anywhere in `Run`.
- [ ] 4.5 Create `cmd/aliasdeck/serve.go` — flags/env → `server.Run`; the only `main` file importing server packages; register in `cmd/aliasdeck/root.go`.
- [ ] 4.6 Verify: `deps_test.go` (1.5) and `go vet` still green with real server wiring present.

## Phase 5: Server API (`internal/api`)

- [ ] 5.1 RED: `internal/api/router_test.go` — explicit `[]route{method,pattern,handler}` slice; a route missing its required-token-kind declaration fails registration (threat matrix: HTTP routing).
- [ ] 5.2 GREEN: `internal/api/router.go` — route slice, `http.TimeoutHandler(mux, 20s, ...)` (bounded op).
- [ ] 5.3 RED: `internal/api/middleware_test.go` — `http.MaxBytesReader(w, r.Body, 1<<20)` rejects an oversized body before it is fully read (bounded op).
- [ ] 5.4 GREEN: `internal/api/middleware.go`.
- [ ] 5.5 RED: `internal/api/errors_test.go` — one `{"error":{"code","message","details"}}` shape across endpoints.
- [ ] 5.6 GREEN: `internal/api/errors.go`.
- [ ] 5.7 RED: `internal/api/{aliases,profiles,devices}_test.go` — unauthenticated CRUD rejected; authenticated create+list round trip; `validate.Command`/`Description` failures are `400`; `validate.Name` warnings over `serverValidationShells` never block (design decision 16).
- [ ] 5.8 GREEN: `internal/api/{aliases,profiles,devices}.go`.
- [ ] 5.9 RED: `internal/api/auth_test.go` — enrollment-token generation/consumption, login/logout, device token rotation/revocation.
- [ ] 5.10 GREEN: `internal/api/auth.go`.
- [ ] 5.11 Write `docs/openapi.yaml` covering every declared route.
- [ ] 5.12 GREEN: `internal/api/openapi.go` — embed and serve `GET /api/v1/openapi.yaml`.
- [ ] 5.13 RED+GREEN: `internal/api/openapi_coverage_test.go` — bidirectional 1:1 match between the route slice and `openapi.yaml`; an undocumented added route and a removed-but-still-documented route both fail, naming it.

## Phase 6: Server Sync (`internal/sync`, `internal/api/sync.go`)

- [ ] 6.1 RED: `internal/sync/resolve_test.go` — `Resolve(ctx, store, dev)` filters identically to local `FileSource` resolution on a shared fixture (no SQL-side filtering, design decision 4).
- [ ] 6.2 GREEN: `internal/sync/resolve.go` — load the full enabled alias set + targeting from the store, call `domain.Resolve`.
- [ ] 6.3 RED: `internal/api/sync_test.go` — response `{revision, device{id,name,platform,shell,profileIds}, aliases[{name,command,description}], generatedAt}`; no server-side alias ID anywhere (threat matrix: sync response contract).
- [ ] 6.4 GREEN: `internal/api/sync.go` — `GET /api/v1/sync` handler; persists client-reported platform/shell + `last_seen_at`/`last_sync_at` on the same GET (design decision 10); unknown platform/shell is `400` naming the valid set.
- [ ] 6.5 Golden: `internal/api/testdata/{sync_response,error_response}.golden`.
- [ ] 6.6 RED+GREEN: revoked or rotated device token fails sync with an actionable message (scripted store state).

## Phase 7: ServerSource & Credentials (`internal/source`, `internal/config`)

- [ ] 7.1 RED: `internal/source/url_test.go` — `ValidateServerURL`: https accepted, loopback http accepted, remote http refused, opt-out accepted, unparseable/non-http scheme rejected.
- [ ] 7.2 GREEN: `internal/source/url.go` — `ValidateServerURL(raw, allowHTTP)` (design decision 13); checked at `login` and re-checked on every `sync`.
- [ ] 7.3 RED: `internal/config/credentials_test.go` — round trip, `0600`, atomic-write cleanup on failure (`t.TempDir()`).
- [ ] 7.4 GREEN: `internal/config/credentials.go` + `CredentialsFile(base)` in `paths.go` (design decision 14).
- [ ] 7.5 RED: `internal/source/server_test.go` — `httptest.NewServer` scripted: revision mismatch rejected; oversize body truncated-and-failed via `io.LimitReader(resp.Body, 1<<20)` (bounded op); non-2xx mapped; offline hard error naming the URL, no cache; `Stale` always false; `http.Client{Timeout:30s}`, no retries (bounded op).
- [ ] 7.6 GREEN: `internal/source/server.go` — `ServerSource{URL,Token,Client,AllowHTTP}` implementing `Resolve` verbatim + optional `ResolveReporter`.
- [ ] 7.7 RED+GREEN: threat matrix (sync response as hostile input, client side) — a hostile server-stored alias is dropped by `validate.FilterValid` identically to `FileSource`/`GitSource`.
- [ ] 7.8 RED: `internal/app/doctor_test.go` (server-source case) — `Doctor` explains exactly what `FilterValid` dropped, using the unfiltered alias set.
- [ ] 7.9 GREEN (same pair as 7.8, design decision 12): add `source.UnfilteredResolver{ ResolveUnfiltered(ctx, dev) (domain.ResolvedConfig, error) }`; `ServerSource` implements it; `internal/app/doctor.go` type-asserts it, mirroring `ResolveReporter` (M3 decision 14). Do not land this interface without this `Doctor` use.

## Phase 8: CLI Wiring (`internal/app`, `cmd/aliasdeck`)

- [ ] 8.1 GREEN: `internal/app/context.go` `resolveSource` — add `config.SourceTypeServer` building `*source.ServerSource` from `devCfg.Source.URL`, the credentials file, and `source.allowInsecureHTTP`.
- [ ] 8.2 RED: `internal/app/login_test.go` — per 1.1: authenticates against the server, stores the session outside `config.yaml`/device-token file; `--password-stdin` behind `isInteractive` (bounded op), never a terminal prompt; incorrect password rejected.
- [ ] 8.3 GREEN: `internal/app/login.go` + `cmd/aliasdeck/login.go`.
- [ ] 8.4 RED: `internal/app/register_test.go` — enrollment token exchanged for a device token stored separately at `0600`; `config.yaml`'s `source.type` becomes `server`; invalid/consumed token leaves it unchanged, exits non-zero.
- [ ] 8.5 GREEN: `internal/app/register.go` + `cmd/aliasdeck/register.go`.
- [ ] 8.6 RED: `internal/app/logout_test.go` — per 1.1: removes the local session only; succeeds even when the server is unreachable.
- [ ] 8.7 GREEN: `internal/app/logout.go` + `cmd/aliasdeck/logout.go`.
- [ ] 8.8 RED: `internal/app/status_test.go` (server case) — reports `ServerSource` + URL; token value never appears in output.
- [ ] 8.9 GREEN: extend `internal/app/status.go`.
- [ ] 8.10 RED: `internal/app/edit_test.go` (server case) — `edit` fails with an explicit error pointing at the API, no file opened; `edit --config` still opens `config.yaml`.
- [ ] 8.11 GREEN: extend `internal/app/edit.go`.
- [ ] 8.12 RED: `internal/app/uninstall_test.go` (server case) — `uninstall` removes the credentials file alongside existing cleanup.
- [ ] 8.13 GREEN: extend `internal/app/uninstall.go`.
- [ ] 8.14 Register `serve`/`login`/`register`/`logout` in `cmd/aliasdeck/root.go` (consolidates 4.5/8.3/8.5/8.7).

## Phase 9: Cross-Cutting Verification

- [ ] 9.1 RED+GREEN: byte-identity integration test — the same aliases through `FileSource` and through serve→sync produce identical rendered bytes and identical `ComputeRevision` (proposal success criterion 2).
- [ ] 9.2 RED+GREEN: full in-process integration test — serve → login → enrollment token → register → sync, `httptest.Server` + `t.Cleanup` shutdown, ephemeral listeners only, no sleep.
- [ ] 9.3 Verify: `internal/archtest/deps_test.go` still passes against the complete implementation.
- [ ] 9.4 Verify: renderer golden files, real bash/zsh injection test, real `pwsh` test are byte-for-byte untouched and still green — **flag immediately if any prior task touched them**; none is expected to.
- [ ] 9.5 Verify: `go test ./...` and `make cover` — new packages ≥70% coverage; `make check` green.

## Phase 10: Release, CI & Docs

- [ ] 10.1 `.goreleaser.yaml` — confirm the six darwin/linux/windows amd64/arm64 targets still build `CGO_ENABLED=0` with the embedded server + `modernc.org/sqlite`.
- [ ] 10.2 `.github/workflows/ci.yml` — wire the 25 MB binary-size gate: measure each built artifact, fail naming the artifact and size if exceeded. **Over-budget contingency**: exclude `serve` via a build tag as an additive follow-up (design decision 1) — not a v0.3 redesign — flagged to the maintainer before release, never silently patched.
- [ ] 10.3 `.github/workflows/ci.yml` — wire `sqlc generate` drift check (2.6) and the OpenAPI route-coverage check (5.13) as CI jobs.
- [ ] 10.4 `docs/PROJECT.md` §9, §10 — correct: `cmd/aliasdeck-server/` not created; root `migrations/` moves to `internal/store/migrations/`; record new deps.
- [ ] 10.5 `docs/API.md` — write the API reference from `docs/openapi.yaml`.
- [ ] 10.6 `README.md` — update the status table for server support.
- [ ] 10.7 `openspec/config.yaml` — note new deps/packages under `context`.
- [ ] 10.8 Release notes — state that migrations are forward-only and a binary downgrade requires restoring the database file backed up before upgrading.
- [ ] 10.9 Final `make check && make cover`; manual smoke: `./aliasdeck serve` in an empty directory starts, migrates, prints the one-time operator credential, serves `/api/v1` with no external service.

## Parallelization

- Phase 1 must land first in full (skeleton + `deps_test.go`); everything else is graded against it.
- Phase 7.1–7.4 (URL guard + credentials file) have no server-side dependency and MAY start immediately after Phase 1, in parallel with Phase 2.
- Phase 3 can start once 2.1's interfaces exist (a fake `Store` suffices for auth's own tests), in parallel with 2.2–2.9's SQLite/migration work.
- Phase 4's skeleton (4.1–4.4) can proceed in parallel with Phase 3; its full wiring (4.6) waits on Phase 5.
- Phase 5 needs Phase 2 (store) and Phase 3 (auth middleware) merged.
- Phase 6 needs Phase 2; can run parallel to Phase 5's non-sync handlers, but 6.3–6.6 need Phase 5's router.
- Phase 7.5–7.9 need Phase 6's response DTO shape.
- Phase 8 needs Phases 3, 6, and 7.
- Phases 9 and 10 are strictly last.
