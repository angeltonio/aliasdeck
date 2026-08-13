package api

import (
	"net/http"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// profilesPattern and profilePattern mirror aliasesPattern/aliasPattern:
// the collection and one profile by id.
const (
	profilesPattern = "/api/v1/profiles"
	profilePattern  = "/api/v1/profiles/{id}"
)

// Profiles carry no shell- or command-syntax fields (domain.Profile is
// Name/Description only), so unlike aliases there is nothing here for
// internal/validate to check — store-level uniqueness (store.ErrConflict on
// a duplicate Name) is the only write constraint.

func (a *api) handleProfilesList(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.Profiles().List(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *api) handleProfilesCreate(w http.ResponseWriter, r *http.Request) {
	var in domain.Profile
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := a.store.Profiles().Create(r.Context(), in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *api) handleProfilesGet(w http.ResponseWriter, r *http.Request) {
	out, err := a.store.Profiles().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) handleProfilesUpdate(w http.ResponseWriter, r *http.Request) {
	var in domain.Profile
	if !decodeJSON(w, r, &in) {
		return
	}
	in.ID = r.PathValue("id")
	out, err := a.store.Profiles().Update(r.Context(), in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) handleProfilesDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.Profiles().Delete(r.Context(), r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
