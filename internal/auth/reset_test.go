package auth

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
)

// resetAt is the fixed clock every reset test injects, so an assertion on
// the revocation timestamp is about the value ResetPassword passed through
// rather than about when the suite happened to run.
var resetAt = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// revocation records one RevokeSubject call.
type revocation struct {
	kind    store.TokenKind
	subject string
	at      time.Time
}

// fakeResetTokenRepo records revocations and can be made to fail, which is
// the only token behavior the reset path exercises.
type fakeResetTokenRepo struct {
	revocations []revocation
	revokeErr   error
}

func (r *fakeResetTokenRepo) Create(_ context.Context, _ store.Token) error { return nil }

func (r *fakeResetTokenRepo) ByLookup(_ context.Context, _ string) (store.Token, error) {
	return store.Token{}, store.ErrNotFound
}

func (r *fakeResetTokenRepo) ConsumeEnrollment(_ context.Context, _ string, _ domain.Device) (domain.Device, error) {
	return domain.Device{}, store.ErrNotFound
}

func (r *fakeResetTokenRepo) Revoke(_ context.Context, _ string, _ time.Time) error {
	return store.ErrNotFound
}

func (r *fakeResetTokenRepo) RevokeSubject(_ context.Context, kind store.TokenKind, subjectID string, at time.Time) error {
	if r.revokeErr != nil {
		return r.revokeErr
	}
	r.revocations = append(r.revocations, revocation{kind: kind, subject: subjectID, at: at})
	return nil
}

// fakeResetStore is fakeBootstrapStore plus a working Tokens(): the reset
// path revokes sessions, so a nil token repo would only ever prove that a
// nil dereference panics.
type fakeResetStore struct {
	operators *fakeOperatorRepo
	tokens    *fakeResetTokenRepo
}

func (f *fakeResetStore) Aliases() store.AliasRepo      { return nil }
func (f *fakeResetStore) Devices() store.DeviceRepo     { return nil }
func (f *fakeResetStore) Profiles() store.ProfileRepo   { return nil }
func (f *fakeResetStore) Tokens() store.TokenRepo       { return f.tokens }
func (f *fakeResetStore) Operators() store.OperatorRepo { return f.operators }
func (f *fakeResetStore) Audit() store.AuditRepo        { return noopAuditRepo{} }
func (f *fakeResetStore) Close() error                  { return nil }

// newResetStore returns a store holding one operator whose password is
// oldPassword, hashed the way the real bootstrap path would hash it.
func newResetStore(t *testing.T, username, oldPassword string) *fakeResetStore {
	t.Helper()
	hash, err := HashPassword(oldPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	return &fakeResetStore{
		operators: &fakeOperatorRepo{operators: []store.Operator{{
			ID: "operator-1", Username: username, PasswordHash: []byte(hash),
		}}},
		tokens: &fakeResetTokenRepo{},
	}
}

func storedHash(t *testing.T, st *fakeResetStore, username string) string {
	t.Helper()
	o, err := st.operators.ByUsername(context.Background(), username)
	if err != nil {
		t.Fatalf("ByUsername(%q) error = %v", username, err)
	}
	return string(o.PasswordHash)
}

func TestResetPasswordReplacesTheStoredHashAndRejectsTheOldPassword(t *testing.T) {
	st := newResetStore(t, "admin", "old-password-that-is-long-enough")

	if err := ResetPassword(context.Background(), st, fixedNow(resetAt), "admin", "brand-new-password"); err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}

	hash := storedHash(t, st, "admin")
	ok, err := VerifyPassword("brand-new-password", hash)
	if err != nil || !ok {
		t.Fatalf("VerifyPassword(new) = %v, %v; want true, nil", ok, err)
	}

	// The point of a reset is that the previous credential stops working.
	// Asserting only that the new one works would pass against a no-op.
	ok, err = VerifyPassword("old-password-that-is-long-enough", hash)
	if ok {
		t.Fatal("VerifyPassword(old) = true; the previous password still authenticates after a reset")
	}
	if !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("VerifyPassword(old) error = %v, want ErrPasswordMismatch", err)
	}
}

func TestResetPasswordRevokesOnlyTheOperatorsSessions(t *testing.T) {
	st := newResetStore(t, "admin", "old-password-that-is-long-enough")

	if err := ResetPassword(context.Background(), st, fixedNow(resetAt), "admin", "brand-new-password"); err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}

	if len(st.tokens.revocations) != 1 {
		t.Fatalf("revocations = %#v, want exactly one", st.tokens.revocations)
	}
	got := st.tokens.revocations[0]
	want := revocation{kind: store.TokenKindSession, subject: "operator-1", at: resetAt}
	if got != want {
		t.Fatalf("revocation = %#v, want %#v", got, want)
	}
}

func TestResetPasswordReportsSurvivingSessionsWithoutUndoingTheChange(t *testing.T) {
	st := newResetStore(t, "admin", "old-password-that-is-long-enough")
	st.tokens.revokeErr = errors.New("store: boom")

	err := ResetPassword(context.Background(), st, fixedNow(resetAt), "admin", "brand-new-password")
	if !errors.Is(err, ErrSessionsSurvivedReset) {
		t.Fatalf("ResetPassword() error = %v, want ErrSessionsSurvivedReset", err)
	}

	// The password change is already durable at this point. Reporting the
	// revocation failure must not imply the credential did not change, or an
	// operator would retry against a password that no longer exists.
	ok, err := VerifyPassword("brand-new-password", storedHash(t, st, "admin"))
	if err != nil || !ok {
		t.Fatalf("VerifyPassword(new) = %v, %v; want the new password to work despite the revocation failure", ok, err)
	}
}

func TestResetPasswordRefusesAnUnknownOperatorWithoutTouchingAnyRow(t *testing.T) {
	st := newResetStore(t, "admin", "old-password-that-is-long-enough")
	before := storedHash(t, st, "admin")

	err := ResetPassword(context.Background(), st, fixedNow(resetAt), "nobody", "brand-new-password")
	if !errors.Is(err, ErrUnknownOperator) {
		t.Fatalf("ResetPassword(unknown) error = %v, want ErrUnknownOperator", err)
	}
	if after := storedHash(t, st, "admin"); after != before {
		t.Fatal("the existing operator's hash changed while resetting a username that does not exist")
	}
	if len(st.tokens.revocations) != 0 {
		t.Fatalf("revocations = %#v, want none for an unknown operator", st.tokens.revocations)
	}
}

func TestResetPasswordRefusesAnEmptyUsername(t *testing.T) {
	st := newResetStore(t, "admin", "old-password-that-is-long-enough")

	if err := ResetPassword(context.Background(), st, fixedNow(resetAt), "   ", "brand-new-password"); !errors.Is(err, ErrUnknownOperator) {
		t.Fatalf("ResetPassword(blank username) error = %v, want ErrUnknownOperator", err)
	}
}

func TestResetPasswordEnforcesTheSameStrengthFloorAsBootstrap(t *testing.T) {
	tooShort := strings.Repeat("a", minAdminPasswordLength-1)

	for _, tc := range []struct{ name, password string }{
		{name: "shorter than the floor", password: tooShort},
		{name: "whitespace only", password: "              "},
		{name: "empty", password: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newResetStore(t, "admin", "old-password-that-is-long-enough")
			before := storedHash(t, st, "admin")

			err := ResetPassword(context.Background(), st, fixedNow(resetAt), "admin", tc.password)
			if !errors.Is(err, ErrWeakAdminPassword) {
				t.Fatalf("ResetPassword(%q) error = %v, want ErrWeakAdminPassword", tc.password, err)
			}
			if after := storedHash(t, st, "admin"); after != before {
				t.Fatal("a rejected password still replaced the stored hash")
			}
		})
	}
}

func TestResetPasswordFromEnvUsesTheOverrideAndPrintsNothing(t *testing.T) {
	st := newResetStore(t, "admin", "old-password-that-is-long-enough")
	var out bytes.Buffer
	getenv := func(key string) string {
		if key == AdminPasswordEnv {
			return "supplied-by-the-operator"
		}
		return ""
	}

	if err := ResetPasswordFromEnvOrGenerated(context.Background(), st, fixedNow(resetAt), getenv, &out, "", "admin"); err != nil {
		t.Fatalf("ResetPasswordFromEnvOrGenerated() error = %v", err)
	}

	ok, err := VerifyPassword("supplied-by-the-operator", storedHash(t, st, "admin"))
	if err != nil || !ok {
		t.Fatalf("VerifyPassword(env password) = %v, %v; want true, nil", ok, err)
	}
	// A password the operator already knows must not be echoed anywhere.
	if out.Len() != 0 {
		t.Fatalf("out = %q, want nothing written when the password came from %s", out.String(), AdminPasswordEnv)
	}
}

func TestResetPasswordFromEnvRejectsAWeakOverrideBeforeChangingAnything(t *testing.T) {
	st := newResetStore(t, "admin", "old-password-that-is-long-enough")
	before := storedHash(t, st, "admin")
	var out bytes.Buffer
	getenv := func(string) string { return "short" }

	err := ResetPasswordFromEnvOrGenerated(context.Background(), st, fixedNow(resetAt), getenv, &out, "", "admin")
	if !errors.Is(err, ErrWeakAdminPassword) {
		t.Fatalf("ResetPasswordFromEnvOrGenerated() error = %v, want ErrWeakAdminPassword", err)
	}
	if after := storedHash(t, st, "admin"); after != before {
		t.Fatal("a weak override still replaced the stored hash")
	}
}

func TestResetPasswordGeneratedPasswordIsPrintedAndActuallyWorks(t *testing.T) {
	st := newResetStore(t, "admin", "old-password-that-is-long-enough")
	var out bytes.Buffer

	if err := ResetPasswordFromEnvOrGenerated(context.Background(), st, fixedNow(resetAt), noEnv, &out, "", "admin"); err != nil {
		t.Fatalf("ResetPasswordFromEnvOrGenerated() error = %v", err)
	}

	printed := out.String()
	if !strings.Contains(printed, "admin") {
		t.Fatalf("out = %q, want it to name the operator that was reset", printed)
	}

	// Recover the generated password from the notice and prove the database
	// accepts it. Printing a password the store never took would send an
	// operator to a login that cannot succeed.
	fields := strings.Fields(strings.TrimSpace(printed))
	password := fields[len(fields)-1]
	if len(password) != generatedPasswordLength {
		t.Fatalf("recovered password %q has length %d, want %d", password, len(password), generatedPasswordLength)
	}
	ok, err := VerifyPassword(password, storedHash(t, st, "admin"))
	if err != nil || !ok {
		t.Fatalf("VerifyPassword(printed password) = %v, %v; want the printed password to authenticate", ok, err)
	}
}

func TestResetPasswordGeneratedPasswordGoesToA0600FileWhenStdoutIsNotAConsole(t *testing.T) {
	st := newResetStore(t, "admin", "old-password-that-is-long-enough")
	var out bytes.Buffer
	path := filepath.Join(t.TempDir(), "reset-password.txt")

	if err := ResetPasswordFromEnvOrGenerated(context.Background(), st, fixedNow(resetAt), noEnv, &out, path, "admin"); err != nil {
		t.Fatalf("ResetPasswordFromEnvOrGenerated() error = %v", err)
	}

	// The notice names the file; the secret itself must not be in it, because
	// this branch exists precisely for the case where out is a log.
	notice := out.String()
	if !strings.Contains(notice, path) {
		t.Fatalf("out = %q, want it to name %s", notice, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	password := strings.TrimSpace(string(data))
	if strings.Contains(notice, password) {
		t.Fatal("the generated password was printed to out as well as written to the file")
	}
	ok, err := VerifyPassword(password, storedHash(t, st, "admin"))
	if err != nil || !ok {
		t.Fatalf("VerifyPassword(file password) = %v, %v; want the written password to authenticate", ok, err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%s) error = %v", path, err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Fatalf("mode = %04o, want 0600", mode)
		}
	}
}

func TestResetPasswordDoesNotWriteAPasswordFileWhenTheResetFails(t *testing.T) {
	st := newResetStore(t, "admin", "old-password-that-is-long-enough")
	var out bytes.Buffer
	path := filepath.Join(t.TempDir(), "reset-password.txt")

	err := ResetPasswordFromEnvOrGenerated(context.Background(), st, fixedNow(resetAt), noEnv, &out, path, "nobody")
	if !errors.Is(err, ErrUnknownOperator) {
		t.Fatalf("ResetPasswordFromEnvOrGenerated(unknown) error = %v, want ErrUnknownOperator", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("Stat(%s) = %v; a password file was written for a reset that never happened", path, statErr)
	}
}
