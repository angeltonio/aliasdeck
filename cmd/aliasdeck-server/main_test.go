package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// These tests deliberately never invoke newRootCmd()'s RunE: doing so would
// start server.Run, a long-running process, which is exactly what this
// project's own incident (a previous agent binding a fixed port, finding it
// occupied, and killing the unrelated process holding it) says never to do
// in a test, a smoke check, or manual verification. Every test below
// exercises resolveServeDBPath, the flag definitions, and the two
// terminal-detection helpers directly and in isolation — none binds a
// socket, and any that touches a filesystem path uses t.TempDir().

// TestDefaultServeAddrIsLoopbackOnly is the regression test for the
// bounded-review finding that a zero-flag start bound every interface:
// defaultServeAddr must name an explicit loopback host, never an empty host
// (which net.Listen("tcp", …) treats as the wildcard address).
func TestDefaultServeAddrIsLoopbackOnly(t *testing.T) {
	host, _, ok := strings.Cut(defaultServeAddr, ":")
	if !ok {
		t.Fatalf("defaultServeAddr = %q, want a host:port pair", defaultServeAddr)
	}
	if host != "127.0.0.1" && host != "localhost" {
		t.Fatalf("defaultServeAddr = %q, want a loopback host, got host %q", defaultServeAddr, host)
	}
}

// TestNewRootCmdAddrFlagDefaultsToLoopbackAndDocumentsWidening asserts the
// flag wiring directly on the constructed *cobra.Command: the --addr
// default must be defaultServeAddr, and the help text must name both the
// default and what widening it means.
func TestNewRootCmdAddrFlagDefaultsToLoopbackAndDocumentsWidening(t *testing.T) {
	cmd := newRootCmd()

	flag := cmd.Flags().Lookup("addr")
	if flag == nil {
		t.Fatal("newRootCmd() has no --addr flag")
	}
	if flag.DefValue != defaultServeAddr {
		t.Errorf("--addr default = %q, want %q", flag.DefValue, defaultServeAddr)
	}
	if !strings.Contains(flag.Usage, "loopback") {
		t.Errorf("--addr usage = %q, want it to name the loopback default", flag.Usage)
	}
	if !strings.Contains(flag.Usage, "0.0.0.0") && !strings.Contains(flag.Usage, "other machines") {
		t.Errorf("--addr usage = %q, want it to explain what widening the bind means", flag.Usage)
	}
}

// TestNewRootCmdDBFlagDefaultsToEmpty asserts --db's default is empty
// (meaning "derive it from config.Base"), matching resolveServeDBPath's own
// precedence.
func TestNewRootCmdDBFlagDefaultsToEmpty(t *testing.T) {
	cmd := newRootCmd()

	flag := cmd.Flags().Lookup("db")
	if flag == nil {
		t.Fatal("newRootCmd() has no --db flag")
	}
	if flag.DefValue != "" {
		t.Errorf("--db default = %q, want empty", flag.DefValue)
	}
}

// TestNewRootCmdServesWithoutAVerb keeps the half of the original "no
// subcommands" design choice that still holds: serving is what the binary
// does, so the root command runs it directly and `aliasdeck-server serve`
// never becomes the spelling. Adding reset-password did not change that.
func TestNewRootCmdServesWithoutAVerb(t *testing.T) {
	cmd := newRootCmd()
	if cmd.RunE == nil {
		t.Error("newRootCmd() has no RunE — serving must not require a verb")
	}
}

// TestNewRootCmdRegistersOnlyTheRecoveryVerb pins the subcommand set. The
// original test asserted zero subcommands; reset-password earns one by not
// serving at all — it mutates a stored credential and exits. Naming the set
// exactly, rather than counting it, keeps this a real guard: a second verb
// added by accident still fails here, and a rename has to be deliberate.
func TestNewRootCmdRegistersOnlyTheRecoveryVerb(t *testing.T) {
	got := make([]string, 0, 1)
	for _, sub := range newRootCmd().Commands() {
		got = append(got, sub.Name())
	}

	want := []string{"reset-password"}
	if !slices.Equal(got, want) {
		t.Errorf("newRootCmd() subcommands = %v, want exactly %v", got, want)
	}
}

// TestResolveServeDBPathExplicitTakesPrecedence proves the explicit --db
// value is returned verbatim, without consulting config.Base or touching
// the filesystem at all.
func TestResolveServeDBPathExplicitTakesPrecedence(t *testing.T) {
	const explicit = "/some/explicit/path/server.db"

	got, err := resolveServeDBPath(explicit)
	if err != nil {
		t.Fatalf("resolveServeDBPath(%q) = _, %v, want nil error", explicit, err)
	}
	if got != explicit {
		t.Errorf("resolveServeDBPath(%q) = %q, want it returned verbatim", explicit, got)
	}
}

// TestResolveServeDBPathDefaultUnderConfigBase proves the default path
// derivation and the base-directory creation side effect, isolated to a
// t.TempDir() via ALIASDECK_HOME exactly like cmd/aliasdeck's own
// filesystem-touching tests.
func TestResolveServeDBPathDefaultUnderConfigBase(t *testing.T) {
	base := filepath.Join(t.TempDir(), ".config", "aliasdeck")
	t.Setenv("ALIASDECK_HOME", base)

	got, err := resolveServeDBPath("")
	if err != nil {
		t.Fatalf("resolveServeDBPath(\"\") = _, %v, want nil error", err)
	}

	want := filepath.Join(base, serverDBFileName)
	if got != want {
		t.Errorf("resolveServeDBPath(\"\") = %q, want %q", got, want)
	}

	info, err := os.Stat(base)
	if err != nil {
		t.Fatalf("stat %s: %v, want resolveServeDBPath to have created the base directory", base, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s exists but is not a directory", base)
	}
}

// TestResolveServeDBPathIsIdempotentAgainstAnExistingDirectory proves the
// base directory being pre-existing is not an error: MkdirAll on an
// existing directory is a documented no-op.
func TestResolveServeDBPathIsIdempotentAgainstAnExistingDirectory(t *testing.T) {
	base := filepath.Join(t.TempDir(), ".config", "aliasdeck")
	t.Setenv("ALIASDECK_HOME", base)

	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("seeding base directory: %v", err)
	}

	if _, err := resolveServeDBPath(""); err != nil {
		t.Fatalf("resolveServeDBPath(\"\") against a pre-existing base directory = %v, want nil error", err)
	}
}

// TestIsTerminalWriterFalseForNonFileWriter proves the non-*os.File branch:
// a bytes.Buffer (what every test in this codebase injects instead of a
// real console) must be classified as not a terminal, which is what routes
// auth.Bootstrap toward the 0600 file instead of assuming a console exists.
func TestIsTerminalWriterFalseForNonFileWriter(t *testing.T) {
	var buf bytes.Buffer
	if isTerminalWriter(&buf) {
		t.Fatal("isTerminalWriter(*bytes.Buffer) = true, want false — only a real *os.File can be a terminal")
	}
}

// TestIsTerminalWriterFalseForRegularFile proves the *os.File branch that
// this environment can actually exercise without a pty: a plain regular
// file is an *os.File, so it reaches go-isatty's probe, and that probe must
// report false for it. The true branch (a real terminal) is not
// independently testable here — doing so would require a pty, which this
// project does not depend on — and is reported as such rather than papered
// over with a fake.
func TestIsTerminalWriterFalseForRegularFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "not-a-terminal")
	if err != nil {
		t.Fatalf("os.CreateTemp: %v", err)
	}
	defer f.Close()

	if isTerminalWriter(f) {
		t.Fatal("isTerminalWriter() on a regular file = true, want false")
	}
}

// TestBootstrapPasswordFilePathEmptyWhenTerminal proves the routing
// decision's terminal branch: server.Config.BootstrapPasswordFile must stay
// empty so auth.Bootstrap keeps printing directly to a real console.
func TestBootstrapPasswordFilePathEmptyWhenTerminal(t *testing.T) {
	got := bootstrapPasswordFilePath(true, "/base/server.db")
	if got != "" {
		t.Errorf("bootstrapPasswordFilePath(true, ...) = %q, want empty", got)
	}
}

// TestBootstrapPasswordFilePathDerivedFromDBPathWhenNotTerminal proves the
// other branch: the password file must sit as a sibling of the database
// file, under the same operator-owned directory.
func TestBootstrapPasswordFilePathDerivedFromDBPathWhenNotTerminal(t *testing.T) {
	got := bootstrapPasswordFilePath(false, "/base/server.db")
	want := filepath.Join("/base", bootstrapPasswordFileName)
	if got != want {
		t.Errorf("bootstrapPasswordFilePath(false, %q) = %q, want %q", "/base/server.db", got, want)
	}
}

// TestResetPasswordFilePathRoutesLikeBootstrapButToItsOwnFile proves the
// reset path makes the same terminal/file decision, and that it never lands
// on bootstrapPasswordFileName: an operator recovering access must be able
// to tell the file the reset just wrote from the one first start left
// behind, and a reset must never look like it changed nothing.
func TestResetPasswordFilePathRoutesLikeBootstrapButToItsOwnFile(t *testing.T) {
	if got := resetPasswordFilePath(true, "/base/server.db"); got != "" {
		t.Errorf("resetPasswordFilePath(true, ...) = %q, want empty", got)
	}

	got := resetPasswordFilePath(false, "/base/server.db")
	want := filepath.Join("/base", resetPasswordFileName)
	if got != want {
		t.Errorf("resetPasswordFilePath(false, %q) = %q, want %q", "/base/server.db", got, want)
	}
	if resetPasswordFileName == bootstrapPasswordFileName {
		t.Error("the reset and bootstrap password files share a name — a reset would overwrite first start's file")
	}
}

// TestRunUnknownFlagExitsNonZero proves run()'s exit-code mapping end to
// end for the one error class this test can safely trigger without
// starting the server: an unrecognized flag. It never reaches RunE.
func TestRunUnknownFlagExitsNonZero(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"--not-a-real-flag"}, &out, &errOut)
	if code != 1 {
		t.Errorf("run([--not-a-real-flag]) exit code = %d, want 1", code)
	}
	if errOut.Len() == 0 {
		t.Error("run([--not-a-real-flag]) wrote nothing to stderr, want cobra's own flag error")
	}
}
