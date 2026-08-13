package api

import (
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// updateGolden mirrors internal/renderers/golden_test.go's own -update flag
// convention: regenerate deliberately with
// `go test ./internal/api -run TestGolden -update` and read the diff before
// committing. A response's exact bytes are a contract with whatever parses
// them downstream — the "no server-side alias ID" property task 6.3 exists
// for is exactly the kind of accidental regression a byte-for-byte pin
// catches that a field-by-field assertion could still miss if a new,
// unexpected field appeared alongside the ones already checked.
var updateGolden = flag.Bool("update", false, "rewrite golden files instead of comparing against them")

// TestGoldenSyncResponse pins GET /api/v1/sync's exact response bytes
// (task 6.5): revision, device, aliases, generatedAt, and nothing else —
// adding a server-side alias ID becomes a visible diff here, not just a
// field-level test failure.
func TestGoldenSyncResponse(t *testing.T) {
	s := newFakeStore()

	if _, err := s.Profiles().Create(t.Context(), domain.Profile{ID: "prof-development", Name: "development"}); err != nil {
		t.Fatalf("seeding profile: %v", err)
	}
	if _, err := s.Aliases().Create(t.Context(), domain.Alias{
		ID: "alias-dps", Name: "dps", Command: "docker ps", Description: "list containers", Enabled: true,
	}); err != nil {
		t.Fatalf("seeding alias: %v", err)
	}
	if _, err := s.Aliases().Create(t.Context(), domain.Alias{
		ID: "alias-winonly", Name: "winonly", Command: "Get-Process", Enabled: true,
		Platforms: []domain.Platform{domain.PlatformWindows},
	}); err != nil {
		t.Fatalf("seeding alias: %v", err)
	}
	s.devices["device-golden"] = domain.Device{
		ID: "device-golden", Name: "laptop", Platform: domain.PlatformLinux, Shell: domain.ShellBash,
		ProfileIDs: []string{"prof-development"},
	}
	deviceToken := mintDeviceTokenFor(s, "device-golden")

	fixedNow := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	h, err := NewRouter(s, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatalf("NewRouter(...) = %v, want nil", err)
	}

	rec := doRequest(h, http.MethodGet, syncPattern+"?platform=macos&shell=zsh", deviceToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want %d, body=%s", syncPattern, rec.Code, http.StatusOK, rec.Body.String())
	}

	compareGolden(t, "sync_response.golden", rec.Body.Bytes())
}

// TestGoldenErrorResponse pins the shape of sync's own actionable 401
// (design decision 28) — the one error response in this API that
// deliberately does not say "unauthorized".
func TestGoldenErrorResponse(t *testing.T) {
	h := newTestRouter(t, newFakeStore())

	rec := doRequest(h, http.MethodGet, syncPattern+"?platform=macos&shell=zsh", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET %s without auth = %d, want %d", syncPattern, rec.Code, http.StatusUnauthorized)
	}

	compareGolden(t, "error_response.golden", rec.Body.Bytes())
}

// compareGolden is this package's own minimal version of
// internal/renderers/golden_test.go's pattern, sized for two files instead
// of a table: read testdata/name, or write it under -update.
func compareGolden(t *testing.T, name string, got []byte) {
	t.Helper()

	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("creating testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("writing golden %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden %s (run with -update to create it): %v", path, err)
	}

	var gotIndented, wantIndented interface{}
	if err := json.Unmarshal(got, &gotIndented); err != nil {
		t.Fatalf("got is not valid JSON: %v", err)
	}
	if err := json.Unmarshal(want, &wantIndented); err != nil {
		t.Fatalf("golden %s is not valid JSON: %v", path, err)
	}

	gotBytes, _ := json.MarshalIndent(gotIndented, "", "  ")
	wantBytes, _ := json.MarshalIndent(wantIndented, "", "  ")
	if string(gotBytes) != string(wantBytes) {
		t.Fatalf("golden %s mismatch:\n got=%s\nwant=%s", path, gotBytes, wantBytes)
	}
}
