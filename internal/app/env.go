// Package app holds one use case per AliasDeck command (init, sync, status,
// list, doctor, edit, uninstall), each returning a report struct that
// cmd/aliasdeck prints and maps to an exit code.
//
// Every use case is a function over Env rather than the real operating
// system, so every command is table-driven testable under t.TempDir()
// without touching the real machine (design decision 1).
package app

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/angeltonio/aliasdeck/internal/config"
)

// Env supplies every side-effecting input a use case needs: I/O streams,
// environment variables, the user's home directory, the current time, and
// PATH resolution.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Getenv  func(key string) string
	HomeDir func() (string, error)
	Now     func() time.Time

	// LookPath resolves an executable name to a path, mirroring
	// exec.LookPath. Edit uses it to validate $EDITOR's binary before ever
	// invoking exec.Command, so an unresolvable editor fails with a clear
	// error rather than a raw "file not found" from the OS.
	LookPath func(file string) (string, error)

	// RunCommand executes a process. Agent lifecycle operations inject this
	// boundary in tests instead of invoking launchctl.
	RunCommand func(ctx context.Context, name string, args ...string) ([]byte, error)
	UserID     func() int
	MkdirAll   func(path string, perm os.FileMode) error
	WriteFile  func(path string, data []byte, perm os.FileMode) error
	Remove     func(path string) error
	Stat       func(path string) (os.FileInfo, error)
}

// OSEnv returns an Env backed by the real process: os.Stdin/Stdout/Stderr,
// os.Getenv, os.UserHomeDir, time.Now, and exec.LookPath.
func OSEnv() Env {
	return Env{
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Getenv:   os.Getenv,
		HomeDir:  os.UserHomeDir,
		Now:      time.Now,
		LookPath: exec.LookPath,
		RunCommand: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
		UserID:    os.Getuid,
		MkdirAll:  os.MkdirAll,
		WriteFile: os.WriteFile,
		Remove:    os.Remove,
		Stat:      os.Stat,
	}
}

// ConfigEnv adapts Env to config.Env: every command resolves paths through
// internal/config, which only needs Getenv and HomeDir.
func (e Env) ConfigEnv() config.Env {
	return config.Env{Getenv: e.Getenv, HomeDir: e.HomeDir}
}
