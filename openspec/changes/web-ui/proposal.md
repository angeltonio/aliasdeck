# Proposal: Web UI — Milestone 5

## Intent

v0.3 shipped a complete control plane with no way to operate it except `curl` and the CLI — and the CLI deliberately cannot do most of it. `aliasdeck edit` **refuses** to open anything under a server source and points at the API (`cli-commands` spec, "Editing aliases under a server source is refused"); no CLI command touches profiles at all; devices are manageable only through six REST routes. The operator who self-hosts AliasDeck today has to hand-write HTTP requests to change an alias they defined themselves.

This milestone closes that gap and, with it, decides the first public release: **v0.3 stays unreleased on `main`; the first tag ships with the UI.** A control plane whose only client is a `curl` invocation is not a product a stranger can adopt, and shipping it first would spend the one first impression the project gets.

Two decisions ride along because this is when they cost least. First-run setup replaces the terminal-only bootstrap password, because this milestone's target user is exactly the one who cannot read it. And `owner_id` lands on `aliases`, `profiles` and `devices` now — pointing at the single `admin` operator, with no multi-user behavior built — because migrations are forward-only and retrofitting ownership later is a backfill plus every query, route and screen.

## Scope

### In Scope

- **Stack**: Go `html/template` + htmx, served from the same binary. No Node, no bundler, no JS package manager. Assets embedded via `embed.FS`
- **Screens**: login / first-run setup · alias list with create, edit, delete · profile list with create, edit, delete · device list and detail (rename, profile membership, revoke, rotate token, delete) · add-device flow
- **Add-device flow ("copy the commands")** — a headline feature, not a nicety: the UI mints an enrollment token and shows the exact `aliasdeck register --url <url> --token <token>` line to paste on the new machine, with a copy button. Pure presentation over routes that already work
- **First-run setup**: on first start with no operator, the server serves a setup screen where the operator chooses their own password. **Loopback only** — if the bind was widened before setup completed, the server refuses to serve it and says why
- **Browser session transport**: a cookie credential accepted on UI routes only; `/api/v1` stays `Authorization: Bearer` and unchanged
- **`owner_id`** on `aliases`, `profiles`, `devices`, backfilled to the single `admin` operator by a forward-only migration
- **Backend gaps**: `RevokedAt` on the device wire representation; static-asset and page routes reconciled with `validateRoute` and the OpenAPI coverage test

### Out of Scope

- **Live rendered preview** (§4.2, §16 list it under M5) — it needs a new backend route *and* collides with design decision 2's import-graph assertion, which forbids any server package importing `internal/renderers`. That is a design reversal, not a screen
- Enrollment-token listing and revoke-before-use — mint-and-show only
- Operator-side per-device preview of resolved aliases
- Search, filtering, tags (§4.2 lists them; deferred)
- Audit trail and version history — already an M6 deferral
- **Any multi-account or account-management screen.** `owner_id` buys the option; this milestone does not spend it: no second account, no invitations, no permission checks, no UI difference

## Capabilities

### New Capabilities

- `web-ui`: server-rendered operator interface — screens, page routing and declaration rules, browser session transport and its CSRF posture, static asset serving, first-run setup screen, and the UI's own exposure guard when the bind is widened

### Modified Capabilities

- `server-auth`: "One Operator Account, Bootstrapped on First Start" changes shape — an empty database no longer generates and delivers a password; it enters a setup-pending state. `ALIASDECK_ADMIN_PASSWORD` survives as the headless path. A cookie becomes a second presentation of the same opaque session token, accepted on UI routes only
- `server-api`: the device representation gains revoked state; the route-declaration rule extends to non-API routes; OpenAPI coverage is scoped explicitly to `/api/v1`
- `server-persistence`: `owner_id` columns and their forward-only backfill migration
- `server-runtime`: `serve` gains the UI, its startup exposure check, and the setup-pending state
- `release-distribution`: the first tag ships the UI; the 25 MB budget must still hold with templates and assets embedded

## Resolved Decisions

Settled by the owner before spec and design. Treat as inputs.

| Question | Decision | Rationale |
|---|---|---|
| Does the first release include the UI? | **Yes.** v0.3 stays unreleased on `main`; the first tag ships with the UI. | A control plane operable only by hand-written HTTP is not adoptable by a stranger, and the first tag is the one first impression available. |
| Stack | **Go templates + htmx. No Node.** | The MVP's screens are forms and tables, where a SPA buys little. Node in the build, CI and contributor onboarding is a permanent tax on a project whose identity is one static binary and a Go-only toolchain (§3.2, §9.1, §16, §17). **Measured**: the released binary is 12.37 MB (windows/amd64, the largest of six targets) against a 25 MB budget — ~12.6 MB of headroom. Size does not constrain either choice; this is a maintenance decision, not a resource one. **Counter-argument, recorded honestly**: a live rendered-shell preview is the screen that would most justify revisiting this, and it is the one screen deferred here. |
| `owner_id` now or later? | **Now, on `aliases`/`profiles`/`devices`, pointing at the single `admin` operator. Multi-user is not built.** | Cheap insurance. Retrofitting ownership later means a migration with backfill plus every query, route and screen. Migrations are forward-only (design decision 5), so early is when it costs least. The owner's position on teams is "not a bad idea, keep it in mind" — this buys the option without spending it. |
| First-run credential delivery | **A loopback-only setup screen where the operator chooses their own password**, replacing decision 22's terminal-or-`0600`-file delivery for the interactive path. If the bind was widened before setup completed, the server refuses to serve the setup screen and says why. | Decision 22's two destinations — terminal stdout, or a `0600` file on the server host — are both unusable by this milestone's target user, who is not comfortable in a terminal and may be running under systemd where there is no terminal at all. A default password was rejected outright: on a network-reachable server it is compromised within hours by routine scanning. Decision 21's loopback default is what makes choose-your-own-password safe at first start. `ALIASDECK_ADMIN_PASSWORD` remains for headless and automated installs. |
| "Copy the commands" flow | **In scope, headline feature.** | It is pure presentation over `POST /api/v1/enrollment-tokens` and turns machine enrollment from a documentation problem into two clicks. |
| Browser session: cookie or storage? | **Cookie**, `HttpOnly` + `SameSite=Strict`, `Secure` whenever the connection is not loopback plaintext, scoped to the UI path and **never accepted on `/api/v1`**. | A token in `localStorage` is readable by any script that runs on the page: one XSS is a silent credential exfiltration with no revocation signal. It also cannot be attached to a request without JavaScript, which means writing the JS layer the no-Node decision exists to avoid. The cost is honest and named: a cookie is an *ambient* credential, which is precisely the property that creates CSRF — a class the Bearer-only API does not have today. Confining the cookie to UI routes keeps `/api/v1` non-ambient and confines the CSRF surface to the UI's own form posts, where `SameSite=Strict` plus a per-session double-submit token on every state-changing form is the mitigation. |
| Static assets vs. `validateRoute` and OpenAPI coverage | **UI pages and assets are a second route table with the same declaration rule, and the coverage test is scoped to `/api/v1` on both sides.** | The safety property of decision 15 is "no route becomes reachable without declaring how it is guarded" — that must cover UI routes too, so `validateRoute`'s discipline extends rather than being carved out. The *documentation* property is different: `docs/openapi.yaml` describes a machine API contract, and listing HTML pages in it would make the document lie about what it is. Scoping the bidirectional comparison to `/api/v1` keeps decision 15's guarantee exact instead of quietly weakening it to "most routes". |
| `RevokedAt` on the device wire | **Added.** | The `devices` table has `revoked_at` and `DeviceRepo.Revoke` sets it, but `domain.Device` carries no such field and `toDomainDevice` does not map it. A device list cannot show revoked state today — verified in `internal/domain/device.go` and `internal/store/sqlitestore/devices.go`. |

### Contradictions with `docs/PROJECT.md` §17 (flagged per `openspec/config.yaml`)

§9.4, §16's Milestone 5 entry and §17's **Web** row all say *Vite + React + Tailwind + shadcn/ui, embedded via `embed.FS`*. This proposal overturns the framework choice and keeps the embedding. §9.1's "loss of shared types … recovered by generating a TypeScript client from the OpenAPI spec" also lapses — there is no TypeScript client to generate. These three sections must be corrected as part of this change, not left to disagree with the shipped binary.

## Approach

A new `internal/web` package owns templates, handlers and embedded assets. It renders HTML against the same `internal/store` seam the API uses; it does **not** call `/api/v1` over HTTP from inside its own process. It joins `internal/archtest`'s denylist: no UI package may import `internal/renderers`, for the same reason no API package may (§3.7, §6.1).

Pages live under their own path prefix and their own route table, registered on the same mux, each declaring its guard exactly as `validateRoute` already demands. Login exchanges the operator password for the same opaque session token `POST /api/v1/auth/login` mints (`server-auth`: opaque, hashed, 24 h, revocable) and sets it as a cookie; the token model, its lifetime and its revocation path are unchanged. The API surface is untouched: 23 routes in, 23 routes out, same shapes, same `Authorization: Bearer`.

First-run setup is a startup state, not a screen decision: `Operators().Count() == 0` puts the server in setup-pending, where the only reachable page is the setup form and it is served only to a loopback client.

## Affected Areas

| Area | Impact | Description |
|---|---|---|
| `internal/web/` | New | Templates, page handlers, embedded assets, cookie session, CSRF token |
| `internal/api/router.go` | Modified | Second route table registered alongside `routes()`; `validateRoute` extended |
| `internal/api/openapi_coverage_test.go` | Modified | Comparison scoped to `/api/v1` on both sides, with the reason recorded in the test |
| `internal/api/devices.go` | Modified | Revoked state on the device DTO |
| `internal/domain/device.go` | Modified | `RevokedAt` field |
| `internal/store/migrations/` | New file | Forward-only `owner_id` migration with backfill |
| `internal/store/sqlitestore/`, `query.sql` | Modified | `owner_id` in queries; regenerated `sqlc` output |
| `internal/auth/bootstrap.go` | Modified | Setup-pending state; env override retained |
| `internal/server/` | Modified | UI wiring, exposure check, startup line |
| `internal/archtest/deps_test.go` | Modified | `internal/web` added to the renderer-import denylist |
| `docs/PROJECT.md` §9.1/§9.4/§16/§17, `README.md`, `docs/API.md` | Modified | Stack correction, status table, revoked device field |
| `internal/renderers`, `internal/validate` | **Unchanged** | No golden or injection test edits expected |

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| **A browser-first UI raises the pressure to widen `--addr` past loopback, and nothing protects browser traffic.** `ServerSource` enforces HTTPS-or-loopback on every sync (decision 13, re-checked per request); the browser side has no equivalent. An operator who widens the bind sends session cookies over plaintext LAN HTTP with no code-level guard | **High** | Mirror decision 13's posture on the server side: when `--addr` is not loopback, `serve` refuses to serve the UI unless the operator passes an explicit opt-out naming the assertion they are making (a TLS terminator is in front) — the same shape as `register --allow-insecure`, which exists because a rule with no escape hatch gets worked around in worse ways. Cookies carry `Secure` unless loopback. The startup line names what is exposed. **Rejected**: warning-only (nothing stops the traffic); building TLS into the server (a certificate lifecycle M4 already declined); a hard refusal with no opt-out (Tailscale and reverse-proxy deployments are real, and this rule would be defeated by disabling the guard, not by fixing the transport) |
| A cookie makes the UI CSRF-exposed — a class the Bearer-only API never had | Medium | `SameSite=Strict` plus a per-session double-submit token on every state-changing form; the cookie is rejected on `/api/v1`, so the API keeps its non-ambient-credential property intact |
| `owner_id` is dead weight if multi-user never happens | Medium | Accepted, deliberately: one nullable-then-backfilled column on three tables against a forward-only migration regime. The cost of being wrong here is three columns; the cost of being wrong the other way is a backfill plus every query, route and screen |
| The `owner_id` migration makes binary downgrade require a database restore | Medium | Already this project's stated posture (design "Migration / Rollout"); the release notes MUST say so before the upgrade, not after |
| Templates and assets push the binary past 25 MB | Low | 12.6 MB of measured headroom; HTML and htmx are kilobytes. CI's existing size gate is the check |
| The UI drifts from the API and becomes a second source of behavior | Medium | `internal/web` reads the same `internal/store` seam; validation and error semantics stay in the layers that own them, never re-implemented per screen |
| A UI route ships unguarded | Medium | `validateRoute`'s declaration rule extends to the UI table; a route declaring neither public nor a guard fails registration, exactly as today |

## Rollback Plan

Additive on the client: no CLI command, config file or device code path changes, so a v0.3-era device is unaffected either way. Reverting the merge removes `internal/web`, the second route table and the cookie path, and returns `go test ./...` to its baseline.

Operationally there is one asymmetry that must reach the release notes: the `owner_id` migration is forward-only, so a server that has started once on this version cannot be downgraded by swapping the binary back — it requires restoring the database file, exactly as design decision 5's newer-database refusal already dictates. Since this is the first public tag, there is no earlier released server to downgrade *to*; the constraint matters for operators running `main`.

Withdrawing a bad first tag is deleting the tag and reverting the tap and bucket bumps.

## Dependencies

- htmx, vendored as a static asset — not a package-manager dependency. No Node, no bundler, no JS lockfile anywhere in the build
- No new Go runtime dependency expected: `html/template`, `net/http` and `embed` are stdlib. Any addition must be justified against §9's dependency posture in design

## Success Criteria

- [ ] A fresh `./aliasdeck serve` in an empty directory, opened in a browser at the loopback default, walks an operator from setup through creating an alias without a terminal step
- [ ] Widening `--addr` before setup completes refuses to serve the setup screen and says why; widening it after setup refuses to serve the UI without an explicit opt-out
- [ ] Alias, profile and device management is complete enough that an operator never needs `curl` for the MVP screens
- [ ] The add-device flow mints a token and shows a copyable `aliasdeck register` command that succeeds unmodified on a second machine
- [ ] A revoked device is visibly revoked in the device list
- [ ] The session cookie is `HttpOnly`, `SameSite=Strict`, `Secure` off loopback, and is rejected on every `/api/v1` route
- [ ] Every UI route declares its guard; a route declaring neither fails registration
- [ ] `docs/openapi.yaml` still matches `/api/v1` bidirectionally, and still describes 23 routes plus any this milestone adds
- [ ] No Node, npm, or bundler appears in the build, CI, or contributor setup
- [ ] `CGO_ENABLED=0` to six targets, under 25 MB, with sizes recorded
- [ ] `make check` green; `internal/web` ≥70% coverage; renderer golden files and the real-shell injection test untouched
