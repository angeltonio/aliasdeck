package web

import (
	"errors"
	"net/http"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/angeltonio/aliasdeck/internal/validate"
)

// aliasesPageData is both aliases.html's (the full page) and
// alias_panel.html's (the htmx-swapped fragment) data shape, so a create
// response can re-render exactly the fragment the initial page load
// produced.
type aliasesPageData struct {
	pageData
	Title   string
	Active  string
	Aliases []domain.Alias
	// EditingID names the one alias the panel renders as an inline edit
	// form instead of a read-only row. Empty renders every row read-only,
	// which is also the shape every non-edit response produces.
	EditingID string
	FormError string
}

func (a *webapp) handleAliasesPage(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.Aliases().List(r.Context())
	if err != nil {
		http.Error(w, translate(requestLanguage(r), "error.alias_load"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	view := pageDataFor(r)
	_ = a.tmpl.aliases.ExecuteTemplate(w, "base", aliasesPageData{pageData: view, Title: translate(view.Lang, "aliases.title"), Active: "aliases", Aliases: list})
}

// handleAliasesCreate validates and persists a new alias from the create
// form, then responds with the freshly re-rendered alias_panel fragment —
// the htmx swap target the create form's hx-target points at. This is
// the "without a full page reload" interaction. Capacity and name conflict
// outcomes match the API, including the SQLite store's atomic bounded insert.
func (a *webapp) handleAliasesCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.respondAliasPanel(r, w, http.StatusBadRequest, translate(requestLanguage(r), "error.alias_form"))
		return
	}

	al := domain.Alias{
		Name:        r.FormValue("name"),
		Command:     r.FormValue("command"),
		Description: r.FormValue("description"),
		Enabled:     true,
	}

	if err := validate.Command(al.Command); err != nil {
		a.respondAliasPanel(r, w, http.StatusBadRequest, localizeValidationError(requestLanguage(r), err))
		return
	}
	if err := validate.Description(al.Description); err != nil {
		a.respondAliasPanel(r, w, http.StatusBadRequest, localizeValidationError(requestLanguage(r), err))
		return
	}

	if _, err := store.CreateAliasWithinLimit(r.Context(), a.store.Aliases(), al, validate.MaxAliases); err != nil {
		if errors.Is(err, store.ErrCapacity) {
			a.respondAliasPanel(r, w, http.StatusBadRequest, translate(requestLanguage(r), "error.alias_capacity"))
			return
		}
		if errors.Is(err, store.ErrConflict) {
			a.respondAliasPanel(r, w, http.StatusConflict, translate(requestLanguage(r), "error.alias_conflict"))
			return
		}
		a.respondAliasPanel(r, w, http.StatusConflict, formatted(requestLanguage(r), "error.alias_create", err.Error()))
		return
	}

	a.respondAliasPanel(r, w, http.StatusOK, "")
}

// handleAliasesEdit re-renders the panel with one row swapped for an inline
// edit form. It is a GET that changes nothing: the row it opens is decided
// by the response, never by server-side state, so two operators editing at
// once cannot see each other's open form.
func (a *webapp) handleAliasesEdit(w http.ResponseWriter, r *http.Request) {
	a.respondAliasPanelEditing(r, w, http.StatusOK, r.PathValue("id"), "")
}

// handleAliasesPanel re-renders the panel with no row in edit mode. It is
// what Cancel asks for, and it reloads from the store rather than trusting
// the browser to still hold the pre-edit values.
func (a *webapp) handleAliasesPanel(w http.ResponseWriter, r *http.Request) {
	a.respondAliasPanel(r, w, http.StatusOK, "")
}

// handleAliasesUpdate applies an edit to one alias.
//
// It loads the stored alias first and overwrites only the three fields this
// form actually shows. That is not a shortcut — it is the whole point.
// store.AliasRepo.Update replaces targeting wholesale (setAliasProfiles and
// setAliasDevices clear the join rows before reinserting), so sending an
// alias built from the form alone would silently drop the platforms, shells,
// tags, profiles and devices it was targeted at. That is the same data loss
// operators already hit by deleting and recreating an alias to change it,
// which is exactly what this handler exists to end; reproducing it here
// would make the fix cosmetic.
func (a *webapp) handleAliasesUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	lang := requestLanguage(r)

	if err := r.ParseForm(); err != nil {
		a.respondAliasPanelEditing(r, w, http.StatusBadRequest, id, translate(lang, "error.alias_form"))
		return
	}

	existing, err := a.store.Aliases().Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.respondAliasPanel(r, w, http.StatusNotFound, translate(lang, "error.alias_missing"))
			return
		}
		http.Error(w, translate(lang, "error.alias_load"), http.StatusInternalServerError)
		return
	}

	updated := existing
	updated.Name = r.FormValue("name")
	updated.Command = r.FormValue("command")
	updated.Description = r.FormValue("description")

	if err := validate.Command(updated.Command); err != nil {
		a.respondAliasPanelEditing(r, w, http.StatusBadRequest, id, localizeValidationError(lang, err))
		return
	}
	if err := validate.Description(updated.Description); err != nil {
		a.respondAliasPanelEditing(r, w, http.StatusBadRequest, id, localizeValidationError(lang, err))
		return
	}

	if _, err := a.store.Aliases().Update(r.Context(), updated); err != nil {
		switch {
		case errors.Is(err, store.ErrConflict):
			a.respondAliasPanelEditing(r, w, http.StatusConflict, id, translate(lang, "error.alias_conflict"))
		case errors.Is(err, store.ErrNotFound):
			a.respondAliasPanel(r, w, http.StatusNotFound, translate(lang, "error.alias_missing"))
		default:
			a.respondAliasPanelEditing(r, w, http.StatusInternalServerError, id, formatted(lang, "error.alias_update", err.Error()))
		}
		return
	}

	a.respondAliasPanel(r, w, http.StatusOK, "")
}

// handleAliasesDelete removes one alias and, on success, responds with an
// empty body: the delete button's hx-target="closest tr"/hx-swap="outerHTML"
// replaces the row with nothing, which is what makes it visually
// disappear without a page reload.
func (a *webapp) handleAliasesDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.store.Aliases().Delete(r.Context(), id); err != nil {
		http.Error(w, translate(requestLanguage(r), "error.alias_delete"), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// respondAliasPanel re-lists every alias and renders alias_panel.html
// with status and formError, the one place every alias-mutating handler
// in this file funnels its response through so the fragment stays
// consistent regardless of which action produced it.
func (a *webapp) respondAliasPanel(r *http.Request, w http.ResponseWriter, status int, formError string) {
	a.respondAliasPanelEditing(r, w, status, "", formError)
}

// respondAliasPanelEditing is respondAliasPanel with one row left open for
// editing. A failed edit comes back through here rather than through the
// read-only panel so the operator keeps the form they were filling in,
// instead of losing their input to a validation message.
func (a *webapp) respondAliasPanelEditing(r *http.Request, w http.ResponseWriter, status int, editingID, formError string) {
	list, err := a.store.Aliases().List(r.Context())
	if err != nil {
		http.Error(w, translate(requestLanguage(r), "error.alias_load"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = a.tmpl.aliasPanel.ExecuteTemplate(w, "alias_panel", aliasesPageData{
		pageData: pageDataFor(r), Aliases: list, EditingID: editingID, FormError: formError,
	})
}
