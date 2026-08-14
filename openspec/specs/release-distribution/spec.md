# Release Distribution Specification

## Purpose

Defines cross-compiled release artifacts, the Homebrew tap formula, and the install script that make `v0.1` installable on macOS and Linux (PROJECT.md §9.2, §9.5, §16).

## Requirements

### Requirement: Cross-Compiled Release Artifacts

A tagged release MUST produce static binaries for `darwin`/`linux`/`windows` on `amd64`/`arm64` via `goreleaser`, with no cgo dependency, including when the embedded server (`aliasdeck serve`, SQLite via `modernc.org/sqlite`) is compiled in.

#### Scenario: Tag triggers build
- GIVEN a pushed `v0.3.x` tag
- WHEN the release pipeline runs
- THEN six static binaries are produced (darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/amd64, windows/arm64)

#### Scenario: Server embedding requires no cgo
- GIVEN the embedded server code compiled into the release binary
- WHEN cross-compilation runs with `CGO_ENABLED=0`
- THEN the build succeeds on all six targets

### Requirement: Homebrew Tap Formula

The release MUST publish a formula to a `homebrew-tap` repository that installs the architecture-matching binary and places `aliasdeck` on `PATH`.

#### Scenario: Install via tap
- GIVEN a published tap formula for the tagged version
- WHEN a user runs `brew install aliasdeck`
- THEN a runnable `aliasdeck` binary matching their architecture is installed

### Requirement: Install Script

The repository MUST provide `scripts/install.sh` that detects OS/architecture, downloads the matching release artifact, and installs it without requiring Homebrew.

#### Scenario: Supported platform
- GIVEN a supported OS/arch (macOS or Linux, amd64 or arm64)
- WHEN `curl -sSL .../install.sh | sh` runs
- THEN the matching binary is downloaded and installed to a location on `PATH`

#### Scenario: Unsupported platform
- GIVEN an unsupported OS/arch combination
- WHEN the install script runs
- THEN it exits with a clear error and performs no partial install

### Requirement: Scoop Bucket Manifest

The release MUST publish a manifest to a `scoop-bucket` repository that installs the architecture-matching Windows binary and places `aliasdeck` on `PATH`, mirroring the Homebrew tap. The bucket repository and its push credentials MUST be verified to exist before a tag is pushed, so a missing bucket fails the release pipeline rather than silently omitting Windows distribution.

#### Scenario: Install via Scoop
- GIVEN a published Scoop manifest for the tagged version
- WHEN a user runs `scoop install aliasdeck`
- THEN a runnable `aliasdeck.exe` matching their architecture is installed

#### Scenario: Missing bucket fails loudly before tagging
- GIVEN the `scoop-bucket` repository does not yet exist
- WHEN the release pipeline runs
- THEN it fails with an explicit error before the tag is considered released, not a silent skip

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
