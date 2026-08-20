package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/store"
)

// fakeOperatorRepo and fakeBootstrapStore are in-memory stand-ins for
// store.Store so Bootstrap's tests stay pure unit tests with no database,
// matching the testing strategy's "N/A — pure unit tests" row for
// internal/auth. Only Operators() needs to behave like the real thing:
// Bootstrap never touches the other four repos.
type fakeOperatorRepo struct {
	operators []store.Operator
}

func (f *fakeOperatorRepo) Create(_ context.Context, o store.Operator) (store.Operator, error) {
	for _, existing := range f.operators {
		if existing.Username == o.Username {
			return store.Operator{}, store.ErrConflict
		}
	}
	if o.ID == "" {
		o.ID = fmt.Sprintf("operator-%d", len(f.operators)+1)
	}
	f.operators = append(f.operators, o)
	return o, nil
}

func (f *fakeOperatorRepo) Get(_ context.Context, id string) (store.Operator, error) {
	for _, o := range f.operators {
		if o.ID == id {
			return o, nil
		}
	}
	return store.Operator{}, store.ErrNotFound
}

func (f *fakeOperatorRepo) ByUsername(_ context.Context, username string) (store.Operator, error) {
	for _, o := range f.operators {
		if o.Username == username {
			return o, nil
		}
	}
	return store.Operator{}, store.ErrNotFound
}

func (f *fakeOperatorRepo) Count(_ context.Context) (int, error) {
	return len(f.operators), nil
}

func (f *fakeOperatorRepo) UpdatePasswordHash(_ context.Context, username string, hash []byte) (store.Operator, error) {
	for i, o := range f.operators {
		if o.Username == username {
			f.operators[i].PasswordHash = hash
			return f.operators[i], nil
		}
	}
	return store.Operator{}, store.ErrNotFound
}

type fakeBootstrapStore struct {
	operators *fakeOperatorRepo
}

func newFakeBootstrapStore() *fakeBootstrapStore {
	return &fakeBootstrapStore{operators: &fakeOperatorRepo{}}
}

func (f *fakeBootstrapStore) Aliases() store.AliasRepo      { return nil }
func (f *fakeBootstrapStore) Devices() store.DeviceRepo     { return nil }
func (f *fakeBootstrapStore) Profiles() store.ProfileRepo   { return nil }
func (f *fakeBootstrapStore) Tokens() store.TokenRepo       { return nil }
func (f *fakeBootstrapStore) Operators() store.OperatorRepo { return f.operators }
func (f *fakeBootstrapStore) Close() error                  { return nil }

func noEnv(string) string { return "" }

func TestBootstrapGeneratesAndPrintsPasswordOnceOnAnEmptyDatabase(t *testing.T) {
	ctx := context.Background()
	st := newFakeBootstrapStore()
	var out bytes.Buffer

	if err := Bootstrap(ctx, st, noEnv, &out, ""); err != nil {
		t.Fatalf("Bootstrap(): %v", err)
	}

	if n, _ := st.operators.Count(ctx); n != 1 {
		t.Fatalf("operator count after Bootstrap() = %d, want 1", n)
	}
	if out.Len() == 0 {
		t.Fatal("Bootstrap() printed nothing, want the generated password printed once")
	}

	op := st.operators.operators[0]
	if len(op.PasswordHash) == 0 {
		t.Fatal("Bootstrap() left the operator's PasswordHash empty")
	}
}

func TestBootstrapGeneratesDifferentPasswordsAcrossFreshDatabases(t *testing.T) {
	// This is the mutation-detecting test for "generated password comes
	// from crypto/rand, never a fixed value": a hardcoded bootstrap
	// password would print identically on every fresh database.
	ctx := context.Background()

	var firstOut, secondOut bytes.Buffer
	if err := Bootstrap(ctx, newFakeBootstrapStore(), noEnv, &firstOut, ""); err != nil {
		t.Fatalf("first Bootstrap(): %v", err)
	}
	if err := Bootstrap(ctx, newFakeBootstrapStore(), noEnv, &secondOut, ""); err != nil {
		t.Fatalf("second Bootstrap(): %v", err)
	}

	if firstOut.String() == secondOut.String() {
		t.Fatalf("Bootstrap() printed the same output for two independent fresh databases: %q", firstOut.String())
	}
}

func TestBootstrapEnvironmentOverrideIsHonoredAndNotPrinted(t *testing.T) {
	ctx := context.Background()
	st := newFakeBootstrapStore()
	var out bytes.Buffer

	getenv := func(key string) string {
		if key == AdminPasswordEnv {
			return "a-fixed-operator-password"
		}
		return ""
	}

	if err := Bootstrap(ctx, st, getenv, &out, ""); err != nil {
		t.Fatalf("Bootstrap(): %v", err)
	}

	if out.Len() != 0 {
		t.Fatalf("Bootstrap() printed %q when the password came from %s, want nothing printed", out.String(), AdminPasswordEnv)
	}

	op := st.operators.operators[0]
	ok, err := VerifyPassword("a-fixed-operator-password", string(op.PasswordHash))
	if err != nil {
		t.Fatalf("VerifyPassword() on the stored hash: %v", err)
	}
	if !ok {
		t.Fatal("the operator created by Bootstrap() does not verify against the ALIASDECK_ADMIN_PASSWORD value")
	}
}

func TestBootstrapDoesNotReprintOnSubsequentStart(t *testing.T) {
	ctx := context.Background()
	st := newFakeBootstrapStore()
	var firstOut bytes.Buffer

	if err := Bootstrap(ctx, st, noEnv, &firstOut, ""); err != nil {
		t.Fatalf("first Bootstrap(): %v", err)
	}
	if firstOut.Len() == 0 {
		t.Fatal("first Bootstrap() printed nothing, want the generated password")
	}

	var secondOut bytes.Buffer
	if err := Bootstrap(ctx, st, noEnv, &secondOut, ""); err != nil {
		t.Fatalf("second Bootstrap() (restart): %v", err)
	}
	if secondOut.Len() != 0 {
		t.Fatalf("second Bootstrap() printed %q, want nothing on a restart against an existing operator", secondOut.String())
	}

	if n, _ := st.operators.Count(ctx); n != 1 {
		t.Fatalf("operator count after two Bootstrap() calls = %d, want still 1", n)
	}
}

// TestBootstrapReadsNoStdin is the bounded-operations proof for
// "Operator bootstrap ... Zero stdin reads in serve": Bootstrap's own
// signature never accepts an io.Reader for stdin, so there is nothing for
// it to read — but this test additionally proves that swapping os.Stdin
// for an unreadable, already-closed pipe (as systemd presents when stdin
// is not connected) does not change Bootstrap's behavior at all. If a
// future edit ever added a stdin prompt, this closed pipe would make that
// read return an error or block, and either failure mode fails this test
// rather than hanging a real deployment.
func TestBootstrapReadsNoStdin(t *testing.T) {
	realStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(): %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe write end: %v", err)
	}
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = realStdin
		r.Close()
	})

	ctx := context.Background()
	var out bytes.Buffer
	if err := Bootstrap(ctx, newFakeBootstrapStore(), noEnv, &out, ""); err != nil {
		t.Fatalf("Bootstrap() with stdin closed: %v, want it to never touch stdin", err)
	}
	if out.Len() == 0 {
		t.Fatal("Bootstrap() with stdin closed printed nothing, want the generated password still printed via out")
	}
}

// TestBootstrapRejectsTooShortAdminPassword is the regression test for a
// bounded-review finding: ALIASDECK_ADMIN_PASSWORD="a" flowed straight
// into HashPassword with no rejection and no warning.
func TestBootstrapRejectsTooShortAdminPassword(t *testing.T) {
	ctx := context.Background()
	st := newFakeBootstrapStore()
	var out bytes.Buffer

	const tooShort = "short7"
	getenv := func(key string) string {
		if key == AdminPasswordEnv {
			return tooShort
		}
		return ""
	}

	err := Bootstrap(ctx, st, getenv, &out, "")
	if !errors.Is(err, ErrWeakAdminPassword) {
		t.Fatalf("Bootstrap() with a %d-character %s = %v, want ErrWeakAdminPassword", len(tooShort), AdminPasswordEnv, err)
	}
	if n, _ := st.operators.Count(ctx); n != 0 {
		t.Fatalf("operator count after a rejected Bootstrap() = %d, want 0", n)
	}
}

func TestBootstrapRejectsWhitespaceOnlyAdminPassword(t *testing.T) {
	ctx := context.Background()
	st := newFakeBootstrapStore()
	var out bytes.Buffer

	getenv := func(key string) string {
		if key == AdminPasswordEnv {
			return "                        " // whitespace only, well past minAdminPasswordLength in raw length
		}
		return ""
	}

	err := Bootstrap(ctx, st, getenv, &out, "")
	if !errors.Is(err, ErrWeakAdminPassword) {
		t.Fatalf("Bootstrap() with a whitespace-only %s = %v, want ErrWeakAdminPassword", AdminPasswordEnv, err)
	}
	if n, _ := st.operators.Count(ctx); n != 0 {
		t.Fatalf("operator count after a rejected Bootstrap() = %d, want 0", n)
	}
}

func TestBootstrapAcceptsAdminPasswordAtTheMinimumLength(t *testing.T) {
	ctx := context.Background()
	st := newFakeBootstrapStore()
	var out bytes.Buffer

	minLenPassword := strings.Repeat("a", minAdminPasswordLength)
	getenv := func(key string) string {
		if key == AdminPasswordEnv {
			return minLenPassword
		}
		return ""
	}

	if err := Bootstrap(ctx, st, getenv, &out, ""); err != nil {
		t.Fatalf("Bootstrap() with a %d-character %s = %v, want nil error", minAdminPasswordLength, AdminPasswordEnv, err)
	}
	if n, _ := st.operators.Count(ctx); n != 1 {
		t.Fatalf("operator count after Bootstrap() = %d, want 1", n)
	}
}

// TestBootstrapWritesGeneratedPasswordToFileWhenPathGiven is the regression
// test for the bounded-review finding that a systemd-managed serve's stdout
// is the journal, not a console: when passwordFilePath is non-empty,
// Bootstrap must deliver the secret there instead of through out.
func TestBootstrapWritesGeneratedPasswordToFileWhenPathGiven(t *testing.T) {
	ctx := context.Background()
	st := newFakeBootstrapStore()
	var out bytes.Buffer

	path := filepath.Join(t.TempDir(), "bootstrap-password.txt")

	if err := Bootstrap(ctx, st, noEnv, &out, path); err != nil {
		t.Fatalf("Bootstrap(): %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	// Windows has no POSIX mode bits — Go's Chmod there toggles only the
	// read-only bit, so a file this test just wrote reports 0666 and the
	// 0600 assertion below cannot hold. This mirrors
	// sqlitestore.TestOpenCreatesDatabaseAtRestrictedMode exactly rather
	// than introducing a second way of saying the same thing; design.md's
	// "Windows 0600 Gap" is one gap with one set of mitigations, and the
	// delivery file falls under it for the same reason the database does.
	//
	// The assertion is kept on the platforms that can honor it instead of
	// skipped everywhere, because a test that declines to check the
	// security property on every platform is not a weaker test — it is a
	// test of nothing.
	if runtime.GOOS == "windows" {
		if perm := info.Mode().Perm(); perm&0o200 == 0 {
			t.Errorf("password file mode = %o, want a writable file", perm)
		}
	} else if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("password file mode = %o, want %o", perm, 0o600)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	password := strings.TrimSpace(string(data))
	if password == "" {
		t.Fatal("Bootstrap() wrote an empty password file")
	}

	if strings.Contains(out.String(), password) {
		t.Fatalf("Bootstrap() wrote the password itself to out (%q) when a file path was given — only a path notice must appear there", out.String())
	}
	if !strings.Contains(out.String(), path) {
		t.Fatalf("Bootstrap() output %q does not name the password file path %q", out.String(), path)
	}

	op := st.operators.operators[0]
	ok, err := VerifyPassword(password, string(op.PasswordHash))
	if err != nil {
		t.Fatalf("VerifyPassword(): %v", err)
	}
	if !ok {
		t.Fatal("the password written to the file does not verify against the created operator's hash")
	}
}

// TestBootstrapDoesNotWritePasswordFileOnRestart mirrors
// TestBootstrapDoesNotReprintOnSubsequentStart for the file-delivery path: a
// restart against a database that already has an operator must touch
// neither out nor passwordFilePath.
func TestBootstrapDoesNotWritePasswordFileOnRestart(t *testing.T) {
	ctx := context.Background()
	st := newFakeBootstrapStore()
	var firstOut bytes.Buffer
	path := filepath.Join(t.TempDir(), "bootstrap-password.txt")

	if err := Bootstrap(ctx, st, noEnv, &firstOut, path); err != nil {
		t.Fatalf("first Bootstrap(): %v", err)
	}
	firstContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading password file after first Bootstrap(): %v", err)
	}

	var secondOut bytes.Buffer
	if err := Bootstrap(ctx, st, noEnv, &secondOut, path); err != nil {
		t.Fatalf("second Bootstrap() (restart): %v", err)
	}
	if secondOut.Len() != 0 {
		t.Fatalf("second Bootstrap() printed %q, want nothing on a restart against an existing operator", secondOut.String())
	}

	secondContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading password file after second Bootstrap(): %v", err)
	}
	if !bytes.Equal(firstContent, secondContent) {
		t.Fatal("second Bootstrap() (restart) modified the password file, want it left untouched")
	}
}

// TestBootstrapFailsCleanlyAndCreatesNoOperatorWhenPasswordFileCannotBeWritten
// proves the delivery-before-create ordering: if the password cannot be
// delivered, no operator must exist afterward, so a later retry (still
// against an empty database) can attempt delivery again instead of leaving
// an operator whose password nobody ever received.
func TestBootstrapFailsCleanlyAndCreatesNoOperatorWhenPasswordFileCannotBeWritten(t *testing.T) {
	ctx := context.Background()
	st := newFakeBootstrapStore()
	var out bytes.Buffer

	dir := t.TempDir()
	// blocker is a regular file; the password file's directory creation
	// must fail trying to create a directory through it.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("seeding blocker file: %v", err)
	}
	path := filepath.Join(blocker, "sub", "bootstrap-password.txt")

	err := Bootstrap(ctx, st, noEnv, &out, path)
	if err == nil {
		t.Fatal("Bootstrap() must return an error when the password file cannot be written")
	}
	if n, _ := st.operators.Count(ctx); n != 0 {
		t.Fatalf("operator count after a failed password delivery = %d, want 0 — a retry must be able to try again cleanly", n)
	}
}

func TestGeneratePasswordProducesFullLengthOutput(t *testing.T) {
	password, err := GeneratePassword(20)
	if err != nil {
		t.Fatalf("GeneratePassword(): %v", err)
	}
	if len(password) != 20 {
		t.Fatalf("GeneratePassword(20) returned a %d-character password, want 20", len(password))
	}
	for _, r := range password {
		if !strings.ContainsRune(generatedPasswordAlphabet, r) {
			t.Fatalf("GeneratePassword() produced character %q outside the declared alphabet", r)
		}
	}
}
