package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
)

// enrollDevice creates a device through the only path that can: consuming an
// enrollment token. Membership rides on the token, never on the device value.
func enrollDevice(t *testing.T, a *webapp, st store.Store, name string, groupIDs ...string) domain.Device {
	t.Helper()
	minted, err := auth.Mint(store.TokenKindEnrollment)
	if err != nil {
		t.Fatalf("auth.Mint(): %v", err)
	}
	if err := st.Tokens().Create(context.Background(), store.Token{
		Kind: store.TokenKindEnrollment, Lookup: minted.Lookup, SecretHash: minted.SecretHash,
		ProfileIDs: groupIDs, CreatedAt: a.now(), ExpiresAt: a.now().Add(enrollmentTokenTTL),
	}); err != nil {
		t.Fatalf("Tokens().Create(): %v", err)
	}
	dev, err := st.Tokens().ConsumeEnrollment(context.Background(), minted.Lookup, domain.Device{
		Name: name, Platform: domain.PlatformMacOS, Shell: domain.ShellZsh,
	})
	if err != nil {
		t.Fatalf("enrolling %q: %v", name, err)
	}
	return dev
}

func submitDeviceEdit(t *testing.T, a *webapp, id string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/devices/"+id, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	a.handleDevicesUpdate(rec, authed(req))
	return rec
}

func groupIDsOf(t *testing.T, st store.Store, deviceID string) []string {
	t.Helper()
	d, err := st.Devices().Get(context.Background(), deviceID)
	if err != nil {
		t.Fatalf("Get(device): %v", err)
	}
	return d.ProfileIDs
}

// TestDeviceEditMovesItBetweenGroups is the reason this screen changed.
// Membership was decided by the enrollment token and nothing afterwards could
// alter it from the browser, so a machine enrolled into the wrong group was
// stuck there short of re-enrolling it.
func TestDeviceEditMovesItBetweenGroups(t *testing.T) {
	a, st := newAliasTestApp(t)
	laptops := seedProfile(t, st, "laptops")
	servers := seedProfile(t, st, "servers")
	dev := enrollDevice(t, a, st, "work-mac", laptops.ID)

	if got := groupIDsOf(t, st, dev.ID); len(got) != 1 || got[0] != laptops.ID {
		t.Fatalf("membership before = %v, want [%s]", got, laptops.ID)
	}

	rec := submitDeviceEdit(t, a, dev.ID, url.Values{"name": {"work-mac"}, "groups": {servers.ID}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", rec.Code, rec.Body.String())
	}
	if got := groupIDsOf(t, st, dev.ID); len(got) != 1 || got[0] != servers.ID {
		t.Fatalf("membership after = %v, want [%s]", got, servers.ID)
	}
}

func TestDeviceEditJoinsSeveralGroupsAtOnce(t *testing.T) {
	a, st := newAliasTestApp(t)
	laptops := seedProfile(t, st, "laptops")
	servers := seedProfile(t, st, "servers")
	dev := enrollDevice(t, a, st, "work-mac")

	rec := submitDeviceEdit(t, a, dev.ID, url.Values{"name": {"work-mac"}, "groups": {laptops.ID, servers.ID}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", rec.Code, rec.Body.String())
	}
	if got := groupIDsOf(t, st, dev.ID); len(got) != 2 {
		t.Fatalf("membership = %v, want both groups", got)
	}
}

// TestDeviceEditWithNoGroupsCheckedRemovesEveryMembership pins the one case a
// checkbox form makes easy to get wrong: an unchecked box sends nothing, so
// the absent key has to read as "belongs to nothing" rather than as "leave it
// alone", or a membership could never be removed.
func TestDeviceEditWithNoGroupsCheckedRemovesEveryMembership(t *testing.T) {
	a, st := newAliasTestApp(t)
	laptops := seedProfile(t, st, "laptops")
	dev := enrollDevice(t, a, st, "work-mac", laptops.ID)

	rec := submitDeviceEdit(t, a, dev.ID, url.Values{"name": {"work-mac"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", rec.Code, rec.Body.String())
	}
	if got := groupIDsOf(t, st, dev.ID); len(got) != 0 {
		t.Fatalf("membership = %v, want none left", got)
	}
}

// TestDeviceEditLeavesObservedFactsAlone guards the read-only half of the
// row. Platform and shell are what the device reported on sync, not operator
// input, so an edit must never be able to rewrite them.
func TestDeviceEditLeavesObservedFactsAlone(t *testing.T) {
	a, st := newAliasTestApp(t)
	dev := enrollDevice(t, a, st, "work-mac")

	rec := submitDeviceEdit(t, a, dev.ID, url.Values{
		"name": {"renamed"}, "platform": {"windows"}, "shell": {"powershell"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", rec.Code, rec.Body.String())
	}

	after, err := st.Devices().Get(context.Background(), dev.ID)
	if err != nil {
		t.Fatalf("Get(device): %v", err)
	}
	if after.Name != "renamed" {
		t.Fatalf("name = %q, want the rename applied", after.Name)
	}
	if after.Platform != domain.PlatformMacOS || after.Shell != domain.ShellZsh {
		t.Fatalf("platform/shell = %s/%s, want the enrolled macos/zsh — an edit rewrote an observed fact", after.Platform, after.Shell)
	}
}

func TestDeviceEditRequiresANameAndKeepsTheRowOpen(t *testing.T) {
	a, st := newAliasTestApp(t)
	dev := enrollDevice(t, a, st, "work-mac")

	rec := submitDeviceEdit(t, a, dev.ID, url.Values{"name": {"   "}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `hx-put="/devices/`+dev.ID+`"`) {
		t.Fatalf("a rejected edit closed the row:\n%s", rec.Body.String())
	}
	if got := groupIDsOf(t, st, dev.ID); len(got) != 0 {
		t.Fatalf("membership = %v; a rejected edit still wrote", got)
	}
	after, _ := st.Devices().Get(context.Background(), dev.ID)
	if after.Name != "work-mac" {
		t.Fatalf("name = %q, want it unchanged by a rejected edit", after.Name)
	}
}

func TestDeviceEditOnAMissingDeviceReportsIt(t *testing.T) {
	a, _ := newAliasTestApp(t)

	rec := submitDeviceEdit(t, a, "does-not-exist", url.Values{"name": {"whatever"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %q", rec.Code, rec.Body.String())
	}
}

func TestDeviceEditRowRendersCheckboxesAndNoFormElement(t *testing.T) {
	a, st := newAliasTestApp(t)
	laptops := seedProfile(t, st, "laptops")
	servers := seedProfile(t, st, "servers")
	dev := enrollDevice(t, a, st, "work-mac", laptops.ID)

	req := httptest.NewRequest(http.MethodGet, "/devices/"+dev.ID+"/edit", nil)
	req.SetPathValue("id", dev.ID)
	rec := httptest.NewRecorder()
	a.handleDevicesEdit(rec, authed(req))

	body := rec.Body.String()
	if strings.Contains(body, "<form") {
		t.Fatalf("the edit row rendered a <form> inside a table row:\n%s", body)
	}
	// The group it belongs to is checked; the one it does not is offered
	// unchecked, which is what makes moving it possible at all.
	if !strings.Contains(body, `name="groups" value="`+laptops.ID+`" checked`) {
		t.Fatalf("the current group is not checked:\n%s", body)
	}
	if !strings.Contains(body, `name="groups" value="`+servers.ID+`" `) {
		t.Fatalf("the other group is not offered:\n%s", body)
	}
	if !strings.Contains(body, `hx-include="closest tr"`) {
		t.Fatalf("the edit row does not gather its inputs with hx-include:\n%s", body)
	}
}

func TestDevicePanelShowsMembershipAndADashWhenThereIsNone(t *testing.T) {
	a, st := newAliasTestApp(t)
	laptops := seedProfile(t, st, "laptops")
	enrollDevice(t, a, st, "in-a-group", laptops.ID)
	enrollDevice(t, a, st, "loose")

	req := httptest.NewRequest(http.MethodGet, "/devices/panel", nil)
	rec := httptest.NewRecorder()
	a.handleDevicesPanel(rec, authed(req))

	body := rec.Body.String()
	if !strings.Contains(body, `<span class="badge">laptops</span>`) {
		t.Fatalf("the panel does not show the group a device belongs to:\n%s", body)
	}
	if !strings.Contains(body, `<span class="muted">—</span>`) {
		t.Fatalf("a device in no group is not shown as such:\n%s", body)
	}
}
