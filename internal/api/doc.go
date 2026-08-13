// Package api implements the `/api/v1` HTTP boundary: the route slice,
// request/response DTOs, middleware bounds and the error shape. It calls
// internal/auth for identity and internal/sync for resolution; it MUST NOT
// import internal/renderers — the server transmits data, the client produces
// shell syntax (design decision 2).
//
// This file is a skeleton only (Milestone 4, Phase 1): it exists so the
// import graph internal/archtest verifies is present from the first commit
// on. Behavior lands in Phase 5.
package api
