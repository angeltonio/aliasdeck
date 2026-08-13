package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// TestProfilesEndpointsRejectUnauthenticatedRequests is
// TestAliasesEndpointsRejectUnauthenticatedRequests's sibling for profiles.
func TestProfilesEndpointsRejectUnauthenticatedRequests(t *testing.T) {
	h := newTestRouter(t, newFakeStore())

	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/profiles"},
		{http.MethodPost, "/api/v1/profiles"},
		{http.MethodGet, "/api/v1/profiles/some-id"},
		{http.MethodPut, "/api/v1/profiles/some-id"},
		{http.MethodDelete, "/api/v1/profiles/some-id"},
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

// TestAuthenticatedProfileCreateAndListRoundTrips mirrors the alias round
// trip for the profile collection.
func TestAuthenticatedProfileCreateAndListRoundTrips(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	token := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	body, _ := json.Marshal(domain.Profile{Name: "Homelab", Description: "self-hosted machines"})
	createRec := doRequest(h, http.MethodPost, "/api/v1/profiles", token, body)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("POST /api/v1/profiles = %d, want %d, body=%s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}

	listRec := doRequest(h, http.MethodGet, "/api/v1/profiles", token, nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/profiles = %d, want %d", listRec.Code, http.StatusOK)
	}
	var list []domain.Profile
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decoding list response: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Homelab" {
		t.Fatalf("list = %+v, want exactly one profile named %q", list, "Homelab")
	}
}

// TestProfilesCreateRejectsDuplicateName proves the store's ErrConflict
// sentinel reaches the wire as 409 through this endpoint specifically —
// writeStoreError's own mapping is errors_test.go's job; this is the
// integration of that mapping into a real handler.
func TestProfilesCreateRejectsDuplicateName(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	token := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	if _, err := s.Profiles().Create(t.Context(), domain.Profile{Name: "Homelab"}); err != nil {
		t.Fatalf("seeding a profile: %v", err)
	}

	body, _ := json.Marshal(domain.Profile{Name: "Homelab"})
	rec := doRequest(h, http.MethodPost, "/api/v1/profiles", token, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST /api/v1/profiles with a duplicate name = %d, want %d", rec.Code, http.StatusConflict)
	}
}

// TestAliasesCreateRejectsDanglingProfileReference proves store.ErrInvalidReference
// (design decision 18) reaches the wire as 422 through the alias endpoint
// when ProfileIDs names a profile that does not exist.
func TestAliasesCreateRejectsDanglingProfileReference(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	token := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	body, _ := json.Marshal(domain.Alias{
		Name: "gs", Command: "git status", Enabled: true,
		ProfileIDs: []string{"does-not-exist"},
	})
	rec := doRequest(h, http.MethodPost, "/api/v1/aliases", token, body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST /api/v1/aliases with a dangling profile reference = %d, want %d, body=%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	var decoded struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	if decoded.Error.Code != codeInvalidReference {
		t.Fatalf("error.code = %q, want %q", decoded.Error.Code, codeInvalidReference)
	}
}
