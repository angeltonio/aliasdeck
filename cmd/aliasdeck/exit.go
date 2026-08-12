package main

import (
	"errors"

	"github.com/angeltonio/aliasdeck/internal/app"
)

// Exit codes (design, "Paths, Detection, Exit Codes"):
//
//	0 success, including a no-op skip and warnings-only doctor
//	1 runtime failure: I/O, render refused, unsupported shell, backend: chezmoi
//	2 usage error (Cobra's own flag/argument validation)
//	3 invalid configuration: a parse failure, or doctor finding SeverityError
//	4 not initialized (config.yaml absent)
const (
	exitOK             = 0
	exitRuntimeError   = 1
	exitUsageError     = 2
	exitInvalidConfig  = 3
	exitNotInitialized = 4
)

// exitError lets a command carry a precise exit code alongside an error
// that may already have been fully reported to the user (Err == nil), so
// run() never prints a redundant "Error:" line for output a command has
// already written itself (e.g. doctor's issue list).
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error { return e.err }

// exitCodeFor maps an error returned from a command's RunE to an exit code,
// for errors that were not already wrapped in an *exitError.
func exitCodeFor(err error) int {
	if err == nil {
		return exitOK
	}
	if errors.Is(err, app.ErrNotInitialized) {
		return exitNotInitialized
	}
	var ce app.ConfigError
	if errors.As(err, &ce) {
		return exitInvalidConfig
	}
	return exitRuntimeError
}
