package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testGeneratedPath = "/home/user/.config/aliasdeck/aliases.zsh"
	testHome          = "/home/user"
)

func TestBootstrapLine(t *testing.T) {
	tests := []struct {
		name          string
		generatedPath string
		home          string
		want          string
	}{
		{
			name:          "path under home becomes $HOME-relative",
			generatedPath: testGeneratedPath,
			home:          testHome,
			want:          `[ -f "$HOME/.config/aliasdeck/aliases.zsh" ] && . "$HOME/.config/aliasdeck/aliases.zsh"`,
		},
		{
			name:          "path outside home is used verbatim",
			generatedPath: "/etc/aliasdeck/aliases.zsh",
			home:          testHome,
			want:          `[ -f "/etc/aliasdeck/aliases.zsh" ] && . "/etc/aliasdeck/aliases.zsh"`,
		},
		{
			name:          "prefix collision is not mistaken for a home-relative path",
			generatedPath: "/home/user2/aliases.zsh",
			home:          "/home/user",
			want:          `[ -f "/home/user2/aliases.zsh" ] && . "/home/user2/aliases.zsh"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BootstrapLine(tt.generatedPath, tt.home); got != tt.want {
				t.Errorf("BootstrapLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAddBootstrapFixtures(t *testing.T) {
	tests := []struct {
		name    string
		content string // "" means the rc file does not exist yet
		create  bool
	}{
		{name: "trailing newline", content: "export PATH=$PATH:/usr/local/bin\n", create: true},
		{name: "no trailing newline", content: "export PATH=$PATH:/usr/local/bin", create: true},
		{name: "empty file", content: "", create: true},
		{name: "file does not exist", content: "", create: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			rcPath := filepath.Join(dir, ".zshrc")
			if tt.create {
				if err := os.WriteFile(rcPath, []byte(tt.content), 0o644); err != nil {
					t.Fatalf("seeding rc file: %v", err)
				}
			}

			block, err := AddBootstrap(rcPath, testGeneratedPath, testHome)
			if err != nil {
				t.Fatalf("AddBootstrap() returned an error: %v", err)
			}
			if block == "" {
				t.Fatal("AddBootstrap() must return the exact appended block on first insertion")
			}

			got, err := os.ReadFile(rcPath)
			if err != nil {
				t.Fatalf("reading rc file: %v", err)
			}

			if string(got) != tt.content+block {
				t.Errorf("rc file content = %q, want %q", got, tt.content+block)
			}
			if strings.Count(string(got), beginMarker) != 1 {
				t.Errorf("rc file must contain exactly one bootstrap marker, got %d", strings.Count(string(got), beginMarker))
			}
			if !strings.HasSuffix(string(got), "\n") {
				t.Error("the appended block must end with a trailing newline")
			}
		})
	}
}

func TestAddBootstrapIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".zshrc")
	if err := os.WriteFile(rcPath, []byte("existing content\n"), 0o644); err != nil {
		t.Fatalf("seeding rc file: %v", err)
	}

	firstBlock, err := AddBootstrap(rcPath, testGeneratedPath, testHome)
	if err != nil {
		t.Fatalf("first AddBootstrap() returned an error: %v", err)
	}
	afterFirst, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file after first add: %v", err)
	}

	secondBlock, err := AddBootstrap(rcPath, testGeneratedPath, testHome)
	if err != nil {
		t.Fatalf("second AddBootstrap() returned an error: %v", err)
	}
	if secondBlock != "" {
		t.Errorf("second AddBootstrap() block = %q, want empty string (no-op)", secondBlock)
	}

	afterSecond, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file after second add: %v", err)
	}
	if string(afterFirst) != string(afterSecond) {
		t.Errorf("repeated init must not duplicate the bootstrap block:\nafter first:  %q\nafter second: %q", afterFirst, afterSecond)
	}
	if firstBlock == "" {
		t.Fatal("first AddBootstrap() must return a non-empty block")
	}
}

func TestAddBootstrapNoOpsOnManuallyCraftedPreExistingBlock(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".zshrc")
	// A block that was never produced by AddBootstrap itself, e.g. hand-edited
	// or copied from another machine's rc file.
	preExisting := "some line\n" + beginMarker + "\ncustom sourcing line\n" + endMarker + "\n"
	if err := os.WriteFile(rcPath, []byte(preExisting), 0o644); err != nil {
		t.Fatalf("seeding rc file: %v", err)
	}

	block, err := AddBootstrap(rcPath, testGeneratedPath, testHome)
	if err != nil {
		t.Fatalf("AddBootstrap() returned an error: %v", err)
	}
	if block != "" {
		t.Errorf("AddBootstrap() block = %q, want empty string when a marker is already present", block)
	}

	got, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file: %v", err)
	}
	if string(got) != preExisting {
		t.Errorf("rc file must be left untouched when already bootstrapped:\ngot:  %q\nwant: %q", got, preExisting)
	}
}

func TestRemoveBootstrapExactByteRestore(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".zshrc")
	original := "# my zshrc\nexport EDITOR=vim\n"
	if err := os.WriteFile(rcPath, []byte(original), 0o644); err != nil {
		t.Fatalf("seeding rc file: %v", err)
	}

	block, err := AddBootstrap(rcPath, testGeneratedPath, testHome)
	if err != nil {
		t.Fatalf("AddBootstrap() returned an error: %v", err)
	}

	exact, err := RemoveBootstrap(rcPath, block)
	if err != nil {
		t.Fatalf("RemoveBootstrap() returned an error: %v", err)
	}
	if !exact {
		t.Error("RemoveBootstrap() must report an exact restore when the recorded block matches verbatim")
	}

	got, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file: %v", err)
	}
	if string(got) != original {
		t.Errorf("rc file after full install/uninstall cycle = %q, want byte-identical %q", got, original)
	}
}

func TestRemoveBootstrapFallsBackWhenUserEditedInsideBlock(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".zshrc")
	original := "# my zshrc\nexport EDITOR=vim\n"
	if err := os.WriteFile(rcPath, []byte(original), 0o644); err != nil {
		t.Fatalf("seeding rc file: %v", err)
	}

	recordedBlock, err := AddBootstrap(rcPath, testGeneratedPath, testHome)
	if err != nil {
		t.Fatalf("AddBootstrap() returned an error: %v", err)
	}

	// The user hand-edits the sourcing line inside the marker block, so the
	// exact recordedBlock bytes no longer appear anywhere in the file.
	edited, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file: %v", err)
	}
	editedContent := strings.Replace(string(edited),
		BootstrapLine(testGeneratedPath, testHome), "# user note: I customized this", 1)
	if editedContent == string(edited) {
		t.Fatal("test setup did not actually edit the sourcing line")
	}
	if err := os.WriteFile(rcPath, []byte(editedContent), 0o644); err != nil {
		t.Fatalf("writing edited rc file: %v", err)
	}

	exact, err := RemoveBootstrap(rcPath, recordedBlock)
	if err != nil {
		t.Fatalf("RemoveBootstrap() returned an error: %v", err)
	}
	if exact {
		t.Error("RemoveBootstrap() must report a non-exact restore when it falls back to marker scanning")
	}

	got, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file: %v", err)
	}
	if string(got) != original {
		t.Errorf("marker-scan fallback must still restore the surrounding content exactly:\ngot:  %q\nwant: %q", got, original)
	}
}

func TestBootstrapSymlinkedRCStaysSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real-zshrc")
	if err := os.WriteFile(real, []byte("original content\n"), 0o644); err != nil {
		t.Fatalf("seeding real rc file: %v", err)
	}
	rcPath := filepath.Join(dir, ".zshrc")
	if err := os.Symlink(real, rcPath); err != nil {
		t.Fatalf("creating symlink: %v", err)
	}

	block, err := AddBootstrap(rcPath, testGeneratedPath, testHome)
	if err != nil {
		t.Fatalf("AddBootstrap() returned an error: %v", err)
	}

	info, err := os.Lstat(rcPath)
	if err != nil {
		t.Fatalf("lstat rc path: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("AddBootstrap() must not replace a symlinked rc file with a regular file")
	}
	target, err := os.Readlink(rcPath)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != real {
		t.Errorf("symlink target = %q, want %q (must be preserved)", target, real)
	}

	content, err := os.ReadFile(real)
	if err != nil {
		t.Fatalf("reading real target: %v", err)
	}
	if string(content) != "original content\n"+block {
		t.Errorf("real target content = %q, want the block appended", content)
	}

	exact, err := RemoveBootstrap(rcPath, block)
	if err != nil {
		t.Fatalf("RemoveBootstrap() returned an error: %v", err)
	}
	if !exact {
		t.Error("RemoveBootstrap() must exactly restore a symlinked rc file")
	}

	info, err = os.Lstat(rcPath)
	if err != nil {
		t.Fatalf("lstat rc path after remove: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("RemoveBootstrap() must not replace a symlinked rc file with a regular file")
	}
}

func TestRemoveBootstrapMarkerLikeTextInHostileRCNotCorrupted(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "marker text embedded mid-line",
			content: `echo "before # >>> aliasdeck >>> after"` + "\nsome other content\n",
		},
		{
			name:    "unmatched begin marker with no end marker",
			content: beginMarker + "\nsomething but no closing marker\n",
		},
		{
			name:    "end marker appears before begin marker",
			content: endMarker + "\nunrelated\n" + beginMarker + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			rcPath := filepath.Join(dir, ".zshrc")
			if err := os.WriteFile(rcPath, []byte(tt.content), 0o644); err != nil {
				t.Fatalf("seeding hostile rc file: %v", err)
			}

			exact, err := RemoveBootstrap(rcPath, "")
			if err != nil {
				t.Fatalf("RemoveBootstrap() returned an error: %v", err)
			}
			if !exact {
				t.Error("RemoveBootstrap() must report exact when it finds no well-formed marker pair and leaves the file untouched")
			}

			got, err := os.ReadFile(rcPath)
			if err != nil {
				t.Fatalf("reading rc file: %v", err)
			}
			if string(got) != tt.content {
				t.Errorf("hostile rc content must be left untouched:\ngot:  %q\nwant: %q", got, tt.content)
			}
		})
	}
}
