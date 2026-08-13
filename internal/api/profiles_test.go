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

// createTestProfile creates a profile through the real handler and returns
// its assigned id, mirroring aliases_test.go's createTestAlias.
func createTestProfile(t *testing.T, h http.Handler, token, name string) string {
	t.Helper()
	body, _ := json.Marshal(domain.Profile{Name: name})
	rec := doRequest(h, http.MethodPost, "/api/v1/profiles", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/v1/profiles(%q) = %d, want %d, body=%s", name, rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created domain.Profile
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decoding create response: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("created profile %q has no id", name)
	}
	return created.ID
}

// TestProfilesGetReturnsTheRequestedProfileByID is WARNING 3's own RED
// test for handleProfilesGet: two distinct profiles, GET by the second
// id must return exactly that one. Mutation this test detects: a handler
// ignoring r.PathValue("id") and returning some fixed profile instead.
func TestProfilesGetReturnsTheRequestedProfileByID(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	token := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	_ = createTestProfile(t, h, token, "Homelab")
	workID := createTestProfile(t, h, token, "Work")

	rec := doRequest(h, http.MethodGet, "/api/v1/profiles/"+workID, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/profiles/%s = %d, want %d, body=%s", workID, rec.Code, http.StatusOK, rec.Body.String())
	}
	var got domain.Profile
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding get response: %v", err)
	}
	if got.ID != workID || got.Name != "Work" {
		t.Fatalf("GET /api/v1/profiles/%s returned %+v, want the profile named %q with that id", workID, got, "Work")
	}
}

// TestProfilesUpdateAppliesTheRequestBody is WARNING 3's own RED test for
// handleProfilesUpdate: a PUT changing Description must be reflected on a
// subsequent GET. Mutation this test detects: a handler that decodes the
// body but discards it instead of calling Profiles().Update with it.
func TestProfilesUpdateAppliesTheRequestBody(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	token := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	id := createTestProfile(t, h, token, "Homelab")

	body, _ := json.Marshal(domain.Profile{Name: "Homelab", Description: "self-hosted machines, updated"})
	updateRec := doRequest(h, http.MethodPut, "/api/v1/profiles/"+id, token, body)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("PUT /api/v1/profiles/%s = %d, want %d, body=%s", id, updateRec.Code, http.StatusOK, updateRec.Body.String())
	}

	getRec := doRequest(h, http.MethodGet, "/api/v1/profiles/"+id, token, nil)
	var got domain.Profile
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding get response: %v", err)
	}
	if got.Description != "self-hosted machines, updated" {
		t.Fatalf("Description after update = %q, want %q — the update body was not applied", got.Description, "self-hosted machines, updated")
	}
}

// TestProfilesDeleteRemovesTheProfile is WARNING 3's own RED test for
// handleProfilesDelete: a subsequent GET after delete must 404. Mutation
// this test detects: a no-op handler answering 204 without ever calling
// Profiles().Delete.
func TestProfilesDeleteRemovesTheProfile(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	token := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	id := createTestProfile(t, h, token, "Homelab")

	deleteRec := doRequest(h, http.MethodDelete, "/api/v1/profiles/"+id, token, nil)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/v1/profiles/%s = %d, want %d, body=%s", id, deleteRec.Code, http.StatusNoContent, deleteRec.Body.String())
	}

	getRec := doRequest(h, http.MethodGet, "/api/v1/profiles/"+id, token, nil)
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("GET a deleted profile = %d, want %d — the delete did not actually remove it", getRec.Code, http.StatusNotFound)
	}
}
