# Proposal: Self-hosted server — Milestone 4 (v0.3)

## Intent

v0.2 is a complete standalone product on three operating systems, but a device's aliases can only come from a file it can already reach. `config.SourceTypeServer` exists in the schema and `resolveSource` rejects it with "not supported in this version". Milestone 4 makes it real: `aliasdeck serve` plus `ServerSource`, so aliases are edited in one place and pulled by many machines (§16, §4.2). The central boundary is unchanged and non-negotiable — **the server transmits data, the client produces shell syntax** (§3.7, §6.1). `ConfigSource.Resolve` keeps its exact signature; `ServerSource` joins `FileSource` and `GitSource` without widening it (§7).

## Scope

### In Scope

- `aliasdeck serve` — a subcommand of the existing binary; embedded migrations applied on startup; bounded timeouts and graceful shutdown
- `internal/store` — repository interfaces plus the SQLite implementation on `modernc.org/sqlite` (pure Go), with a backend conformance suite
- `internal/auth` — single operator account, opaque hashed sessions, device registration, device tokens hashed at rest, rotation and revocation (§12)
- `internal/api` — REST CRUD for aliases, profiles and devices under `/api/v1`, plus a checked-in OpenAPI document
- `internal/sync` — server-side resolution that calls `domain.Resolve`, and `GET /api/v1/sync`
- `internal/source/server.go` — `ServerSource`; `aliasdeck login` / `register`; token stored separately at `0600` (§7.3)
- Server-aware `status`, `doctor`, `list`, `edit`

### Out of Scope

- Web UI (M5) — the server must be complete and usable without it
- MCP server (M7); multi-user, RBAC, shared/team aliases (§13)
- Auto-sync, websockets, push invalidation, `ETag` (§13)
- `ChezmoiBackend` (§11); change-history audit log, diff, rollback (M6)
- A shipped PostgreSQL implementation, and a published Docker image (§9.5) — both deferred, neither foreclosed

## Capabilities

### New Capabilities

- `server-runtime`: `aliasdeck serve`, flags/env, startup migrations, bounded I/O, graceful shutdown, health endpoint, single-binary guarantee
- `server-persistence`: repository interfaces, SQLite schema and forward-only embedded migrations, pure-Go driver, backend conformance suite
- `server-auth`: operator bootstrap, sessions, device registration, hashed device tokens, rotation, revocation
- `server-api`: `/api/v1` CRUD, error shape, request bounds, OpenAPI coverage
- `server-sync`: server-side resolution reusing `domain.Resolve`, the sync response contract, and the "no shell code leaves the server" guarantee
- `server-source`: `ServerSource`, token storage, validate-on-receive, timeouts, offline behavior

### Modified Capabilities

- `config-source`: `server` becomes an implemented type; the one-source-per-device rule (§7.1) gains a third arm and still forbids fallback
- `standalone-config`: `source.type: server` fields (`url`), token file location outside `config.yaml`
- `cli-commands`: `login`, `register`, `logout`; `status`/`doctor`/`list`/`edit` under a server source
- `release-distribution`: `CGO_ENABLED=0` to six targets must still hold with the server embedded

## Approach

The client pipeline is untouched. `ServerSource.Resolve` posts the device's identity, receives a `domain.ResolvedConfig` as JSON, and hands it to the same `validate → render → apply → state` path the other two sources feed. On the server, `GET /api/v1/sync` loads the device's aliases from the store and calls the same `domain.Resolve`, so a revision computed remotely is byte-identical to one computed locally from equivalent input.

The server is a new set of packages behind one HTTP boundary: `internal/api` (routing, middleware), `internal/auth` (identity), `internal/store` (persistence), `internal/sync` (resolution). None of them is imported by the CLI's existing pipeline.

## Resolved Decisions

Settled before spec and design. Treat as inputs.

| Question | Decision | Rationale |
|----------|----------|-----------|
| How a new machine registers | **Single-use enrollment tokens.** The operator generates one on the server and supplies it to the new machine; `register` consumes it and receives a device token. Operator credentials never leave the operator's own machine. | Typing the credential that administers the whole server into every machine — including work laptops, VMs and containers — makes its blast radius the number of machines you own. A leaked enrollment token is revoked on its own; a leaked operator password is a full rotation. |
| Server alias IDs in the sync response | **Not exposed.** The client receives name, command, description and targeting — nothing it does not need. | The client renders; it has no use for a server-side identifier, and anything sent is something that has to stay compatible. |
| Binary size | **25 MB, enforced in CI.** | The pure-Go SQLite driver is multi-MB and every standalone user downloads it. A budget with room over today's ~8 MB catches a doubling without failing on ordinary growth. |
| Non-HTTPS server URLs | **Refused, with a loopback exception and an explicit opt-out flag.** | A homelab behind Tailscale or a reverse proxy on a private network is a real deployment, and a rule with no escape hatch gets worked around in worse ways. |
| Separate server binary? | **No — `aliasdeck serve` on the existing binary.** `cmd/aliasdeck-server/` is not created; PROJECT.md §10 is corrected. | §3.2, §16 and §17 all say one static binary. §10's layout predates them. |
| Who authenticates, with no UI? | **One operator account.** First start on an empty database creates it and prints a generated password once, overridable by `ALIASDECK_ADMIN_PASSWORD`. Never a stdin prompt. | Multi-user is §13. A prompt in a service that may run under systemd is the blocking-operation trap this project has already shipped three times. |
| Session format | **Opaque random tokens, hashed at rest, with expiry.** No JWT. | Revocable without a denylist, no signing-key lifecycle, no dependency. Same shape as device tokens. |
| Who owns platform and shell at sync time? | **Client owns platform and shell; server owns profile membership.** The device record is updated with what the client reports. | Shell changes locally and instantly; profile assignment is the reason the control plane exists. |
| `ServerSource` when the server is unreachable | **Hard error naming the URL. No response cache.** The already-generated file keeps working. | A cached response would be a second source of truth on the device — exactly what §7.1 forbids. `GitSource`'s cache is a user-inspectable checkout; a JSON blob is not. |
| TLS | **Server speaks HTTP; TLS terminates at a reverse proxy (documented).** `ServerSource` refuses a non-`https://` URL unless the host is loopback or the user opts out explicitly. | Delivers §12's "TLS expected in production" without a certificate lifecycle in v0.3, and makes the insecure case a deliberate act. |
| PostgreSQL in v0.3 | **Interface and conformance suite ship; only SQLite is implemented.** No SQLite type appears in the interface. | §9.3 asks for the seam, §16 asks for SQLite. Shipping an untested second backend serves neither. |
| Audit scope | **Timestamps only** (`createdAt`/`updatedAt`, `lastSeenAt`/`lastSyncAt`), per §4.2. | The change-history trail belongs with version/diff/rollback in M6. |
| `edit` under a server source | **Explicit error pointing at the API.** | There is no local `aliases.yaml` to open. Opening the wrong file silently would be worse. |

## Affected Areas

| Area | Impact |
|------|--------|
| `internal/api`, `internal/store`, `internal/auth`, `internal/sync`, `migrations/` | New — server packages and embedded SQL |
| `internal/source/server.go` | New — `ServerSource` |
| `internal/app` (`context.go`, `doctor.go`, `edit.go`, `status.go`, new `login.go`/`register.go`) | Modified — server arm in `resolveSource`, credential use cases, server-aware diagnostics |
| `cmd/aliasdeck/` | Modified — `serve`, `login`, `register`, `logout` |
| `internal/config` (`device.go`, `paths.go`) | Modified — `source.url`, token file path |
| `internal/domain`, `renderers`, `validate` | Unchanged — no golden or injection test edits expected |
| `.goreleaser.yaml`, `.github/workflows/ci.yml` | Modified — `CGO_ENABLED=0` proof, artifact size record, sqlc/OpenAPI drift checks |
| `docs/PROJECT.md` §10, `docs/API.md`, `README.md` | Modified — layout correction, API reference, status table |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Embedding the server grows the binary every standalone user downloads (`modernc.org/sqlite` is multi-MB) | High | Record artifact size in CI with an agreed budget; a build tag excluding `serve` is an additive follow-up, not a v0.3 redesign |
| An operation waits on something that never arrives — accept loop, SQLite lock, HTTP client (shipped three times already) | High | Explicit `http.Server` timeouts, context deadline on every store call, WAL plus `busy_timeout`, bounded shutdown drain, `ServerSource` client timeout, zero stdin prompts in `serve` |
| Server tests bind fixed ports, sleep, or leak goroutines | High | Ephemeral listeners (`:0`/`httptest`), `t.TempDir()` databases, `t.Cleanup` shutdown, no wall-clock assertions |
| Device token file unprotected on Windows — the `0600` gap from v0.2 | High | Separate token file, gap documented, `doctor` warns on Windows, rotation and revocation as the compensating control |
| A compromised server writes shell code onto every device | Low by design | Response is data; `ServerSource` re-runs `validate.FilterValid`; the CLI renders and escapes |
| Startup migrations damage an existing database | Medium | Forward-only, transactional, version-recorded; refuse to start on an unknown or newer schema version; release notes require a file backup before upgrade |
| OpenAPI drifts from the implementation | Medium | Document checked in; a route-coverage test fails when a route is undocumented |
| The repository interface quietly becomes SQLite-shaped | Medium | Conformance suite written against the interface; no driver types in signatures |

## Rollback Plan

Additive on every axis. `serve`, `login` and `register` are new commands, and `source.type: server` was previously a hard error, so no existing file or git device executes any new code path. Reverting the merge restores v0.2 behavior exactly and `go test ./...` returns to its baseline. Operationally, rolling back the server is stopping the process; the SQLite database is one file, and because migrations are forward-only, downgrading the binary requires restoring that file — the release notes must say so explicitly. A bad v0.3 is withdrawn by deleting the tag and reverting the tap and bucket bumps; v0.2 devices are unaffected because their config schema did not change.

## Dependencies

- `modernc.org/sqlite` (pure Go, mandatory — a cgo driver breaks the six-target release pipeline), a migration runner (`goose` or `golang-migrate`), a password KDF from `golang.org/x/crypto`, `sqlc` at build time, `chi` only if middleware ergonomics justify it (§9.3)
- No Node and no web toolchain in this milestone

## Success Criteria

- [ ] `./aliasdeck serve` in an empty directory starts, migrates, prints the one-time operator credential and serves `/api/v1` with no external service
- [ ] `login` → `register` → `sync` produces the same generated bytes as the equivalent `aliases.yaml` through `FileSource`
- [ ] No sync response contains shell syntax; a hostile alias stored server-side is dropped by the client and explained by `doctor`
- [ ] A revoked or rotated device token fails `sync` with an actionable message
- [ ] Every server operation is bounded — the suite has no sleep, no fixed port, and no timeout-dependent flake
- [ ] `CGO_ENABLED=0` cross-compilation to all six targets succeeds, with artifact size recorded
- [ ] The server suite passes on macOS, Linux and Windows runners
- [ ] The OpenAPI document covers every route
- [ ] `make check` green; new packages ≥70% coverage
