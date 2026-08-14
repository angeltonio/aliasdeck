# Server API Specification

## Purpose

Defines the `/api/v1` REST surface for aliases, profiles and devices, its error shape, request bounds, and OpenAPI coverage (PROJECT.md §4.2, §9.3).

## Requirements

### Requirement: REST CRUD Under /api/v1

The system MUST expose CRUD endpoints for aliases, profiles, and devices under `/api/v1`, gated by operator session authentication.

#### Scenario: Unauthenticated request rejected
- GIVEN a request to a CRUD endpoint with no valid session
- WHEN it is sent
- THEN it is rejected with an authentication error and no data is returned or modified

#### Scenario: Authenticated CRUD succeeds
- GIVEN a valid operator session
- WHEN an alias is created via `POST /api/v1/aliases`
- THEN it is persisted and returned in a subsequent listing

### Requirement: Consistent Error Shape

Every API error response MUST use one consistent JSON error shape across all endpoints, including a machine-readable code and a human-readable message.

#### Scenario: Validation error uses the standard shape
- GIVEN a malformed request body
- WHEN submitted to any CRUD endpoint
- THEN the response uses the standard error shape with a non-2xx status

### Requirement: Bounded Request Size

Every endpoint accepting a request body MUST enforce a maximum payload size and reject oversized requests before fully buffering them in memory.

#### Scenario: Oversized payload rejected
- GIVEN a request body exceeding the configured size limit
- WHEN sent to any endpoint
- THEN it is rejected before the full body is read into memory

### Requirement: OpenAPI Coverage of Every Route

The system MUST ship a checked-in OpenAPI document and MUST fail a route-coverage test when a registered route is undocumented.

#### Scenario: Route coverage test catches drift
- GIVEN a new route added to the router without a corresponding OpenAPI entry
- WHEN the route-coverage test runs
- THEN it fails, naming the undocumented route
