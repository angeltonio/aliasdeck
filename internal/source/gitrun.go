package source

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RunGit is GitSource's default Run implementation: `git -C <dir> <args>`,
// executed directly via exec.CommandContext — never a shell, so an argv
// element containing shell metacharacters is always one literal argument,
// never a second command — with the environment design decision 15
// requires so a credential prompt can never hang sync forever (the same
// class of failure this project already fixed for the stdin prompt).
func RunGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
