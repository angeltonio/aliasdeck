package app

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// EditTarget selects which configuration file `edit` opens.
type EditTarget string

const (
	EditTargetAliases EditTarget = "aliases"
	EditTargetConfig  EditTarget = "config"
)

// EditOptions configures Edit.
type EditOptions struct {
	Options
	// Target defaults to EditTargetAliases when empty.
	Target EditTarget
}

// EditReport records what Edit ran, for informational output.
type EditReport struct {
	Path   string
	Editor string
	Args   []string
}

// ErrEditorNotSet is returned when $EDITOR is unset or blank.
var ErrEditorNotSet = errors.New("$EDITOR is not set; export EDITOR to use `aliasdeck edit`")

// Edit opens the target file in $EDITOR and returns once the editor exits.
// It never syncs, renders, or applies anything as a side effect
// (cli-commands spec, "edit Opens $EDITOR Without Side Effects").
//
// $EDITOR is split on whitespace into an argv and passed directly to
// exec.Command — never to a shell (design decision 8; threat matrix
// "Editor subprocess"). This is what makes `code -w` work while making
// `$EDITOR="x; rm -rf ."` inert: the semicolon and everything after it are
// just literal argv entries for a program named "x;", which LookPath never
// resolves to anything a shell would interpret, so nothing ever executes.
//
// The documented cost is that a quoted path inside $EDITOR (e.g.
// `"/Applications/My Editor" -w`) is split incorrectly on its internal
// space. That is an accepted limitation, not a bug: the only fix would be
// handing $EDITOR to a shell, and a shell is a code-execution vector this
// package refuses to open.
func Edit(_ context.Context, env Env, opts EditOptions) (EditReport, error) {
	dc, err := loadDeviceContext(env, opts.Options)
	if err != nil {
		return EditReport{}, err
	}

	path := dc.AliasesPath
	if opts.Target == EditTargetConfig {
		path = dc.ConfigPath
	}

	editorVar := env.Getenv("EDITOR")
	if strings.TrimSpace(editorVar) == "" {
		return EditReport{}, ErrEditorNotSet
	}

	fields := strings.Fields(editorVar)
	bin, argv := fields[0], fields[1:]

	resolved, err := env.LookPath(bin)
	if err != nil {
		return EditReport{}, fmt.Errorf("editor %q from $EDITOR is not an executable on PATH: %w", bin, err)
	}

	args := append(append([]string{}, argv...), path)
	cmd := exec.Command(resolved, args...)
	cmd.Stdin = env.Stdin
	cmd.Stdout = env.Stdout
	cmd.Stderr = env.Stderr

	if err := cmd.Run(); err != nil {
		return EditReport{}, fmt.Errorf("running editor %q: %w", editorVar, err)
	}

	return EditReport{Path: path, Editor: bin, Args: args}, nil
}
