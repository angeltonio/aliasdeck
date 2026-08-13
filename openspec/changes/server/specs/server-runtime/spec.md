# Server Runtime Specification

## Purpose

Defines `aliasdeck serve` — the server subcommand of the existing binary — its startup sequence, bounded I/O, graceful shutdown, health endpoint, and single-binary guarantee (PROJECT.md §3.2, §9.3).

## Requirements

### Requirement: Single Binary Serve Command

The system MUST expose `aliasdeck serve` as a subcommand of the existing `aliasdeck` binary. The system MUST NOT introduce a second binary or a separate `cmd/aliasdeck-server` entry point.

#### Scenario: serve subcommand available
- GIVEN the released `aliasdeck` binary
- WHEN `aliasdeck serve` is invoked
- THEN the same binary starts the server; no other executable is required

#### Scenario: no second binary shipped
- GIVEN the release artifacts for a version
- WHEN inspected
- THEN exactly one binary exists per platform/architecture, containing both CLI and serve commands

### Requirement: Migrations Apply on Startup, Idempotently

`aliasdeck serve` MUST apply all pending embedded migrations before accepting any HTTP connection, and MUST refuse to start if the database's recorded schema version is newer than the binary supports.

#### Scenario: Clean start applies migrations
- GIVEN an empty database file
- WHEN `aliasdeck serve` starts
- THEN all migrations run in order and connections are accepted only after they succeed

#### Scenario: Repeated start is idempotent
- GIVEN a database already at the latest schema version
- WHEN `aliasdeck serve` starts a second time
- THEN no migration is reapplied and no data is duplicated or altered

#### Scenario: Newer schema version refused
- GIVEN a database schema version newer than the running binary supports
- WHEN `aliasdeck serve` starts
- THEN it refuses to start and reports the version mismatch

### Requirement: Bounded I/O and Graceful Shutdown

Every HTTP handler and store call MUST run under a bounded context deadline. On a termination signal, the server MUST stop accepting new connections and wait for in-flight requests to finish within a bounded drain period before exiting.

#### Scenario: Slow handler is bounded
- GIVEN a request whose downstream store call would block indefinitely
- WHEN the configured deadline elapses
- THEN the request fails with a timeout error instead of hanging the server

#### Scenario: Graceful shutdown drains in-flight requests
- GIVEN an in-flight request when a termination signal arrives
- WHEN the signal is received
- THEN new connections are refused, the in-flight request finishes within the drain period, and the process then exits

### Requirement: Health Endpoint Requires No Authentication

The server MUST expose a health endpoint reporting readiness, reachable without a session or device token.

#### Scenario: Health check succeeds after startup
- GIVEN the server finished migrations and startup
- WHEN the health endpoint is requested
- THEN it responds success with no authentication required

### Requirement: Zero Stdin Prompts

`aliasdeck serve` MUST NOT block on any interactive stdin prompt during startup or operation.

#### Scenario: Start under a service manager
- GIVEN `aliasdeck serve` started with stdin closed, as under systemd
- WHEN it starts
- THEN startup completes without waiting on stdin
