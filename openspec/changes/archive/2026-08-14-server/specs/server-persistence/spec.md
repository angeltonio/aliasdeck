# Server Persistence Specification

## Purpose

Defines the `internal/store` repository interfaces, the SQLite implementation on a pure-Go driver, forward-only embedded migrations, and the backend conformance suite (PROJECT.md §9.3).

## Requirements

### Requirement: Repository Interfaces Carry No Driver Types

Repository interfaces MUST NOT expose any SQLite- or PostgreSQL-specific type in their method signatures.

#### Scenario: Interface signature is backend-neutral
- GIVEN the repository interfaces in `internal/store`
- WHEN inspected
- THEN no parameter or return type references a driver-specific type

### Requirement: SQLite Is the Only Implemented Backend

The system MUST ship a SQLite implementation using a pure-Go driver (`modernc.org/sqlite`, no cgo). The system MUST NOT ship a PostgreSQL implementation in this milestone.

#### Scenario: SQLite backend passes conformance suite
- GIVEN the SQLite repository implementation
- WHEN the backend conformance suite runs against it
- THEN all conformance cases pass

#### Scenario: No PostgreSQL implementation shipped
- GIVEN the v0.3 codebase
- WHEN searched for a PostgreSQL repository implementation
- THEN none exists; only the interface and conformance suite are present

### Requirement: Backend Conformance Suite Is Interface-Only

The system MUST provide a conformance test suite, written only against the repository interface, that any future backend implementation MUST pass to be considered compliant.

#### Scenario: Conformance suite is backend-agnostic
- GIVEN the conformance suite
- WHEN read
- THEN it references only interface types, never a concrete driver type

### Requirement: Forward-Only, Idempotent Migrations

Migrations MUST be embedded in the binary and applied only forward, each recorded with a version so re-application is a no-op. Repeated startup against an already-migrated database MUST NOT alter or duplicate data.

#### Scenario: Second startup is a no-op
- GIVEN a database already migrated to the latest version
- WHEN the server starts again
- THEN the migration runner detects the current version and applies nothing

#### Scenario: Migration failure aborts startup transactionally
- GIVEN a migration that fails partway through
- WHEN it is applied
- THEN the failed migration is rolled back and the server does not start in a partially migrated state
