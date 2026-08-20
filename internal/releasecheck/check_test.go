package releasecheck

import (
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/app"
)

func TestValidate(t *testing.T) {
	// Derive the expected tag from the client version so a version bump does
	// not require editing these cases; the binding is what is under test, not
	// any particular release number.
	current := "v" + app.Version

	tests := []struct {
		name, version, sourceRef, wantErr string
	}{
		{name: "exact release binding", version: current, sourceRef: current},
		{name: "tag disagrees with client", version: "v0.0.1", sourceRef: "v0.0.1", wantErr: "does not match client version"},
		{name: "manual source diverges", version: current, sourceRef: "main", wantErr: "must equal release tag"},
		{name: "raw commit is not an equivalent source selector", version: current, sourceRef: "0123456789abcdef", wantErr: "must equal release tag"},
		{name: "missing v prefix", version: app.Version, sourceRef: app.Version, wantErr: "stable vX.Y.Z tag"},
		{name: "prerelease is not silently published as stable", version: current + "-rc.1", sourceRef: current + "-rc.1", wantErr: "stable vX.Y.Z tag"},
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
