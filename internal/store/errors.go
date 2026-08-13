package store

import "errors"

// ErrNotFound is returned by any repository method when the requested
// record does not exist.
//
// Callers compare with errors.Is, never a driver-specific "no rows" error —
// that is exactly the leak design decision 3 forbids.
var ErrNotFound = errors.New("store: not found")

// ErrConflict is returned when a write would violate a uniqueness
// constraint (an alias, profile or operator name/username already taken,
// or an enrollment token already consumed).
var ErrConflict = errors.New("store: conflict")
