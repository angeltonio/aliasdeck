package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// promptYesNo asks question on env.Stdout and reads a single line from
// env.Stdin, defaulting to false on an empty answer or EOF so that a
// non-interactive invocation never silently edits a file the user owns.
//
// When stdin is not a terminal the question is not asked at all. Reading it
// would block forever rather than fail: an open pipe that never delivers a
// line is the normal shape of stdin under `curl … | sh`, in a container build
// and in CI, and a hang with no diagnostic is far worse than a declined
// prompt. Callers print how to opt in explicitly.
func promptYesNo(env Env, question string) (bool, error) {
	if !isInteractive(env.Stdin) {
		return false, nil
	}

	fmt.Fprintf(env.Stdout, "%s [y/N]: ", question)

	scanner := bufio.NewScanner(env.Stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, fmt.Errorf("reading confirmation: %w", err)
		}
		return false, nil
	}

	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}

// isInteractive reports whether r is attached to a terminal that a person
// could answer from.
//
// Only an *os.File can be inspected this way. Anything else is a reader a
// caller supplied deliberately — a test buffer, a scripted answer — and is
// treated as interactive so injected input still works.
func isInteractive(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return true
	}

	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
