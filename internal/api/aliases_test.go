package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/validate"
)

// newTestRouter is this file's (and profiles_test.go's/devices_test.go's)
// shared way of building a real router over a fake store — every test
// exercises the actual NewRouter/RequireKind/withMaxBytes chain, not a bare
// handler function, so a route missing its auth guard or its body bound
// would fail these tests too, not just router_test.go's synthetic ones.
func newTestRouter(t *testing.T, s *fakeStore) http.Handler {
	t.Helper()
	h, err := NewRouter(s, time.Now)
	if err != nil {
		t.Fatalf("NewRouter(...) = %v, want nil", err)
	}
	return h
}

func doRequest(h http.Handler, method, path, token string, body []byte) *httptest.ResponseRecorder {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// TestAliasesEndpointsRejectUnauthenticatedRequests is the server-api
// spec's "Unauthenticated request rejected" scenario, exercised against
// every alias route: none of list/create/get/update/delete may be reached
// without a valid session. Mutation this test detects: removing
// RequiredKind (or the Public/RequiredKind declaration entirely) from any
// one of these routes in router.go's routes() — that route would then
// either fail registration (caught by router_test.go) or, if left
// Public by mistake, respond 200/404 instead of 401 here.
func TestAliasesEndpointsRejectUnauthenticatedRequests(t *testing.T) {
	h := newTestRouter(t, newFakeStore())

	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/v1/aliases"},
		{http.MethodPost, "/api/v1/aliases"},
		{http.MethodGet, "/api/v1/aliases/some-id"},
		{http.MethodPut, "/api/v1/aliases/some-id"},
		{http.MethodDelete, "/api/v1/aliases/some-id"},
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

// TestAuthenticatedAliasCreateAndListRoundTrips is the server-api spec's
// "Authenticated CRUD succeeds" scenario verbatim: a valid session creating
// an alias must see it in a subsequent list. Mutation this test detects: a
// handler that decodes the body but never calls Aliases().Create (or
// discards the result) — the list would come back empty.
func TestAuthenticatedAliasCreateAndListRoundTrips(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	token := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	body, _ := json.Marshal(domain.Alias{Name: "gs", Command: "git status", Enabled: true})
	createRec := doRequest(h, http.MethodPost, "/api/v1/aliases", token, body)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("POST /api/v1/aliases = %d, want %d, body=%s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}

	var created aliasResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decoding create response: %v", err)
	}
	if created.ID == "" {
		t.Fatal("created alias has no id")
	}

	listRec := doRequest(h, http.MethodGet, "/api/v1/aliases", token, nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/aliases = %d, want %d", listRec.Code, http.StatusOK)
	}
	var list []domain.Alias
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decoding list response: %v", err)
	}
	if len(list) != 1 || list[0].Name != "gs" {
		t.Fatalf("list = %+v, want exactly one alias named %q", list, "gs")
	}
}

// TestAliasesCreateRejectsInvalidCommand is design decision 16's blocking
// half: validate.Command failures are 400. Mutation this test detects:
// calling validate.Command but ignoring its error (or removing the call
// outright) — an alias with an empty command would be persisted instead of
// rejected.
func TestAliasesCreateRejectsInvalidCommand(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	token := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	body, _ := json.Marshal(domain.Alias{Name: "broken", Command: "   "})
	rec := doRequest(h, http.MethodPost, "/api/v1/aliases", token, body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/v1/aliases with an empty command = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	list, err := s.Aliases().List(t.Context())
	if err != nil {
		t.Fatalf("Aliases().List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("an alias was persisted despite a rejected command: %+v", list)
	}
}

// TestAliasesCreateAcceptsNameWarningAndStoresIt is design decision 16's
// non-blocking half, and the exact property the milestone's own
// instructions single out: a validate.Name issue over
// serverValidationShells must never block a write. "process" is a
// PowerShell reserved word (internal/validate/name.go's powershellReserved)
// but a perfectly ordinary bash/zsh identifier, so it is guaranteed to
// produce exactly one per-shell warning without being rejected outright.
// Mutation this test detects: calling validate.Name (or aggregating
// nameWarnings) in validateAliasWrite and treating its error as blocking —
// this alias would come back 400 and never reach the store at all.
func TestAliasesCreateAcceptsNameWarningAndStoresIt(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	token := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	body, _ := json.Marshal(domain.Alias{Name: "process", Command: "ps aux", Enabled: true})
	rec := doRequest(h, http.MethodPost, "/api/v1/aliases", token, body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/v1/aliases with a name warning (no blocking issue) = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var created aliasResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decoding create response: %v", err)
	}
	if len(created.NameWarnings) == 0 {
		t.Fatal("expected at least one name warning (name \"process\" is PowerShell-reserved), got none")
	}
	found := false
	for _, w := range created.NameWarnings {
		if strings.Contains(w, "powershell") {
			found = true
		}
	}
	if !found {
		t.Fatalf("nameWarnings = %v, want one mentioning powershell", created.NameWarnings)
	}

	list, err := s.Aliases().List(t.Context())
	if err != nil {
		t.Fatalf("Aliases().List: %v", err)
	}
	if len(list) != 1 || list[0].Name != "process" {
		t.Fatalf("the alias with a name warning was not persisted: list=%+v", list)
	}
}

// TestAliasesCreateRejectsOnceAtMaxAliases is WARNING 5's own RED test:
// design decision 4 assumes validate.MaxAliases already bounds the alias
// set to justify never SQL-filtering it, but nothing enforced that bound
// from this server's own create path before this correction. Seeding the
// store directly (bypassing HTTP) to exactly the cap, then attempting one
// more create through the real handler, is what proves the enforcement
// lives in handleAliasesCreate itself, not merely in the fake store's own
// bookkeeping. Mutation this test detects: removing the
// CreateAliasWithinLimit call from
// handleAliasesCreate — a 5001st alias would then be accepted (201)
// instead of rejected (400).
func TestAliasesCreateRejectsOnceAtMaxAliases(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	token := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	for i := 0; i < validate.MaxAliases; i++ {
		if _, err := s.Aliases().Create(context.Background(), domain.Alias{
			Name: fmt.Sprintf("seed-%d", i), Command: "true", Enabled: true,
		}); err != nil {
			t.Fatalf("seeding alias %d/%d: %v", i, validate.MaxAliases, err)
		}
	}

	body, _ := json.Marshal(domain.Alias{Name: "one-too-many", Command: "true", Enabled: true})
	rec := doRequest(h, http.MethodPost, "/api/v1/aliases", token, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/v1/aliases at the MaxAliases cap = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	var decoded struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	if decoded.Error.Code != codeTooManyAliases {
		t.Fatalf("error.code = %q, want %q", decoded.Error.Code, codeTooManyAliases)
	}

	list, err := s.Aliases().List(t.Context())
	if err != nil {
		t.Fatalf("Aliases().List: %v", err)
	}
	if len(list) != validate.MaxAliases {
		t.Fatalf("alias count after the rejected create = %d, want exactly %d (the seeded amount, unchanged)", len(list), validate.MaxAliases)
	}
}

// createTestAlias creates an alias through the real handler (not a store
// shortcut) and returns its assigned id, so WARNING 3's own tests below
// exercise the exact same write path every other test in this file does.
func createTestAlias(t *testing.T, h http.Handler, token, name, command string) string {
	t.Helper()
	body, _ := json.Marshal(domain.Alias{Name: name, Command: command, Enabled: true})
	rec := doRequest(h, http.MethodPost, "/api/v1/aliases", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/v1/aliases(%q) = %d, want %d, body=%s", name, rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created aliasResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decoding create response: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("created alias %q has no id", name)
	}
	return created.ID
}

// TestAliasesGetReturnsTheRequestedAliasByID is WARNING 3's own RED test
// for handleAliasesGet: it creates two distinct aliases and asserts GET by
// id returns exactly the one asked for, never the other. Mutation this
// test detects: a handler that ignores r.PathValue("id") and always
// returns some fixed alias (e.g. the first one created) — this test fails
// the moment it asks for the second alias by its own id and gets the
// first one back instead.
func TestAliasesGetReturnsTheRequestedAliasByID(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	token := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	_ = createTestAlias(t, h, token, "first", "echo first")
	secondID := createTestAlias(t, h, token, "second", "echo second")

	rec := doRequest(h, http.MethodGet, "/api/v1/aliases/"+secondID, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/aliases/%s = %d, want %d, body=%s", secondID, rec.Code, http.StatusOK, rec.Body.String())
	}
	var got domain.Alias
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding get response: %v", err)
	}
	if got.ID != secondID || got.Name != "second" {
		t.Fatalf("GET /api/v1/aliases/%s returned %+v, want the alias named %q with that id", secondID, got, "second")
	}
}

// TestAliasesUpdateAppliesTheRequestBody is WARNING 3's own RED test for
// handleAliasesUpdate: it creates an alias, PUTs a changed command, and
// asserts a subsequent GET reflects the change. Mutation this test
// detects: a handler that decodes the body but discards it (e.g. calling
// Aliases().Update with the original, unmodified value, or not calling
// Update at all) — the alias's command would still read the original
// value afterward.
func TestAliasesUpdateAppliesTheRequestBody(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	token := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	id := createTestAlias(t, h, token, "gs", "git status")

	body, _ := json.Marshal(domain.Alias{Name: "gs", Command: "git status -sb", Enabled: true})
	updateRec := doRequest(h, http.MethodPut, "/api/v1/aliases/"+id, token, body)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("PUT /api/v1/aliases/%s = %d, want %d, body=%s", id, updateRec.Code, http.StatusOK, updateRec.Body.String())
	}

	getRec := doRequest(h, http.MethodGet, "/api/v1/aliases/"+id, token, nil)
	var got domain.Alias
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding get response: %v", err)
	}
	if got.Command != "git status -sb" {
		t.Fatalf("Command after update = %q, want %q — the update body was not applied", got.Command, "git status -sb")
	}
}

// TestAliasesDeleteRemovesTheAlias is WARNING 3's own RED test for
// handleAliasesDelete: it creates an alias, deletes it, and asserts a
// subsequent GET returns 404. Mutation this test detects: a handler that
// responds 204 without ever calling Aliases().Delete (a no-op delete) —
// the alias would still be gettable afterward.
func TestAliasesDeleteRemovesTheAlias(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	token := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	id := createTestAlias(t, h, token, "gs", "git status")

	deleteRec := doRequest(h, http.MethodDelete, "/api/v1/aliases/"+id, token, nil)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/v1/aliases/%s = %d, want %d, body=%s", id, deleteRec.Code, http.StatusNoContent, deleteRec.Body.String())
	}

	getRec := doRequest(h, http.MethodGet, "/api/v1/aliases/"+id, token, nil)
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("GET a deleted alias = %d, want %d — the delete did not actually remove it", getRec.Code, http.StatusNotFound)
	}
}
