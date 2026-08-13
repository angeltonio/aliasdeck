# Server Auth Specification

## Purpose

Defines operator bootstrap, opaque hashed sessions, single-use enrollment tokens for device registration, hashed device tokens, rotation, and revocation (PROJECT.md §4.2, §12).

## Requirements

### Requirement: One Operator Account, Bootstrapped on First Start

On first start against an empty database, the system MUST create exactly one operator account and print a generated password once. `ALIASDECK_ADMIN_PASSWORD`, when set, MUST be used instead of generating one. The system MUST NOT prompt interactively for a password, and MUST NOT write the password to any log.

#### Scenario: First start generates and prints a password
- GIVEN an empty database and no `ALIASDECK_ADMIN_PASSWORD`
- WHEN `aliasdeck serve` starts for the first time
- THEN a random password is generated, printed once to the console, and never written to a log

#### Scenario: Environment override is honored
- GIVEN `ALIASDECK_ADMIN_PASSWORD` set and an empty database
- WHEN the server starts
- THEN the operator account is created with that password and none is generated

#### Scenario: Subsequent starts do not reprint
- GIVEN an operator account already exists
- WHEN the server restarts
- THEN no password is generated or printed

### Requirement: Opaque Hashed Sessions, No JWT

Sessions MUST be opaque random tokens with an expiry, hashed before storage. The system MUST NOT use JWTs or any self-contained signed token format.

#### Scenario: Session token stored hashed
- GIVEN a successful operator login
- WHEN the session is persisted
- THEN the stored value is a hash of the token, not the token itself

#### Scenario: Expired session rejected
- GIVEN a session past its expiry
- WHEN presented to an authenticated endpoint
- THEN the request is rejected as unauthenticated

### Requirement: Single-Use Enrollment Tokens for Device Registration

The operator MUST be able to generate a single-use enrollment token. `register`, given a valid unused enrollment token, MUST exchange it for a device token and mark the enrollment token consumed in the same operation. A second registration with an already-consumed token MUST be refused.

#### Scenario: Enrollment token registers a device
- GIVEN a freshly generated enrollment token
- WHEN `register` presents it to the server
- THEN a device token is issued and the enrollment token is marked consumed

#### Scenario: Reused enrollment token is refused
- GIVEN an enrollment token already consumed by a prior registration
- WHEN a second `register` attempt presents the same token
- THEN the server refuses the request and no second device token is issued

#### Scenario: Operator credentials never leave the operator's machine
- GIVEN a device registering with an enrollment token
- WHEN registration completes
- THEN no operator password or session was transmitted from the registering machine

### Requirement: Device Tokens Hashed at Rest

Device tokens MUST be stored hashed, never in plaintext, such that a database dump does not yield a usable token.

#### Scenario: Database dump does not expose a usable token
- GIVEN a device token issued to a registered device
- WHEN the store's underlying table is dumped
- THEN the dumped value cannot authenticate as that device against the API

### Requirement: Device Token Rotation

The system MUST support rotating a device's token, invalidating the previous one, and issuing a new one, without requiring re-enrollment.

#### Scenario: Old token invalid after rotation
- GIVEN a device token that has been rotated
- WHEN the previous token is presented to `sync`
- THEN the request is rejected as unauthenticated

### Requirement: Immediate Device Revocation

Revoking a device MUST invalidate its token immediately; any use of that token after revocation MUST fail authentication on its very next use.

#### Scenario: Revoked token rejected on next use
- GIVEN a device whose token is revoked
- WHEN that device's next `sync` request presents the revoked token
- THEN the request fails with an actionable error naming that the device was revoked
