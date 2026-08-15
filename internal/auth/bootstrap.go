package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
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
//
// passwordFilePath is the caller's resolved answer to "is out actually a
// console a person will read?" (design decision 22). When it is empty, the
// generated password is printed to out exactly as before. When it is not
// empty, out is not treated as a safe destination for the secret itself —
// under systemd's default StandardOutput=journal, out is stdout, and stdout
// is the journal: persistent, journalctl-queryable, and exactly the "any
// log" server-auth spec.md forbids twice. In that case Bootstrap instead
// writes the password to passwordFilePath, atomically and at mode 0600
// (mirroring internal/state.Save's existing atomic-write pattern — this is
// the project's second use of it, not a third convention), and out receives
// only a short notice naming that path, never the password. Bootstrap
// itself never inspects os.Stdout or performs any terminal detection: that
// decision belongs to the caller, who is the only one that knows what out
// really is.
//
// Delivery — printing to out, or writing passwordFilePath — happens before
// the operator row is created. If delivery fails, Bootstrap returns an
// error and creates no operator, so the next start (still against an empty
// database) can retry cleanly instead of leaving an operator whose password
// nobody ever received.
//
// Bootstrap never writes to any logger — out is the caller's own
// io.Writer, so the generated password's only destination is whatever the
// caller wires out to (the process's stdout in production), never a log
// sink.
func Bootstrap(ctx context.Context, st store.Store, getenv func(string) string, out io.Writer, passwordFilePath string) error {
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

	if generated {
		if err := deliverGeneratedPassword(out, passwordFilePath, password); err != nil {
			return fmt.Errorf("auth: delivering bootstrap password: %w", err)
		}
	}

	if _, err := st.Operators().Create(ctx, store.Operator{
		Username:     bootstrapUsername,
		PasswordHash: []byte(hash),
	}); err != nil {
		return fmt.Errorf("auth: creating bootstrap operator: %w", err)
	}

	return nil
}

// BootstrapWithSetup keeps the non-interactive environment override while
// replacing the generated fixed admin account with a credentialed web setup
// flow for fresh interactive deployments.
func BootstrapWithSetup(ctx context.Context, st store.Store, getenv func(string) string, out io.Writer, setupCredentialPath string) error {
	// A blank path is retained for embedded/test callers that use the legacy
	// Bootstrap contract. Production wiring always supplies the server data
	// directory path, so interactive deployments cannot silently fall back.
	if setupCredentialPath == "" {
		return Bootstrap(ctx, st, getenv, out, "")
	}
	if getenv(AdminPasswordEnv) != "" {
		return Bootstrap(ctx, st, getenv, out, "")
	}
	count, err := st.Operators().Count(ctx)
	if err != nil {
		return fmt.Errorf("auth: counting operators: %w", err)
	}
	if count > 0 {
		return nil
	}
	return EnsureSetupCredential(setupCredentialPath, out)
}

// deliverGeneratedPassword is Bootstrap's routing decision (design decision
// 22): print directly to out when passwordFilePath is empty (out is a real
// console), or write the password to passwordFilePath and print only its
// path otherwise. It is exercised by both branches directly in
// bootstrap_test.go — the axis under test is exactly this parameter, not any
// terminal probe, which stays out of this package entirely.
func deliverGeneratedPassword(out io.Writer, passwordFilePath, password string) error {
	if passwordFilePath == "" {
		_, err := fmt.Fprintf(out, "Generated operator password for %q (save this now — it will not be shown again): %s\n",
			bootstrapUsername, password)
		return err
	}

	if err := writeBootstrapPasswordFile(passwordFilePath, password); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "Generated operator password for %q written to %s (mode 0600) — read it, then secure or remove the file; it will not be written again.\n",
		bootstrapUsername, passwordFilePath)
	return err
}

// writeBootstrapPasswordFile writes password to path atomically and at mode
// 0600: a temp file in the same directory, chmod'd before any content
// touches it, then a rename — the same pattern internal/state.Save already
// uses for state.json, so this is that pattern's second call site, not a
// new one. The directory is created if missing (0755, matching config.Base's
// existing directory convention) so a fresh install's first start does not
// depend on some other command having run first.
func writeBootstrapPasswordFile(path, password string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".bootstrap-password.*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := writeSyncCloseBootstrapPassword(tmp, password); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", tmpPath, path, err)
	}
	return nil
}

func writeSyncCloseBootstrapPassword(f *os.File, password string) error {
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return fmt.Errorf("setting mode on %s: %w", f.Name(), err)
	}
	if _, err := f.WriteString(password + "\n"); err != nil {
		f.Close()
		return fmt.Errorf("writing %s: %w", f.Name(), err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("syncing %s: %w", f.Name(), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", f.Name(), err)
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
