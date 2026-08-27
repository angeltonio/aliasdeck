package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/angeltonio/aliasdeck/internal/store/sqlitestore"
)

func newAliasTestApp(t *testing.T) (*webapp, *sqlitestore.SQLiteStore) {
	t.Helper()
	st, err := sqlitestore.Open(context.Background(), filepath.Join(t.TempDir(), "aliasdeck.db"))
	if err != nil {
		t.Fatalf("sqlitestore.Open() returned an error: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	templates, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates() returned an error: %v", err)
	}
	now := time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)
	return &webapp{store: st, now: func() time.Time { return now }, tmpl: templates}, st
}

// authed wires a request the way requireSession would have, so a handler
// test exercises the handler rather than the middleware.
func authed(req *http.Request) *http.Request {
	return req.WithContext(withSubject(req.Context(), webSubject{TokenID: "session-1", OperatorID: "operator-a"}))
}

func submitAliasEdit(t *testing.T, a *webapp, id string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/aliases/"+id, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	a.handleAliasesUpdate(rec, authed(req))
	return rec
}

// fullyTargetedAlias is an alias carrying every targeting dimension the
// store persists in a join table or an encoded column. The edit form shows
// none of them, which is exactly why they are what the tests below watch.
func fullyTargetedAlias(t *testing.T, st store.Store) domain.Alias {
	t.Helper()
	ctx := context.Background()

	profile, err := st.Profiles().Create(ctx, domain.Profile{Name: "workstations"})
	if err != nil {
		t.Fatalf("creating profile: %v", err)
	}
	// A device can only come into existence by consuming an enrollment
	// token — DeviceRepo has no Create at all — so the targeting fixture
	// has to go through the real enrollment path.
	lookup := createEnrollmentToken(t, st, time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC))
	device, err := st.Tokens().ConsumeEnrollment(ctx, lookup, domain.Device{
		Name: "laptop", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh,
	})
	if err != nil {
		t.Fatalf("enrolling the fixture device: %v", err)
	}

	created, err := st.Aliases().Create(ctx, domain.Alias{
		Name:        "gs",
		Command:     "git status",
		Description: "show the working tree",
		Enabled:     true,
		Tags:        []string{"git", "daily"},
		Platforms:   []domain.Platform{domain.PlatformMacOS, domain.PlatformLinux},
		Shells:      []domain.Shell{domain.ShellZsh},
		ProfileIDs:  []string{profile.ID},
		DeviceIDs:   []string{device.ID},
	})
	if err != nil {
		t.Fatalf("creating alias: %v", err)
	}
	return created
}

// TestAliasEditPreservesTheTargetingTheFormDoesNotShow is the reason this
// handler loads the stored alias instead of building one from the form.
// AliasRepo.Update replaces targeting wholesale, so every dimension the row
// does not render has to be carried through. The row now renders groups,
// platforms and shells, which it therefore owns; tags and per-device
// targeting it does not, so those must survive untouched. If this fails,
// editing has started dropping targeting the operator cannot even see.
func TestAliasEditPreservesTheTargetingTheFormDoesNotShow(t *testing.T) {
	a, st := newAliasTestApp(t)
	before := fullyTargetedAlias(t, st)
	if len(before.Tags) == 0 || len(before.DeviceIDs) == 0 {
		t.Fatalf("fixture = %+v, want it to carry both tags and device targeting", before)
	}

	// Post only the three text fields and the group it already had: the
	// platform and shell boxes come back unchecked, which the domain model
	// reads as "every one".
	rec := submitAliasEdit(t, a, before.ID, url.Values{
		"name":        {"gst"},
		"command":     {"git status --short"},
		"description": {"show the working tree, briefly"},
		"groups":      {before.ProfileIDs[0]},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", rec.Code, rec.Body.String())
	}

	after, err := st.Aliases().Get(context.Background(), before.ID)
	if err != nil {
		t.Fatalf("Get() after the edit: %v", err)
	}

	if after.Name != "gst" || after.Command != "git status --short" {
		t.Fatalf("edited fields = %q/%q, want gst/git status --short", after.Name, after.Command)
	}

	// Dimensions the form does not render survived.
	if len(after.Tags) != len(before.Tags) {
		t.Fatalf("tags = %v, want %v — the edit dropped targeting the form never showed", after.Tags, before.Tags)
	}
	if len(after.DeviceIDs) != len(before.DeviceIDs) || after.DeviceIDs[0] != before.DeviceIDs[0] {
		t.Fatalf("device targeting = %v, want %v — the edit dropped targeting the form never showed", after.DeviceIDs, before.DeviceIDs)
	}
	if !after.Enabled {
		t.Fatal("the alias was disabled by an edit that never showed an enabled field")
	}

	// Dimensions the form does render followed what it posted.
	if len(after.ProfileIDs) != 1 || after.ProfileIDs[0] != before.ProfileIDs[0] {
		t.Fatalf("group targeting = %v, want the posted %v", after.ProfileIDs, before.ProfileIDs)
	}
	if len(after.Platforms) != 0 {
		t.Fatalf("platforms = %v, want none — unchecked boxes mean every platform", after.Platforms)
	}
	if len(after.Shells) != 0 {
		t.Fatalf("shells = %v, want none — unchecked boxes mean every shell", after.Shells)
	}
}

// TestAliasEditRetargetsGroupsPlatformsAndShells is the other half: what the
// row does render must actually take effect, or the screen would look like it
// changed targeting while changing nothing.
func TestAliasEditRetargetsGroupsPlatformsAndShells(t *testing.T) {
	a, st := newAliasTestApp(t)
	before := fullyTargetedAlias(t, st)
	other := seedProfile(t, st, "servers")

	rec := submitAliasEdit(t, a, before.ID, url.Values{
		"name": {"gs"}, "command": {"git status"}, "description": {""},
		"groups":    {other.ID},
		"platforms": {"windows"},
		"shells":    {"powershell", "bash"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", rec.Code, rec.Body.String())
	}

	after, err := st.Aliases().Get(context.Background(), before.ID)
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if len(after.ProfileIDs) != 1 || after.ProfileIDs[0] != other.ID {
		t.Fatalf("groups = %v, want [%s]", after.ProfileIDs, other.ID)
	}
	if len(after.Platforms) != 1 || after.Platforms[0] != domain.PlatformWindows {
		t.Fatalf("platforms = %v, want [windows]", after.Platforms)
	}
	if len(after.Shells) != 2 {
		t.Fatalf("shells = %v, want two", after.Shells)
	}
}

// TestAliasEditRejectsAnUnknownPlatformWithoutWriting covers the one thing a
// checkbox value can be that a text field cannot: a value the domain does not
// define, hand-posted past the rendered options.
func TestAliasEditRejectsAnUnknownPlatformWithoutWriting(t *testing.T) {
	a, st := newAliasTestApp(t)
	before := fullyTargetedAlias(t, st)

	rec := submitAliasEdit(t, a, before.ID, url.Values{
		"name": {"gs"}, "command": {"git status"}, "platforms": {"solaris"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %q", rec.Code, rec.Body.String())
	}

	after, err := st.Aliases().Get(context.Background(), before.ID)
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if len(after.Platforms) != 2 {
		t.Fatalf("platforms = %v, want the original two — a rejected edit wrote anyway", after.Platforms)
	}
}

func TestAliasEditFormRendersTheStoredValues(t *testing.T) {
	a, st := newAliasTestApp(t)
	created := fullyTargetedAlias(t, st)

	req := httptest.NewRequest(http.MethodGet, "/aliases/"+created.ID+"/edit", nil)
	req.SetPathValue("id", created.ID)
	rec := httptest.NewRecorder()
	a.handleAliasesEdit(rec, authed(req))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`hx-put="/aliases/` + created.ID + `"`,
		`name="name" value="gs"`,
		`name="command" value="git status"`,
		`hx-include="closest tr"`,
		`hx-get="/aliases/panel"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("edit form does not contain %q:\n%s", want, body)
		}
	}
}

// TestAliasEditRowUsesNoFormElement guards a mistake this row was written
// with once. A <form> is not valid inside a <tr>: an HTML parser hoists it
// out of the table, so the inputs stop being associated with it and
// submission silently does nothing. No Go handler test can see that — the
// handler is fed a request body directly and never parses markup the way a
// browser does — so the invariant is pinned here instead. The row gathers
// its inputs with hx-include and gets its CSRF token from csrf.js's header,
// exactly as the delete button already does.
func TestAliasEditRowUsesNoFormElement(t *testing.T) {
	a, st := newAliasTestApp(t)
	created := fullyTargetedAlias(t, st)

	req := httptest.NewRequest(http.MethodGet, "/aliases/"+created.ID+"/edit", nil)
	req.SetPathValue("id", created.ID)
	rec := httptest.NewRecorder()
	a.handleAliasesEdit(rec, authed(req))

	if body := rec.Body.String(); strings.Contains(body, "<form") {
		t.Fatalf("the edit row rendered a <form> inside a table row, which a browser hoists out of the table:\n%s", body)
	}
}

func TestAliasEditCancelReturnsTheReadOnlyPanel(t *testing.T) {
	a, st := newAliasTestApp(t)
	created := fullyTargetedAlias(t, st)

	req := httptest.NewRequest(http.MethodGet, "/aliases/panel", nil)
	rec := httptest.NewRecorder()
	a.handleAliasesPanel(rec, authed(req))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, `hx-put=`) {
		t.Fatalf("cancel still rendered an edit form:\n%s", body)
	}
	if !strings.Contains(body, `hx-get="/aliases/`+created.ID+`/edit"`) {
		t.Fatalf("cancel did not restore the row's Edit button:\n%s", body)
	}
}

func TestAliasEditRejectsARenameOntoAnExistingNameAndKeepsTheForm(t *testing.T) {
	a, st := newAliasTestApp(t)
	first := fullyTargetedAlias(t, st)
	if _, err := st.Aliases().Create(context.Background(), domain.Alias{Name: "taken", Command: "echo hi", Enabled: true}); err != nil {
		t.Fatalf("creating the second alias: %v", err)
	}

	rec := submitAliasEdit(t, a, first.ID, url.Values{
		"name": {"taken"}, "command": {"git status"}, "description": {""},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %q", rec.Code, rec.Body.String())
	}
	// The operator keeps the row they were editing rather than losing it to
	// the error message.
	if !strings.Contains(rec.Body.String(), `hx-put="/aliases/`+first.ID+`"`) {
		t.Fatalf("a rejected rename closed the edit form:\n%s", rec.Body.String())
	}

	unchanged, err := st.Aliases().Get(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if unchanged.Name != "gs" {
		t.Fatalf("name = %q, want the rejected rename to have changed nothing", unchanged.Name)
	}
}

func TestAliasEditRejectsAnInvalidCommandWithoutTouchingTheStore(t *testing.T) {
	a, st := newAliasTestApp(t)
	created := fullyTargetedAlias(t, st)

	rec := submitAliasEdit(t, a, created.ID, url.Values{
		"name": {"gs"}, "command": {"git status\nrm -rf /"}, "description": {""},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %q", rec.Code, rec.Body.String())
	}

	unchanged, err := st.Aliases().Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if unchanged.Command != "git status" {
		t.Fatalf("command = %q, want the rejected multi-line body to have changed nothing", unchanged.Command)
	}
}

func TestAliasEditOnAMissingAliasReportsItWithoutAnEditForm(t *testing.T) {
	a, _ := newAliasTestApp(t)

	rec := submitAliasEdit(t, a, "does-not-exist", url.Values{
		"name": {"gs"}, "command": {"git status"}, "description": {""},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %q", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "hx-put=") {
		t.Fatalf("a missing alias still rendered an edit form:\n%s", rec.Body.String())
	}
}
