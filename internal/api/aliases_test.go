package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
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
