// Command aliasdeck is the standalone CLI: it resolves a device's aliases
// from a local source, renders them for the detected shell, and applies
// them to the machine (PROJECT.md §4.1).
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the root command and returns the process exit code. It is
// separated from main so cmd/aliasdeck itself stays testable without
// calling os.Exit.
func run(args []string, stdout, stderr io.Writer) int {
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	cmd, err := root.ExecuteC()
	if err == nil {
		return exitOK
	}

	var ee *exitError
	if errors.As(err, &ee) {
		if ee.err != nil {
			fmt.Fprintln(stderr, "Error:", ee.err)
		}
		return ee.code
	}

	fmt.Fprintln(stderr, "Error:", err)

	// SilenceUsage stays false until a command's own RunE takes over past
	// Cobra's flag/argument validation (see each command's RunE); if it is
	// still false here, the error came from that validation, not from our
	// own logic, so it is a usage error regardless of its text.
	if cmd != nil && !cmd.SilenceUsage {
		return exitUsageError
	}
	return exitCodeFor(err)
}
