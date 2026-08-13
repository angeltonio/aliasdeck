package source

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRunGitSetsNonInteractiveEnvironment is the RED test for the "Git
// subprocess environment" threat-matrix row (design decision 15): a
// credential prompt would hang sync forever, the same class of failure
// already fixed for the stdin prompt elsewhere in this project.
//
// It never shells out to real git: a fake executable named "git" is placed
// first on PATH, so this proves what RunGit's own subprocess environment
// contains without any network access or a real git binary.
func TestRunGitSetsNonInteractiveEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake git script requires a POSIX shell")
	}

	work := t.TempDir()
	envFile := filepath.Join(work, "env.out")
	scriptDir := t.TempDir()
	script := "#!/bin/sh\nenv > " + envFile + "\n"
	if err := os.WriteFile(filepath.Join(scriptDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake git script: %v", err)
	}
	t.Setenv("PATH", scriptDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if _, err := RunGit(context.Background(), work, "status"); err != nil {
		t.Fatalf("RunGit() returned an error: %v", err)
	}

	env, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("reading captured environment: %v", err)
	}
	for _, want := range []string{
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
	} {
		if !strings.Contains(string(env), want) {
			t.Errorf("captured environment does not contain %q:\n%s", want, env)
		}
	}
}

// TestRunGitNeverInvokesAShell proves RunGit hands git's argv directly to
// exec, never through "sh -c": a hostile argv element containing shell
// metacharacters must arrive at the fake "git" script as one literal
// argument and must never trigger a second command.
func TestRunGitNeverInvokesAShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake git script requires a POSIX shell")
	}

	work := t.TempDir()
	argsFile := filepath.Join(work, "args.out")
	scriptDir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\n"
	if err := os.WriteFile(filepath.Join(scriptDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake git script: %v", err)
	}
	t.Setenv("PATH", scriptDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	pwned := filepath.Join(work, "pwned")
	hostile := "clone; touch " + pwned

	if _, err := RunGit(context.Background(), work, hostile); err != nil {
		t.Fatalf("RunGit() returned an error: %v", err)
	}

	if _, err := os.Stat(pwned); err == nil {
		t.Fatal("hostile argv element was interpreted by a shell instead of passed through literally")
	}

	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("reading captured args: %v", err)
	}
	want := "-C\n" + work + "\n" + hostile + "\n"
	if string(got) != want {
		t.Errorf("captured args = %q, want %q", got, want)
	}
}

// TestRunGitReturnsStderrOnFailure pins that a failing git invocation
// surfaces the actual error text, so an unreachable remote produces an
// actionable message rather than a bare exit-status error.
func TestRunGitReturnsStderrOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake git script requires a POSIX shell")
	}

	work := t.TempDir()
	scriptDir := t.TempDir()
	script := "#!/bin/sh\necho 'fatal: could not resolve host' >&2\nexit 128\n"
	if err := os.WriteFile(filepath.Join(scriptDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake git script: %v", err)
	}
	t.Setenv("PATH", scriptDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := RunGit(context.Background(), work, "fetch")
	if err == nil {
		t.Fatal("RunGit() must return an error when git exits non-zero")
	}
	if !strings.Contains(err.Error(), "could not resolve host") {
		t.Errorf("error %q does not surface git's stderr", err)
	}
}

// TestRunGitTimesOutOnAStalledRemote pins the bound on a git invocation.
//
// The non-interactive environment stops a credential prompt from hanging; it
// does nothing about a remote that accepts a connection and then says nothing,
// which is what a firewall dropping packets or a captive portal looks like.
// Measured before this bound existed: a sync against such a remote produced no
// output and never returned.
//
// The listener here accepts and holds the connection without ever replying,
// which is precisely that shape. The context is given a short deadline so the
// test does not have to wait out GitTimeout.
func TestRunGitTimesOutOnAStalledRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	defer ln.Close()

	// Hold accepted connections open and say nothing. Closing would let git
	// fail fast, which is the case this test is not about.
	var mu sync.Mutex
	var held []net.Conn
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			c.Close()
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Not t.TempDir(): on Windows a directory cannot be removed while any
	// handle to it is open, and the git this test deliberately kills may still
	// hold one when the test ends. t.TempDir()'s cleanup fails the test for
	// that, which would report a fixture problem as a product failure.
	// Cleanup here is best-effort for the same reason.
	workDir, err := os.MkdirTemp("", "aliasdeck-gittimeout")
	if err != nil {
		t.Fatalf("creating work dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workDir) })

	start := time.Now()
	_, err = RunGit(ctx, workDir, "clone", "--quiet", "--",
		"git://"+ln.Addr().String()+"/repo", filepath.Join(workDir, "checkout"))

	if err == nil {
		t.Fatal("RunGit returned no error against a remote that never replies")
	}
	// Generous, but far below Go's own ten-minute test panic: the failure this
	// guards against is not "slightly slow", it is "never returns". The first
	// Windows run of this test hung for the full ten minutes because killing
	// git left its output pipes held open by a transport helper.
	if elapsed := time.Since(start); elapsed > 60*time.Second {
		t.Errorf("RunGit took %s to give up; the deadline was not enforced", elapsed)
	}
}
