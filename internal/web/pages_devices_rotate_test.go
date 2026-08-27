package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/store"
)

func postRotate(t *testing.T, a *webapp, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/devices/"+id+"/token", nil)
	req.SetPathValue("id", id)
	req.Host = "aliases.example"
	rec := httptest.NewRecorder()
	a.handleDevicesRotateToken(rec, authed(req))
	return rec
}

// tokenFromCommand pulls the minted token out of the adoption command the
// panel rendered. Reading it back from the response is the only way a test
// can see it — which is the point of a one-time secret, and also why the
// assertions below go through the store rather than trusting the handler.
func tokenFromCommand(t *testing.T, body string) string {
	t.Helper()
	// Matched on the token itself, not on surrounding quotes: html/template
	// escapes them to &#39; on the way out.
	m := regexp.MustCompile(`add_[A-Za-z0-9_.:-]+`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no device token in the rendered command:\n%s", body)
	}
	return m[0]
}

// TestRotateKillsTheOldTokenAndIssuesAWorkingOne is the property rotation
// exists for. Minting before revoking would leave a window where both
// credentials authenticate, which is precisely what someone rotating a leaked
// token is trying to close.
func TestRotateKillsTheOldTokenAndIssuesAWorkingOne(t *testing.T) {
	a, st := newAliasTestApp(t)
	dev := enrollDevice(t, a, st, "work-mac")
	old := issueDeviceToken(t, a, st, dev.ID)

	rec := postRotate(t, a, dev.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", rec.Code, rec.Body.String())
	}

	if tokenIsLive(t, st, old) {
		t.Fatal("the previous token still authenticates after a rotation")
	}

	parsed, err := auth.Parse(tokenFromCommand(t, rec.Body.String()))
	if err != nil {
		t.Fatalf("the rendered command carries an unparseable token: %v", err)
	}
	if parsed.Kind != store.TokenKindDevice {
		t.Fatalf("minted kind = %q, want a device token", parsed.Kind)
	}
	if parsed.Lookup == old {
		t.Fatal("rotation re-issued the same token it was supposed to replace")
	}
	if !tokenIsLive(t, st, parsed.Lookup) {
		t.Fatal("the newly issued token is not live in the store")
	}

	// It must belong to this device, or the operator would install a
	// credential that authenticates as something else.
	fresh, err := st.Tokens().ByLookup(context.Background(), parsed.Lookup)
	if err != nil {
		t.Fatalf("ByLookup(new): %v", err)
	}
	if fresh.SubjectID != dev.ID {
		t.Fatalf("new token subject = %q, want %q", fresh.SubjectID, dev.ID)
	}
}

// TestRotateShowsTheAdoptionCommandExactlyOnce covers the contract that makes
// the secret usable without making it retrievable.
func TestRotateShowsTheAdoptionCommandExactlyOnce(t *testing.T) {
	a, st := newAliasTestApp(t)
	dev := enrollDevice(t, a, st, "work-mac")

	rec := postRotate(t, a, dev.ID)
	body := rec.Body.String()

	if !strings.Contains(body, "aliasdeck register --url") {
		t.Fatalf("the response does not carry an adoption command:\n%s", body)
	}
	if !strings.Contains(body, "--device-token") || !strings.Contains(body, "--force") {
		t.Fatalf("the command is missing the flags that make it work:\n%s", body)
	}
	if !strings.Contains(body, "add_") {
		t.Fatalf("the command carries no device token:\n%s", body)
	}
	if !strings.Contains(body, "work-mac") {
		t.Fatalf("the panel does not say which device was rotated:\n%s", body)
	}

	// Any later panel load must not reveal it. This is the whole reason
	// RotatedCommand is set by one handler and nowhere else.
	req := httptest.NewRequest(http.MethodGet, "/devices/panel", nil)
	again := httptest.NewRecorder()
	a.handleDevicesPanel(again, authed(req))
	if strings.Contains(again.Body.String(), "--device-token") {
		t.Fatalf("reloading the panel revealed the rotated token again:\n%s", again.Body.String())
	}
}

// TestRotatedCommandUsesThePublicURLWhenConfigured matters because the
// operator pastes this on another machine. Behind a reverse proxy the request
// host is the internal one, and a command pointing there would simply fail.
func TestRotatedCommandUsesThePublicURLWhenConfigured(t *testing.T) {
	a, st := newAliasTestApp(t)
	public, err := url.Parse("https://aliases.example.com")
	if err != nil {
		t.Fatal(err)
	}
	a.publicURL = public
	dev := enrollDevice(t, a, st, "work-mac")

	body := postRotate(t, a, dev.ID).Body.String()
	if !strings.Contains(body, "https://aliases.example.com") {
		t.Fatalf("the command does not use the configured public URL:\n%s", body)
	}
	if strings.Contains(body, "aliases.example'") {
		t.Fatalf("the command used the request host instead of the public URL:\n%s", body)
	}
}

func TestRotateOnAMissingDeviceReportsIt(t *testing.T) {
	a, _ := newAliasTestApp(t)

	if rec := postRotate(t, a, "does-not-exist"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestRotateLeavesTheDeviceItself pins the difference from revoking: the
// machine keeps its identity, its name and its groups. Only the secret moved.
func TestRotateLeavesTheDeviceItself(t *testing.T) {
	a, st := newAliasTestApp(t)
	laptops := seedProfile(t, st, "laptops")
	dev := enrollDevice(t, a, st, "work-mac", laptops.ID)

	if rec := postRotate(t, a, dev.ID); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	after, err := st.Devices().Get(context.Background(), dev.ID)
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if after.ID != dev.ID || after.Name != "work-mac" {
		t.Fatalf("device = %+v, want it unchanged", after)
	}
	if after.RevokedAt != nil {
		t.Fatal("rotating marked the device revoked; that is what revoking is for")
	}
	if len(after.ProfileIDs) != 1 || after.ProfileIDs[0] != laptops.ID {
		t.Fatalf("group membership = %v, want it kept", after.ProfileIDs)
	}
}
