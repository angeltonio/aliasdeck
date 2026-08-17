// Package watchconfig owns the synchronization interval contract shared by
// the CLI, LaunchAgent installer, and server-generated enrollment command.
package watchconfig

import (
	"fmt"
	"time"
)

const (
	DefaultInterval = 30 * time.Second
	MinInterval     = time.Second
	MaxInterval     = 24 * time.Hour
)

// EnrollmentIntervals is the closed set the Add device UI may embed in a
// token-bearing command. CLI overrides intentionally retain the wider bounds.
var EnrollmentIntervals = []time.Duration{
	5 * time.Second,
	30 * time.Second,
	time.Minute,
	5 * time.Minute,
}

// Parse resolves an optional Go duration. Empty selects the production
// default; explicit values must stay inside the operational safety bounds.
func Parse(raw string) (time.Duration, error) {
	if raw == "" {
		return DefaultInterval, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", raw, err)
	}
	if err := Validate(d); err != nil {
		return 0, err
	}
	return d, nil
}

func Validate(d time.Duration) error {
	if d < MinInterval || d > MaxInterval {
		return fmt.Errorf("interval must be between %s and %s", MinInterval, MaxInterval)
	}
	return nil
}

func ValidateEnrollment(d time.Duration) error {
	for _, allowed := range EnrollmentIntervals {
		if d == allowed {
			return nil
		}
	}
	return fmt.Errorf("enrollment interval must be one of 5s, 30s, 1m0s, 5m0s")
}
