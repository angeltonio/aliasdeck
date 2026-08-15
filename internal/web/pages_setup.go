package web

import (
	"errors"
	"net/http"

	"github.com/angeltonio/aliasdeck/internal/auth"
)

type setupPageData struct {
	Error, Credential, Username string
}

func (a *webapp) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	credential := r.URL.Query().Get("credential")
	if credential == "" || !auth.SetupEnabled(a.setupCredentialPath) {
		http.NotFound(w, r)
		return
	}
	a.renderSetup(w, http.StatusOK, setupPageData{Credential: credential})
}

func (a *webapp) renderSetup(w http.ResponseWriter, status int, data setupPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = a.tmpl.setup.Execute(w, data)
}

func (a *webapp) handleSetupSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderSetup(w, http.StatusBadRequest, setupPageData{Error: "the setup form could not be read"})
		return
	}
	data := setupPageData{Credential: r.FormValue("credential"), Username: r.FormValue("username")}
	err := auth.CompleteSetup(r.Context(), a.store, a.setupCredentialPath, data.Credential, data.Username, r.FormValue("password"), r.FormValue("confirmation"))
	if err == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	status := http.StatusBadRequest
	if errors.Is(err, auth.ErrSetupDisabled) || errors.Is(err, auth.ErrInvalidSetupCredential) {
		status = http.StatusNotFound
	}
	switch {
	case errors.Is(err, auth.ErrMismatchedSetupPassword):
		data.Error = "passwords do not match"
	case errors.Is(err, auth.ErrWeakSetupPassword):
		data.Error = "password must be at least 12 characters"
	case status == http.StatusNotFound:
		data.Error = "this setup link is invalid or has already been used"
	default:
		data.Error = "could not create the operator account"
	}
	a.renderSetup(w, status, data)
}
