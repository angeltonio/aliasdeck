package main

import (
	"fmt"

	"github.com/spf13/cobra"

	// Blank-imported to measure its cost on the shipped binary now, before
	// any behavior depends on it (Milestone 4, open item 1.6 / proposal
	// risk "embedding the server grows the binary every standalone user
	// downloads"). Phase 4 replaces this stub with server.Run wiring.
	_ "modernc.org/sqlite"
)

// newServeCmd is a Phase 1 stub: it exists so `aliasdeck serve` links
// modernc.org/sqlite into the release binary and the 25 MB CI budget (open
// item 1.6) can be measured against a real build from the first commit on.
// Phase 4 gives it an actual server.Run.
func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the AliasDeck server (not implemented yet).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			fmt.Fprintln(cmd.OutOrStdout(), "not implemented")
			return nil
		},
	}
}
