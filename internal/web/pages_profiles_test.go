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

func submitProfileForm(t *testing.T, a *webapp, method, path, id string, form url.Values, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if id != "" {
		req.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	h(rec, authed(req))
	return rec
}

func seedProfile(t *testing.T, st store.Store, name string) domain.Profile {
	t.Helper()
	p, err := st.Profiles().Create(context.Background(), domain.Profile{Name: name, Description: "seeded"})
	if err != nil {
		t.Fatalf("creating profile %q: %v", name, err)
	}
	return p
}

func TestProfileCreatePersistsAndRendersTheRow(t *testing.T) {
	a, st := newAliasTestApp(t)

	rec := submitProfileForm(t, a, http.MethodPost, "/profiles", "",
		url.Values{"name": {"workstations"}, "description": {"the everyday laptops"}}, a.handleProfilesCreate)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", rec.Code, rec.Body.String())
	}

	list, err := st.Profiles().List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("profiles = %v, err = %v; want exactly one", list, err)
	}
	if list[0].Name != "workstations" || list[0].Description != "the everyday laptops" {
		t.Fatalf("stored profile = %+v, want the submitted values", list[0])
	}
	if body := rec.Body.String(); !strings.Contains(body, "workstations") {
		t.Fatalf("panel does not show the new group:\n%s", body)
	}
}

func TestProfileCreateRequiresAName(t *testing.T) {
	a, st := newAliasTestApp(t)

	rec := submitProfileForm(t, a, http.MethodPost, "/profiles", "",
		url.Values{"name": {"   "}, "description": {"no name"}}, a.handleProfilesCreate)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	list, err := st.Profiles().List(context.Background())
	if err != nil || len(list) != 0 {
		t.Fatalf("profiles = %v, err = %v; want none persisted", list, err)
	}
}

func TestProfileCreateRejectsADuplicateName(t *testing.T) {
	a, st := newAliasTestApp(t)
	seedProfile(t, st, "workstations")

	rec := submitProfileForm(t, a, http.MethodPost, "/profiles", "",
		url.Values{"name": {"workstations"}}, a.handleProfilesCreate)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %q", rec.Code, rec.Body.String())
	}
}

func TestProfileEditRenamesWithoutCreatingASecondRow(t *testing.T) {
	a, st := newAliasTestApp(t)
	created := seedProfile(t, st, "workstations")

	rec := submitProfileForm(t, a, http.MethodPut, "/profiles/"+created.ID, created.ID,
		url.Values{"name": {"laptops"}, "description": {"renamed"}}, a.handleProfilesUpdate)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", rec.Code, rec.Body.String())
	}

	list, err := st.Profiles().List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("profiles = %v, err = %v; want exactly one — an edit must not insert", list, err)
	}
	if list[0].ID != created.ID {
		t.Fatalf("id = %q, want the original %q preserved", list[0].ID, created.ID)
	}
	if list[0].Name != "laptops" || list[0].Description != "renamed" {
		t.Fatalf("profile = %+v, want the rename applied", list[0])
	}
}

// TestProfileEditKeepsTheDeviceMembershipItAlreadyHad guards the property
// that made alias editing worth building: renaming a group must not detach
// the devices in it. device_profiles is a join table, and a rename that went
// through a delete-and-recreate would cascade it away.
func TestProfileEditKeepsTheDeviceMembershipItAlreadyHad(t *testing.T) {
	a, st := newAliasTestApp(t)
	created := seedProfile(t, st, "workstations")

	// Group membership is carried by the enrollment token, not by the device
	// the caller hands to ConsumeEnrollment: tokens.go decodes the token's
	// ProfileIds and overwrites dev.ProfileIDs from them. A fixture that set
	// the field on the device would silently enroll into no group at all.
	minted, err := auth.Mint(store.TokenKindEnrollment)
	if err != nil {
		t.Fatalf("auth.Mint(): %v", err)
	}
	if err := st.Tokens().Create(context.Background(), store.Token{
		Kind: store.TokenKindEnrollment, Lookup: minted.Lookup, SecretHash: minted.SecretHash,
		ProfileIDs: []string{created.ID},
		CreatedAt:  a.now(), ExpiresAt: a.now().Add(enrollmentTokenTTL),
	}); err != nil {
		t.Fatalf("Tokens().Create(): %v", err)
	}
	device, err := st.Tokens().ConsumeEnrollment(context.Background(), minted.Lookup, domain.Device{
		Name: "laptop", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh,
	})
	if err != nil {
		t.Fatalf("enrolling the fixture device: %v", err)
	}
	if len(device.ProfileIDs) != 1 {
		t.Fatalf("fixture device joined %v, want the seeded group", device.ProfileIDs)
	}

	rec := submitProfileForm(t, a, http.MethodPut, "/profiles/"+created.ID, created.ID,
		url.Values{"name": {"laptops"}}, a.handleProfilesUpdate)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", rec.Code, rec.Body.String())
	}

	after, err := st.Devices().Get(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("Get(device): %v", err)
	}
	if len(after.ProfileIDs) != 1 || after.ProfileIDs[0] != created.ID {
		t.Fatalf("device membership = %v, want it unchanged at %v — the rename detached the device", after.ProfileIDs, []string{created.ID})
	}
}

func TestProfileEditRejectsARenameOntoAnExistingName(t *testing.T) {
	a, st := newAliasTestApp(t)
	first := seedProfile(t, st, "workstations")
	seedProfile(t, st, "servers")

	rec := submitProfileForm(t, a, http.MethodPut, "/profiles/"+first.ID, first.ID,
		url.Values{"name": {"servers"}}, a.handleProfilesUpdate)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `hx-put="/profiles/`+first.ID+`"`) {
		t.Fatalf("a rejected rename closed the edit row:\n%s", rec.Body.String())
	}

	unchanged, err := st.Profiles().Get(context.Background(), first.ID)
	if err != nil || unchanged.Name != "workstations" {
		t.Fatalf("profile = %+v, err = %v; want the rejected rename to have changed nothing", unchanged, err)
	}
}

func TestProfileEditRowUsesNoFormElement(t *testing.T) {
	a, st := newAliasTestApp(t)
	created := seedProfile(t, st, "workstations")

	req := httptest.NewRequest(http.MethodGet, "/profiles/"+created.ID+"/edit", nil)
	req.SetPathValue("id", created.ID)
	rec := httptest.NewRecorder()
	a.handleProfilesEdit(rec, authed(req))

	body := rec.Body.String()
	if strings.Contains(body, "<form") {
		t.Fatalf("the edit row rendered a <form> inside a table row, which a browser hoists out of the table:\n%s", body)
	}
	if !strings.Contains(body, `hx-include="closest tr"`) {
		t.Fatalf("the edit row does not gather its inputs with hx-include:\n%s", body)
	}
}

func TestProfileDeleteRemovesItAndReportsAMissingOne(t *testing.T) {
	a, st := newAliasTestApp(t)
	created := seedProfile(t, st, "workstations")

	req := httptest.NewRequest(http.MethodDelete, "/profiles/"+created.ID, nil)
	req.SetPathValue("id", created.ID)
	rec := httptest.NewRecorder()
	a.handleProfilesDelete(rec, authed(req))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", rec.Code, rec.Body.String())
	}

	list, err := st.Profiles().List(context.Background())
	if err != nil || len(list) != 0 {
		t.Fatalf("profiles = %v, err = %v; want none left", list, err)
	}

	// A second delete of the same id must report the absence rather than
	// reading as another success.
	again := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodDelete, "/profiles/"+created.ID, nil)
	req2.SetPathValue("id", created.ID)
	a.handleProfilesDelete(again, authed(req2))
	if again.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", again.Code)
	}
}

// TestProfileDeleteConfirmationSaysWhatItCascades matters because the
// schema's ON DELETE CASCADE silently detaches aliases and devices. A bare
// "are you sure?" would hide the only consequence worth confirming.
func TestProfileDeleteConfirmationSaysWhatItCascades(t *testing.T) {
	a, st := newAliasTestApp(t)
	seedProfile(t, st, "workstations")

	req := httptest.NewRequest(http.MethodGet, "/profiles/panel", nil)
	rec := httptest.NewRecorder()
	a.handleProfilesPanel(rec, authed(req))

	body := rec.Body.String()
	if !strings.Contains(body, "hx-confirm=") {
		t.Fatalf("delete asks for no confirmation:\n%s", body)
	}
	if !strings.Contains(body, "stop reaching") {
		t.Fatalf("the confirmation does not say what deleting cascades:\n%s", body)
	}
}
