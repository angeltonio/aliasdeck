// Package verify holds Milestone 4's cross-cutting verification tests
// (Phase 9): tests that must import both the client-side rendering
// pipeline (internal/renderers, internal/source, internal/app) and the
// full server stack (internal/server, internal/api, internal/auth,
// internal/store, internal/sync) in the same test binary, to prove the
// project's central promise end to end — a server-backed device and a
// file-backed device produce the same shell (docs/PROJECT.md §3.7, §6.1;
// proposal.md success criterion 2).
//
// internal/archtest/deps_test.go asserts two things about the import
// graph: no package under internal/{server,api,auth,store,sync} may
// depend on internal/renderers, and internal/source/internal/app may
// never depend on internal/store or modernc.org/sqlite. This package sits
// outside every one of those directories on purpose — it is the one place
// in this module allowed to depend on both halves at once, because its
// entire job is proving they agree.
//
// Nothing here is production code: this package ships no non-test symbol
// beyond this doc comment, and is never imported by anything else in the
// module.
package verify
