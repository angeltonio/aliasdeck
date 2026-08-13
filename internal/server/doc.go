// Package server is the composition root for `aliasdeck serve`: it wires
// internal/store, internal/auth, internal/api and internal/sync into one
// http.Server, applies startup migrations, and owns bounded shutdown.
//
// This file is a skeleton only (Milestone 4, Phase 1): it exists so the
// import graph internal/archtest verifies is present from the first commit
// on. Behavior lands in Phase 4.
package server
