package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"strings"

	"github.com/angeltonio/aliasdeck/internal/store"
)

// AdminPasswordEnv is the environment variable that, when set, supplies the
// bootstrap operator's password instead of one being generated (server-auth
// spec, "Environment override is honored").
const AdminPasswordEnv = "ALIASDECK_ADMIN_PASSWORD"

// bootstrapUsername is the one operator account Bootstrap creates. v0.3
// bootstraps exactly one operator with a fixed username; a later milestone
// can add operator management once there is more than one to manage.
const bootstrapUsername = "admin"

// generatedPasswordLength is the length of a generated bootstrap password.
const generatedPasswordLength = 24

// generatedPasswordAlphabet deliberately excludes visually ambiguous
// characters (0/O, 1/l/I) since this password is meant to be read off a
// terminal and retyped once.
const generatedPasswordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

// minAdminPasswordLength is the minimum length Bootstrap requires from an
// operator-supplied ALIASDECK_ADMIN_PASSWORD. NIST SP 800-63B sets 8
// characters as its floor for a memorized secret; this project doubles
// that floor because the credential it gates is not a scoped user account
// but the one operator identity with full read/write access to every
// alias, profile, device and token in the store. It is never applied to a
// generated password: GeneratePassword's alphabet and 24-character length
// already clear this floor on their own.
const minAdminPasswordLength = 12

// Bootstrap ensures exactly one operator account exists, creating it only
// when the database is empty (server-auth spec, "One Operator Account,
// Bootstrapped on First Start"). It takes no stdin input whatsoever —
// getenv is the only external input besides st — matching the bounded
// operation "Operator bootstrap ... Zero stdin reads in serve": a server
// started under systemd with stdin closed must never hang waiting on a
// prompt, and there is structurally nothing here that could read one.
//
// out receives the generated password exactly once, and only when one was
// actually generated: never when ALIASDECK_ADMIN_PASSWORD supplied it, and
// never on a restart against a database that already has an operator.
// Bootstrap never writes to any logger — out is the caller's own
// io.Writer, so the generated password's only destination is whatever the
// caller wires out to (the process's stdout in production), never a log
// sink.
func Bootstrap(ctx context.Context, st store.Store, getenv func(string) string, out io.Writer) error {
	count, err := st.Operators().Count(ctx)
	if err != nil {
		return fmt.Errorf("auth: counting operators: %w", err)
	}
	if count > 0 {
		return nil
	}

	password := getenv(AdminPasswordEnv)
	generated := password == ""
	if generated {
		password, err = GeneratePassword(generatedPasswordLength)
		if err != nil {
			return fmt.Errorf("auth: generating bootstrap password: %w", err)
		}
	} else if err := validateAdminPassword(password); err != nil {
		return err
	}

	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("auth: hashing bootstrap password: %w", err)
	}

	if _, err := st.Operators().Create(ctx, store.Operator{
		Username:     bootstrapUsername,
		PasswordHash: []byte(hash),
	}); err != nil {
		return fmt.Errorf("auth: creating bootstrap operator: %w", err)
	}

	if generated {
		fmt.Fprintf(out, "Generated operator password for %q (save this now — it will not be shown again): %s\n",
			bootstrapUsername, password)
	}
	return nil
}

// validateAdminPassword rejects an operator-supplied
// ALIASDECK_ADMIN_PASSWORD that is empty, all whitespace, or shorter than
// minAdminPasswordLength (bounded-review finding: setting it to "a"
// produced a working single-character admin password with no rejection
// and no warning).
func validateAdminPassword(password string) error {
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("auth: %s is set but empty or all whitespace: %w", AdminPasswordEnv, ErrWeakAdminPassword)
	}
	if len(password) < minAdminPasswordLength {
		return fmt.Errorf("auth: %s must be at least %d characters, got %d: %w", AdminPasswordEnv, minAdminPasswordLength, len(password), ErrWeakAdminPassword)
	}
	return nil
}

// GeneratePassword returns a length-character password drawn from
// crypto/rand, never math/rand: a predictable bootstrap credential defeats
// the entire point of generating one. Each character is chosen via
// rand.Int against generatedPasswordAlphabet's length, which avoids
// modulo bias without needing to reject and retry any byte values.
func GeneratePassword(length int) (string, error) {
	alphabetSize := big.NewInt(int64(len(generatedPasswordAlphabet)))

	buf := make([]byte, length)
	for i := range buf {
		idx, err := rand.Int(rand.Reader, alphabetSize)
		if err != nil {
			return "", fmt.Errorf("auth: drawing a random password character: %w", err)
		}
		buf[i] = generatedPasswordAlphabet[idx.Int64()]
	}
	return string(buf), nil
}
