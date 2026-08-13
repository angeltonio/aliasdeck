package source

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// RunGit is GitSource's default Run implementation: `git -C <dir> <args>`,
// executed directly via exec.CommandContext — never a shell, so an argv
// element containing shell metacharacters is always one literal argument,
// never a second command — with the environment design decision 15
// requires so a credential prompt can never hang sync forever (the same
// class of failure this project already fixed for the stdin prompt).
// GitTimeout bounds a single git invocation.
//
// The non-interactive environment below stops a credential prompt from
// hanging; it does nothing about a network that accepts a connection and then
// says nothing, which is what a firewall silently dropping packets, a captive
// portal, or a half-open connection looks like. Measured: without this, a sync
// against such a remote produced no output and never returned.
//
// This project has now fixed the same shape of failure three times — a prompt
// reading a pipe that never delivers, a curl with no timeout in the install
// script, and this. The pattern is always an operation that waits on something
// that may never arrive, in a program a script is expected to call.
const GitTimeout = 2 * time.Minute

func RunGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, GitTimeout)
	defer cancel()

	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)

	// Killing git is not enough to unblock Run.
	//
	// exec.CommandContext kills the process when the deadline passes, but Run
	// then waits for the output pipes to close, and git's own transport
	// helpers inherit those handles. A killed git can leave a grandchild
	// holding the write end open, and the copying goroutine blocks forever on
	// a read that will never return.
	//
	// Measured on Windows: the timeout below fired, git died, and the test
	// still hung until Go's own ten-minute panic. WaitDelay bounds that
	// second wait and force-closes the pipes.
	cmd.WaitDelay = 10 * time.Second
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=10",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// A timeout reads as a bare "signal: killed" otherwise, which tells
		// the user nothing about what was waited on or for how long.
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("git %s: gave up after %s with no response from the remote",
				strings.Join(args, " "), GitTimeout)
		}
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
