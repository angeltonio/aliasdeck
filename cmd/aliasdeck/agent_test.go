package main

import (
	"strings"
	"testing"
	"time"
)

func TestAgentInstallIntervalDefaultsAndValidation(t *testing.T) {
	cmd := newAgentInstallCmd()
	got, err := cmd.Flags().GetDuration("interval")
	if err != nil {
		t.Fatal(err)
	}
	if got != 30*time.Second {
		t.Fatalf("default agent interval = %s, want 30s", got)
	}

	cmd.SetArgs([]string{"--interval=500ms"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "between 1s and 24h0m0s") {
		t.Fatalf("unsafe interval error = %v, want bounded validation", err)
	}
}

func TestAgentCommandSupportsMacOSAndWindowsOnly(t *testing.T) {
	for _, goos := range []string{"darwin", "windows"} {
		if err := requireAgentOS(goos); err != nil {
			t.Fatalf("requireAgentOS(%q) = %v, want nil", goos, err)
		}
	}
	if err := requireAgentOS("linux"); err == nil || !strings.Contains(err.Error(), "macOS and Windows") {
		t.Fatalf("requireAgentOS(linux) = %v, want unsupported-platform error", err)
	}
}
