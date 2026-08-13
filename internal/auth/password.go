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
//
// m=64 MiB, t=1, p=4: OWASP's Password Storage Cheat Sheet floor for
// Argon2id at p=1 is m=19 MiB/t=2 (or m=47 MiB/t=1 for a single pass);
// this project's 64 MiB clears that floor with room to spare while moving
// work onto parallelism (p=4) rather than iterations, since threads are
// cheap on the class of machine this server targets and this hash never
// runs more than once per login. Measured on this project's reference
// hardware: ~12.8 ms wall time and 64 MiB resident per HashPassword or
// VerifyPassword call — fast enough that one operator's login is
// imperceptible, and exactly why *concurrent* calls still need a bound
// (design's Bounded Operations table, "Concurrent password verification":
// 10 concurrent logins already hold ~640 MiB before any single call is
// itself a problem).
const (
	argon2Time    = 1
	argon2Memory  = 64 * 1024 // KiB, ~64 MiB
	argon2Threads = 4

	// argon2MaxMemory, argon2MaxTime and argon2MaxThreads cap what
	// VerifyPassword will accept out of a stored hash's own encoded
	// parameters (bounded-review finding: a well-formed-looking hash with
	// an oversized m= reached argon2.IDKey uncapped and hung the process
	// allocating memory until it was killed by SIGTERM — no error, no
	// panic, and no Phase 4 recovery middleware can catch an allocation
	// that never returns). The multipliers give real headroom above the
	// shipped parameters for a future bump without recompiling every hash
	// in the database, while staying nowhere near the four-orders-of-
	// magnitude gap the reported hash (m=4000000000, ~4 TB) exploited.
	argon2MaxMemory  = 8 * argon2Memory
	argon2MaxTime    = 8 * argon2Time
	argon2MaxThreads = 8 * argon2Threads

	argon2KeyLen = 32
	saltLength   = 16
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
	if memory > argon2MaxMemory || time > argon2MaxTime || uint32(threads) > argon2MaxThreads {
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
