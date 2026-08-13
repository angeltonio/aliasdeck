# Design: Self-hosted server — Milestone 4 (v0.3)

## Technical Approach

The client pipeline is unchanged. A third `ConfigSource` joins `FileSource` and `GitSource`, and a new HTTP boundary sits behind `aliasdeck serve` in the same binary.

```text
                 ┌── aliasdeck serve ─────────────────────────┐
                 │ api (router, mw, DTOs) ─→ auth ─→ store    │
                 │        └─→ sync.Resolve ─→ domain.Resolve  │  ← no renderers import
                 └──────────────────┬─────────────────────────┘
                                    │ neutral JSON + revision
   FileSource ─┐                    ▼
   GitSource  ─┼─→ ServerSource ─→ domain.Resolve ─→ revision check
               └────────────────────────┬───────────────────────
                                        ▼
                    FilterValid ─→ Render ─→ NativeBackend ─→ state.json
```

`domain.Resolve` runs on both ends. Server-side it filters the full alias set for the device; client-side it re-runs over the already-filtered wire set (all `Enabled`, no targeting), which is an identity operation whose only job is recomputing the revision. `ComputeRevision` hashes exactly platform, shell, and per-alias name/command/description — precisely the fields the wire carries — so the client can verify the server's revision byte-for-byte and hard-fail on a mismatch. That is the mechanical proof behind success criterion 2.

`internal/domain`, `internal/validate` and `internal/renderers` are read-only for this milestone.

## Architecture Decisions

| # | Decision | Choice | Rejected | Rationale |
|---|---|---|---|---|
| 1 | Package layout | `internal/server` (composition root, flags, `Run(ctx)`), `internal/api` (router, middleware, wire DTOs), `internal/auth`, `internal/store` (+`sqlitestore`, `storetest`, `migrations`), `internal/sync`. `cmd/aliasdeck/serve.go` is the only file in `main` that imports any of them. | A separate `cmd/aliasdeck-server/`; a `noserve` build tag | §3.2/§16/§17 all say one static binary; the proposal settled it. Go links by import, so `serve` costs binary size, not runtime — the 25 MB CI budget is where that cost is policed, and a build tag would double the release and test matrix to solve a problem the budget already measures. |
| 2 | Boundary enforcement | `internal/archtest/deps_test.go` shells `go list -deps` and asserts: no package under `internal/{server,api,auth,store,sync}` depends on `internal/renderers`; `internal/source` and `internal/app` do not depend on `internal/store` or `modernc.org/sqlite`. | A code-review convention | §3.7 and §6.1 are the project's central decision. A convention is not a mechanism; an import graph assertion fails the build the first time someone reaches for `renderers.Render` in a handler. |
| 3 | Repository seam | `store.Store` exposes five repos (`Aliases`, `Devices`, `Profiles`, `Tokens`, `Operators`). Every method takes `ctx` first and returns `domain` types or `store.ErrNotFound`/`ErrConflict`. No `*sql.DB`, no driver type, no SQLite dialect string in any signature. | Passing `*sql.DB` around; a single fat interface | §9.3 asks for the seam and the proposal ships SQLite only. Driver types in signatures are how an interface quietly becomes SQLite-shaped (proposal risk 7). |
| 4 | No SQL-side filtering | The store returns the full enabled alias set with its targeting; `internal/sync` calls `domain.Resolve`. Targeting is never translated into a `WHERE` clause. | Resolving with SQL joins | A second implementation of `AppliesTo` is a second set of resolution bugs and breaks byte-identity with `FileSource`. `validate.MaxAliases` already bounds the set size. |
| 5 | Migrations | `pressly/goose/v3` in library mode over `embed.FS` at `internal/store/migrations/NNNN_*.sql`, forward-only, each file in one transaction. Startup pre-flight compares `goose.GetDBVersion` against the highest embedded version and **refuses to start when the database is newer**. | golang-migrate (source drivers pull networked backends); a hand-rolled runner; `migrations/` at the repo root | Go `embed` cannot escape its package directory, so root `migrations/` is not implementable — §10 is corrected here the same way it is corrected for `cmd/aliasdeck-server/`. The newer-database refusal is ours regardless of runner: it is what makes a binary downgrade a loud failure instead of silent corruption. |
| 6 | Queries | `sqlc` generates `internal/store/sqlitestore` from `query.sql`; generated code is checked in and CI fails on `sqlc generate` diff. | An ORM; hand-written `database/sql` | §9.3. The drift check is what keeps the checked-in code honest. |
| 7 | SQLite connection policy | One `*sql.DB` with `SetMaxOpenConns(1)`; DSN pragmas `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=on`. | Separate read/write pools | Serializing every statement removes `SQLITE_BUSY` as a failure class outright. The throughput a personal control plane needs does not notice; an unbounded lock wait is exactly the trap this project has shipped three times. |
| 8 | Token model | One `tokens` table, three `kind`s. Wire form `ad<k>_<lookup>.<secret>`: `lookup` is 16 random bytes stored in plain text under a UNIQUE index, `secret` is 32 random bytes stored as `sha256` and compared with `subtle.ConstantTimeCompare`. | Per-row bcrypt scan; JWT; three tables | The index makes authentication one row lookup, never a scan and never a KDF per request. A 256-bit CSPRNG secret gains nothing from a work factor and a CPU-bound hash on every request is a DoS lever. The *operator password* is human-transported and therefore uses `argon2id` (`golang.org/x/crypto/argon2`). |
| 9 | Sync request/response | `GET /api/v1/sync?platform=&shell=` with `Authorization: Bearer <device token>`. Response: `{revision, device{id,name,platform,shell,profileIds}, aliases[{name,command,description}], generatedAt}`. Server alias IDs never appear. | `POST` with a body; marshalling `domain.ResolvedConfig` directly | Proposal fixes `GET`. Direct marshalling would leak `id`, `tags`, `platforms`, `shells`, `profileIds`, `deviceIds` and both timestamps — resolved decision 2 forbids the first and the rest is compatibility surface bought for nothing. The DTO's three fields are exactly `ComputeRevision`'s inputs, which is what makes decision 10 possible. |
| 10 | Sync writes on a GET | The handler persists the client-reported `platform`/`shell` onto the device row and stamps `last_seen_at`/`last_sync_at`. | A separate heartbeat endpoint; a `POST /sync` | The client owns platform and shell (resolved decision), so the report has to land somewhere, and a second round trip to record it doubles the failure surface of the only endpoint that matters. This is access bookkeeping, not resource mutation; an unknown platform or shell is a `400` naming the valid set, never a silent default. |
| 11 | `ServerSource` shape | `internal/source/server.go`: `*ServerSource{URL, Token, Client, AllowHTTP}` implementing `Resolve` verbatim, plus the existing optional `ResolveReporter`. `LastResolve` reports `FetchedAt` on every success and **`Stale: false` unconditionally**. | A response cache with staleness | `Stale` can never lie because a stale response can never exist — offline is a hard error naming the URL (resolved decision). `FetchedAt` still gives `status` "when did this device last reach the server". |
| 12 | Doctor under a server source | New additive optional interface `source.UnfilteredResolver { ResolveUnfiltered(ctx, dev) (domain.ResolvedConfig, error) }`, type-asserted by `Doctor` — the same pattern as `ResolveReporter` (M3 decision 14). `ServerSource` implements it; `FileSource`/`GitSource` do not, because `doctor` already re-reads their file itself. | Widening `ConfigSource.Resolve`; a second HTTP call from `doctor` | §7's signature is verbatim and shared. `doctor` needs the *unfiltered* set to explain what `FilterValid` dropped; `Resolve` returns the filtered one. This is success criterion 3's mechanism. |
| 13 | Transport guard | `source.ValidateServerURL(raw, allowHTTP)` requires `https://` unless the host is loopback (`127.0.0.0/8`, `::1`, `localhost`) or `config.yaml` carries `source.allowInsecureHTTP: true`, set only by `login --allow-insecure`. Checked at `login` **and** on every `sync`. | Warning only; no escape hatch | Resolved decision. Re-checking on every sync means editing the URL to `http://` by hand does not quietly downgrade a device that was enrolled securely. |
| 14 | Device credential storage | `<base>/credentials.json`, mode `0600`, atomic write mirroring `state.Save`, holding `{version, serverUrl, deviceId, deviceToken, obtainedAt}`. New `internal/config/credentials.go`; `CredentialsFile(base)` in `paths.go`. | Inside `config.yaml`; inside `state.json` | §7.3 says the token is stored separately and never in `config.yaml`. `state.json` is machine state users are encouraged to inspect and paste into issues; a live credential must not travel with it. |
| 15 | OpenAPI honesty | Hand-written `docs/openapi.yaml`, embedded and served at `GET /api/v1/openapi.yaml`. Routes are declared as a `[]route{method, pattern, handler}` slice that the router registers from; a coverage test asserts a **bidirectional** 1:1 match between that slice and the paths/methods parsed from the embedded YAML. | `swaggo` annotations; `oapi-codegen` handler generation | Both make the server's shape depend on a generator for six endpoint groups. The coverage test converts documentation discipline into a compile-and-test obligation — and it fails on a *removed* documented route too, which annotation generators do not. |
| 16 | Server-side write validation | `validate.Command`/`validate.Description` failures are `400`. `validate.Name` runs per shell over a locally-declared `serverValidationShells` var and returns `warnings[]` without blocking. | Importing `renderers.Supported()`; blocking on any name issue | Importing `renderers` into the server is the exact violation decision 2 forbids, so the shell list is duplicated deliberately and the cost noted. Blocking would make the server refuse a name that is legal for every device that will ever ask for it; the client is the last line of defense (§12.1), not the server. |

## Bounded Operations

Every waiting operation, and its bound. This table is a design requirement, not documentation.

| Operation | Bound | Location |
|---|---|---|
| Startup migrations | `context.WithTimeout(ctx, 30s)` around the whole run | `internal/store/migrate.go` |
| Accept / read / write | `http.Server{ReadHeaderTimeout: 5s, ReadTimeout: 15s, WriteTimeout: 30s, IdleTimeout: 60s, MaxHeaderBytes: 64<<10}` | `internal/server` |
| Handler execution | `http.TimeoutHandler(mux, 20s, …)` — every handler ctx carries a deadline; a slow store cannot pin a connection | `internal/api/router.go` |
| Request body | `http.MaxBytesReader(w, r.Body, 1<<20)` — matches the existing 1 MiB config cap | `internal/api/middleware.go` |
| Every store call | `ctx` is the first parameter of every repository method; `busy_timeout=5000` plus `SetMaxOpenConns(1)` (decision 7) | `internal/store/sqlitestore` |
| Shutdown | SIGINT/SIGTERM → `srv.Shutdown(ctx)` with a 10 s drain, then `srv.Close()` unconditionally; `Run` returns either way | `internal/server` |
| `ServerSource` request | `http.Client{Timeout: 30s}` plus the caller's ctx. **No retries** — a retry is a second unbounded thing | `internal/source/server.go` |
| Response read | `io.LimitReader(resp.Body, 1<<20)` before `json.Decode` | `internal/source/server.go` |
| Operator bootstrap | Password from `crypto/rand` or `ALIASDECK_ADMIN_PASSWORD`, printed once. **Zero stdin reads in `serve`** | `internal/auth/bootstrap.go` |
| `login` | `--password-stdin` reads a piped stream behind the existing `isInteractive` guard; never a terminal prompt | `internal/app/login.go` |

## Interfaces

```go
// internal/store — no driver types anywhere in this file.
type Store interface {
	Aliases() AliasRepo
	Devices() DeviceRepo
	Profiles() ProfileRepo
	Tokens() TokenRepo
	Operators() OperatorRepo
	Close() error
}

type TokenKind string // "session" | "enrollment" | "device"

type Token struct {
	ID, Lookup string
	Kind       TokenKind
	SubjectID  string // operator id, device id, or "" for an unconsumed enrollment
	SecretHash []byte // sha256
	ProfileIDs []string // enrollment only: what the registered device joins
	CreatedAt  time.Time
	ExpiresAt  time.Time // zero = never
	UsedAt     time.Time // enrollment only
	RevokedAt  time.Time
}

// ConsumeEnrollment is atomic: the UPDATE ... WHERE used_at IS NULL and the
// device INSERT share one transaction, so two racing register calls produce
// exactly one device.
type TokenRepo interface {
	Create(ctx context.Context, t Token) error
	ByLookup(ctx context.Context, lookup string) (Token, error)
	ConsumeEnrollment(ctx context.Context, lookup string, dev domain.Device) (domain.Device, error)
	Revoke(ctx context.Context, id string, at time.Time) error
	RevokeSubject(ctx context.Context, kind TokenKind, subjectID string, at time.Time) error
}

// internal/sync — the only server-side resolution path.
func Resolve(ctx context.Context, st store.Store, dev domain.Device) (domain.ResolvedConfig, error)

// internal/source — additive optional interface (decision 12).
type UnfilteredResolver interface {
	ResolveUnfiltered(ctx context.Context, dev domain.Device) (domain.ResolvedConfig, error)
}
```

Token lifetimes and revocation:

| Kind | Subject | Lifetime | Single use | Revocation |
|---|---|---|---|---|
| `session` | operator | 24 h `expires_at`, not sliding | no | `logout` sets `revoked_at`; `RevokeSubject` is "log out everywhere" |
| `enrollment` | none until consumed | 15 min default (`--ttl`) | **yes**, under `UPDATE … WHERE used_at IS NULL` inside the registration transaction | operator revokes before use |
| `device` | device | none | no | `revoked_at`; rotation mints new + revokes old in one transaction, returning the new token once |

An enrollment token authorizes exactly one route: `POST /api/v1/devices/register`. Operator credentials never reach the registering machine.

Error shape, closed code set: `{"error":{"code":"invalid_platform","message":"…","details":{…}}}`.

## Windows `0600` Gap

The v0.2 gap now covers a file holding a live server credential. Go's `Chmod` on Windows toggles only the read-only bit; `0600` is not an ACL. Mitigation is layered, and none of the layers is "we fixed permissions":

1. **Blast radius, by construction.** `credentials.json` holds a *device* token and nothing else — that is what the enrollment-token decision buys. A stolen device token reads one device's aliases; it cannot create aliases, devices, profiles or tokens, and it is not an operator session.
2. **Placement.** The file sits under `%USERPROFILE%\.config\aliasdeck`, whose inherited ACL is already user + administrators. The gap is "not enforced by us", not "world-readable by default".
3. **Detection.** `doctor` on Windows warns, names the file, and points at rotation. The server's per-device `last_seen_at` lets an operator spot a sync they did not perform.
4. **Response.** Revoke-and-rotate is one operator action and invalidates the stolen token immediately.
5. **Not attempted in v0.3.** DPAPI, Windows Credential Manager and an OS keychain are deferred (§12, "OS keychain later"). Stating that is part of the mitigation: a half-implemented keychain would be worse than a documented gap with a working revocation path.

## Storage Schema

`schema_migrations` (goose) · `operators(id, username UNIQUE, password_hash, created_at, updated_at)` · `profiles(id, name UNIQUE, description, created_at, updated_at)` · `aliases(id, name UNIQUE, command, description, enabled, platforms, shells, tags, created_at, updated_at)` · `alias_profiles(alias_id, profile_id)` · `alias_devices(alias_id, device_id)` · `devices(id, name, platform, shell, client_version, created_at, updated_at, last_seen_at, last_sync_at, revoked_at)` · `device_profiles(device_id, profile_id)` · `tokens(id, kind, subject_id, lookup UNIQUE, secret_hash, profile_ids, created_at, expires_at, used_at, revoked_at)`.

`platforms`/`shells`/`tags` are JSON text: they are small sets that only `domain.Resolve` reads, and normalizing them into tables would buy query shapes decision 4 forbids. Profile and device membership *are* join tables because they are the control plane's actual subject. Timestamps only, per the audit decision.

## File Changes

| File | Action | Description |
|---|---|---|
| `cmd/aliasdeck/serve.go` | Create | `serve` flags/env → `server.Run`; the only `main` file importing server packages |
| `cmd/aliasdeck/{login,register,logout}.go` | Create | Credential commands |
| `cmd/aliasdeck/root.go` | Modify | Register the four new commands |
| `internal/server/{server,config}.go` | Create | Composition root, `Run(ctx)`, signal handling, bounded shutdown |
| `internal/api/{router,middleware,errors,openapi}.go` | Create | Route slice, bounds, error shape, embedded spec |
| `internal/api/{aliases,profiles,devices,auth,sync}.go` | Create | `/api/v1` handlers and wire DTOs |
| `internal/auth/{token,password,bootstrap,middleware}.go` | Create | Minting, hashing, kind checks, first-start operator |
| `internal/store/{store,errors,migrate}.go` | Create | Interfaces, sentinel errors, goose runner + newer-DB refusal |
| `internal/store/migrations/0001_init.sql` | Create | Embedded forward-only schema |
| `internal/store/sqlitestore/*` (+ `query.sql`, `sqlc.yaml`) | Create | The only implementation |
| `internal/store/storetest/conformance.go` | Create | Backend conformance suite |
| `internal/sync/resolve.go` | Create | `domain.Resolve` on the server side |
| `internal/source/{server,url}.go` | Create | `ServerSource`, scheme guard |
| `internal/config/credentials.go` | Create | `0600` atomic credential file |
| `internal/config/{device,paths}.go` | Modify | `source.allowInsecureHTTP`; `CredentialsFile` |
| `internal/app/context.go` | Modify | `SourceTypeServer` arm in `resolveSource` |
| `internal/app/{login,register,logout}.go` | Create | Credential use cases |
| `internal/app/{doctor,status,list,edit,uninstall}.go` | Modify | Server-aware diagnosis, `edit` hard error naming the API, credential removal |
| `internal/archtest/deps_test.go` | Create | Import-graph boundary assertions (decision 2) |
| `docs/openapi.yaml`, `docs/API.md` | Create | Checked-in contract and reference |
| `docs/PROJECT.md` (§9, §10), `README.md`, `openspec/config.yaml` | Modify | Layout corrections, new deps, status table |
| `.goreleaser.yaml`, `.github/workflows/ci.yml` | Modify | `CGO_ENABLED=0` six-target proof, 25 MB size gate, sqlc/OpenAPI drift checks |
| `internal/{domain,validate,renderers}` | **Unchanged** | No golden or injection test edits expected |

## Testing Strategy

| Layer | What | Approach |
|---|---|---|
| Unit | Token mint/parse/verify; wrong secret, expired, revoked, wrong kind for the route | Table-driven, injected `now func() time.Time` — expiry is never tested by sleeping |
| Unit | `ValidateServerURL`: https, loopback http, remote http refused, opt-out accepted, unparseable, non-http scheme | Table-driven |
| Unit | `ServerSource`: revision mismatch rejected, oversize body truncated-and-failed, non-2xx mapped, offline hard error naming the URL, `Stale` always false | `httptest.NewServer` with scripted handlers |
| Unit | Credential file round-trip, `0600`, atomic-write cleanup on failure | `t.TempDir()` |
| Conformance | Repository contract: CRUD fidelity, `ErrNotFound`/`ErrConflict`, cascade behavior, deterministic list ordering, cancelled-ctx writes nothing, **two concurrent `ConsumeEnrollment` calls yield exactly one device** | `storetest.Run(t, newStore)`, driven today by `sqlitestore`, unchanged by a future backend |
| Integration | Migrations on an empty DB, re-run idempotent, refusal on a newer version | `t.TempDir()` database files |
| Integration | `serve` → `login` → enrollment token → `register` → `sync`, in-process | `httptest.Server` + `t.Cleanup` shutdown; ephemeral listeners only |
| Integration | **Byte-identity**: the same aliases through `FileSource` and through the server produce identical rendered bytes and identical revision | Shared fixture, two pipelines, one comparison |
| Integration | Revoked and rotated device tokens fail `sync` with an actionable message | Scripted store state |
| Golden | Sync and error response JSON shapes | `internal/api/testdata/*.golden` — adding an ID to the sync response becomes a visible diff |
| Contract | Route slice ↔ `openapi.yaml`, both directions | Coverage test (decision 15) |
| Architecture | Import-graph assertions | `go list -deps`, skipped when `go` is absent from PATH |
| **Unchanged** | Renderer golden files, the real bash/zsh injection test, the real `pwsh` test | **Never modified or weakened** |

No test binds a fixed port, sleeps, or asserts on wall-clock duration. New packages target ≥70 % coverage; `make check` stays green.

## Threat Matrix

| Boundary | Applicability | Design response | Planned RED test |
|---|---|---|---|
| Documentation-like paths | N/A — no file-class execution decision | — | — |
| Git repository selection / commit / push / PR commands | N/A — the server runs no VCS commands and `GitSource` is unchanged | — | — |
| Editor subprocess | N/A — `edit` is unchanged; under a server source it is a hard error before any exec | — | — |
| **HTTP routing and authentication** | Applicable | Explicit method+pattern route slice, no wildcards; every route declares a required token kind; unauthenticated is the default and must be opted out of per route | A device token must be refused on an operator route; a missing kind declaration must fail the route-registration test |
| **Sync response as hostile input** | Applicable | `ServerSource` re-runs `validate.FilterValid`; the renderer `guard` re-validates again; response body capped at 1 MiB | A hostile alias stored server-side must be dropped by the client, never written, and explained by `doctor` |
| **Token handling** | Applicable | Indexed lookup + constant-time secret compare; tokens never logged; enrollment single-use under a transactional guard; only hashes at rest | A timing-distinguishable compare must fail review; a replayed enrollment token must be refused |
| **SQL construction** | Applicable | `sqlc`-generated parameterized statements only; no string concatenation into SQL anywhere | An alias name containing `'; DROP TABLE` must round-trip as literal text |
| **Migration execution** | Applicable | Forward-only, transactional, version-recorded; refuse to start on an unknown or newer schema | A database at a version above the binary must refuse to start, not migrate down |
| **Credential file** | Applicable | Separate `0600` atomic file; Windows gap documented with revocation as the compensating control | Credentials must never appear in `config.yaml`, `state.json`, or any log line |
| **Shell code egress** | Applicable | Import-graph assertion: no server package may depend on `internal/renderers` | A handler importing `renderers` must fail `internal/archtest` |

## Migration / Rollout

Additive on every axis. `state.json` stays `version: 1`; only `SourceType` gains the value `"server"`. `source.url` already parses in `internal/config`. One forward-compatibility note that must reach the release notes: `config.yaml` parsing uses `KnownFields(true)`, so a v0.2 binary reading a config containing the new `source.allowInsecureHTTP` field fails to parse it — acceptable only because such a config also carries `type: server`, which v0.2 rejects anyway. Server rollback is stopping the process; because migrations are forward-only, downgrading the binary requires restoring the database file, and the release notes MUST say so before an upgrade, not after.

## Open Questions

None blocking. Two `docs/PROJECT.md` §10 corrections land with this change: `cmd/aliasdeck-server/` is not created (proposal decision), and root `migrations/` moves to `internal/store/migrations/` because Go `embed` cannot reference a directory outside its own package.
