package server

import (
	"testing"
	"time"
)

// TestConfigWithDefaultsAppliesEveryDefault is the regression test for the
// bounded-review finding that defaultShutdownTimeout could be changed to
// any value without failing anything: every existing server_test.go and
// shutdown_test.go case injects its own ShutdownTimeout, so nothing
// previously asserted what withDefaults() itself produces for a Config left
// at its zero value — the exact Config cmd/aliasdeck/serve.go constructs in
// production, and the exact 10s bound design.md's Bounded Operations table
// documents for "Shutdown".
func TestConfigWithDefaultsAppliesEveryDefault(t *testing.T) {
	got := Config{}.withDefaults()

	const wantShutdownTimeout = 10 * time.Second
	if got.ShutdownTimeout != wantShutdownTimeout {
		t.Errorf("withDefaults().ShutdownTimeout = %v, want %v (design's Bounded Operations table, \"Shutdown ... srv.Shutdown(ctx) with a 10s drain\")",
			got.ShutdownTimeout, wantShutdownTimeout)
	}
	if got.Getenv == nil {
		t.Error("withDefaults().Getenv is nil, want os.Getenv")
	}
	if got.Stdout == nil {
		t.Error("withDefaults().Stdout is nil, want io.Discard")
	}
	if got.OpenStore == nil {
		t.Error("withDefaults().OpenStore is nil, want a sqlitestore.Open-backed default")
	}
	if got.Listen == nil {
		t.Error("withDefaults().Listen is nil, want a net.Listen-backed default")
	}
	if got.BootstrapPasswordFile != "" {
		t.Errorf("withDefaults().BootstrapPasswordFile = %q, want empty (console printing) when the caller does not set it", got.BootstrapPasswordFile)
	}
}

// TestConfigWithDefaultsPreservesExplicitShutdownTimeout proves the other
// half: an explicitly injected ShutdownTimeout (every test in this package
// besides the one above sets one) must survive withDefaults() unchanged,
// so the short bounds shutdown_test.go relies on to avoid a real 10s wait
// keep working.
func TestConfigWithDefaultsPreservesExplicitShutdownTimeout(t *testing.T) {
	const injected = 20 * time.Millisecond
	got := Config{ShutdownTimeout: injected}.withDefaults()
	if got.ShutdownTimeout != injected {
		t.Errorf("withDefaults().ShutdownTimeout = %v, want the injected %v preserved", got.ShutdownTimeout, injected)
	}
}

// TestConfigWithDefaultsNeverMutatesReceiver proves withDefaults returns a
// copy: calling it must not leave the original Config's zero-value fields
// populated, since Run relies on cfg.withDefaults() being safe to call
// without affecting anything the caller still holds a reference to.
func TestConfigWithDefaultsNeverMutatesReceiver(t *testing.T) {
	cfg := Config{}
	_ = cfg.withDefaults()

	if cfg.ShutdownTimeout != 0 {
		t.Errorf("original Config.ShutdownTimeout = %v after withDefaults(), want it left at the zero value", cfg.ShutdownTimeout)
	}
	if cfg.Getenv != nil {
		t.Error("original Config.Getenv is non-nil after withDefaults(), want it left at the zero value")
	}
}

func TestLocalSetupEnabledParsesStrictBooleanConfiguration(t *testing.T) {
	for _, tt := range []struct {
		value   string
		want    bool
		wantErr bool
	}{
		{value: "", want: false},
		{value: "false", want: false},
		{value: "true", want: true},
		{value: "TRUE", wantErr: true},
		{value: "1", wantErr: true},
		{value: "yes", wantErr: true},
	} {
		t.Run(tt.value, func(t *testing.T) {
			got, err := localSetupEnabled(func(name string) string {
				if name != localSetupEnv {
					t.Fatalf("environment lookup = %q, want %q", name, localSetupEnv)
				}
				return tt.value
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("localSetupEnabled(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("localSetupEnabled(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestResolveEnrollmentWatchInterval(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		explicit  time.Duration
		want      time.Duration
		wantError bool
	}{
		{name: "production default", want: 30 * time.Second},
		{name: "dev environment", env: "5s", want: 5 * time.Second},
		{name: "explicit config", env: "5s", explicit: time.Minute, want: time.Minute},
		{name: "malformed environment", env: "fast", wantError: true},
		{name: "too fast environment", env: "500ms", wantError: true},
		{name: "valid CLI duration but not UI preset", env: "10s", wantError: true},
		{name: "too slow explicit", explicit: 25 * time.Hour, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{EnrollmentWatchInterval: tt.explicit, Getenv: func(name string) string {
				if name != enrollmentWatchIntervalEnv {
					t.Fatalf("environment lookup = %q, want %q", name, enrollmentWatchIntervalEnv)
				}
				return tt.env
			}}
			got, err := resolveEnrollmentWatchInterval(cfg)
			if (err != nil) != tt.wantError {
				t.Fatalf("resolveEnrollmentWatchInterval() error = %v, wantError %t", err, tt.wantError)
			}
			if !tt.wantError && got != tt.want {
				t.Fatalf("resolveEnrollmentWatchInterval() = %s, want %s", got, tt.want)
			}
		})
	}
}
