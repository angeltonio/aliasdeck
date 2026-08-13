# Server Source Specification

## Purpose

Defines `ServerSource`, its unmodified `ConfigSource` contract, token storage, validate-on-receive, TLS enforcement, and offline behavior (PROJECT.md §7, §7.1, §12).

## Requirements

### Requirement: ServerSource Implements ConfigSource Unmodified

`ServerSource` MUST implement the existing `ConfigSource.Resolve(ctx, device) (ResolvedConfig, error)` signature with no additional parameter or widened contract, calling `GET /api/v1/sync` and decoding the response into a `ResolvedConfig`.

#### Scenario: Signature unchanged
- GIVEN `ServerSource`
- WHEN compared against `FileSource` and `GitSource`
- THEN all three satisfy the identical `ConfigSource` interface with no widened signature

### Requirement: Server Response Is Hostile Input

`ServerSource`'s output MUST pass through the same `validate.FilterValid` path as `FileSource` and `GitSource` before reaching `renderers.Render`. No lesser scrutiny may apply because the origin is a server.

#### Scenario: Hostile server-stored alias filtered identically
- GIVEN a sync response containing an alias name with a shell metacharacter
- WHEN resolved
- THEN the entry is dropped by `validate.FilterValid`, identically to a local file with the same entry, before rendering

### Requirement: Device Token Stored Outside config.yaml

The device token MUST be stored in a file separate from `config.yaml`, at `0600` where the OS supports it, and MUST NOT appear in `config.yaml` itself.

#### Scenario: Token absent from config.yaml
- GIVEN a device configured with `source.type: server`
- WHEN `config.yaml` is inspected
- THEN it contains the server URL but no token value

### Requirement: HTTPS Required Unless Loopback or Explicit Opt-Out

`ServerSource` MUST refuse to resolve against a `source.url` that is not `https://`, unless the host is a loopback address or an explicit opt-out flag/setting is present.

#### Scenario: Non-HTTPS URL refused
- GIVEN `source.url: http://aliases.example.com`
- WHEN `sync` runs
- THEN it fails before any request is sent, naming the insecure URL

#### Scenario: Loopback HTTP is allowed
- GIVEN `source.url: http://127.0.0.1:8080`
- WHEN `sync` runs
- THEN the request proceeds without requiring the opt-out flag

#### Scenario: Opt-out flag permits a non-loopback HTTP URL
- GIVEN a non-loopback `http://` URL and the explicit opt-out flag set
- WHEN `sync` runs
- THEN the request proceeds

### Requirement: Unreachable Server Is a Hard Error, No Response Cache

When the server is unreachable, `ServerSource.Resolve` MUST return an error naming the URL and MUST NOT fall back to any previously cached response. The most recently generated local file MUST remain untouched.

#### Scenario: Unreachable server fails sync
- GIVEN `source.type: server` with an unreachable URL
- WHEN `sync` runs
- THEN it fails with an error naming the URL, and no cached response is substituted

#### Scenario: Previously generated file still works
- GIVEN a device with an existing generated alias file and now an unreachable server
- WHEN `sync` fails
- THEN the previously generated file is left untouched and remains usable

### Requirement: Bounded Client Timeout

`ServerSource`'s HTTP client MUST enforce a request timeout so an unresponsive server cannot hang `sync` indefinitely.

#### Scenario: Slow server times out
- GIVEN a server that accepts the connection but never responds
- WHEN `sync` runs
- THEN it fails after the configured timeout rather than hanging

### Requirement: Sync Request Never Follows a Redirect

`ServerSource` MUST refuse any HTTP redirect returned by the sync endpoint, before the redirect target is ever contacted, regardless of the target's scheme or host. This exists because Go's default redirect handling forwards the `Authorization` header whenever the redirect target's host matches the original request's host — a comparison that ignores scheme — so an unrefused redirect could re-send the device token to an `http://` target even though the configured `source.url` is `https://`.

#### Scenario: Same-host scheme-downgrading redirect refused
- GIVEN a sync endpoint that answers with a redirect to the same host over `http://`
- WHEN `sync` runs
- THEN the request fails naming the refused redirect, and the device token is never sent to the redirect target
