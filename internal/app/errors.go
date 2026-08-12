package app

import "errors"

// ErrNotInitialized is returned when config.yaml does not exist yet.
// cmd/aliasdeck maps it to exit code 4 and names `aliasdeck init` in the
// printed message (design, "Paths, Detection, Exit Codes").
var ErrNotInitialized = errors.New("aliasdeck is not initialized in this environment; run `aliasdeck init` first")

// ConfigError marks a failure caused by invalid on-disk configuration — a
// parse error in config.yaml or aliases.yaml — rather than a transient
// runtime failure. cmd/aliasdeck maps it to exit code 3.
type ConfigError struct{ Err error }

func (e ConfigError) Error() string { return e.Err.Error() }
func (e ConfigError) Unwrap() error { return e.Err }
