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
	Title     string
	Active    string
	Aliases   []domain.Alias
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
	list, err := a.store.Aliases().List(r.Context())
	if err != nil {
		http.Error(w, translate(requestLanguage(r), "error.alias_load"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = a.tmpl.aliasPanel.ExecuteTemplate(w, "alias_panel", aliasesPageData{pageData: pageDataFor(r), Aliases: list, FormError: formError})
}
