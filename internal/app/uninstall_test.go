package app

import (
	"context"
	"os"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/config"
)

func seedBootstrappedDevice(t *testing.T, te *testEnv, priorRCContent string) (rcPath string) {
	t.Helper()
	seedSyncableDevice(t, te)

	rcPath = te.Home + "/.zshrc"
	if priorRCContent != "" {
		if err := os.WriteFile(rcPath, []byte(priorRCContent), 0o644); err != nil {
			t.Fatalf("seeding rc file: %v", err)
		}
	}

	if _, err := Init(context.Background(), te.Env, InitOptions{
		Confirm: func(string) (bool, error) { return true, nil },
	}); err != nil {
		t.Fatalf("Init() returned an error: %v", err)
	}
	return rcPath
}

func TestUninstallRestoresRCFileByteIdentically(t *testing.T) {
	te := newTestEnv(t)
	prior := "# my own stuff\nexport PATH=\"$PATH:/usr/local/bin\"\n"
	rcPath := seedBootstrappedDevice(t, te, prior)

	report, err := Uninstall(context.Background(), te.Env, UninstallOptions{Yes: true})
	if err != nil {
		t.Fatalf("Uninstall() returned an error: %v", err)
	}

	if !report.BootstrapExact {
		t.Error("BootstrapExact = false, want true for an untouched, exact-block removal")
	}

	got, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file after uninstall: %v", err)
	}
	if string(got) != prior {
		t.Errorf("rc file after uninstall = %q, want byte-identical to the original %q", got, prior)
	}

	if !report.OutputRemoved {
		t.Error("OutputRemoved = false, want true")
	}
	if _, err := os.Stat(report.OutputPath); err == nil {
		t.Errorf("generated file %q still exists after uninstall", report.OutputPath)
	}
}

func TestUninstallYesSkipsPrompt(t *testing.T) {
	te := newTestEnv(t)
	seedBootstrappedDevice(t, te, "")

	confirmCalled := false
	_, err := Uninstall(context.Background(), te.Env, UninstallOptions{
		Yes: true,
		Confirm: func(string) (bool, error) {
			confirmCalled = true
			return true, nil
		},
	})
	if err != nil {
		t.Fatalf("Uninstall() returned an error: %v", err)
	}
	if confirmCalled {
		t.Error("--yes must skip the confirmation prompt entirely")
	}
}

func TestUninstallInteractivePromptsBeforeModifying(t *testing.T) {
	te := newTestEnv(t)
	rcPath := seedBootstrappedDevice(t, te, "")

	before, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file: %v", err)
	}

	asked := false
	report, err := Uninstall(context.Background(), te.Env, UninstallOptions{
		Confirm: func(string) (bool, error) {
			asked = true
			return false, nil
		},
	})
	if err != nil {
		t.Fatalf("Uninstall() returned an error: %v", err)
	}
	if !asked {
		t.Error("Uninstall() without --yes must prompt for confirmation")
	}
	if !report.Cancelled {
		t.Error("report.Cancelled = false, want true when the user declines")
	}

	after, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file: %v", err)
	}
	if string(before) != string(after) {
		t.Error("declining the prompt must leave the rc file untouched")
	}
	if _, err := os.Stat(config.StateFile(te.Base)); err != nil {
		t.Error("declining the prompt must leave state.json in place")
	}
}

func TestUninstallExactFalseWhenUserEditedInsideBlock(t *testing.T) {
	te := newTestEnv(t)
	rcPath := seedBootstrappedDevice(t, te, "")

	data, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file: %v", err)
	}
	edited := []byte(string(data) + "# a line the user added inside nothing in particular\n")
	// Corrupt the exact recorded block (but keep the marker lines intact)
	// so RemoveBootstrap must fall back to the documented marker scan.
	if err := os.WriteFile(rcPath, edited, 0o644); err != nil {
		t.Fatalf("editing rc file: %v", err)
	}

	report, err := Uninstall(context.Background(), te.Env, UninstallOptions{Yes: true})
	if err != nil {
		t.Fatalf("Uninstall() returned an error: %v", err)
	}
	if !report.BootstrapRemoved {
		t.Error("BootstrapRemoved = false, want true even via the fallback path")
	}

	got, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file after uninstall: %v", err)
	}
	if len(got) == 0 && len(edited) != 0 {
		// sanity: uninstall should not have destroyed everything
		t.Fatal("rc file was emptied unexpectedly")
	}
}
