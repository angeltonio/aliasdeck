# AliasDeck Server API

This is a human-readable reference for the `/api/v1` control-plane surface
exposed by `aliasdeck-server`. It is written from [`docs/openapi.yaml`](openapi.yaml),
the source of truth: that document is embedded in the server binary and
served live at `GET /api/v1/openapi.yaml`, and a bidirectional coverage test
(`internal/api/openapi_coverage_test.go`) fails the build if the router's own
route table and `docs/openapi.yaml` ever disagree, in either direction. If
this page and the served spec conflict, the served spec is right — please
open an issue.

23 routes in total: 2 unauthenticated (health, the spec document itself), 4
auth/enrollment routes, 5 alias routes, 5 profile routes, 6 device routes,
and one device-gated sync route.

## Base URL

Everything below is relative to `/api/v1` on whatever address `aliasdeck
serve --addr` was given. The default is `127.0.0.1:8080` — loopback only.
TLS is not built in; put a reverse proxy in front for anything reachable
beyond loopback.

## Authentication

Every route except `GET /health` and `GET /openapi.yaml` requires an
`Authorization: Bearer <token>` header. There are three kinds of token, each
accepted only on the routes that declare it:

| Kind | Wire prefix | Minted by | Accepted on |
|---|---|---|---|
| `session` | `ads_` | `POST /auth/login` | Every alias/profile/device/enrollment-token route, `POST /auth/logout` |
| `enrollment` | `ade_` | `POST /enrollment-tokens` | `POST /devices/register` only, and only once — it is consumed atomically on success |
| `device` | `add_` | `POST /devices/register`, `POST /devices/{id}/token` | `GET /sync` only |

Presenting the wrong kind of token for a route (e.g. a device token on
`GET /aliases`) is refused exactly like a missing one. `GET /sync`'s 401
names the one recovery action a device has — re-register — rather than the
word "unauthorized"; every other route's 401 is a uniform, non-enumerable
message that does not distinguish "no such token" from "expired" from
"revoked".

There is exactly one operator account, fixed at the username `admin`; v0.3
has no multi-operator support and no `--username` flag.

## Error shape

Every error response, on every route, uses one shape:

```json
{
  "error": {
    "code": "not_found",
    "message": "human-readable explanation",
    "details": {}
  }
}
```

`details` is optional and omitted when empty. Internal error text never
appears in `message` — only actionable, closed-vocabulary explanations do.

| Status | Meaning |
|---|---|
| `400` | The request body or a query parameter failed validation (includes an unknown/missing `platform` or `shell` on `GET /sync`, and `validate.Command`/`validate.Description` failures on alias writes) |
| `401` | No valid token was presented for a route that requires one, or the token's kind does not match |
| `404` | No resource with that id exists |
| `409` | The write collides with an existing resource's name |
| `422` | The write names another resource (a profile or device id) that does not exist — a dangling reference, distinct from a name collision |

Alias/profile writes may also return `nameWarnings` in a `200`/`201` body
(never as an error): shell-name issues that `validate.Name` flags but does
not block, since the server checks a fixed reference shell list, not every
shell a future client might add.

## Health and the spec document

| Method | Path | Auth |
|---|---|---|
| `GET` | `/health` | none |
| `GET` | `/openapi.yaml` | none |

`GET /health` returns `{"status":"ok"}` and nothing else — no schema
version, no build metadata, no operator or device identity. `GET
/openapi.yaml` returns this document's own machine-readable source, exactly
as embedded in the binary.

## Auth and enrollment

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `POST` | `/auth/login` | none | Exchange the operator's username/password for a 24-hour session token |
| `POST` | `/auth/logout` | session | Revoke the calling session token |
| `POST` | `/enrollment-tokens` | session | Mint a single-use enrollment token (default 15-minute TTL), optionally naming the profiles a device joins |
| `POST` | `/devices/register` | enrollment | Exchange a single-use enrollment token for a device token; the enrollment token is consumed atomically — a replay is refused and mints no second device |

`POST /auth/login`'s request body is `{"username", "password"}`; the
response is `{"token", "expiresAt"}`, shown once. Enrollment and device
tokens follow the same `{"token", "expiresAt"}` / `{"deviceId",
"deviceToken"}` shapes — see `docs/openapi.yaml`'s `SessionToken`,
`EnrollmentToken`, and `DeviceToken` schemas for the exact fields.

## Aliases

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET` | `/aliases` | session | List every alias, targeting intact |
| `POST` | `/aliases` | session | Create an alias |
| `GET` | `/aliases/{id}` | session | Get one alias |
| `PUT` | `/aliases/{id}` | session | Replace an alias |
| `DELETE` | `/aliases/{id}` | session | Delete an alias |

Create/replace validate `command` and `description` (`400` on failure) and
never validate `name` against a shell's naming rules server-side — that
check runs per shell in `nameWarnings` and never blocks a write, because the
server does not know every shell a future client can render for. Creating
past `validate.MaxAliases` (5,000) is `400`. There is no server-side
enforcement of `renderers.Supported()` — that boundary is deliberate (the
server transmits data, never shell code; see `docs/PROJECT.md` §3.7) — so
the client's own validation on sync/render remains the last line of
defense.

## Profiles

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET` | `/profiles` | session | List every profile |
| `POST` | `/profiles` | session | Create a profile |
| `GET` | `/profiles/{id}` | session | Get one profile |
| `PUT` | `/profiles/{id}` | session | Replace a profile |
| `DELETE` | `/profiles/{id}` | session | Delete a profile |

Profile and device list endpoints carry no pagination or response cap: this
is a single-operator control plane, and the one operator managing their own
data by hand is the accepted bound (see `design.md` decision 26).

## Devices

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET` | `/devices` | session | List every device |
| `GET` | `/devices/{id}` | session | Get one device |
| `PUT` | `/devices/{id}` | session | Rename a device or change its profile membership |
| `DELETE` | `/devices/{id}` | session | Delete a device (also revokes its device token) |
| `POST` | `/devices/{id}/revoke` | session | Revoke a device and every device-kind token it holds |
| `POST` | `/devices/{id}/token` | session | Revoke a device's existing token(s) and issue a new one |

Deleting or revoking a device always revokes its token first — a deleted
device's old token can never authenticate a later `GET /sync` call.

## Sync

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET` | `/sync?platform=<p>&shell=<s>` | device | Resolve this device's aliases and record its reported platform/shell |

`platform` and `shell` are both required query parameters (`macos` \|
`linux` \| `windows`, and `zsh` \| `bash` \| `powershell`); an unknown or
missing value is `400`, naming the valid set — never a silent default.

The response:

```json
{
  "revision": "…",
  "device": {
    "id": "…", "name": "…", "platform": "…", "shell": "…",
    "profileIds": ["…"]
  },
  "aliases": [
    {"name": "…", "command": "…", "description": "…"}
  ],
  "generatedAt": "2026-01-01T00:00:00Z"
}
```

carries no server-side alias id, no rendered shell syntax, and no per-alias
targeting — the client renders, the server only transmits data (design
decision 9). `revision` hashes exactly the fields above, so the client can
recompute it locally and hard-fail on a mismatch, catching a partial or
tampered response.

Profile membership always comes from the stored device row, never from the
request — a client cannot ask to be resolved as if it belonged to a profile
it was not assigned. The same call persists the reported `platform`/`shell`
and stamps `lastSeenAt`/`lastSyncAt` on the device; if that bookkeeping
write fails, the already-resolved aliases are still served with `200` — a
device is never denied its aliases because a timestamp could not be
written.
