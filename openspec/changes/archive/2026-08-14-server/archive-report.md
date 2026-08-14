# Archive Report: Self-hosted server — Milestone 4 (v0.3)

**Change**: server  
**Status**: Complete and Archived  
**Archive Date**: 2026-08-14  
**Merged to**: `main` at commit `62043da`  
**All 10 Phases**: Complete — 10/10 with all tasks marked `[x]`

## Summary

The complete `server` change (Milestone 4, v0.3) has been successfully archived. All delta specifications have been merged into the main specifications, and the change folder has been moved to the archive location.

### What Was Archived

**Source Location**: `openspec/changes/server/`  
**Archive Location**: `openspec/changes/archive/2026-08-14-server/`

**Key Files**:
- `proposal.md` — 117 lines: Intent, scope, capabilities, approach, resolved decisions, risks, rollback plan, dependencies, success criteria
- `design.md` — 200+ lines: Technical approach, 35 architecture decisions, bounded operations table, interfaces, Windows 0600 gap mitigation, storage schema, file changes, testing strategy
- `apply-progress.md` — Comprehensive record of all 10 phases of implementation with detailed progress notes, corrections, and mutation verification
- `tasks.md` — 174 lines: All tasks for Phases 1-10, each marked complete `[x]`, including review workload forecast and suggested work units

**Delta Specifications** (10 new/modified specs merged into main specs):
1. `server-runtime/spec.md` — NEW — Created at `openspec/specs/server-runtime/spec.md`
2. `server-persistence/spec.md` — NEW — Created at `openspec/specs/server-persistence/spec.md`
3. `server-api/spec.md` — NEW — Created at `openspec/specs/server-api/spec.md`
4. `server-sync/spec.md` — NEW — Created at `openspec/specs/server-sync/spec.md`
5. `server-auth/spec.md` — NEW — Created at `openspec/specs/server-auth/spec.md`
6. `server-source/spec.md` — NEW — Created at `openspec/specs/server-source/spec.md`
7. `config-source/spec.md` — MERGED — Modified existing spec to add ServerSource scenarios (2 modified requirements, 1 new scenario per requirement)
8. `standalone-config/spec.md` — MERGED — Modified existing spec to add server source strictness (1 modified requirement, 2 new scenarios)
9. `cli-commands/spec.md` — MERGED — Added 3 new requirements (register, login, logout) + modified 2 existing requirements (status, edit) with new ServerSource scenarios
10. `release-distribution/spec.md` — MERGED — Modified 1 requirement (Cross-Compiled Release Artifacts) + added 1 new requirement (Binary Size Budget) with 2 new scenarios

## Merge Summary

### Main Specs Before Merge
**Count**: 7 spec files  
**Files**:
- sync-state/spec.md
- native-apply/spec.md
- powershell-render/spec.md
- config-source/spec.md
- standalone-config/spec.md
- cli-commands/spec.md
- release-distribution/spec.md

### Main Specs After Merge
**Count**: 13 spec files (7 original + 6 new)  
**Files**:
- sync-state/spec.md (unchanged)
- native-apply/spec.md (unchanged)
- powershell-render/spec.md (unchanged)
- config-source/spec.md (MERGED — ServerSource scenarios added)
- standalone-config/spec.md (MERGED — server source strictness added)
- cli-commands/spec.md (MERGED — 3 new commands, 2 modified requirements)
- release-distribution/spec.md (MERGED — server embedding + binary size budget)
- **server-runtime/spec.md (NEW)**
- **server-persistence/spec.md (NEW)**
- **server-api/spec.md (NEW)**
- **server-sync/spec.md (NEW)**
- **server-auth/spec.md (NEW)**
- **server-source/spec.md (NEW)**

### Merge Operations

**Config Source (`config-source/spec.md`)**:
- Modified: "Every Source Is Hostile Input" — updated to include ServerSource (added ServerSource scenario)
- Modified: "Exactly One Source Per Device" — updated to include ServerSource (added ServerSource scenario)

**Standalone Config (`standalone-config/spec.md`)**:
- Modified: "config.yaml Strict Schema and Device Identity" — updated to require `source.url` for server and exclude token from config.yaml (added 2 scenarios)

**CLI Commands (`cli-commands/spec.md`)**:
- ADDED: "register Consumes a Single-Use Enrollment Token" — 2 scenarios
- ADDED: "login Authenticates the Operator" — 2 scenarios
- ADDED: "logout Clears the Locally Stored Session" — 2 scenarios
- Modified: "status Always Reports the Active Source" — added ServerSource URL reporting with token protection (added 1 scenario)
- Modified: "edit Opens $EDITOR Without Side Effects" — added explicit error when used with ServerSource (added 2 scenarios)

**Release Distribution (`release-distribution/spec.md`)**:
- Modified: "Cross-Compiled Release Artifacts" — updated to include server embedding and SQLite (added 1 scenario)
- ADDED: "Binary Size Budget" — 25 MB limit with CI enforcement (2 scenarios)

**New Specifications**:
- server-runtime/spec.md: 5 requirements covering single binary, migrations, bounded I/O, health endpoint, zero stdin
- server-persistence/spec.md: 4 requirements covering interfaces, SQLite backend, conformance suite, forward-only migrations
- server-api/spec.md: 4 requirements covering CRUD, error shape, request bounds, OpenAPI coverage
- server-sync/spec.md: 4 requirements covering server resolution, response shape, identical output across platforms, ownership split
- server-auth/spec.md: 7 requirements covering operator bootstrap, sessions, enrollment tokens, device tokens, rotation, revocation
- server-source/spec.md: 7 requirements covering ConfigSource contract, hostile input handling, token storage, HTTPS guard, offline behavior, timeouts, redirect refusal

## Completion Verification

### Tasks Completion
- **Phase 1**: Open Decisions & Foundation Skeleton — 6/6 tasks complete
- **Phase 2**: Server Persistence — 9/9 tasks complete
- **Phase 3**: Server Auth — 9/9 tasks complete
- **Phase 4**: Server Runtime — 6/6 tasks complete
- **Phase 5**: Server API — 16/16 tasks complete
- **Phase 6**: Server Sync — 7/7 tasks complete
- **Phase 7**: ServerSource & Credentials — 10/10 tasks complete
- **Phase 8**: CLI Wiring — 15/15 tasks complete
- **Phase 9**: Cross-Cutting Verification — 5/5 tasks complete
- **Phase 10**: Release, CI & Docs — 9/9 tasks complete

**Total**: 92/92 tasks complete (100%)

### Review Status
- **Review Receipt**: All phases completed with bounded reviews where required
- **Verification Report**: All 10 phases verified (Phase 9's byte-identity and full flow integration tests passed)
- **Implementation Status**: All code merged to main at commit `62043da`

## Archive Contents

**Artifacts Preserved**:
- proposal.md — Full proposal with intent, scope, capabilities, approach, resolved decisions, risks, rollback plan
- design.md — 35 architecture decisions with rationale, bounded operations table, interfaces specification, Windows gap mitigation
- apply-progress.md — Detailed progress notes from all 10 phases, including corrections and mutation verification results
- tasks.md — Complete task list for all 10 phases with all tasks marked complete

**Spec Files Integrated**:
- 4 specs modified: config-source, standalone-config, cli-commands, release-distribution
- 6 specs created: server-runtime, server-persistence, server-api, server-sync, server-auth, server-source
- All merged into main spec location: `openspec/specs/`

## Delta Spec Merge Verification

All delta specifications from `openspec/changes/server/specs/` have been successfully:
1. **Read in full** — No truncation or paraphrasing
2. **Merged verbatim** — ADDED requirements appended, MODIFIED requirements replaced with delta content
3. **Placed in main specs** — New specs created, existing specs updated at `openspec/specs/`

### Changed Files vs. New Files
- **Changed (merged)**: 4 specs (config-source, standalone-config, cli-commands, release-distribution)
- **New (copied)**: 6 specs (server-runtime, server-persistence, server-api, server-sync, server-auth, server-source)
- **Total specs after**: 13 (previously 7)

## What Was NOT Moved to Archive

The delta specs themselves (`openspec/changes/server/specs/*.md`) are not copied to the archive because:
1. They have been merged into the main specs (`openspec/specs/`)
2. The merged specs are the source of truth going forward
3. The proposal, design, and apply-progress files provide complete traceability
4. The tasks and apply-progress documents record every change and verification

## Closure

This archive completes the SDD cycle for the `server` change:
- ✅ Proposal created and reviewed
- ✅ Spec designed with 35 architecture decisions
- ✅ 92 tasks implemented and verified across 10 phases
- ✅ All verification checks passed
- ✅ Delta specs merged into main specs
- ✅ Change archived with full traceability

The `server` change (Milestone 4, v0.3) is now part of the main specification and ready for release.
