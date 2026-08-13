package apply

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

const (
	testGeneratedPath = "/home/user/.config/aliasdeck/aliases.zsh"
	testHome          = "/home/user"
)

func TestBootstrapLine(t *testing.T) {
	tests := []struct {
		name          string
		shell         domain.Shell
		generatedPath string
		home          string
		want          string
	}{
		{
			name:          "zsh: path under home becomes $HOME-relative",
			shell:         domain.ShellZsh,
			generatedPath: testGeneratedPath,
			home:          testHome,
			want:          `[ -f "$HOME/.config/aliasdeck/aliases.zsh" ] && . "$HOME/.config/aliasdeck/aliases.zsh"`,
		},
		{
			name:          "zsh: path outside home is used verbatim",
			shell:         domain.ShellZsh,
			generatedPath: "/etc/aliasdeck/aliases.zsh",
			home:          testHome,
			want:          `[ -f "/etc/aliasdeck/aliases.zsh" ] && . "/etc/aliasdeck/aliases.zsh"`,
		},
		{
			name:          "zsh: prefix collision is not mistaken for a home-relative path",
			shell:         domain.ShellZsh,
			generatedPath: "/home/user2/aliases.zsh",
			home:          "/home/user",
			want:          `[ -f "/home/user2/aliases.zsh" ] && . "/home/user2/aliases.zsh"`,
		},
		{
			// bash shares the POSIX branch byte-for-byte with zsh (design
			// decision 3): the guard and the quoting are shell-syntax
			// identical, only the shell detected by AliasDeck differs.
			name:          "bash: identical POSIX form to zsh",
			shell:         domain.ShellBash,
			generatedPath: testGeneratedPath,
			home:          testHome,
			want:          `[ -f "$HOME/.config/aliasdeck/aliases.zsh" ] && . "$HOME/.config/aliasdeck/aliases.zsh"`,
		},
		{
			name:          "zsh: home empty uses the path verbatim",
			shell:         domain.ShellZsh,
			generatedPath: testGeneratedPath,
			home:          "",
			want:          `[ -f "/home/user/.config/aliasdeck/aliases.zsh" ] && . "/home/user/.config/aliasdeck/aliases.zsh"`,
		},
		{
			// Windows-shaped path (design decision 4, Defect A). generatedPath
			// and home are built the way config.Base()/config.ExpandPath
			// actually build them: with filepath.Join, so on a real Windows
			// build they contain '\' and this exact test — unmodified —
			// exercises the true separator-sensitive branch of
			// filepath.Rel. On this non-Windows CI host, filepath.Join uses
			// '/'; the algorithm under test (filepath.Rel, reject a
			// ".."-bearing result, filepath.ToSlash) is otherwise identical,
			// so it is proven here and will be proven again, unchanged, once
			// Phase 8 runs this suite under GOOS=windows.
			name:          "powershell: Windows-shaped path under $HOME becomes $HOME-relative with forward slashes",
			shell:         domain.ShellPowerShell,
			generatedPath: filepath.Join("C:", "Users", "bob", ".config", "aliasdeck", "aliases.ps1"),
			home:          filepath.Join("C:", "Users", "bob"),
			want:          `if (Test-Path -LiteralPath "$HOME/.config/aliasdeck/aliases.ps1") { . "$HOME/.config/aliasdeck/aliases.ps1" }`,
		},
		{
			name:          "powershell: Windows-shaped path outside $HOME is used verbatim",
			shell:         domain.ShellPowerShell,
			generatedPath: filepath.Join("C:", "Windows", "aliasdeck", "aliases.ps1"),
			home:          filepath.Join("C:", "Users", "bob"),
			want: fmt.Sprintf(`if (Test-Path -LiteralPath %q) { . %q }`,
				filepath.Join("C:", "Windows", "aliasdeck", "aliases.ps1"),
				filepath.Join("C:", "Windows", "aliasdeck", "aliases.ps1")),
		},
		{
			name:          "powershell: home empty uses the path verbatim",
			shell:         domain.ShellPowerShell,
			generatedPath: filepath.Join("C:", "Users", "bob", "aliases.ps1"),
			home:          "",
			want: fmt.Sprintf(`if (Test-Path -LiteralPath %q) { . %q }`,
				filepath.Join("C:", "Users", "bob", "aliases.ps1"),
				filepath.Join("C:", "Users", "bob", "aliases.ps1")),
		},
		{
			// Double-quoted-context escaper (design decision 5), applied
			// only to the part after the literal "$HOME/" prefix so $HOME
			// itself keeps expanding as a PowerShell variable rather than
			// being escaped to a literal "`$HOME".
			name:          "powershell: backtick, double-quote and dollar in the relative remainder are escaped",
			shell:         domain.ShellPowerShell,
			generatedPath: filepath.Join("C:", "Users", "bob", "weird`n\"a$b", "aliases.ps1"),
			home:          filepath.Join("C:", "Users", "bob"),
			want:          "if (Test-Path -LiteralPath \"$HOME/weird``n\"\"a`$b/aliases.ps1\") { . \"$HOME/weird``n\"\"a`$b/aliases.ps1\" }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BootstrapLine(tt.shell, tt.generatedPath, tt.home); got != tt.want {
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

			block, err := AddBootstrap(rcPath, domain.ShellZsh, testGeneratedPath, testHome)
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

	firstBlock, err := AddBootstrap(rcPath, domain.ShellZsh, testGeneratedPath, testHome)
	if err != nil {
		t.Fatalf("first AddBootstrap() returned an error: %v", err)
	}
	afterFirst, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file after first add: %v", err)
	}

	secondBlock, err := AddBootstrap(rcPath, domain.ShellZsh, testGeneratedPath, testHome)
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

	block, err := AddBootstrap(rcPath, domain.ShellZsh, testGeneratedPath, testHome)
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

	block, err := AddBootstrap(rcPath, domain.ShellZsh, testGeneratedPath, testHome)
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

	recordedBlock, err := AddBootstrap(rcPath, domain.ShellZsh, testGeneratedPath, testHome)
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
		BootstrapLine(domain.ShellZsh, testGeneratedPath, testHome), "# user note: I customized this", 1)
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

	block, err := AddBootstrap(rcPath, domain.ShellZsh, testGeneratedPath, testHome)
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

// TestDetectEOL pins design decision 6: the block appended to an rc file
// must match that file's own line-ending convention, never the rendering
// machine's. CRLF is picked only when the existing content already
// contains one; every other case — including "no content yet" — defaults
// to a plain LF.
func TestDetectEOL(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		want     string
	}{
		{name: "empty content defaults to LF", existing: "", want: "\n"},
		{name: "LF-only content stays LF", existing: "alias a='b'\nalias c='d'\n", want: "\n"},
		{name: "CRLF content is detected", existing: "alias a='b'\r\nalias c='d'\r\n", want: "\r\n"},
		{name: "one CRLF among mostly LF still counts", existing: "alias a='b'\nalias c='d'\r\n", want: "\r\n"},
		{name: "no trailing newline at all defaults to LF", existing: "alias a='b'", want: "\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectEOL([]byte(tt.existing)); got != tt.want {
				t.Errorf("detectEOL(%q) = %q, want %q", tt.existing, got, tt.want)
			}
		})
	}
}

// TestBootstrapCRLFAddRemoveRoundTrip is the focused counterpart to
// roundtrip_test.go's realistic-file CRLF case: a $PROFILE-shaped CRLF file,
// added to and then removed from, must come back byte-identical, including
// every CRLF (native-apply spec, "CRLF $PROFILE restored byte-identically").
func TestBootstrapCRLFAddRemoveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, "profile.ps1")
	original := "Set-PSReadLineOption -EditMode Emacs\r\nSet-Alias ll Get-ChildItem\r\n"
	if err := os.WriteFile(rcPath, []byte(original), 0o644); err != nil {
		t.Fatalf("seeding CRLF profile: %v", err)
	}

	generated := filepath.Join(dir, ".config", "aliasdeck", "aliases.ps1")
	block, err := AddBootstrap(rcPath, domain.ShellPowerShell, generated, dir)
	if err != nil {
		t.Fatalf("AddBootstrap: %v", err)
	}
	if !strings.Contains(block, "\r\n") {
		t.Fatalf("block appended to a CRLF file must itself use CRLF, got %q", block)
	}
	if strings.Contains(strings.ReplaceAll(block, "\r\n", ""), "\n") {
		t.Fatalf("block must not mix bare LF into a CRLF file, got %q", block)
	}

	exact, err := RemoveBootstrap(rcPath, block)
	if err != nil {
		t.Fatalf("RemoveBootstrap: %v", err)
	}
	if !exact {
		t.Error("RemoveBootstrap() must report an exact restore for the unedited recorded block")
	}

	restored, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading restored profile: %v", err)
	}
	if string(restored) != original {
		t.Errorf("CRLF profile not restored byte-identically\noriginal: %q\nrestored: %q", original, restored)
	}
}

// TestRemoveBootstrapMarkerScanFallbackOnCRLFProfile is the proof the
// ordering rule in tasks.md 4.6/4.7 exists for: indexOfLine (bootstrap.go)
// required content[end] == '\n' to recognize a marker's end-of-line, which a
// CRLF-terminated marker line fails. That fallback was latent only because
// AliasDeck always wrote LF markers, even into a CRLF file; preserving CRLF
// (design decision 6) is exactly what activates it, so decision 7's fix
// must land in the same change.
//
// This test forces the marker-scan fallback (by editing inside the block, so
// the exact recorded bytes no longer match) on a CRLF $PROFILE, and proves
// the block is still found and removed, with the user's own content intact.
func TestRemoveBootstrapMarkerScanFallbackOnCRLFProfile(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, "profile.ps1")
	original := "Set-PSReadLineOption -EditMode Emacs\r\nSet-Alias ll Get-ChildItem\r\n"
	if err := os.WriteFile(rcPath, []byte(original), 0o644); err != nil {
		t.Fatalf("seeding CRLF profile: %v", err)
	}

	generated := filepath.Join(dir, ".config", "aliasdeck", "aliases.ps1")
	recordedBlock, err := AddBootstrap(rcPath, domain.ShellPowerShell, generated, dir)
	if err != nil {
		t.Fatalf("AddBootstrap: %v", err)
	}

	line := BootstrapLine(domain.ShellPowerShell, generated, dir)
	seeded, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading seeded profile: %v", err)
	}
	// The user hand-edits the sourcing line inside the marker block, keeping
	// the file's own CRLF convention, so the exact recordedBlock bytes no
	// longer appear anywhere in the file and RemoveBootstrap must fall back
	// to scanning for the marker lines themselves.
	edited := strings.Replace(string(seeded), line, "# user note: I customized this", 1)
	if edited == string(seeded) {
		t.Fatal("test setup did not actually edit the sourcing line")
	}
	if err := os.WriteFile(rcPath, []byte(edited), 0o644); err != nil {
		t.Fatalf("writing edited profile: %v", err)
	}

	exact, err := RemoveBootstrap(rcPath, recordedBlock)
	if err != nil {
		t.Fatalf("RemoveBootstrap() returned an error: %v", err)
	}
	if exact {
		t.Fatal("RemoveBootstrap() must report a non-exact restore when it falls back to marker scanning")
	}

	got, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading rc file: %v", err)
	}
	if string(got) != original {
		t.Errorf("marker-scan fallback on a CRLF profile must still restore the surrounding content exactly:\ngot:  %q\nwant: %q", got, original)
	}
}
