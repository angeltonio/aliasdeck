package releasecheck

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name, version, sourceRef, wantErr string
	}{
		{name: "exact release binding", version: "v0.5.4", sourceRef: "v0.5.4"},
		{name: "tag disagrees with client", version: "v0.5.3", sourceRef: "v0.5.3", wantErr: "does not match client version"},
		{name: "manual source diverges", version: "v0.5.4", sourceRef: "main", wantErr: "must equal release tag"},
		{name: "raw commit is not an equivalent source selector", version: "v0.5.4", sourceRef: "0123456789abcdef", wantErr: "must equal release tag"},
		{name: "missing v prefix", version: "0.5.4", sourceRef: "0.5.4", wantErr: "stable vX.Y.Z tag"},
		{name: "prerelease is not silently published as stable", version: "v0.5.4-rc.1", sourceRef: "v0.5.4-rc.1", wantErr: "stable vX.Y.Z tag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.version, tt.sourceRef)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
