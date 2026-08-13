# Release Distribution Specification

## Purpose

Defines cross-compiled release artifacts, the Homebrew tap formula, and the install script that make `v0.1` installable on macOS and Linux (PROJECT.md §9.2, §9.5, §16).

## Requirements

### Requirement: Cross-Compiled Release Artifacts

A tagged release MUST produce static binaries for `darwin`/`linux` on `amd64`/`arm64` via `goreleaser`, with no cgo dependency.

#### Scenario: Tag triggers build
- GIVEN a pushed `v0.1.x` tag
- WHEN the release pipeline runs
- THEN four static binaries (darwin/amd64, darwin/arm64, linux/amd64, linux/arm64) are produced

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
