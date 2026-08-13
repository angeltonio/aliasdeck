package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

func TestNativeBackendOutputPath(t *testing.T) {
	tests := []struct {
		shell domain.Shell
		ext   string
	}{
		{domain.ShellZsh, "zsh"},
		{domain.ShellBash, "bash"},
		{domain.ShellPowerShell, "ps1"},
	}

	backend := NativeBackend{Base: "/home/user/.config/aliasdeck"}
	for _, tt := range tests {
		t.Run(string(tt.shell), func(t *testing.T) {
			got, err := backend.OutputPath(domain.Device{Shell: tt.shell})
			if err != nil {
				t.Fatalf("OutputPath() returned an error: %v", err)
			}
			want := filepath.Join("/home/user/.config/aliasdeck", "aliases."+tt.ext)
			if got != want {
				t.Errorf("OutputPath() = %q, want %q", got, want)
			}
		})
	}
}

// TestNativeBackendOutputPathUnsupportedShell pins the "no extension defined"
// error path itself, now using a shell outside domain.AllShells (every real
// shell — zsh, bash, powershell — has a defined extension as of this
// change; see the inverted PowerShell case in TestNativeBackendOutputPath
// above, native-apply spec "PowerShell Output File").
func TestNativeBackendOutputPathUnsupportedShell(t *testing.T) {
	backend := NativeBackend{Base: "/home/user/.config/aliasdeck"}
	if _, err := backend.OutputPath(domain.Device{Shell: domain.Shell("fish")}); err == nil {
		t.Fatal("OutputPath() must return an error for a shell with no generated-file extension")
	}
}

func TestNativeBackendApplyHappyPath(t *testing.T) {
	dir := t.TempDir()
	backend := NativeBackend{Base: dir}
	cfg := domain.ResolvedConfig{
		Device:      domain.Device{Shell: domain.ShellZsh},
		GeneratedAt: time.Now(),
	}
	rendered := "alias ll='ls -la'\n"

	if err := backend.Apply(context.Background(), cfg, rendered); err != nil {
		t.Fatalf("Apply() returned an error: %v", err)
	}

	path, err := backend.OutputPath(cfg.Device)
	if err != nil {
		t.Fatalf("OutputPath() returned an error: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading generated file: %v", err)
	}
	if string(got) != rendered {
		t.Errorf("generated file content = %q, want %q", got, rendered)
	}
}

func TestNativeBackendApplyNoPartialWriteOnInterruption(t *testing.T) {
	dir := t.TempDir()
	backend := NativeBackend{Base: dir}
	cfg := domain.ResolvedConfig{Device: domain.Device{Shell: domain.ShellZsh}}
	path, err := backend.OutputPath(cfg.Device)
	if err != nil {
		t.Fatalf("OutputPath() returned an error: %v", err)
	}

	priorContent := "alias existing='echo prior valid content'\n"
	if err := os.WriteFile(path, []byte(priorContent), 0o644); err != nil {
		t.Fatalf("seeding prior generated file: %v", err)
	}

	original := osRename
	osRename = func(oldpath, newpath string) error {
		return errors.New("forced interruption before rename")
	}
	defer func() { osRename = original }()

	if err := backend.Apply(context.Background(), cfg, "alias new='should never land'\n"); err == nil {
		t.Fatal("Apply() must propagate the interruption")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading generated file after interruption: %v", err)
	}
	if string(got) != priorContent {
		t.Errorf("generated file after an interrupted apply = %q, want the untouched prior valid content %q", got, priorContent)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading dir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("interrupted apply left a temp file behind: %s", e.Name())
		}
	}
}

func TestChezmoiBackendFailsExplicitly(t *testing.T) {
	backend := ChezmoiBackend{}
	dev := domain.Device{Shell: domain.ShellZsh}
	cfg := domain.ResolvedConfig{Device: dev}

	if _, err := backend.OutputPath(dev); err == nil {
		t.Fatal("ChezmoiBackend.OutputPath() must return an error")
	} else if !strings.Contains(err.Error(), "not implemented in v0.1") {
		t.Errorf("OutputPath() error = %q, want it to mention \"not implemented in v0.1\"", err)
	}

	err := backend.Apply(context.Background(), cfg, "alias x='y'\n")
	if err == nil {
		t.Fatal("ChezmoiBackend.Apply() must fail rather than silently no-op")
	}
	if !strings.Contains(err.Error(), "not implemented in v0.1") {
		t.Errorf("Apply() error = %q, want it to mention \"not implemented in v0.1\"", err)
	}
}

func TestBackendsSatisfySyncBackendInterface(t *testing.T) {
	var _ SyncBackend = NativeBackend{}
	var _ SyncBackend = ChezmoiBackend{}
}
