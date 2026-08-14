# Delta for Release Distribution

## MODIFIED Requirements

### Requirement: Cross-Compiled Release Artifacts

A tagged release MUST produce static binaries for `darwin`/`linux`/`windows` on `amd64`/`arm64` via `goreleaser`, with no cgo dependency, including when the embedded server (`aliasdeck serve`, SQLite via `modernc.org/sqlite`) is compiled in.
(Previously: no-cgo applied to the standalone CLI only; it now explicitly covers the embedded server and its pure-Go SQLite driver.)

#### Scenario: Tag triggers build
- GIVEN a pushed `v0.3.x` tag
- WHEN the release pipeline runs
- THEN six static binaries are produced (darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/amd64, windows/arm64)

#### Scenario: Server embedding requires no cgo
- GIVEN the embedded server code compiled into the release binary
- WHEN cross-compilation runs with `CGO_ENABLED=0`
- THEN the build succeeds on all six targets

## ADDED Requirements

### Requirement: Binary Size Budget

Each released binary MUST NOT exceed 25 MB, and CI MUST record the artifact size and fail the build if the budget is exceeded.

#### Scenario: Binary within budget
- GIVEN a built release binary
- WHEN CI records its size
- THEN the size is under 25 MB and the build passes

#### Scenario: Binary exceeding budget fails CI
- GIVEN a built release binary exceeding 25 MB
- WHEN CI checks the recorded size
- THEN the build fails, naming the artifact and its size
