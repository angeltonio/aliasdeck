package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
)

// auditedApp returns an app whose session resolves to a real operator, so the
// recorded actor name is the one the store actually holds rather than a value
// the test invented.
func auditedApp(t *testing.T) (*webapp, store.Store, string) {
	t.Helper()
	a, st := newAliasTestApp(t)
	op, err := st.Operators().Create(context.Background(), store.Operator{
		Username: "admin", PasswordHash: []byte("irrelevant"),
	})
	if err != nil {
		t.Fatalf("creating operator: %v", err)
	}
	return a, st, op.ID
}

func asOperator(req *http.Request, operatorID string) *http.Request {
	return req.WithContext(withSubject(req.Context(), webSubject{TokenID: "session-1", OperatorID: operatorID}))
}

func auditEvents(t *testing.T, st store.Store) []store.AuditEvent {
	t.Helper()
	events, err := st.Audit().Recent(context.Background(), 50)
	if err != nil {
		t.Fatalf("Recent(): %v", err)
	}
	return events
}

func onlyEvent(t *testing.T, st store.Store) store.AuditEvent {
	t.Helper()
	events := auditEvents(t, st)
	if len(events) != 1 {
		t.Fatalf("recorded %d events, want exactly one: %+v", len(events), events)
	}
	return events[0]
}

// TestAuditRecordsWhoActedAndWhatOn covers the question the table exists for.
// An entry naming only an action is not an answer.
func TestAuditRecordsWhoActedAndWhatOn(t *testing.T) {
	a, st, operatorID := auditedApp(t)

	form := url.Values{"name": {"gs"}, "command": {"git status"}}
	req := httptest.NewRequest(http.MethodPost, "/aliases", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.handleAliasesCreate(rec, asOperator(req, operatorID))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200", rec.Code)
	}

	e := onlyEvent(t, st)
	if e.Action != store.AuditAliasCreated {
		t.Errorf("Action = %q, want %q", e.Action, store.AuditAliasCreated)
	}
	if e.ActorID != operatorID {
		t.Errorf("ActorID = %q, want %q", e.ActorID, operatorID)
	}
	if e.ActorName != "admin" {
		t.Errorf("ActorName = %q, want the operator's username at the time", e.ActorName)
	}
	if e.SubjectKind != "alias" || e.SubjectLabel != "gs" {
		t.Errorf("subject = %s/%s, want alias/gs", e.SubjectKind, e.SubjectLabel)
	}
	if e.SubjectID == "" {
		t.Error("SubjectID is empty; the record cannot be correlated with the row")
	}
	if !e.At.Equal(a.now()) {
		t.Errorf("At = %v, want the injected clock %v", e.At, a.now())
	}
}

// TestAuditRecordsTheNameOfSomethingDeleted is the case denormalizing the
// label pays for. After the row is gone there is nothing left to resolve, so a
// record holding only an id would say a deletion happened without saying what
// was deleted.
func TestAuditRecordsTheNameOfSomethingDeleted(t *testing.T) {
	a, st, operatorID := auditedApp(t)
	created, err := st.Aliases().Create(context.Background(), domain.Alias{Name: "doomed", Command: "echo 1", Enabled: true})
	if err != nil {
		t.Fatalf("creating alias: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/aliases/"+created.ID, nil)
	req.SetPathValue("id", created.ID)
	rec := httptest.NewRecorder()
	a.handleAliasesDelete(rec, asOperator(req, operatorID))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", rec.Code)
	}

	e := onlyEvent(t, st)
	if e.Action != store.AuditAliasDeleted {
		t.Errorf("Action = %q, want %q", e.Action, store.AuditAliasDeleted)
	}
	if e.SubjectLabel != "doomed" {
		t.Errorf("SubjectLabel = %q, want the name the alias had before it was deleted", e.SubjectLabel)
	}
	if _, err := st.Aliases().Get(context.Background(), created.ID); err == nil {
		t.Fatal("the alias still exists; this test is not exercising the case it claims to")
	}
}

// TestAuditRecordsEveryOperatorSurfaceOfThisPackage walks each mutation the
// browser offers. A log that covers most actions is one an operator would
// trust for the one it missed.
func TestAuditRecordsEveryOperatorSurfaceOfThisPackage(t *testing.T) {
	a, st, operatorID := auditedApp(t)
	ctx := context.Background()

	group, err := st.Profiles().Create(ctx, domain.Profile{Name: "laptops"})
	if err != nil {
		t.Fatalf("seeding group: %v", err)
	}
	dev := enrollDevice(t, a, st, "work-mac")
	alias, err := st.Aliases().Create(ctx, domain.Alias{Name: "gs", Command: "git status", Enabled: true})
	if err != nil {
		t.Fatalf("seeding alias: %v", err)
	}

	form := func(v url.Values) *strings.Reader { return strings.NewReader(v.Encode()) }
	post := func(method, path, id string, body *strings.Reader, h http.HandlerFunc) {
		t.Helper()
		req := httptest.NewRequest(method, path, body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if id != "" {
			req.SetPathValue("id", id)
		}
		req.Host = "aliases.example"
		rec := httptest.NewRecorder()
		h(rec, asOperator(req, operatorID))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s status = %d, want 200: %s", method, path, rec.Code, rec.Body.String())
		}
	}

	post(http.MethodPut, "/aliases/"+alias.ID, alias.ID, form(url.Values{"name": {"gst"}, "command": {"git status -s"}}), a.handleAliasesUpdate)
	post(http.MethodPost, "/profiles", "", form(url.Values{"name": {"servers"}}), a.handleProfilesCreate)
	post(http.MethodPut, "/profiles/"+group.ID, group.ID, form(url.Values{"name": {"portables"}}), a.handleProfilesUpdate)
	post(http.MethodDelete, "/profiles/"+group.ID, group.ID, form(url.Values{}), a.handleProfilesDelete)
	post(http.MethodPut, "/devices/"+dev.ID, dev.ID, form(url.Values{"name": {"renamed"}}), a.handleDevicesUpdate)
	post(http.MethodPost, "/devices/"+dev.ID+"/token", dev.ID, form(url.Values{}), a.handleDevicesRotateToken)
	post(http.MethodPost, "/devices/"+dev.ID+"/revoke", dev.ID, form(url.Values{}), a.handleDevicesRevoke)

	seen := map[store.AuditAction]bool{}
	for _, e := range auditEvents(t, st) {
		seen[e.Action] = true
	}
	for _, want := range []store.AuditAction{
		store.AuditAliasUpdated,
		store.AuditGroupCreated,
		store.AuditGroupUpdated,
		store.AuditGroupDeleted,
		store.AuditDeviceUpdated,
		store.AuditDeviceRotated,
		store.AuditDeviceRevoked,
	} {
		if !seen[want] {
			t.Errorf("no %q event was recorded", want)
		}
	}
}

// TestAuditIgnoresDeviceTrafficIsNotTestableHere documents the boundary this
// package cannot prove. Sync and heartbeat are internal/api's routes, and the
// reason they are excluded — Touch and Heartbeat run every few seconds per
// device — is why the write points live in handlers at all. What this package
// can assert is the other half: a read changes nothing.
func TestAuditRecordsNothingForReads(t *testing.T) {
	a, st, operatorID := auditedApp(t)
	dev := enrollDevice(t, a, st, "work-mac")

	for _, call := range []func(){
		func() {
			req := httptest.NewRequest(http.MethodGet, "/devices/panel", nil)
			a.handleDevicesPanel(httptest.NewRecorder(), asOperator(req, operatorID))
		},
		func() {
			req := httptest.NewRequest(http.MethodGet, "/devices/"+dev.ID+"/preview", nil)
			req.SetPathValue("id", dev.ID)
			a.handleDevicePreview(httptest.NewRecorder(), asOperator(req, operatorID))
		},
		func() {
			req := httptest.NewRequest(http.MethodGet, "/aliases/panel", nil)
			a.handleAliasesPanel(httptest.NewRecorder(), asOperator(req, operatorID))
		},
	} {
		call()
	}

	if events := auditEvents(t, st); len(events) != 0 {
		t.Fatalf("reads recorded %d events: %+v", len(events), events)
	}
}

// TestAuditSurvivesAnUnknownOperator proves a missing name does not cost the
// record. Knowing which operator id acted is most of the answer, and refusing
// to record without a name would lose it entirely.
func TestAuditSurvivesAnUnknownOperator(t *testing.T) {
	a, st := newAliasTestApp(t)

	form := url.Values{"name": {"gs"}, "command": {"git status"}}
	req := httptest.NewRequest(http.MethodPost, "/aliases", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.handleAliasesCreate(rec, asOperator(req, "operator-that-does-not-exist"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200 — auditing must never fail the mutation", rec.Code)
	}

	e := onlyEvent(t, st)
	if e.ActorID != "operator-that-does-not-exist" {
		t.Errorf("ActorID = %q, want the id recorded even without a name", e.ActorID)
	}
	if e.ActorName != "" {
		t.Errorf("ActorName = %q, want empty rather than invented", e.ActorName)
	}
}

// failingAuditStore is a real store whose audit repo always fails, which is
// the only way to exercise the branch that matters: the mutation committed,
// the record did not.
type failingAuditStore struct{ store.Store }

func (f failingAuditStore) Audit() store.AuditRepo { return failingAuditRepo{} }

type failingAuditRepo struct{}

func (failingAuditRepo) Append(context.Context, store.AuditEvent) error {
	return errors.New("audit: disk is full")
}

func (failingAuditRepo) Recent(context.Context, int) ([]store.AuditEvent, error) { return nil, nil }

func (failingAuditRepo) Count(context.Context) (int, error) { return 0, nil }

// TestAuditFailureDoesNotFailTheMutationItRecords is the property that keeps
// this feature from becoming a new way for writes to fail. The alias was
// created and committed before the append ran; reporting an error now would
// tell an operator their action did not happen when it did.
//
// The cost is real and deliberate: the record is lost silently. That trade is
// documented on webapp.audit.
func TestAuditFailureDoesNotFailTheMutationItRecords(t *testing.T) {
	a, st, operatorID := auditedApp(t)
	a.store = failingAuditStore{st}

	form := url.Values{"name": {"gs"}, "command": {"git status"}}
	req := httptest.NewRequest(http.MethodPost, "/aliases", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.handleAliasesCreate(rec, asOperator(req, operatorID))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — a failed audit must not report a failed mutation", rec.Code)
	}

	aliases, err := st.Aliases().List(context.Background())
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(aliases) != 1 || aliases[0].Name != "gs" {
		t.Fatalf("aliases = %+v, want the create to have persisted", aliases)
	}
}
