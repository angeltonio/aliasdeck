// Package auth mints and verifies operator sessions, device tokens and the
// one-time operator bootstrap credential. It MUST NOT import
// internal/renderers — the server transmits data, the client produces shell
// syntax (design decision 2).
//
// This file is a skeleton only (Milestone 4, Phase 1): it exists so the
// import graph internal/archtest verifies is present from the first commit
// on. Behavior lands in Phase 3.
package auth
