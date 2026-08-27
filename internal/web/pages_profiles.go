package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/angeltonio/aliasdeck/internal/validate"
)

// profilesPageData is both profiles.html's (the full page) and
// profile_panel.html's (the htmx-swapped fragment) data shape, mirroring
// aliasesPageData so both screens behave the same way under htmx.
type profilesPageData struct {
	pageData
	Title    string
	Active   string
	Profiles []domain.Profile
	// EditingID names the one profile rendered as an inline edit row.
	EditingID string
	FormError string
}

func (a *webapp) handleProfilesPage(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.Profiles().List(r.Context())
	if err != nil {
		http.Error(w, translate(requestLanguage(r), "error.profile_load"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	view := pageDataFor(r)
	_ = a.tmpl.profiles.ExecuteTemplate(w, "base", profilesPageData{
		pageData: view, Title: translate(view.Lang, "profiles.title"), Active: "profiles", Profiles: list,
	})
}

func (a *webapp) handleProfilesCreate(w http.ResponseWriter, r *http.Request) {
	lang := requestLanguage(r)
	if err := r.ParseForm(); err != nil {
		a.respondProfilePanel(r, w, http.StatusBadRequest, translate(lang, "error.profile_form"))
		return
	}

	p := domain.Profile{
		Name:        strings.TrimSpace(r.FormValue("name")),
		Description: r.FormValue("description"),
	}
	if msg, ok := a.validateProfile(lang, p); !ok {
		a.respondProfilePanel(r, w, http.StatusBadRequest, msg)
		return
	}

	if _, err := a.store.Profiles().Create(r.Context(), p); err != nil {
		if errors.Is(err, store.ErrConflict) {
			a.respondProfilePanel(r, w, http.StatusConflict, translate(lang, "error.profile_conflict"))
			return
		}
		a.respondProfilePanel(r, w, http.StatusInternalServerError, formatted(lang, "error.profile_create", err.Error()))
		return
	}
	a.respondProfilePanel(r, w, http.StatusOK, "")
}

// handleProfilesEdit re-renders the panel with one row open for editing, and
// handleProfilesPanel re-renders it with none — the same pair the alias
// screen uses, so Cancel reloads from the store rather than trusting the
// browser to still hold the pre-edit values.
func (a *webapp) handleProfilesEdit(w http.ResponseWriter, r *http.Request) {
	a.respondProfilePanelEditing(r, w, http.StatusOK, r.PathValue("id"), "")
}

func (a *webapp) handleProfilesPanel(w http.ResponseWriter, r *http.Request) {
	a.respondProfilePanel(r, w, http.StatusOK, "")
}

// handleProfilesUpdate applies an edit to one profile. Unlike an alias,
// domain.Profile has no targeting to preserve — Name and Description are the
// whole record — so the update is built from the form directly. It still
// loads the stored profile first, to tell a missing id apart from a
// successful no-op rename.
func (a *webapp) handleProfilesUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	lang := requestLanguage(r)

	if err := r.ParseForm(); err != nil {
		a.respondProfilePanelEditing(r, w, http.StatusBadRequest, id, translate(lang, "error.profile_form"))
		return
	}

	existing, err := a.store.Profiles().Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.respondProfilePanel(r, w, http.StatusNotFound, translate(lang, "error.profile_missing"))
			return
		}
		http.Error(w, translate(lang, "error.profile_load"), http.StatusInternalServerError)
		return
	}

	updated := existing
	updated.Name = strings.TrimSpace(r.FormValue("name"))
	updated.Description = r.FormValue("description")
	if msg, ok := a.validateProfile(lang, updated); !ok {
		a.respondProfilePanelEditing(r, w, http.StatusBadRequest, id, msg)
		return
	}

	if _, err := a.store.Profiles().Update(r.Context(), updated); err != nil {
		switch {
		case errors.Is(err, store.ErrConflict):
			a.respondProfilePanelEditing(r, w, http.StatusConflict, id, translate(lang, "error.profile_conflict"))
		case errors.Is(err, store.ErrNotFound):
			a.respondProfilePanel(r, w, http.StatusNotFound, translate(lang, "error.profile_missing"))
		default:
			a.respondProfilePanelEditing(r, w, http.StatusInternalServerError, id, formatted(lang, "error.profile_update", err.Error()))
		}
		return
	}
	a.respondProfilePanel(r, w, http.StatusOK, "")
}

// handleProfilesDelete removes one profile.
//
// Deleting cascades to alias_profiles and device_profiles, so every alias
// aimed at this profile stops reaching the devices that were in it, and
// every device loses its membership. Nothing here can soften that — the
// cascade is the schema's — so the confirmation the row asks for says what
// will happen rather than asking a bare "are you sure?".
func (a *webapp) handleProfilesDelete(w http.ResponseWriter, r *http.Request) {
	lang := requestLanguage(r)
	if err := a.store.Profiles().Delete(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.respondProfilePanel(r, w, http.StatusNotFound, translate(lang, "error.profile_missing"))
			return
		}
		a.respondProfilePanel(r, w, http.StatusInternalServerError, translate(lang, "error.profile_delete"))
		return
	}
	a.respondProfilePanel(r, w, http.StatusOK, "")
}

// validateProfile applies the only two constraints a profile has: a name is
// required, and both fields are single-line text rendered back into HTML.
// validate.Description already rejects the control characters that class of
// field must not carry, so it is reused rather than re-implemented.
func (a *webapp) validateProfile(lang language, p domain.Profile) (string, bool) {
	if p.Name == "" {
		return translate(lang, "error.profile_name_required"), false
	}
	if err := validate.Description(p.Name); err != nil {
		return localizeValidationError(lang, err), false
	}
	if err := validate.Description(p.Description); err != nil {
		return localizeValidationError(lang, err), false
	}
	return "", true
}

func (a *webapp) respondProfilePanel(r *http.Request, w http.ResponseWriter, status int, formError string) {
	a.respondProfilePanelEditing(r, w, status, "", formError)
}

func (a *webapp) respondProfilePanelEditing(r *http.Request, w http.ResponseWriter, status int, editingID, formError string) {
	list, err := a.store.Profiles().List(r.Context())
	if err != nil {
		http.Error(w, translate(requestLanguage(r), "error.profile_load"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = a.tmpl.profilePanel.ExecuteTemplate(w, "profile_panel", profilesPageData{
		pageData: pageDataFor(r), Profiles: list, EditingID: editingID, FormError: formError,
	})
}
