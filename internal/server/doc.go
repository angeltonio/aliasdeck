// Package server is the composition root for `aliasdeck serve`: it opens
// and migrates internal/store, bootstraps the first operator via
// internal/auth, and serves one bounded http.Server behind Run(ctx).
//
// Phase 4 (this phase) wires the composition root, the bounded http.Server,
// startup migration ordering, and the unauthenticated health endpoint.
// internal/api's full router — the CRUD handlers, sync, and every
// authenticated route — lands in Phase 5 and replaces this package's
// placeholder handler wholesale, never its health path or its bounds.
package server
