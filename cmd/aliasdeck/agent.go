package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/angeltonio/aliasdeck/internal/app"
	"github.com/spf13/cobra"
)

func newAgentCmd() *cobra.Command {
	agent := &cobra.Command{Use: "agent", Short: "Manage automatic startup for aliasdeck watch.", Args: cobra.NoArgs}
	agent.AddCommand(newAgentInstallCmd(), newAgentStatusCmd(), newAgentUninstallCmd())
	return agent
}

func newAgentInstallCmd() *cobra.Command {
	return &cobra.Command{Use: "install", Short: "Install and start the macOS LaunchAgent.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceUsage = true
		if err := requireMacOS(); err != nil {
			return err
		}
		executable, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolving aliasdeck executable: %w", err)
		}
		status, err := app.AgentInstall(cmd.Context(), app.OSEnv(), app.AgentOptions{Executable: executable})
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "installed and loaded %s\n", status.Path)
		return nil
	}}
}

func newAgentStatusCmd() *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Show macOS LaunchAgent status.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceUsage = true
		if err := requireMacOS(); err != nil {
			return err
		}
		status, err := app.AgentStatusFor(cmd.Context(), app.OSEnv())
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "path: %s\ninstalled: %t\nloaded: %t\n", status.Path, status.Installed, status.Loaded)
		return nil
	}}
}

func newAgentUninstallCmd() *cobra.Command {
	return &cobra.Command{Use: "uninstall", Short: "Stop and remove the macOS LaunchAgent.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceUsage = true
		if err := requireMacOS(); err != nil {
			return err
		}
		path, err := app.AgentUninstall(cmd.Context(), app.OSEnv())
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "uninstalled %s\n", path)
		return nil
	}}
}

func requireMacOS() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("agent commands are supported only on macOS")
	}
	return nil
}
