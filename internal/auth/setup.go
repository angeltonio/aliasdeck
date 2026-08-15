package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/angeltonio/aliasdeck/internal/store"
)

const (
	setupCredentialLength  = 32
	minSetupUsernameLength = 1
)

var (
	ErrSetupDisabled           = errors.New("auth: first-run setup is disabled")
	ErrInvalidSetupCredential  = errors.New("auth: invalid setup credential")
	ErrWeakSetupPassword       = errors.New("auth: setup password too weak")
	ErrMismatchedSetupPassword = errors.New("auth: setup passwords do not match")
)

// EnsureSetupCredential creates the one-time credential used by the
// interactive first-run flow. The file is deliberately kept beside the
// database and is never exposed by an HTTP handler.
func EnsureSetupCredential(path string, out io.Writer) error {
	if path == "" {
		return fmt.Errorf("auth: setup credential path is empty")
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("auth: checking setup credential: %w", err)
	}
	token := make([]byte, setupCredentialLength)
	if _, err := rand.Read(token); err != nil {
		return fmt.Errorf("auth: generating setup credential: %w", err)
	}
	if err := writeSecretFile(path, string(token)); err != nil {
		return err
	}
	if out != nil {
		_, _ = out.Write([]byte(fmt.Sprintf("AliasDeck first-run setup credential written to %s (mode 0600). Open /setup?credential=<value from this file> once to create the operator account.\n", path)))
	}
	return nil
}

func SetupEnabled(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// CompleteSetup validates and creates the first operator, then atomically
// renames the credential out of the live path. The operator uniqueness check
// makes replay safe even if a process exits between the database commit and
// the rename.
func CompleteSetup(ctx context.Context, st store.Store, credentialPath, credential, username, password, confirmation string) error {
	if credentialPath == "" || !SetupEnabled(credentialPath) {
		return ErrSetupDisabled
	}
	lock, err := os.OpenFile(credentialPath+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrSetupDisabled
	}
	_ = lock.Close()
	defer os.Remove(credentialPath + ".lock")
	want, err := os.ReadFile(credentialPath)
	if err != nil {
		return ErrSetupDisabled
	}
	gotHash, wantHash := sha256.Sum256([]byte(strings.TrimSpace(credential))), sha256.Sum256([]byte(strings.TrimSpace(string(want))))
	if subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) != 1 {
		return ErrInvalidSetupCredential
	}
	username = strings.TrimSpace(username)
	if len(username) < minSetupUsernameLength {
		return fmt.Errorf("auth: username is required")
	}
	if strings.TrimSpace(password) == "" || len(password) < minAdminPasswordLength {
		return ErrWeakSetupPassword
	}
	if password != confirmation {
		return ErrMismatchedSetupPassword
	}
	count, err := st.Operators().Count(ctx)
	if err != nil {
		return fmt.Errorf("auth: counting operators: %w", err)
	}
	if count > 0 {
		return ErrSetupDisabled
	}
	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("auth: hashing setup password: %w", err)
	}
	if _, err := st.Operators().Create(ctx, store.Operator{Username: username, PasswordHash: []byte(hash)}); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return ErrSetupDisabled
		}
		return fmt.Errorf("auth: creating setup operator: %w", err)
	}
	consumed := credentialPath + ".consumed"
	if err := os.Rename(credentialPath, consumed); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("auth: consuming setup credential: %w", err)
	}
	_ = os.Remove(consumed)
	return nil
}

func writeSecretFile(path, value string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("auth: creating setup directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".setup-credential.*.tmp")
	if err != nil {
		return fmt.Errorf("auth: creating setup credential: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(value + "\n"); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("auth: installing setup credential: %w", err)
	}
	return nil
}
