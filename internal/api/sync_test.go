package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// registerSyncTestDevice drives the real devicesRegisterPattern handler
// (like registerTestDevice in devices_test.go) but lets a sync test choose
// platform, shell and profile membership — profileIDs travels through the
// enrollment token exactly as production ConsumeEnrollment requires
// (server-auth spec: profile membership is set by the token, never by the
// register request body).
func registerSyncTestDevice(t *testing.T, h http.Handler, s *fakeStore, name string, platform domain.Platform, shell domain.Shell, profileIDs []string) (deviceID, deviceToken string) {
	t.Helper()
	enrollment := mintEnrollmentToken(s, profileIDs, time.Now().Add(15*time.Minute))

	body, _ := json.Marshal(registerRequest{Name: name, Platform: platform, Shell: shell})
	rec := doRequest(h, http.MethodPost, devicesRegisterPattern, enrollment, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %s = %d, want %d, body=%s", devicesRegisterPattern, rec.Code, http.StatusCreated, rec.Body.String())
	}
	var got deviceTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding register response: %v", err)
	}
	return got.DeviceID, got.DeviceToken
}

// TestSyncRejectsUnauthenticatedRequests is the general auth-boundary check
// every other route family gets: no bearer token at all must be refused.
func TestSyncRejectsUnauthenticatedRequests(t *testing.T) {
	h := newTestRouter(t, newFakeStore())
	rec := doRequest(h, http.MethodGet, syncPattern+"?platform=macos&shell=zsh", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET %s without auth = %d, want %d", syncPattern, rec.Code, http.StatusUnauthorized)
	}
}

// TestSyncRejectsAnOperatorSessionToken proves the device-only boundary: an
// operator session — valid for every other guarded route in this API — must
// not authenticate sync. Mutation this test detects: registering syncPattern
// with RequiredKind: store.TokenKindSession (or Public) instead of
// store.TokenKindDevice.
func TestSyncRejectsAnOperatorSessionToken(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	session := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	rec := doRequest(h, http.MethodGet, syncPattern+"?platform=macos&shell=zsh", session, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET %s with an operator session = %d, want %d", syncPattern, rec.Code, http.StatusUnauthorized)
	}
}

// TestSyncResolvesAndRespondsWithTheDesignatedShape is task 6.3's own RED
// test: the response body must be exactly
// {revision, device{id,name,platform,shell,profileIds},
// aliases[{name,command,description}], generatedAt} — nothing more.
// Decoding into a generic map (rather than syncResponse itself) is
// deliberate: unmarshalling into the production struct would silently
// discard a field a regression added that isn't declared on that struct,
// which is exactly the failure mode "no server-side alias ID anywhere in
// the response" must catch. Mutation this test detects: adding an ID field
// tagged json:"id" to syncAlias (internal/api/sync.go) — every alias in
// this fixture has a non-empty store-assigned ID, so the mutated field
// would appear with a real value, not silently as "".
func TestSyncResolvesAndRespondsWithTheDesignatedShape(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	session := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	profRec := doRequest(h, http.MethodPost, profilesPattern, session, mustJSON(domain.Profile{Name: "development"}))
	if profRec.Code != http.StatusCreated {
		t.Fatalf("POST %s = %d, want %d, body=%s", profilesPattern, profRec.Code, http.StatusCreated, profRec.Body.String())
	}
	var prof domain.Profile
	mustUnmarshal(t, profRec.Body.Bytes(), &prof)

	deviceID, deviceToken := registerSyncTestDevice(t, h, s, "laptop", domain.PlatformMacOS, domain.ShellZsh, []string{prof.ID})

	// Matches: no targeting at all.
	createRec := doRequest(h, http.MethodPost, aliasesPattern, session, mustJSON(domain.Alias{
		Name: "dps", Command: "docker ps", Description: "list containers", Enabled: true,
	}))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("POST %s = %d, want %d, body=%s", aliasesPattern, createRec.Code, http.StatusCreated, createRec.Body.String())
	}
	// Excluded: wrong platform.
	otherRec := doRequest(h, http.MethodPost, aliasesPattern, session, mustJSON(domain.Alias{
		Name: "winonly", Command: "Get-Process", Enabled: true, Platforms: []domain.Platform{domain.PlatformWindows},
	}))
	if otherRec.Code != http.StatusCreated {
		t.Fatalf("POST %s = %d, want %d, body=%s", aliasesPattern, otherRec.Code, http.StatusCreated, otherRec.Body.String())
	}

	rec := doRequest(h, http.MethodGet, syncPattern+"?platform=macos&shell=zsh", deviceToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want %d, body=%s", syncPattern, rec.Code, http.StatusOK, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding sync response: %v", err)
	}

	for _, field := range []string{"revision", "device", "aliases", "generatedAt"} {
		if _, ok := body[field]; !ok {
			t.Fatalf("sync response missing top-level field %q: %v", field, body)
		}
	}

	dev, ok := body["device"].(map[string]any)
	if !ok {
		t.Fatalf("device field is not an object: %v", body["device"])
	}
	for _, field := range []string{"id", "name", "platform", "shell", "profileIds"} {
		if _, ok := dev[field]; !ok {
			t.Fatalf("sync response device missing field %q: %v", field, dev)
		}
	}
	if dev["id"] != deviceID {
		t.Fatalf("device.id = %v, want %q", dev["id"], deviceID)
	}
	if dev["platform"] != "macos" || dev["shell"] != "zsh" {
		t.Fatalf("device platform/shell = %v/%v, want macos/zsh", dev["platform"], dev["shell"])
	}

	aliases, ok := body["aliases"].([]any)
	if !ok {
		t.Fatalf("aliases field is not an array: %v", body["aliases"])
	}
	if len(aliases) != 1 {
		t.Fatalf("got %d aliases, want exactly 1 (the untargeted one): %v", len(aliases), aliases)
	}
	aliasFields, ok := aliases[0].(map[string]any)
	if !ok {
		t.Fatalf("alias entry is not an object: %v", aliases[0])
	}
	if aliasFields["name"] != "dps" || aliasFields["command"] != "docker ps" {
		t.Fatalf("unexpected alias entry: %v", aliasFields)
	}
	if _, hasID := aliasFields["id"]; hasID {
		t.Fatalf("sync response alias entry carries a server-side id (threat matrix, sync response contract): %v", aliasFields)
	}
	allowed := map[string]bool{"name": true, "command": true, "description": true}
	for key := range aliasFields {
		if !allowed[key] {
			t.Fatalf("sync response alias entry has unexpected field %q, only name/command/description are allowed: %v", key, aliasFields)
		}
	}
}

// TestSyncPersistsClientReportedPlatformShellAndTimestamps is task 6.4's own
// RED test for design decision 10: the same GET that resolves aliases must
// also persist the platform/shell it was called with, and stamp
// last_seen_at/last_sync_at. Mutation this test detects: handleSync never
// calling Devices().Touch (or calling it with the device's previously
// stored platform/shell instead of what this request reported).
func TestSyncPersistsClientReportedPlatformShellAndTimestamps(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	session := mintSessionFor(s, opID)

	fixedNow := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	h, err := NewRouter(s, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatalf("NewRouter(...) = %v, want nil", err)
	}

	deviceID, deviceToken := registerSyncTestDevice(t, h, s, "laptop", domain.PlatformMacOS, domain.ShellZsh, nil)

	rec := doRequest(h, http.MethodGet, syncPattern+"?platform=linux&shell=bash", deviceToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want %d, body=%s", syncPattern, rec.Code, http.StatusOK, rec.Body.String())
	}

	getRec := doRequest(h, http.MethodGet, "/api/v1/devices/"+deviceID, session, nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET device = %d, want %d, body=%s", getRec.Code, http.StatusOK, getRec.Body.String())
	}
	var dev domain.Device
	mustUnmarshal(t, getRec.Body.Bytes(), &dev)

	if dev.Platform != domain.PlatformLinux || dev.Shell != domain.ShellBash {
		t.Fatalf("device platform/shell = %s/%s, want linux/bash (this sync request's own report)", dev.Platform, dev.Shell)
	}
	if dev.LastSeenAt == nil || !dev.LastSeenAt.Equal(fixedNow) {
		t.Fatalf("device.LastSeenAt = %v, want %v", dev.LastSeenAt, fixedNow)
	}
	if dev.LastSyncAt == nil || !dev.LastSyncAt.Equal(fixedNow) {
		t.Fatalf("device.LastSyncAt = %v, want %v", dev.LastSyncAt, fixedNow)
	}
}

// TestSyncRejectsUnknownPlatformNamingTheValidSet and its shell counterpart
// are design decision 10's explicit requirement: an unknown value is a 400
// that names the valid set, never a silent default. Mutation this test
// detects: handleSync defaulting to some platform/shell instead of
// rejecting, or a 400 whose message drops the enumerated valid values.
func TestSyncRejectsUnknownPlatformNamingTheValidSet(t *testing.T) {
	s := newFakeStore()
	h := newTestRouter(t, s)
	_, deviceToken := registerSyncTestDevice(t, h, s, "laptop", domain.PlatformMacOS, domain.ShellZsh, nil)

	rec := doRequest(h, http.MethodGet, syncPattern+"?platform=commodore64&shell=zsh", deviceToken, nil)
	assertSyncBadRequestNames(t, rec, "commodore64", "macos", "linux", "windows")
}

func TestSyncRejectsUnknownShellNamingTheValidSet(t *testing.T) {
	s := newFakeStore()
	h := newTestRouter(t, s)
	_, deviceToken := registerSyncTestDevice(t, h, s, "laptop", domain.PlatformMacOS, domain.ShellZsh, nil)

	rec := doRequest(h, http.MethodGet, syncPattern+"?platform=macos&shell=fish", deviceToken, nil)
	assertSyncBadRequestNames(t, rec, "fish", "zsh", "bash", "powershell")
}

// TestSyncRequiresPlatformAndShellQueryParams proves an omitted parameter is
// rejected exactly like a garbage one — never silently defaulted.
func TestSyncRequiresPlatformAndShellQueryParams(t *testing.T) {
	s := newFakeStore()
	h := newTestRouter(t, s)
	_, deviceToken := registerSyncTestDevice(t, h, s, "laptop", domain.PlatformMacOS, domain.ShellZsh, nil)

	rec := doRequest(h, http.MethodGet, syncPattern, deviceToken, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("GET %s with no query params = %d, want %d, body=%s", syncPattern, rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func assertSyncBadRequestNames(t *testing.T, rec *httptest.ResponseRecorder, badValue string, mustMention ...string) {
	t.Helper()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("GET %s = %d, want %d, body=%s", syncPattern, rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	var decoded errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	if decoded.Error.Code != codeInvalidRequest {
		t.Fatalf("error.code = %q, want %q", decoded.Error.Code, codeInvalidRequest)
	}
	if !strings.Contains(decoded.Error.Message, badValue) {
		t.Fatalf("error.message = %q, want it to name the rejected value %q", decoded.Error.Message, badValue)
	}
	for _, want := range mustMention {
		if !strings.Contains(decoded.Error.Message, want) {
			t.Fatalf("error.message = %q, want it to name valid value %q (decision 10: name the valid set)", decoded.Error.Message, want)
		}
	}
}

// TestSyncFailsWithARevokedDeviceTokenWithAnActionableMessage is task 6.6's
// RED+GREEN scenario, exercised over real HTTP end to end: register a
// device, revoke it through the real production revoke route, then prove
// its device token no longer authenticates sync — closing the deferral
// devices_test.go's TestDevicesRevokeInvalidatesItsToken left open ("No
// device-kind-gated route exists yet in this batch's scope"). The message
// must be distinguishable from the generic {"error":{"code":"unauthorized",
// "message":"unauthorized"}} every other guarded route answers with
// (router.go's default writeUnauthorized) and must name the recovery
// action. Mutation this test detects: (a) sync accepting a revoked device
// token at all — caught by the status/kind assertions; (b) syncPattern
// falling back to the generic Refuse — caught by the code/message
// assertions below, which fail if router.go's Refuse: writeUnauthorizedDevice
// entry is removed from routes().
func TestSyncFailsWithARevokedDeviceTokenWithAnActionableMessage(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	session := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	deviceID, deviceToken := registerSyncTestDevice(t, h, s, "laptop", domain.PlatformMacOS, domain.ShellZsh, nil)

	revokeRec := doRequest(h, http.MethodPost, "/api/v1/devices/"+deviceID+"/revoke", session, nil)
	if revokeRec.Code != http.StatusNoContent {
		t.Fatalf("POST revoke = %d, want %d, body=%s", revokeRec.Code, http.StatusNoContent, revokeRec.Body.String())
	}

	rec := doRequest(h, http.MethodGet, syncPattern+"?platform=macos&shell=zsh", deviceToken, nil)
	assertActionableDeviceUnauthorized(t, rec)
}

// TestSyncFailsWithARotatedDeviceTokenWithAnActionableMessage is 6.6's other
// half: the OLD token must fail sync the same actionable way after
// rotation, while the NEW token — proved in the same test — still works.
// Mutation this test detects: rotation not calling RevokeSubject on the old
// token before minting the replacement (the old token would still
// authenticate); syncPattern rejecting the brand-new token too (a broken
// rotation flow, not merely a broken revocation one).
func TestSyncFailsWithARotatedDeviceTokenWithAnActionableMessage(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	session := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	deviceID, oldToken := registerSyncTestDevice(t, h, s, "laptop", domain.PlatformMacOS, domain.ShellZsh, nil)

	rotateRec := doRequest(h, http.MethodPost, "/api/v1/devices/"+deviceID+"/token", session, nil)
	if rotateRec.Code != http.StatusOK {
		t.Fatalf("POST rotate = %d, want %d, body=%s", rotateRec.Code, http.StatusOK, rotateRec.Body.String())
	}
	var rotated deviceTokenResponse
	mustUnmarshal(t, rotateRec.Body.Bytes(), &rotated)

	oldRec := doRequest(h, http.MethodGet, syncPattern+"?platform=macos&shell=zsh", oldToken, nil)
	assertActionableDeviceUnauthorized(t, oldRec)

	newRec := doRequest(h, http.MethodGet, syncPattern+"?platform=macos&shell=zsh", rotated.DeviceToken, nil)
	if newRec.Code != http.StatusOK {
		t.Fatalf("GET %s with the freshly rotated token = %d, want %d, body=%s", syncPattern, newRec.Code, http.StatusOK, newRec.Body.String())
	}
}

// TestSyncServesResolvedAliasesWhenTouchFails is bounded-review finding 1's
// own RED+GREEN test: the aliases are already resolved and sitting in
// memory by the time Devices().Touch runs, so a transient failure recording
// last_seen_at/last_sync_at must not throw that work away and answer the
// device with a 500. Mutation this test detects: handleSync returning
// writeStoreError(w, err) (the pre-correction behavior) when Touch fails —
// this test would then see a 500 instead of 200 with the resolved alias.
func TestSyncServesResolvedAliasesWhenTouchFails(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	session := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	_, deviceToken := registerSyncTestDevice(t, h, s, "laptop", domain.PlatformMacOS, domain.ShellZsh, nil)

	createRec := doRequest(h, http.MethodPost, aliasesPattern, session, mustJSON(domain.Alias{
		Name: "dps", Command: "docker ps", Enabled: true,
	}))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("POST %s = %d, want %d, body=%s", aliasesPattern, createRec.Code, http.StatusCreated, createRec.Body.String())
	}

	s.touchErr = errors.New("simulated transient touch failure")

	rec := doRequest(h, http.MethodGet, syncPattern+"?platform=macos&shell=zsh", deviceToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s with a failing Touch = %d, want %d (a bookkeeping failure must not discard a resolved sync), body=%s",
			syncPattern, rec.Code, http.StatusOK, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding sync response: %v", err)
	}
	aliases, ok := body["aliases"].([]any)
	if !ok || len(aliases) != 1 {
		t.Fatalf("aliases = %v, want exactly the one resolved alias despite the Touch failure", body["aliases"])
	}
}

// TestSyncFailsWithADeletedDeviceTokenWithAnActionableMessage is
// bounded-review finding 2's own RED+GREEN test: a deleted device's own
// token must fail sync exactly the same actionable way a revoked one does
// (401, codeInvalidToken, actionable message) — not merely a 404 because
// the device row is gone, which would mean the token itself was still a
// live, unrevoked credential and only failed to authenticate this one
// route by accident (because handleSync happens to load the device row).
// Mutation this test detects: handleDevicesDelete calling
// Devices().Delete without first calling Tokens().RevokeSubject — the
// token would still verify, and this GET would reach the device lookup and
// fail with 404 instead of 401 here.
func TestSyncFailsWithADeletedDeviceTokenWithAnActionableMessage(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	session := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	deviceID, deviceToken := registerSyncTestDevice(t, h, s, "laptop", domain.PlatformMacOS, domain.ShellZsh, nil)

	deleteRec := doRequest(h, http.MethodDelete, "/api/v1/devices/"+deviceID, session, nil)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/v1/devices/%s = %d, want %d, body=%s", deviceID, deleteRec.Code, http.StatusNoContent, deleteRec.Body.String())
	}

	rec := doRequest(h, http.MethodGet, syncPattern+"?platform=macos&shell=zsh", deviceToken, nil)
	assertActionableDeviceUnauthorized(t, rec)
}

func assertActionableDeviceUnauthorized(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET %s = %d, want %d, body=%s", syncPattern, rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	var decoded errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	if decoded.Error.Code != codeInvalidToken {
		t.Fatalf("error.code = %q, want %q", decoded.Error.Code, codeInvalidToken)
	}
	if decoded.Error.Message == "unauthorized" {
		t.Fatal("sync's own 401 message must not be the generic \"unauthorized\" every other route uses — it must be actionable")
	}
	if !strings.Contains(strings.ToLower(decoded.Error.Message), "register") {
		t.Fatalf("error.message = %q, want it to point at re-registering the device", decoded.Error.Message)
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func mustUnmarshal(t *testing.T, data []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decoding %s: %v", data, err)
	}
}
