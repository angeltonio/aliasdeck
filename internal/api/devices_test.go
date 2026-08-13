package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
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
	return registerTestDeviceNamed(t, h, s, "laptop")
}

// registerTestDeviceNamed is registerTestDevice with a caller-chosen name,
// for WARNING 3's own tests that need two distinguishable devices (e.g. to
// prove a Get-by-id handler does not ignore the id and return some other
// device instead).
func registerTestDeviceNamed(t *testing.T, h http.Handler, s *fakeStore, name string) (deviceID, deviceToken string) {
	t.Helper()
	enrollment := mintEnrollmentToken(s, nil, time.Now().Add(15*time.Minute))

	body, _ := json.Marshal(registerRequest{Name: name, Platform: domain.PlatformMacOS, Shell: domain.ShellZsh})
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

// TestDevicesListReturnsRegisteredDevices is WARNING 3's own RED test for
// handleDevicesList: it registers two devices and asserts an authenticated
// list returns exactly those two, by name. Mutation this test detects: a
// handler returning an empty slice, a hard-coded single entry, or ignoring
// the store entirely.
func TestDevicesListReturnsRegisteredDevices(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	session := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	registerTestDeviceNamed(t, h, s, "laptop")
	registerTestDeviceNamed(t, h, s, "desktop")

	rec := doRequest(h, http.MethodGet, "/api/v1/devices", session, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/devices = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got []domain.Device
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding list response: %v", err)
	}
	names := make([]string, len(got))
	for i, d := range got {
		names[i] = d.Name
	}
	sort.Strings(names)
	want := []string{"desktop", "laptop"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] {
		t.Fatalf("device names = %v, want exactly %v", names, want)
	}
}

// TestDevicesGetReturnsTheRequestedDeviceByID is WARNING 3's own RED test
// for handleDevicesGet: two distinct registered devices, GET by id must
// return exactly the one asked for. It deliberately requests "laptop",
// not "desktop": fakeDeviceRepo.List (like the real backend) orders
// deterministically by name, so "desktop" sorts first — a handler that
// ignores r.PathValue("id") and returns list[0] by mistake would still
// look correct if this test asked for the alphabetically-first device,
// passing for the wrong reason. Asking for the device that is NOT first
// in that order is what actually exercises the id lookup. Mutation this
// test detects: a handler ignoring r.PathValue("id") and returning some
// other device (e.g. the first one in a list).
func TestDevicesGetReturnsTheRequestedDeviceByID(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	session := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	registerTestDeviceNamed(t, h, s, "desktop")
	laptopID, _ := registerTestDeviceNamed(t, h, s, "laptop")

	rec := doRequest(h, http.MethodGet, "/api/v1/devices/"+laptopID, session, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/devices/%s = %d, want %d, body=%s", laptopID, rec.Code, http.StatusOK, rec.Body.String())
	}
	var got domain.Device
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding get response: %v", err)
	}
	if got.ID != laptopID || got.Name != "laptop" {
		t.Fatalf("GET /api/v1/devices/%s returned %+v, want the device named %q with that id", laptopID, got, "laptop")
	}
}

// TestDevicesUpdateAppliesNameAndProfileIDs is WARNING 3's own RED test for
// handleDevicesUpdate: a PUT changing Name and ProfileIDs must be reflected
// on a subsequent GET (DeviceRepo.Update's own documented scope — name and
// profile membership only). Mutation this test detects: a handler that
// decodes the body but discards it instead of calling Devices().Update.
func TestDevicesUpdateAppliesNameAndProfileIDs(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	session := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	deviceID, _ := registerTestDevice(t, h, s)
	profileID := createTestProfile(t, h, session, "Homelab")

	body, _ := json.Marshal(domain.Device{Name: "renamed-laptop", ProfileIDs: []string{profileID}})
	updateRec := doRequest(h, http.MethodPut, "/api/v1/devices/"+deviceID, session, body)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("PUT /api/v1/devices/%s = %d, want %d, body=%s", deviceID, updateRec.Code, http.StatusOK, updateRec.Body.String())
	}

	getRec := doRequest(h, http.MethodGet, "/api/v1/devices/"+deviceID, session, nil)
	var got domain.Device
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding get response: %v", err)
	}
	if got.Name != "renamed-laptop" {
		t.Fatalf("Name after update = %q, want %q — the update body was not applied", got.Name, "renamed-laptop")
	}
	if len(got.ProfileIDs) != 1 || got.ProfileIDs[0] != profileID {
		t.Fatalf("ProfileIDs after update = %v, want exactly [%q]", got.ProfileIDs, profileID)
	}
}

// TestDevicesDeleteRemovesTheDevice is WARNING 3's own RED test for
// handleDevicesDelete: a subsequent GET after delete must 404. Mutation
// this test detects: a no-op handler answering 204 without ever calling
// Devices().Delete.
func TestDevicesDeleteRemovesTheDevice(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	session := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	deviceID, _ := registerTestDevice(t, h, s)

	deleteRec := doRequest(h, http.MethodDelete, "/api/v1/devices/"+deviceID, session, nil)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/v1/devices/%s = %d, want %d, body=%s", deviceID, deleteRec.Code, http.StatusNoContent, deleteRec.Body.String())
	}

	getRec := doRequest(h, http.MethodGet, "/api/v1/devices/"+deviceID, session, nil)
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("GET a deleted device = %d, want %d — the delete did not actually remove it", getRec.Code, http.StatusNotFound)
	}
}

// TestDevicesDeleteRevokesItsToken is bounded-review finding 2's own
// store-level RED+GREEN test, sibling to TestDevicesRevokeInvalidatesItsToken:
// deleting a device must also revoke its device-kind token, not merely
// remove the row. Mutation this test detects: handleDevicesDelete calling
// Devices().Delete without also calling Tokens().RevokeSubject — the token
// would still verify (RevokedAt still zero) even though its device is gone.
// TestSyncFailsWithADeletedDeviceTokenWithAnActionableMessage (sync_test.go)
// is this same fix's end-to-end proof against a real device-gated route.
func TestDevicesDeleteRevokesItsToken(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	session := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	deviceID, deviceToken := registerTestDevice(t, h, s)

	deleteRec := doRequest(h, http.MethodDelete, "/api/v1/devices/"+deviceID, session, nil)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/v1/devices/%s = %d, want %d, body=%s", deviceID, deleteRec.Code, http.StatusNoContent, deleteRec.Body.String())
	}

	assertTokenIsRevoked(t, s, deviceToken)
}

// TestDevicesRegisterLeavesADiscoverableDeviceWhenTokenIssuanceFails is
// WARNING 4's register-side RED test (design decision 27): auth.ConsumeEnrollment
// is atomic, but the device-token Create after it is a separate write. This
// forces exactly that second write to fail once and asserts the deliberate,
// documented response: the device is already discoverable (ConsumeEnrollment's
// own atomicity already committed it), the error names its id, and the
// documented recovery path (rotate-token) actually works with no need to
// repeat the single-use enrollment exchange.
func TestDevicesRegisterLeavesADiscoverableDeviceWhenTokenIssuanceFails(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	session := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	enrollment := mintEnrollmentToken(s, nil, time.Now().Add(15*time.Minute))
	s.tokenCreateErr = errors.New("simulated token store failure")

	body, _ := json.Marshal(registerRequest{Name: "laptop", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh})
	rec := doRequest(h, http.MethodPost, devicesRegisterPattern, enrollment, body)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("register with a failing token Create = %d, want %d, body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	var decoded struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	deviceID, _ := decoded.Error.Details["deviceId"].(string)
	if deviceID == "" {
		t.Fatalf("error response did not name the orphaned device id: %s", rec.Body.String())
	}

	getRec := doRequest(h, http.MethodGet, "/api/v1/devices/"+deviceID, session, nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET the orphaned device = %d, want %d — ConsumeEnrollment's own atomicity should have already committed it", getRec.Code, http.StatusOK)
	}

	rotateRec := doRequest(h, http.MethodPost, "/api/v1/devices/"+deviceID+"/token", session, nil)
	if rotateRec.Code != http.StatusOK {
		t.Fatalf("recovery rotate-token = %d, want %d, body=%s — the documented recovery path did not work", rotateRec.Code, http.StatusOK, rotateRec.Body.String())
	}
}

// TestDevicesRotateTokenIsSafeToRetryWhenTokenIssuanceFails is WARNING 4's
// rotate-side RED test (design decision 27): the old device-kind token(s)
// are revoked before the replacement is minted, so a failure in between
// leaves the device with zero valid tokens until this endpoint is retried.
// This forces exactly that failure once, asserts the documented error names
// the device, then retries the identical request and asserts it succeeds —
// RevokeSubject's own "revoked_at IS NULL" filter makes the retry's own
// revoke step a no-op rather than a second failure mode.
func TestDevicesRotateTokenIsSafeToRetryWhenTokenIssuanceFails(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	session := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	deviceID, oldToken := registerTestDevice(t, h, s)

	s.tokenCreateErr = errors.New("simulated token store failure")
	failRec := doRequest(h, http.MethodPost, "/api/v1/devices/"+deviceID+"/token", session, nil)
	if failRec.Code != http.StatusInternalServerError {
		t.Fatalf("rotate with a failing token Create = %d, want %d, body=%s", failRec.Code, http.StatusInternalServerError, failRec.Body.String())
	}
	var decoded struct {
		Error struct {
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(failRec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	if got, _ := decoded.Error.Details["deviceId"].(string); got != deviceID {
		t.Fatalf("error response deviceId = %q, want %q", got, deviceID)
	}
	assertTokenIsRevoked(t, s, oldToken)

	retryRec := doRequest(h, http.MethodPost, "/api/v1/devices/"+deviceID+"/token", session, nil)
	if retryRec.Code != http.StatusOK {
		t.Fatalf("retrying rotate after the forced failure = %d, want %d, body=%s — the documented retry path did not work", retryRec.Code, http.StatusOK, retryRec.Body.String())
	}
}
