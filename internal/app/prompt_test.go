package app

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

// TestPromptYesNoDoesNotBlockOnNonTerminalStdin pins the behaviour that a
// prompt must never hang.
//
// Reading a confirmation from an open pipe that will never carry a line blocks
// indefinitely, and that pipe is the ordinary shape of stdin under
// `curl … | sh`, inside a container build and in CI. An end-to-end run caught
// this: `aliasdeck init` sat waiting forever with no diagnostic instead of
// declining and explaining how to opt in.
func TestPromptYesNoDoesNotBlockOnNonTerminalStdin(t *testing.T) {
	// An os.Pipe read end is a real *os.File that is not a character device,
	// which is exactly the case that used to hang. Nothing is ever written to
	// it, so an implementation that reads would never return.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() {
		r.Close()
		w.Close()
	})

	var out bytes.Buffer
	env := Env{Stdin: r, Stdout: &out}

	done := make(chan struct{})
	var got bool
	var gotErr error

	go func() {
		got, gotErr = promptYesNo(env, "Add the bootstrap line?")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("promptYesNo blocked on a non-terminal stdin instead of declining")
	}

	if gotErr != nil {
		t.Errorf("promptYesNo returned an error: %v", gotErr)
	}
	if got {
		t.Error("promptYesNo consented without anyone answering")
	}
	if out.Len() != 0 {
		t.Errorf("asked a question nobody can answer: %q", out.String())
	}
}

// TestPromptYesNoStillReadsInjectedInput confirms the terminal check did not
// break the tests and scripts that supply an answer through a plain reader.
func TestPromptYesNoStillReadsInjectedInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "y consents", input: "y\n", want: true},
		{name: "yes consents", input: "yes\n", want: true},
		{name: "uppercase Y consents", input: "Y\n", want: true},
		{name: "n declines", input: "n\n", want: false},
		{name: "empty line declines", input: "\n", want: false},
		{name: "anything else declines", input: "maybe\n", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			env := Env{Stdin: strings.NewReader(tt.input), Stdout: &out}

			got, err := promptYesNo(env, "Proceed?")
			if err != nil {
				t.Fatalf("promptYesNo: %v", err)
			}
			if got != tt.want {
				t.Errorf("promptYesNo(%q) = %v, want %v", tt.input, got, tt.want)
			}
			if out.Len() == 0 {
				t.Error("did not ask the question on an injected reader")
			}
		})
	}
}
