package watchconfig

import (
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    time.Duration
		wantErr string
	}{
		{name: "default", want: 30 * time.Second},
		{name: "dev interval", raw: "5s", want: 5 * time.Second},
		{name: "minimum", raw: "1s", want: time.Second},
		{name: "maximum", raw: "24h", want: 24 * time.Hour},
		{name: "invalid syntax", raw: "fast", wantErr: "invalid duration"},
		{name: "below minimum", raw: "999ms", wantErr: "between 1s and 24h0m0s"},
		{name: "above maximum", raw: "25h", wantErr: "between 1s and 24h0m0s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.raw)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Parse(%q) error = %v, want %q", tt.raw, err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("Parse(%q) = %s, %v; want %s, nil", tt.raw, got, err, tt.want)
			}
		})
	}
}

func TestValidateEnrollmentUsesClosedUIPresetSet(t *testing.T) {
	for _, interval := range []time.Duration{5 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute} {
		if err := ValidateEnrollment(interval); err != nil {
			t.Errorf("ValidateEnrollment(%s) = %v", interval, err)
		}
	}
	for _, interval := range []time.Duration{time.Second, 10 * time.Second, 10 * time.Minute} {
		if err := ValidateEnrollment(interval); err == nil {
			t.Errorf("ValidateEnrollment(%s) accepted a non-UI preset", interval)
		}
	}
}
