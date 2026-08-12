package apply

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBootstrapRoundTripOnRealisticRCFiles exercises add-then-remove against rc
// files shaped like the ones people actually have, rather than the minimal
// fixtures that unit tests reach for.
//
// It is deliberately redundant with the focused tests in bootstrap_test.go.
// This package edits a file the user owns and did not create, so the promise
// that uninstalling restores their shell configuration byte for byte is worth
// checking from more than one angle. The cases here — an oh-my-zsh profile, no
// trailing newline, CRLF endings, text that resembles our own marker — are the
// shapes most likely to break a naive implementation.
func TestBootstrapRoundTripOnRealisticRCFiles(t *testing.T) {
	realistic := []struct {
		name string
		rc   string
	}{
		{
			name: "typical zshrc",
			rc: "export ZSH=\"$HOME/.oh-my-zsh\"\nZSH_THEME=\"robbyrussell\"\nplugins=(git docker)\n" +
				"source $ZSH/oh-my-zsh.sh\n\nalias ll='ls -la'\nexport PATH=\"$HOME/bin:$PATH\"\n",
		},
		{name: "no trailing newline", rc: "alias ll='ls -la'"},
		{name: "empty file", rc: ""},
		{name: "only newlines", rc: "\n\n\n"},
		{name: "windows line endings", rc: "alias a='b'\r\nalias c='d'\r\n"},
		{name: "text resembling our marker", rc: "# >>> conda initialize >>>\nexport X=1\n# <<< conda initialize <<<\n"},
		{name: "trailing whitespace preserved", rc: "alias x='y'   \n\t\n"},
		{name: "unicode content", rc: "# configuración del shell\nalias café='echo ☕'\n"},
	}

	for _, tc := range realistic {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			rcPath := filepath.Join(dir, ".zshrc")
			generated := filepath.Join(dir, ".config", "aliasdeck", "aliases.zsh")

			if err := os.WriteFile(rcPath, []byte(tc.rc), 0o644); err != nil {
				t.Fatalf("seeding rc: %v", err)
			}
			original, err := os.ReadFile(rcPath)
			if err != nil {
				t.Fatalf("reading seeded rc: %v", err)
			}

			block, err := AddBootstrap(rcPath, generated, dir)
			if err != nil {
				t.Fatalf("AddBootstrap: %v", err)
			}

			after, err := os.ReadFile(rcPath)
			if err != nil {
				t.Fatalf("reading rc after add: %v", err)
			}
			if string(after) == string(original) {
				t.Fatal("AddBootstrap made no change to the rc file")
			}

			// Adding twice must not append a second block.
			if _, err := AddBootstrap(rcPath, generated, dir); err != nil {
				t.Fatalf("second AddBootstrap: %v", err)
			}
			twice, err := os.ReadFile(rcPath)
			if err != nil {
				t.Fatalf("reading rc after second add: %v", err)
			}
			if string(twice) != string(after) {
				t.Errorf("AddBootstrap is not idempotent:\n first: %q\nsecond: %q", after, twice)
			}

			exact, err := RemoveBootstrap(rcPath, block)
			if err != nil {
				t.Fatalf("RemoveBootstrap: %v", err)
			}
			if !exact {
				t.Error("RemoveBootstrap reported a non-exact removal on an untouched block")
			}

			restored, err := os.ReadFile(rcPath)
			if err != nil {
				t.Fatalf("reading rc after remove: %v", err)
			}
			if string(restored) != string(original) {
				t.Errorf("rc file not restored byte-identically\noriginal: %q\nrestored: %q",
					original, restored)
			}
		})
	}
}

// TestBootstrapNeverTouchesUnrelatedFiles pins the blast radius: adding a
// bootstrap modifies exactly the rc file it was given and nothing beside it.
//
// A user with both .zshrc and .bashrc should never find the wrong one edited.
func TestBootstrapNeverTouchesUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".zshrc")
	neighbour := filepath.Join(dir, ".bashrc")

	if err := os.WriteFile(rcPath, []byte("alias a='b'\n"), 0o644); err != nil {
		t.Fatalf("seeding rc: %v", err)
	}
	if err := os.WriteFile(neighbour, []byte("alias c='d'\n"), 0o644); err != nil {
		t.Fatalf("seeding neighbour: %v", err)
	}

	before, err := os.ReadFile(neighbour)
	if err != nil {
		t.Fatalf("reading neighbour: %v", err)
	}

	if _, err := AddBootstrap(rcPath, filepath.Join(dir, "aliases.zsh"), dir); err != nil {
		t.Fatalf("AddBootstrap: %v", err)
	}

	after, err := os.ReadFile(neighbour)
	if err != nil {
		t.Fatalf("re-reading neighbour: %v", err)
	}
	if string(after) != string(before) {
		t.Error("AddBootstrap modified a shell config it does not own")
	}
}
