// Package store defines the server's persistence seam: Store and its five
// repositories (Aliases, Devices, Profiles, Tokens, Operators), plus the
// sentinel errors ErrNotFound and ErrConflict. No method signature in this
// package names a driver type or a SQLite dialect string (design decision
// 3); internal/store/sqlitestore is the only implementation. This package
// and its subpackages MUST NOT import internal/renderers — the server
// transmits data, the client produces shell syntax (design decision 2).
package store
