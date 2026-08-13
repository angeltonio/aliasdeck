package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashPasswordRoundTrips(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword(): %v", err)
	}
	if hash == "" {
		t.Fatal("HashPassword() returned an empty hash")
	}
	if strings.Contains(hash, "correct horse battery staple") {
		t.Fatal("HashPassword() output contains the plaintext password")
	}

	ok, err := VerifyPassword("correct horse battery staple", hash)
	if err != nil {
		t.Fatalf("VerifyPassword() with the correct password = %v, want nil error", err)
	}
	if !ok {
		t.Fatal("VerifyPassword() rejected the correct password")
	}
}

func TestHashPasswordProducesDifferentHashesForTheSamePassword(t *testing.T) {
	// A fixed salt would make two identical passwords hash identically,
	// which is exactly what a per-hash random salt exists to prevent.
	first, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword() first call: %v", err)
	}
	second, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword() second call: %v", err)
	}
	if first == second {
		t.Fatal("HashPassword() produced identical output for two calls with the same password, want a random salt each time")
	}
}

func TestVerifyPasswordRejectsWrongPassword(t *testing.T) {
	hash, err := HashPassword("the-real-password")
	if err != nil {
		t.Fatalf("HashPassword(): %v", err)
	}

	ok, err := VerifyPassword("not-the-real-password", hash)
	if ok {
		t.Fatal("VerifyPassword() accepted a wrong password")
	}
	if !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("VerifyPassword() with a wrong password = %v, want ErrPasswordMismatch", err)
	}
}

func TestVerifyPasswordRefusesMalformedHashWithoutPanicking(t *testing.T) {
	tests := []string{
		"",
		"not-an-argon2-hash-at-all",
		"argon2id$garbage",
		"bcrypt$v=1$m=1,t=1,p=1$c2FsdA$aGFzaA",
	}

	for _, malformed := range tests {
		t.Run(malformed, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("VerifyPassword() panicked on %q: %v", malformed, r)
				}
			}()

			ok, err := VerifyPassword("anything", malformed)
			if ok {
				t.Fatalf("VerifyPassword() accepted a malformed hash %q", malformed)
			}
			if !errors.Is(err, ErrMalformedPasswordHash) {
				t.Fatalf("VerifyPassword() on malformed hash %q = %v, want ErrMalformedPasswordHash", malformed, err)
			}
		})
	}
}
