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

// ErrCapacity reports that an atomic bounded create refused to grow a
// collection beyond its configured product limit.
var ErrCapacity = errors.New("store: capacity reached")

// ErrInvalidReference is returned when a write names another record that
// does not exist — e.g. an alias or device whose ProfileIDs/DeviceIDs
// includes an ID with no matching row (design decision 18). This is
// distinct from ErrConflict (a name collision) and from ErrNotFound (the
// record identified by the call's own id argument is missing): here the id
// argument is fine, but a value inside the payload points at nothing.
var ErrInvalidReference = errors.New("store: invalid reference")
