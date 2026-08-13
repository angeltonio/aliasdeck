// Package sync is the only server-side resolution path: Resolve loads a
// device's full enabled alias set and targeting from internal/store and
// calls domain.Resolve directly — no SQL-side filtering (design decision 4).
// It MUST NOT import internal/renderers — the server transmits data, the
// client produces shell syntax (design decision 2).
package sync
