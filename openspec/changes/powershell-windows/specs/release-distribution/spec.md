# Delta for Release Distribution

## MODIFIED Requirements

### Requirement: Cross-Compiled Release Artifacts

A tagged release MUST produce static binaries for `darwin`/`linux`/`windows` on `amd64`/`arm64` via `goreleaser`, with no cgo dependency.
(Previously: covered `darwin`/`linux` only, four binaries.)

#### Scenario: Tag triggers build
- GIVEN a pushed `v0.2.x` tag
- WHEN the release pipeline runs
- THEN six static binaries are produced (darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/amd64, windows/arm64)

## ADDED Requirements

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
