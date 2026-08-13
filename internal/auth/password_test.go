package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
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

// validSaltB64 and validHashB64 return base64 that VerifyPassword decodes
// successfully, so the corruption tests below can target exactly one
// component past the "argon2id$v=...$m=...,t=...,p=...$salt$hash" header.
func validSaltB64() string {
	return base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef"))
}

func validHashB64() string {
	return base64.RawStdEncoding.EncodeToString(make([]byte, 32))
}

// wellFormedHash assembles a syntactically valid argon2id-encoded hash
// from its four components, so a test can corrupt exactly one of them and
// still pass every earlier check.
func wellFormedHash(versionPart, paramsPart, salt, hash string) string {
	return fmt.Sprintf("%s$%s$%s$%s$%s", hashFormat, versionPart, paramsPart, salt, hash)
}

// TestVerifyPasswordRefusesCorruptionPastTheHeader is the regression test
// for a bounded-review finding: every input in
// TestVerifyPasswordRefusesMalformedHashWithoutPanicking fails at the very
// first check (`len(parts) != 5 || parts[0] != hashFormat`), leaving every
// later branch — the version parse, the parameter parse, the zero- and
// oversized-parameter guards, and the salt/hash base64 decodes — dead code
// as far as that suite was concerned. Each case here is a well-formed,
// five-part, "argon2id$"-prefixed hash with exactly one component past the
// header corrupted, so it reaches the specific branch it targets.
func TestVerifyPasswordRefusesCorruptionPastTheHeader(t *testing.T) {
	salt := validSaltB64()
	hash := validHashB64()
	goodVersion := "v=19"
	goodParams := fmt.Sprintf("m=%d,t=%d,p=%d", argon2Memory, argon2Time, argon2Threads)

	tests := []struct {
		name    string
		encoded string
	}{
		{"bad version", wellFormedHash("v=abc", goodParams, salt, hash)},
		{"unparseable parameters", wellFormedHash(goodVersion, "m=abc,t=1,p=4", salt, hash)},
		{"zero memory", wellFormedHash(goodVersion, fmt.Sprintf("m=0,t=%d,p=%d", argon2Time, argon2Threads), salt, hash)},
		{"zero time", wellFormedHash(goodVersion, fmt.Sprintf("m=%d,t=0,p=%d", argon2Memory, argon2Threads), salt, hash)},
		{"zero threads", wellFormedHash(goodVersion, fmt.Sprintf("m=%d,t=%d,p=0", argon2Memory, argon2Time), salt, hash)},
		{"oversized memory", wellFormedHash(goodVersion, fmt.Sprintf("m=%d,t=%d,p=%d", argon2MaxMemory+1, argon2Time, argon2Threads), salt, hash)},
		{"oversized time", wellFormedHash(goodVersion, fmt.Sprintf("m=%d,t=%d,p=%d", argon2Memory, argon2MaxTime+1, argon2Threads), salt, hash)},
		{"oversized threads", wellFormedHash(goodVersion, fmt.Sprintf("m=%d,t=%d,p=%d", argon2Memory, argon2Time, argon2MaxThreads+1), salt, hash)},
		{"non-base64 salt", wellFormedHash(goodVersion, goodParams, "not-valid-base64!!!", hash)},
		{"non-base64 hash", wellFormedHash(goodVersion, goodParams, salt, "not-valid-base64!!!")},
		{"empty salt", wellFormedHash(goodVersion, goodParams, "", hash)},
		{"empty hash", wellFormedHash(goodVersion, goodParams, salt, "")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("VerifyPassword() panicked on %q: %v", tt.encoded, r)
				}
			}()

			ok, err := VerifyPassword("anything", tt.encoded)
			if ok {
				t.Fatalf("VerifyPassword() accepted %q", tt.encoded)
			}
			if !errors.Is(err, ErrMalformedPasswordHash) {
				t.Fatalf("VerifyPassword() on %q = %v, want ErrMalformedPasswordHash", tt.encoded, err)
			}
		})
	}
}

// TestVerifyPasswordRejectsTheReportedOversizedMemoryParameterQuickly is
// the regression test for a bounded-review finding: a well-formed
// argon2id hash requesting m=4000000000 (~4 TB) reached argon2.IDKey with
// no upper bound and hung the process allocating memory until it was
// killed by SIGTERM after 120s — never erroring, never panicking, taking
// the whole server down with it rather than one request. The argon2MaxMemory
// cap must reject this before argon2.IDKey is ever called, so this must
// return in well under a second, not two minutes.
func TestVerifyPasswordRejectsTheReportedOversizedMemoryParameterQuickly(t *testing.T) {
	encoded := wellFormedHash("v=19", fmt.Sprintf("m=4000000000,t=%d,p=%d", argon2Time, argon2Threads), validSaltB64(), validHashB64())

	start := time.Now()
	ok, err := VerifyPassword("whatever", encoded)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("VerifyPassword() took %s to reject m=4000000000, want a near-instant rejection before argon2.IDKey ever runs", elapsed)
	}
	if ok {
		t.Fatal("VerifyPassword() accepted a hash requesting ~4 TB of memory")
	}
	if !errors.Is(err, ErrMalformedPasswordHash) {
		t.Fatalf("VerifyPassword() with m=4000000000 = %v, want ErrMalformedPasswordHash", err)
	}
}
