package main

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/angeltonio/aliasdeck/internal/app"
	"github.com/angeltonio/aliasdeck/internal/watchconfig"
	"github.com/spf13/cobra"
)

func newAgentCmd() *cobra.Command {
	agent := &cobra.Command{Use: "agent", Short: "Manage automatic startup for aliasdeck watch.", Args: cobra.NoArgs}
	agent.AddCommand(newAgentInstallCmd(), newAgentStatusCmd(), newAgentUninstallCmd())
	return agent
}

func newAgentInstallCmd() *cobra.Command {
	var interval time.Duration
	cmd := &cobra.Command{Use: "install", Short: "Install and start the user background agent.", Args: cobra.NoArgs, PreRunE: func(_ *cobra.Command, _ []string) error {
		return watchconfig.Validate(interval)
	}, RunE: func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceUsage = true
		if err := requireAgentOS(runtime.GOOS); err != nil {
			return err
		}
		executable, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolving aliasdeck executable: %w", err)
		}
		status, err := app.AgentInstall(cmd.Context(), app.OSEnv(), app.AgentOptions{Executable: executable, AliasDeckHome: os.Getenv("ALIASDECK_HOME"), Interval: interval})
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "installed and loaded %s\n", status.Path)
		return nil
	}}
	cmd.Flags().DurationVar(&interval, "interval", watchconfig.DefaultInterval, "Synchronization interval persisted in the background agent")
	return cmd
}

func newAgentStatusCmd() *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Show user background agent status.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceUsage = true
		if err := requireAgentOS(runtime.GOOS); err != nil {
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
	var ifExecutable string
	var ifHome string
	var ifInterval time.Duration
	cmd := &cobra.Command{Use: "uninstall", Short: "Stop and remove the user background agent.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceUsage = true
		if err := requireAgentOS(runtime.GOOS); err != nil {
			return err
		}
		env := app.OSEnv()
		if ifExecutable != "" {
			path, removed, err := app.AgentUninstallIfOwned(cmd.Context(), env, app.AgentOptions{Executable: ifExecutable, AliasDeckHome: ifHome, Interval: ifInterval})
			if err != nil {
				return err
			}
			if removed {
				fmt.Fprintf(cmd.OutOrStdout(), "uninstalled %s\n", path)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "no matching background agent removed at %s\n", path)
			}
			return nil
		}
		path, err := app.AgentUninstall(cmd.Context(), env)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "uninstalled %s\n", path)
		return nil
	}}
	cmd.Flags().StringVar(&ifExecutable, "if-executable", "", "remove only an agent using this executable")
	cmd.Flags().StringVar(&ifHome, "if-home", "", "with --if-executable, require this ALIASDECK_HOME")
	cmd.Flags().DurationVar(&ifInterval, "if-interval", watchconfig.DefaultInterval, "with --if-executable, require this watcher interval")
	return cmd
}

func requireAgentOS(goos string) error {
	if goos != "darwin" && goos != "windows" {
		return fmt.Errorf("agent commands are supported only on macOS and Windows")
	}
	return nil
}
