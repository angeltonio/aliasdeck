package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeWatchTimer struct {
	c       chan time.Time
	stopped bool
}

func (t *fakeWatchTimer) Chan() <-chan time.Time { return t.c }

func (t *fakeWatchTimer) Stop() bool {
	t.stopped = true
	return true
}

func TestRunWatchRunsImmediatelyAndRepeatsAfterInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var timers []*fakeWatchTimer
	intervals := make(chan time.Duration, 2)
	calls := make(chan string, 4)
	var stdout bytes.Buffer
	done := make(chan error, 1)

	go func() {
		done <- runWatch(ctx, 3*time.Minute, watchDeps{
			heartbeat: func(context.Context) error {
				calls <- "heartbeat"
				return nil
			},
			sync: func(context.Context) error {
				calls <- "sync"
				return nil
			},
			newTimer: func(d time.Duration) watchTimer {
				timer := &fakeWatchTimer{c: make(chan time.Time, 1)}
				timers = append(timers, timer)
				intervals <- d
				return timer
			},
			stdout: &stdout,
			stderr: &bytes.Buffer{},
		})
	}()

	assertWatchCall(t, calls, "heartbeat")
	assertWatchCall(t, calls, "sync")
	if got := <-intervals; got != 3*time.Minute {
		t.Fatalf("watch interval = %s, want %s", got, 3*time.Minute)
	}

	timers[0].c <- time.Now()
	assertWatchCall(t, calls, "heartbeat")
	assertWatchCall(t, calls, "sync")
	if got := <-intervals; got != 3*time.Minute {
		t.Fatalf("repeated watch interval = %s, want %s", got, 3*time.Minute)
	}
	if got := strings.Count(stdout.String(), "watch: heartbeat ok; sync ok\n"); got != 2 {
		t.Fatalf("successful watch messages = %d, want 2: %q", got, stdout.String())
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("runWatch() error = %v, want nil", err)
	}
	if !timers[1].stopped {
		t.Fatal("pending timer was not stopped on cancellation")
	}
}

func TestRunWatchReportsTransientErrorsAndContinues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stderr bytes.Buffer
	calls := make(chan string, 4)
	timers := make(chan *fakeWatchTimer, 2)
	done := make(chan error, 1)

	go func() {
		done <- runWatch(ctx, time.Minute, watchDeps{
			heartbeat: func(context.Context) error {
				calls <- "heartbeat"
				return errors.New("server unavailable")
			},
			sync: func(context.Context) error {
				calls <- "sync"
				return errors.New("source unavailable")
			},
			newTimer: func(time.Duration) watchTimer {
				timer := &fakeWatchTimer{c: make(chan time.Time, 1)}
				timers <- timer
				return timer
			},
			stderr: &stderr,
		})
	}()

	assertWatchCall(t, calls, "heartbeat")
	assertWatchCall(t, calls, "sync")
	firstTimer := <-timers
	firstTimer.c <- time.Now()
	assertWatchCall(t, calls, "heartbeat")
	assertWatchCall(t, calls, "sync")
	<-timers

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("runWatch() error = %v, want nil", err)
	}

	got := stderr.String()
	for _, want := range []string{"watch heartbeat: server unavailable", "watch sync: source unavailable"} {
		if !strings.Contains(got, want) {
			t.Errorf("stderr = %q, want %q", got, want)
		}
	}
}

func TestNewWatchCmdDefaultsAndRejectsNonPositiveInterval(t *testing.T) {
	cmd := newWatchCmd()
	interval, err := cmd.Flags().GetDuration("interval")
	if err != nil {
		t.Fatalf("reading interval flag: %v", err)
	}
	if interval != defaultWatchInterval {
		t.Errorf("default interval = %s, want %s", interval, defaultWatchInterval)
	}

	cmd.SetArgs([]string{"--interval=0"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "greater than zero") {
		t.Errorf("zero interval error = %v, want validation error", err)
	}
}

func assertWatchCall(t *testing.T, calls <-chan string, want string) {
	t.Helper()
	select {
	case got := <-calls:
		if got != want {
			t.Errorf("watch call = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %q", want)
	}
}
