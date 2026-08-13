package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/domain"
)

// TestDevicesEndpointsRejectUnauthenticatedRequests is
// TestAliasesEndpointsRejectUnauthenticatedRequests's sibling for devices,
// including the two auth-adjacent actions (revoke, rotate) that only an
// operator session may call.
func TestDevicesEndpointsRejectUnauthenticatedRequests(t *testing.T) {
	h := newTestRouter(t, newFakeStore())

	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/devices"},
		{http.MethodGet, "/api/v1/devices/some-id"},
		{http.MethodPut, "/api/v1/devices/some-id"},
		{http.MethodDelete, "/api/v1/devices/some-id"},
		{http.MethodPost, "/api/v1/devices/some-id/revoke"},
		{http.MethodPost, "/api/v1/devices/some-id/token"},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			rec := doRequest(h, c.method, c.path, "", []byte(`{}`))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s without auth = %d, want %d", c.method, c.path, rec.Code, http.StatusUnauthorized)
			}
		})
	}
}

// registerTestDevice drives the real devicesRegisterPattern handler (not a
// store shortcut) so a test starts from a device that was actually
// enrolled, exactly like a real one would be.
func registerTestDevice(t *testing.T, h http.Handler, s *fakeStore) (deviceID, deviceToken string) {
	t.Helper()
	enrollment := mintEnrollmentToken(s, nil, time.Now().Add(15*time.Minute))

	body, _ := json.Marshal(registerRequest{Name: "laptop", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh})
	rec := doRequest(h, http.MethodPost, devicesRegisterPattern, enrollment, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %s = %d, want %d, body=%s", devicesRegisterPattern, rec.Code, http.StatusCreated, rec.Body.String())
	}
	var got deviceTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding register response: %v", err)
	}
	if got.DeviceID == "" || got.DeviceToken == "" {
		t.Fatalf("register response missing id or token: %+v", got)
	}
	return got.DeviceID, got.DeviceToken
}

// TestDevicesRotateTokenInvalidatesThePreviousToken is the server-auth
// spec's "Device Token Rotation" scenario: after rotation, the previous
// device token must no longer authenticate anywhere RequiredKind: device
// would be checked. Mutation this test detects: rotating without first
// calling Tokens().RevokeSubject for the old device-kind token(s) — the old
// wire value would still verify.
func TestDevicesRotateTokenInvalidatesThePreviousToken(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	session := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	deviceID, oldToken := registerTestDevice(t, h, s)

	rotateRec := doRequest(h, http.MethodPost, "/api/v1/devices/"+deviceID+"/token", session, nil)
	if rotateRec.Code != http.StatusOK {
		t.Fatalf("POST rotate token = %d, want %d, body=%s", rotateRec.Code, http.StatusOK, rotateRec.Body.String())
	}
	var rotated deviceTokenResponse
	if err := json.Unmarshal(rotateRec.Body.Bytes(), &rotated); err != nil {
		t.Fatalf("decoding rotate response: %v", err)
	}
	if rotated.DeviceToken == oldToken {
		t.Fatal("rotation returned the same wire token as before")
	}

	// No device-kind-gated route exists yet in this batch's scope (sync is
	// Phase 6), so this asserts the property the future route would rely
	// on directly against the store: auth.Verify refuses any token whose
	// RevokedAt is non-zero (internal/auth/token.go), and that is exactly
	// the field RevokeSubject must have set on the old token for rotation
	// to mean anything.
	assertTokenIsRevoked(t, s, oldToken)
}

// assertTokenIsRevoked parses wire (production auth.Parse, not a shortcut)
// to recover its store-side Lookup, fetches the persisted record, and
// fails unless RevokedAt is set — the exact field auth.Verify checks
// before honoring any future request bearing this wire value.
func assertTokenIsRevoked(t *testing.T, s *fakeStore, wire string) {
	t.Helper()
	parsed, err := auth.Parse(wire)
	if err != nil {
		t.Fatalf("auth.Parse(%q): %v", wire, err)
	}
	tok, err := s.Tokens().ByLookup(t.Context(), parsed.Lookup)
	if err != nil {
		t.Fatalf("Tokens().ByLookup: %v", err)
	}
	if tok.RevokedAt.IsZero() {
		t.Fatal("token's RevokedAt is zero — it was not actually revoked")
	}
}

// TestDevicesRevokeInvalidatesItsToken is the server-auth spec's
// "Immediate Device Revocation" scenario: revoking a device must also
// revoke every device-kind token belonging to it in the same call, not
// merely mark the device row. Mutation this test detects: calling
// Devices().Revoke without also calling Tokens().RevokeSubject — the
// device's token would still authenticate after "revocation".
func TestDevicesRevokeInvalidatesItsToken(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	session := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	deviceID, deviceToken := registerTestDevice(t, h, s)

	revokeRec := doRequest(h, http.MethodPost, "/api/v1/devices/"+deviceID+"/revoke", session, nil)
	if revokeRec.Code != http.StatusNoContent {
		t.Fatalf("POST revoke = %d, want %d, body=%s", revokeRec.Code, http.StatusNoContent, revokeRec.Body.String())
	}

	assertTokenIsRevoked(t, s, deviceToken)
}
