package app

import (
	"context"
	"testing"
)

func TestStatusReportsActiveSource(t *testing.T) {
	te := newTestEnv(t)
	seedSyncableDevice(t, te)

	if _, err := Sync(context.Background(), te.Env, Options{}); err != nil {
		t.Fatalf("Sync() returned an error: %v", err)
	}

	report, err := Status(context.Background(), te.Env, Options{})
	if err != nil {
		t.Fatalf("Status() returned an error: %v", err)
	}

	if report.Source.Type != "file" {
		t.Errorf("Source.Type = %q, want %q", report.Source.Type, "file")
	}
	if report.Device.Name != "test-device" {
		t.Errorf("Device.Name = %q, want %q", report.Device.Name, "test-device")
	}
	if report.State.LastSyncAt.IsZero() {
		t.Error("State.LastSyncAt is zero after a successful sync")
	}
	if !report.UpToDate {
		t.Error("UpToDate = false right after a successful sync")
	}
}

func TestStatusReportsNotInitialized(t *testing.T) {
	te := newTestEnv(t)

	_, err := Status(context.Background(), te.Env, Options{})
	if err != ErrNotInitialized {
		t.Errorf("Status() error = %v, want ErrNotInitialized", err)
	}
}
