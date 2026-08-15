package auth

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInteractiveSetupCreatesOperatorAndConsumesCredential(t *testing.T) {
	st := newFakeBootstrapStore()
	path := filepath.Join(t.TempDir(), "setup-credential.txt")
	var out bytes.Buffer
	if err := EnsureSetupCredential(path, &out); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	credential := strings.TrimSpace(string(raw))
	if err := CompleteSetup(context.Background(), st, path, credential, "operator", "correct horse battery staple", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("credential still exists: %v", err)
	}
	if err := CompleteSetup(context.Background(), st, path, credential, "second", "correct horse battery staple", "correct horse battery staple"); !errors.Is(err, ErrSetupDisabled) {
		t.Fatalf("replay error = %v, want ErrSetupDisabled", err)
	}
}

func TestInteractiveSetupRejectsWeakAndMismatchedPasswords(t *testing.T) {
	for _, tt := range []struct {
		name, password, confirmation string
		want                         error
	}{
		{"weak", "short", "short", ErrWeakSetupPassword},
		{"mismatch", "correct horse battery staple", "different password", ErrMismatchedSetupPassword},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newFakeBootstrapStore()
			path := filepath.Join(t.TempDir(), "credential")
			if err := EnsureSetupCredential(path, nil); err != nil {
				t.Fatal(err)
			}
			credential, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			err = CompleteSetup(context.Background(), st, path, strings.TrimSpace(string(credential)), "operator", tt.password, tt.confirmation)
			if !errors.Is(err, tt.want) {
				t.Fatalf("CompleteSetup() = %v, want %v", err, tt.want)
			}
			if n, _ := st.operators.Count(context.Background()); n != 0 {
				t.Fatalf("operator count = %d, want 0", n)
			}
		})
	}
}

func TestBootstrapWithSetupKeepsHeadlessEnvironmentPath(t *testing.T) {
	st := newFakeBootstrapStore()
	getenv := func(key string) string {
		if key == AdminPasswordEnv {
			return "headless operator password"
		}
		return ""
	}
	if err := BootstrapWithSetup(context.Background(), st, getenv, &bytes.Buffer{}, filepath.Join(t.TempDir(), "setup")); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.operators.Count(context.Background()); n != 1 {
		t.Fatalf("operator count = %d, want 1", n)
	}
	if st.operators.operators[0].Username != bootstrapUsername {
		t.Fatalf("username = %q, want %q", st.operators.operators[0].Username, bootstrapUsername)
	}
}
