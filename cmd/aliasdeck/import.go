package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/angeltonio/aliasdeck/internal/app"
	"github.com/spf13/cobra"
)

func newImportCmd() *cobra.Command {
	var from string
	var write bool

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import alias definitions from a shell startup file.",
		Long: `Read the alias definitions out of a shell startup file and merge them
into aliases.yaml.

The file is parsed, never sourced: nothing in it is executed. An alias built
at run time is therefore invisible to this command, which is the intended
trade — reading a config file should not mean running a program.

Without --write nothing is modified. The first run tells you what it found,
what it could not translate, and what would collide.

An existing alias of the same name is never overwritten.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			report, err := app.Import(cmd.Context(), app.OSEnv(), app.ImportOptions{
				Options: app.Options{Shell: shellFlag(cmd)},
				From:    from,
				Write:   write,
			})
			if err != nil {
				return err
			}

			printImportReport(cmd.OutOrStdout(), report)
			return nil
		},
	}

	cmd.Flags().StringVar(&from, "from", "", "startup file to read (default: this device's rc file)")
	cmd.Flags().BoolVar(&write, "write", false, "merge the imported aliases into aliases.yaml")
	return cmd
}

func printImportReport(w io.Writer, r app.ImportReport) {
	fmt.Fprintf(w, "Read %s\n", r.SourcePath)

	if len(r.New) > 0 {
		verb := "Would import"
		if r.Written {
			verb = "Imported"
		}
		fmt.Fprintf(w, "\n%s %d %s:\n", verb, len(r.New), plural(len(r.New), "alias", "aliases"))
		width := 0
		for _, a := range r.New {
			if len(a.Name) > width {
				width = len(a.Name)
			}
		}
		for _, a := range r.New {
			fmt.Fprintf(w, "  %-*s  %s\n", width, a.Name, a.Command)
		}
	}

	if len(r.Unchanged) > 0 {
		fmt.Fprintf(w, "\nAlready in aliases.yaml, unchanged: %s\n", strings.Join(r.Unchanged, ", "))
	}

	// Conflicts come before skips because they are the only part of the
	// report that needs a decision from the reader.
	if len(r.Conflicts) > 0 {
		fmt.Fprintf(w, "\n%d %s already used by a different command — not imported:\n",
			len(r.Conflicts), plural(len(r.Conflicts), "name", "names"))
		for _, c := range r.Conflicts {
			fmt.Fprintf(w, "  %s (line %d)\n", c.Name, c.Line)
			fmt.Fprintf(w, "      aliases.yaml: %s\n", c.Existing)
			fmt.Fprintf(w, "      this file:    %s\n", c.Incoming)
		}
	}

	if len(r.Notes) > 0 {
		fmt.Fprintf(w, "\nImported, but read this:\n")
		for _, n := range r.Notes {
			fmt.Fprintf(w, "  %s (line %d): %s\n", n.Name, n.Line, n.Note)
		}
	}

	if len(r.Skipped) > 0 {
		fmt.Fprintf(w, "\nSkipped %d %s:\n", len(r.Skipped), plural(len(r.Skipped), "line", "lines"))
		for _, s := range r.Skipped {
			fmt.Fprintf(w, "  line %d: %s\n", s.Line, s.Reason)
		}
	}

	switch {
	case r.Written:
		fmt.Fprintf(w, "\nWrote %s. Run `aliasdeck sync` to apply them.\n", r.AliasesPath)
	case len(r.New) > 0:
		fmt.Fprintf(w, "\nNothing was written. Re-run with --write to merge into %s.\n", r.AliasesPath)
	default:
		fmt.Fprintf(w, "\nNothing new to import.\n")
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
