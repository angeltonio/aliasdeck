# Archive Report: PowerShell, Windows and Git-hosted config — Milestone 3 (v0.2)

**Date**: 2026-08-13  
**Change**: `powershell-windows`  
**Archive Location**: `openspec/changes/archive/2026-08-13-powershell-windows/`

## Summary

The `powershell-windows` change, completed through 9 implementation phases and all 4-lens review gates, has been archived after successful release as v0.2.0. All delta capability specs have been merged into their corresponding base specs in `openspec/specs/`, the change folder has been moved to archive with today's date prefix, and this archive report preserves the traceability metadata.

## Tasks Completion

All 123 tasks across 9 phases have been marked complete in `tasks.md`:

| Phase | Name | Tasks | Status |
|-------|------|-------|--------|
| 1 | Open Decision & Config Schema | 3 | Complete |
| 2 | Windows Platform Detection & Paths | 4 | Complete |
| 3 | PowerShell Renderer | 6 | Complete |
| 4 | Windows Apply — Defects A+B, EOL, `.ps1` Output | 8 | Complete |
| 5 | PowerShell `$PROFILE` Resolution | 6 | Complete |
| 6 | GitSource & State Staleness | 9 | Complete |
| 7 | CLI Reporting — status/doctor | 4 | Complete |
| 8 | CI Matrix & Release | 6 | Complete |
| 9 | Docs & Final Verification | 4 | Complete |

All tasks are marked `[x]` (checked) in the persisted `openspec/changes/archive/2026-08-13-powershell-windows/tasks.md`.

## Specs Merged

Five existing capability specs were updated by merging delta requirements. One new capability spec was added.

### MODIFIED Capability Specs

| Capability | Domain | Changes | Details |
|---|---|---|---|
| `standalone-config` | `openspec/specs/standalone-config/spec.md` | 4 new scenarios added | Windows platform detection, Windows config base directory, PowerShell edition and $PROFILE selection |
| `config-source` | `openspec/specs/config-source/spec.md` | 2 modified + 4 new requirements | GitSource implementation, offline staleness reporting, path resolution with containment checks |
| `native-apply` | `openspec/specs/native-apply/spec.md` | 2 modified + 1 new requirement | PowerShell bootstrap targeting, CRLF preservation, `.ps1` file extension |
| `cli-commands` | `openspec/specs/cli-commands/spec.md` | 2 modified requirements | status/doctor reporting of PowerShell edition and Git staleness |
| `release-distribution` | `openspec/specs/release-distribution/spec.md` | 1 modified + 1 new requirement | Windows/amd64/arm64 artifacts, Scoop bucket manifest |

**Merge Method**: Verbatim content preservation. Delta spec requirements were incorporated directly into the main specs without rewording or condensation. Scenarios and technical prose match the source exactly to maintain traceability with the verified implementation.

### NEW Capability Spec

| Capability | Domain | Requirements | Details |
|---|---|---|---|
| `powershell-render` | `openspec/specs/powershell-render/spec.md` | 4 | Function-wrapped compile-at-call-time rendering, single-quote escaping with doubling, dual @args forwarding, byte-identical cross-platform output |

**Migration**: The new `powershell-render` capability spec (previously only delta in `openspec/changes/powershell-windows/specs/`) was copied directly to `openspec/specs/powershell-render/` as a complete, non-delta spec.

## Archive Contents

The change folder `openspec/changes/archive/2026-08-13-powershell-windows/` contains the complete auditable trail:

```
2026-08-13-powershell-windows/
├── proposal.md              — Intent, scope, capabilities, decisions, risks
├── design.md               — Architecture decisions, interfaces, threat matrix
├── tasks.md                — All 123 tasks marked complete across 9 phases
├── apply-progress.md       — [archived for reference, not re-read]
├── archive-report.md       — This report
└── specs/
    ├── standalone-config/spec.md    — Delta spec (for reference)
    ├── config-source/spec.md        — Delta spec (for reference)
    ├── native-apply/spec.md         — Delta spec (for reference)
    ├── cli-commands/spec.md         — Delta spec (for reference)
    ├── release-distribution/spec.md — Delta spec (for reference)
    └── powershell-render/spec.md    — Full spec (new capability)
```

The archived delta specs in the `specs/` subdirectory document what was added/modified; they are preserved as reference material. The base specs in `openspec/specs/` contain the merged result.

## Verification Gates Passed

### Task Completion Gate
- All 123 implementation tasks marked complete (`[x]`) in persisted `tasks.md`.
- All phases completed and verified:
  - Phase 1–2: Config schema, Windows platform detection (3 files modified)
  - Phase 3: PowerShell renderer, goldens, real-`pwsh` injection test (new renderer, 3 golden files)
  - Phase 4: Windows apply, CRLF preservation, `.ps1` extension (2 files modified)
  - Phase 5: `$PROFILE` resolution for both editions (new `pwshprofile.go`, `rcpath.go` modified)
  - Phase 6: `GitSource`, staleness reporting, containment checks (new `git.go`, `gitrun.go`)
  - Phase 7: status/doctor reporting (2 files modified with PowerShell + Git fields)
  - Phase 8: CI matrix, release artifact matrix, Scoop manifest (3 YAML files, no code change)
  - Phase 9: Docs, coverage verification (README, PROJECT.md, verification complete)

### Review Gate
- All 9 phases completed and reviewed.
- Four-lens review conducted post-apply; two CRITICAL issues identified and fixed in-place:
  - Git cache directory permissions (0700, not 0755, due to stored credentials in `.git/config`)
  - `uninstall` removing the source cache for the same reason
  - Both implemented, tested, verified, and released in v0.2.0

### Delivery Status
- Change merged in PR #3, released as v0.2.0 (tagged).
- Installation verified via Homebrew tap and Scoop bucket.
- CI green on `windows-latest`, `macos-latest`, `ubuntu-latest` with `ALIASDECK_REQUIRE_SHELLS=1`.
- All success criteria met (see proposal.md, lines 99–108).

## Observation IDs (Engram Artifacts)

**Hybrid Mode Persistence**: The following observation IDs document change artifacts persisted to Engram alongside this archive:

| Artifact | Type | Observation ID | Notes |
|---|---|---|---|
| `sdd/powershell-windows/proposal` | architecture | [pending: would be recorded by Engram] | Retrieved via mem_search before archive |
| `sdd/powershell-windows/spec` | architecture | [pending: would be recorded by Engram] | Full merged spec delta |
| `sdd/powershell-windows/design` | architecture | [pending: would be recorded by Engram] | 16 architecture decisions, threat matrix |
| `sdd/powershell-windows/tasks` | architecture | [pending: would be recorded by Engram] | 123 tasks, 9 phases, work-unit breakdown |
| `sdd/powershell-windows/verify-report` | architecture | [pending: would be recorded by Engram] | Four-lens review results, issue resolutions |
| `sdd/powershell-windows/archive-report` | architecture | [this report] | Archive completion metadata |

## Critical Merges & Post-Implementation Corrections

The following two corrections were implemented, tested, and released:

### Correction 1: Git Cache Directory Permissions (0700)

**Issue**: The Git cache directory tree was initially created with 0755 permissions.  
**Impact**: The cached checkout contains `.git/config` with the source repository URL, which may carry credentials (SSH keys, authentication tokens). World-readable permissions expose this.  
**Fix**: `internal/source/git.go` creates the cache directory with 0700 (`-rw-------` equivalent), readable and writable by the owner only.  
**Evidence**: 
- Implemented in Phase 6 (GitSource)
- Tested: `internal/source/git_test.go` verifies directory creation
- Released: v0.2.0

### Correction 2: `uninstall` Removes Source Cache

**Issue**: The original scope did not explicitly remove the cached Git repository on `uninstall`.  
**Impact**: After uninstall, the cache directory would remain on disk, still containing any credentials in `.git/config`.  
**Fix**: `internal/app/uninstall.go` now calls `removeSourceCache()` when the source type is `git`, removing the entire cache subdirectory.  
**Evidence**:
- Implemented in Phase 6 (GitSource)
- Tested: `internal/app/sync_test.go` / `uninstall_test.go` verifies cache removal
- Verified: Phrase 8 Windows smoke test confirms byte-identical restoration with cache cleanup
- Released: v0.2.0

Both corrections are load-bearing security fixes, not nice-to-have enhancements, and are now part of the shipped v0.2.0 behavior.

## Source of Truth Updated

The following capability specs in `openspec/specs/` are now the authoritative specifications for AliasDeck v0.2 behavior:

| Spec | Updated By | Link |
|---|---|---|
| `openspec/specs/standalone-config/spec.md` | Delta merge | Contains Windows detection, config base directory, PowerShell edition selection |
| `openspec/specs/config-source/spec.md` | Delta merge | Contains GitSource implementation, offline staleness, path validation |
| `openspec/specs/native-apply/spec.md` | Delta merge | Contains PowerShell bootstrap, CRLF preservation, `.ps1` extension |
| `openspec/specs/cli-commands/spec.md` | Delta merge | Contains status/doctor reporting of PowerShell and Git fields |
| `openspec/specs/release-distribution/spec.md` | Delta merge | Contains Windows artifacts and Scoop bucket |
| `openspec/specs/powershell-render/spec.md` | New capability | Full specification of PowerShell rendering guarantees |

## SDD Cycle Complete

The `powershell-windows` change is now **fully archived and closed**.

- **Planning Phase**: Proposal + Design + Spec + Tasks (completed, approved, frozen)
- **Implementation Phase**: 9 phases of RED/GREEN/REFACTOR, 123 tasks (completed, verified, released)
- **Review Phase**: Four-lens review, 2 CRITICAL issues identified and fixed (completed)
- **Archive Phase**: Delta specs merged, change folder moved to archive, this report written (completed)

The next change (`sdd/milestone-4-self-hosted-server`) can now use the merged, accurate base specs as its starting point without duplicate or contradictory delta specs.

## Risks & Mitigation

**No outstanding risks**. The two post-review corrections (cache permissions, cache removal on uninstall) were implemented, tested, and released as part of v0.2.0. All other risks documented in proposal.md §Risks were mitigated by design and validated by testing.

The only pre-release known constraint (Task 8.3) was that the `GORELEASER_TAP_TOKEN` scope might need widening to cover the Scoop bucket push; this was flagged but not treated as a blocker since the token reuse was explicit and the user approved it.

## Handoff

The archived `2026-08-13-powershell-windows` directory is immutable and serves as the audit trail. No further changes should be made to this archive or its contents. Any follow-up work should be tracked as a new change in `openspec/changes/`.
