package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters for the operator password only (design decision 8:
// the operator password is human-transported, so it uses a KDF with a
// work factor; tokens are 256-bit CSPRNG values compared with sha256 +
// subtle.ConstantTimeCompare instead, because a KDF gains nothing there
// and costs a DoS lever on every request).
const (
	argon2Time    = 1
	argon2Memory  = 64 * 1024 // KiB, ~64 MiB
	argon2Threads = 4
	argon2KeyLen  = 32
	saltLength    = 16
)

// hashFormat identifies HashPassword's encoded output so a future
// parameter change can be detected and rejected rather than silently
// misinterpreted.
const hashFormat = "argon2id"

// HashPassword hashes password with argon2id under a fresh random salt and
// returns a self-describing encoded string carrying the algorithm version,
// parameters, salt and hash — so VerifyPassword never needs a second
// source of truth for what parameters produced a given hash.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generating password salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)

	return fmt.Sprintf("%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
		hashFormat, argon2.Version, argon2Memory, argon2Time, argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword reports whether password matches encoded, a string
// produced by HashPassword. It returns ErrMalformedPasswordHash for any
// encoded value that is not in that exact format — including empty input,
// a different algorithm tag, or corrupted base64 — and ErrPasswordMismatch
// for a well-formed hash that simply does not match. It never panics on
// adversarial input.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != hashFormat {
		return false, ErrMalformedPasswordHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[1], "v=%d", &version); err != nil {
		return false, ErrMalformedPasswordHash
	}

	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, ErrMalformedPasswordHash
	}
	if memory == 0 || time == 0 || threads == 0 {
		return false, ErrMalformedPasswordHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(salt) == 0 {
		return false, ErrMalformedPasswordHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(want) == 0 {
		return false, ErrMalformedPasswordHash
	}

	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) == 1 {
		return true, nil
	}
	return false, ErrPasswordMismatch
}
