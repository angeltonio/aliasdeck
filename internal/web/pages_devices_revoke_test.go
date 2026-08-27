package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
)

func postRevoke(t *testing.T, a *webapp, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/devices/"+id+"/revoke", nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	a.handleDevicesRevoke(rec, authed(req))
	return rec
}

// issueDeviceToken gives a device a live device-kind token, which is what
// revocation actually has to kill.
func issueDeviceToken(t *testing.T, a *webapp, st store.Store, deviceID string) string {
	t.Helper()
	minted, err := auth.Mint(store.TokenKindDevice)
	if err != nil {
		t.Fatalf("auth.Mint(): %v", err)
	}
	if err := st.Tokens().Create(context.Background(), store.Token{
		Kind: store.TokenKindDevice, SubjectID: deviceID,
		Lookup: minted.Lookup, SecretHash: minted.SecretHash, CreatedAt: a.now(),
	}); err != nil {
		t.Fatalf("Tokens().Create(): %v", err)
	}
	return minted.Lookup
}

func tokenIsLive(t *testing.T, st store.Store, lookup string) bool {
	t.Helper()
	tok, err := st.Tokens().ByLookup(context.Background(), lookup)
	if err != nil {
		t.Fatalf("ByLookup(): %v", err)
	}
	// store.Token uses a zero time for "never revoked", unlike
	// domain.Device's pointer — each struct keeps its own convention.
	return tok.RevokedAt.IsZero()
}

// TestRevokeKillsTheDeviceTokenNotOnlyTheRow is the assertion that makes this
// feature real. Marking the row revoked changes nothing a device can observe;
// what stops it is that its device-kind token no longer verifies. A revoke
// that only flipped the row would read as success while the stolen machine
// kept syncing.
func TestRevokeKillsTheDeviceTokenNotOnlyTheRow(t *testing.T) {
	a, st := newAliasTestApp(t)
	dev := enrollDevice(t, a, st, "work-mac")
	lookup := issueDeviceToken(t, a, st, dev.ID)

	if !tokenIsLive(t, st, lookup) {
		t.Fatal("the fixture token was already revoked")
	}

	rec := postRevoke(t, a, dev.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", rec.Code, rec.Body.String())
	}

	if tokenIsLive(t, st, lookup) {
		t.Fatal("the device token still verifies after a revoke")
	}

	after, err := st.Devices().Get(context.Background(), dev.ID)
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if after.RevokedAt == nil {
		t.Fatal("the device row was not marked revoked")
	}
	if !after.RevokedAt.Equal(a.now()) {
		t.Fatalf("RevokedAt = %v, want the injected clock %v", after.RevokedAt, a.now())
	}
}

// TestRevokeKeepsTheDeviceAndItsGroups pins the choice not to delete. An
// operator revoking a stolen laptop is cutting access, not erasing history.
func TestRevokeKeepsTheDeviceAndItsGroups(t *testing.T) {
	a, st := newAliasTestApp(t)
	laptops := seedProfile(t, st, "laptops")
	dev := enrollDevice(t, a, st, "work-mac", laptops.ID)

	if rec := postRevoke(t, a, dev.ID); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	after, err := st.Devices().Get(context.Background(), dev.ID)
	if err != nil {
		t.Fatalf("the device was deleted by a revoke: %v", err)
	}
	if after.Name != "work-mac" {
		t.Fatalf("name = %q, want it kept", after.Name)
	}
	if len(after.ProfileIDs) != 1 || after.ProfileIDs[0] != laptops.ID {
		t.Fatalf("group membership = %v, want it kept at %v", after.ProfileIDs, []string{laptops.ID})
	}
}

// TestRevokedDeviceStaysListedAndOffersNoSecondRevoke covers what the operator
// actually sees: the row remains, it says revoked, and the button is gone so a
// completed action cannot look like it needs repeating.
func TestRevokedDeviceStaysListedAndOffersNoSecondRevoke(t *testing.T) {
	a, st := newAliasTestApp(t)
	dev := enrollDevice(t, a, st, "work-mac")

	rec := postRevoke(t, a, dev.ID)
	body := rec.Body.String()

	if !strings.Contains(body, "device-row-"+dev.ID) {
		t.Fatalf("the revoked device disappeared from the list:\n%s", body)
	}
	if !strings.Contains(body, `<span class="badge stale">revoked</span>`) {
		t.Fatalf("the row does not show the revoked state:\n%s", body)
	}
	if strings.Contains(body, `hx-post="/devices/`+dev.ID+`/revoke"`) {
		t.Fatalf("a revoked device still offers a revoke button:\n%s", body)
	}
	// Preview stays available: what a revoked machine would have received is
	// still a question worth answering.
	if !strings.Contains(body, `/devices/`+dev.ID+`/preview`) {
		t.Fatalf("the revoked row lost its preview link:\n%s", body)
	}
}

func TestRevokeConfirmationSaysWhatItDoesAndWhatItKeeps(t *testing.T) {
	a, st := newAliasTestApp(t)
	enrollDevice(t, a, st, "work-mac")

	req := httptest.NewRequest(http.MethodGet, "/devices/panel", nil)
	rec := httptest.NewRecorder()
	a.handleDevicesPanel(rec, authed(req))

	body := rec.Body.String()
	if !strings.Contains(body, "stops working immediately") {
		t.Fatalf("the confirmation does not say the token dies at once:\n%s", body)
	}
	if !strings.Contains(body, "Aliases and group membership are kept") {
		t.Fatalf("the confirmation does not say what survives:\n%s", body)
	}
}

func TestRevokeOnAMissingDeviceReportsIt(t *testing.T) {
	a, _ := newAliasTestApp(t)

	if rec := postRevoke(t, a, "does-not-exist"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestRevokeIsSafeToRepeat matters because the token step can fail after the
// row step succeeds. The recovery is to click again, so a second call must not
// error out on the already-revoked row.
func TestRevokeIsSafeToRepeat(t *testing.T) {
	a, st := newAliasTestApp(t)
	dev := enrollDevice(t, a, st, "work-mac")
	lookup := issueDeviceToken(t, a, st, dev.ID)

	if rec := postRevoke(t, a, dev.ID); rec.Code != http.StatusOK {
		t.Fatalf("first revoke status = %d, want 200", rec.Code)
	}
	if rec := postRevoke(t, a, dev.ID); rec.Code != http.StatusOK {
		t.Fatalf("second revoke status = %d, want 200 — retrying must be safe", rec.Code)
	}
	if tokenIsLive(t, st, lookup) {
		t.Fatal("the token is live after two revokes")
	}
}

// TestRevokedDeviceIsExcludedFromNothingItAlreadyMatched documents a real
// limit rather than asserting a wish: resolution is a property of targeting,
// so a revoked device still "matches" the aliases it matched before. What
// stops it is authentication, not resolution — and the preview says what the
// targeting says.
func TestRevokedDeviceIsExcludedFromNothingItAlreadyMatched(t *testing.T) {
	a, st := newAliasTestApp(t)
	dev := enrollDevice(t, a, st, "work-mac")
	createAlias(t, st, domain.Alias{Name: "everywhere", Command: "echo 1", Enabled: true})

	if rec := postRevoke(t, a, dev.ID); rec.Code != http.StatusOK {
		t.Fatalf("revoke status = %d, want 200", rec.Code)
	}

	revoked, err := st.Devices().Get(context.Background(), dev.ID)
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	aliases, err := st.Aliases().List(context.Background())
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if !aliases[0].AppliesTo(revoked) {
		t.Fatal("targeting started depending on revocation; revocation is an authentication boundary, not a resolution one")
	}
}
