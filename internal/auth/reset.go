package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/angeltonio/aliasdeck/internal/store"
)

// ErrUnknownOperator reports that no operator holds the requested username,
// so there was no password to replace. It is distinct from a store failure:
// a typo in --username must not read as a broken database.
var ErrUnknownOperator = errors.New("auth: no such operator")

// ErrSessionsSurvivedReset reports that the password was replaced but the
// operator's existing sessions could not be revoked. The password change is
// durable by the time this is returned, so it is deliberately not folded
// into a generic failure: an operator who reset a password *because* they
// believe someone else holds a session needs to know that those sessions may
// still be live, and that the only remaining lever is restarting the server
// or reissuing the reset.
var ErrSessionsSurvivedReset = errors.New("auth: password replaced but existing sessions were not revoked")

// ResetPassword replaces the stored password of the operator named username
// and revokes every session token that operator holds.
//
// This is the recovery path for the case the rest of this package has no
// answer for: Create refuses a username that already exists and
// ALIASDECK_ADMIN_PASSWORD is only consulted while the database has no
// operator at all, so before this existed an operator who lost their
// password had no route back in short of deleting the database and every
// alias, profile and device in it.
//
// Authorization is the database itself. There is no old-password challenge
// and no second factor, because the caller has already had to open the
// store to get here — whoever can do that can read every hash and token in
// it and could rewrite the row by hand. Requiring a challenge would gate
// nothing while making the recovery path useless in exactly the situation it
// exists for. What the boundary must therefore be is filesystem access to
// the database, which is why this lives in a subcommand an operator runs
// against the data directory rather than behind an HTTP route.
//
// Revoking sessions is not incidental. A password is reset either because it
// was forgotten or because it may be known to someone else, and in the
// second case leaving live sessions open would make the reset cosmetic.
func ResetPassword(ctx context.Context, st store.Store, now func() time.Time, username, password string) error {
	if strings.TrimSpace(username) == "" {
		return fmt.Errorf("auth: operator username is empty: %w", ErrUnknownOperator)
	}
	if err := validatePasswordStrength(password, "the new password"); err != nil {
		return err
	}

	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("auth: hashing the new operator password: %w", err)
	}

	operator, err := st.Operators().UpdatePasswordHash(ctx, username, []byte(hash))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("auth: operator %q does not exist: %w", username, ErrUnknownOperator)
		}
		return fmt.Errorf("auth: replacing operator password: %w", err)
	}

	// Ordered after the update on purpose. If revocation is attempted first
	// and the update then fails, the operator is logged out of a password
	// they still have to keep using; this way the credential is always the
	// thing that changed first, and a revocation failure is reported as the
	// narrower problem it is.
	if err := st.Tokens().RevokeSubject(ctx, store.TokenKindSession, operator.ID, now()); err != nil {
		return fmt.Errorf("auth: revoking sessions for operator %q: %w: %w", username, ErrSessionsSurvivedReset, err)
	}
	return nil
}

// ResetPasswordFromEnvOrGenerated is ResetPassword with the same credential
// sourcing and the same delivery routing Bootstrap already uses, so a reset
// behaves like a first start rather than inventing a second convention.
//
// The password comes from ALIASDECK_ADMIN_PASSWORD when it is set, and is
// generated otherwise. It is never taken from a command-line flag: argv is
// visible to every process on the machine through ps and is recorded in
// shell history, which would leak the new credential to precisely the local
// observers this reset may be defending against.
//
// passwordFilePath carries design decision 22's question — "is out actually
// a console a person will read?" — exactly as Bootstrap's does. Empty prints
// the generated password to out; otherwise it is written to that path at
// mode 0600 and only the path is printed.
func ResetPasswordFromEnvOrGenerated(
	ctx context.Context,
	st store.Store,
	now func() time.Time,
	getenv func(string) string,
	out io.Writer,
	passwordFilePath string,
	username string,
) error {
	password := getenv(AdminPasswordEnv)
	generated := password == ""
	if generated {
		var err error
		if password, err = GeneratePassword(generatedPasswordLength); err != nil {
			return fmt.Errorf("auth: generating the new operator password: %w", err)
		}
	} else if err := validatePasswordStrength(password, AdminPasswordEnv); err != nil {
		return err
	}

	if err := ResetPassword(ctx, st, now, username, password); err != nil {
		return err
	}

	// Delivered only after the reset succeeded. Printing a password the
	// database never accepted would send an operator to a login that
	// cannot work, and they would have no way to tell that from a typo.
	if generated {
		if err := deliverResetPassword(out, passwordFilePath, username, password); err != nil {
			return fmt.Errorf("auth: delivering the new operator password: %w", err)
		}
	}
	return nil
}

// deliverResetPassword mirrors deliverGeneratedPassword's routing while
// naming the operator that was actually reset, rather than the fixed
// bootstrap account.
func deliverResetPassword(out io.Writer, passwordFilePath, username, password string) error {
	if passwordFilePath == "" {
		_, err := fmt.Fprintf(out,
			"New password for operator %q (save this now — it will not be shown again): %s\n",
			username, password)
		return err
	}

	if err := writeBootstrapPasswordFile(passwordFilePath, password); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out,
		"New password for operator %q written to %s (mode 0600) — read it, then secure or remove the file; it will not be written again.\n",
		username, passwordFilePath)
	return err
}

// validatePasswordStrength applies minAdminPasswordLength to any
// operator-supplied password, naming source in the error so an operator can
// tell an environment override apart from an interactively supplied one.
func validatePasswordStrength(password, source string) error {
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("auth: %s is empty or all whitespace: %w", source, ErrWeakAdminPassword)
	}
	if len(password) < minAdminPasswordLength {
		return fmt.Errorf("auth: %s must be at least %d characters, got %d: %w",
			source, minAdminPasswordLength, len(password), ErrWeakAdminPassword)
	}
	return nil
}
