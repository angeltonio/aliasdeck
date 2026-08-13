package main

import (
	"fmt"
	"io"
	"sort"

	"github.com/angeltonio/aliasdeck/internal/app"
	"github.com/angeltonio/aliasdeck/internal/validate"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the active configuration without writing anything.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			env := app.OSEnv()
			report, err := app.Doctor(cmd.Context(), env, app.Options{Shell: shellFlag(cmd)})
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			printDoctorReport(out, report)

			// doctor exits 3 only when it found a blocking issue; warnings
			// alone (including undeclared-profile references) stay exit 0
			// (design, "Paths, Detection, Exit Codes").
			if report.Issues.HasErrors() {
				return &exitError{code: exitInvalidConfig}
			}
			return nil
		},
	}
}

func printDoctorReport(out io.Writer, r app.DoctorReport) {
	fmt.Fprintf(out, "Device:   %s (platform=%s, shell=%s)\n", r.Device.Name, r.Device.Platform, r.Device.Shell)
	fmt.Fprintf(out, "Platform: %s\n", r.PlatformProvenance)
	fmt.Fprintf(out, "Shell:    %s\n", r.ShellProvenance)
	fmt.Fprintf(out, "Source:   %s\n", r.AliasesPath)

	issues := make(validate.Issues, len(r.Issues))
	copy(issues, r.Issues)
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].AliasName != issues[j].AliasName {
			return issues[i].AliasName < issues[j].AliasName
		}
		return issues[i].Field < issues[j].Field
	})

	if len(issues) == 0 {
		fmt.Fprintln(out, "No validation issues found.")
	} else {
		fmt.Fprintf(out, "%d issue(s):\n", len(issues))
		for _, issue := range issues {
			fmt.Fprintf(out, "  %s\n", issue.String())
		}
	}

	if len(r.ProfileWarnings) > 0 {
		fmt.Fprintf(out, "%d profile warning(s):\n", len(r.ProfileWarnings))
		for _, w := range r.ProfileWarnings {
			fmt.Fprintf(out, "  %s\n", w)
		}
	}

	// Other-edition PowerShell profile and stale-GitSource warnings
	// (cli-commands spec, "doctor Diagnoses Without Writing"). These are
	// warnings, not Issues: they never affect doctor's exit code.
	if len(r.Warnings) > 0 {
		fmt.Fprintf(out, "%d warning(s):\n", len(r.Warnings))
		for _, w := range r.Warnings {
			fmt.Fprintf(out, "  %s\n", w)
		}
	}
}
