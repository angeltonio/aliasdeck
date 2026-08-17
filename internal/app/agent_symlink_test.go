//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeAgentOptionsPreservesStableSymlinkAcrossUpgrade(t *testing.T) {
	dir := t.TempDir()
	oldTarget := filepath.Join(dir, "Caskroom", "0.5.3", "aliasdeck")
	newTarget := filepath.Join(dir, "Caskroom", "0.5.4", "aliasdeck")
	stable := filepath.Join(dir, "bin", "aliasdeck")
	for _, target := range []string{oldTarget, newTarget} {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(target), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(stable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldTarget, stable); err != nil {
		t.Fatal(err)
	}

	normalized, err := normalizeAgentOptions(AgentOptions{Executable: stable})
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Executable != stable {
		t.Fatalf("normalized executable = %q, want stable symlink %q", normalized.Executable, stable)
	}

	// Simulate a package-manager upgrade: the old version disappears and the
	// stable path is atomically retargeted to the new version.
	if err := os.Remove(stable); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(oldTarget); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(newTarget, stable); err != nil {
		t.Fatal(err)
	}

	plist := string(renderAgentPlist(normalized))
	if !strings.Contains(plist, "<string>"+stable+"</string>") {
		t.Fatalf("plist lost stable executable path after upgrade:\n%s", plist)
	}
	if strings.Contains(plist, oldTarget) || strings.Contains(plist, newTarget) {
		t.Fatalf("plist captured a versioned target instead of the stable symlink:\n%s", plist)
	}
	resolved, err := filepath.EvalSymlinks(normalized.Executable)
	if err != nil {
		t.Fatal(err)
	}
	wantResolved, err := filepath.EvalSymlinks(newTarget)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != wantResolved {
		t.Fatalf("stable plist path resolves to %q after upgrade, want %q", resolved, wantResolved)
	}
}

func TestNormalizeAgentOptionsMakesRelativeInvocationAbsoluteWithoutResolvingIt(t *testing.T) {
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	normalized, err := normalizeAgentOptions(AgentOptions{Executable: filepath.Join("bin", "aliasdeck")})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(working, "bin", "aliasdeck")
	if normalized.Executable != want || !filepath.IsAbs(normalized.Executable) {
		t.Fatalf("normalized executable = %q, want absolute clean path %q", normalized.Executable, want)
	}
}
