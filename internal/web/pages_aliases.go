package web

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
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
	Title  string
	Active string
	// Rows carries each alias together with the targeting choices the row
	// needs to render: every group, platform and shell that exists, each
	// flagged with whether this alias selects it.
	Rows []aliasRow
	// NewTargeting is the same option set with nothing selected, for the
	// create form above the table.
	NewTargeting aliasTargeting
	// EditingID names the one alias the panel renders as an inline edit
	// form instead of a read-only row. Empty renders every row read-only,
	// which is also the shape every non-edit response produces.
	EditingID string
	FormError string
}

// aliasRow is one alias plus its resolved targeting options.
//
// Targeting is a named field rather than an embedded one because
// aliasTargeting and domain.Alias both carry Platforms and Shells. Embedded
// at the same depth those collide, and a template selector for either one
// becomes unresolvable — an error html/template only reports at execute
// time, halfway through a response.
type aliasRow struct {
	domain.Alias
	Targeting aliasTargeting
}

// aliasTargeting is the three dimensions the web form exposes. Per-device
// targeting and tags are deliberately absent: the domain model treats
// profiles as the targeting primitive ("a device subscribes to Development
// and Homelab, not to a list of hostnames"), and an alias aimed at one
// machine by id is the exception the REST API still covers. Because they are
// absent, the update handler must carry them through untouched.
type aliasTargeting struct {
	Groups    []aliasOption
	Platforms []aliasOption
	Shells    []aliasOption
}

// aliasOption is one checkbox: the value posted back, the label shown, and
// whether it is currently selected.
type aliasOption struct {
	Value    string
	Label    string
	Selected bool
}

// aliasRowsFor resolves every alias into its row. An empty selection means
// "every one of them" throughout the domain model (Alias.TargetsPlatform and
// friends), so nothing is pre-checked for an alias that targets everything —
// checking all three boxes would claim the same thing while looking like a
// deliberate narrowing.
func (a *webapp) aliasRowsFor(r *http.Request) ([]aliasRow, aliasTargeting, error) {
	list, err := a.store.Aliases().List(r.Context())
	if err != nil {
		return nil, aliasTargeting{}, err
	}
	groups, err := a.store.Profiles().List(r.Context())
	if err != nil {
		return nil, aliasTargeting{}, err
	}

	blank := aliasTargeting{
		Groups:    make([]aliasOption, 0, len(groups)),
		Platforms: make([]aliasOption, 0, len(domain.AllPlatforms)),
		Shells:    make([]aliasOption, 0, len(domain.AllShells)),
	}
	for _, g := range groups {
		blank.Groups = append(blank.Groups, aliasOption{Value: g.ID, Label: g.Name})
	}
	for _, pl := range domain.AllPlatforms {
		blank.Platforms = append(blank.Platforms, aliasOption{Value: pl.String(), Label: pl.String()})
	}
	for _, sh := range domain.AllShells {
		blank.Shells = append(blank.Shells, aliasOption{Value: sh.String(), Label: sh.String()})
	}

	rows := make([]aliasRow, 0, len(list))
	for _, al := range list {
		selectedGroups := make(map[string]bool, len(al.ProfileIDs))
		for _, id := range al.ProfileIDs {
			selectedGroups[id] = true
		}
		selectedPlatforms := make(map[string]bool, len(al.Platforms))
		for _, pl := range al.Platforms {
			selectedPlatforms[pl.String()] = true
		}
		selectedShells := make(map[string]bool, len(al.Shells))
		for _, sh := range al.Shells {
			selectedShells[sh.String()] = true
		}

		row := aliasRow{Alias: al, Targeting: aliasTargeting{
			Groups:    withSelection(blank.Groups, selectedGroups),
			Platforms: withSelection(blank.Platforms, selectedPlatforms),
			Shells:    withSelection(blank.Shells, selectedShells),
		}}
		rows = append(rows, row)
	}
	return rows, blank, nil
}

// withSelection copies options, flagging the ones present in selected. It
// copies rather than mutating so every row gets its own selection state.
func withSelection(options []aliasOption, selected map[string]bool) []aliasOption {
	out := make([]aliasOption, 0, len(options))
	for _, o := range options {
		o.Selected = selected[o.Value]
		out = append(out, o)
	}
	return out
}

// targetingFromForm reads the three dimensions the form posts back. An
// unchecked group of boxes sends no key at all, which the domain model
// already means as "every one" — so an alias narrowed to nothing is an alias
// that reaches everything, exactly as one created without targeting does.
func targetingFromForm(r *http.Request) ([]string, []domain.Platform, []domain.Shell, error) {
	platforms := make([]domain.Platform, 0, len(r.Form["platforms"]))
	for _, raw := range r.Form["platforms"] {
		pl := domain.Platform(raw)
		if !pl.Valid() {
			return nil, nil, nil, fmt.Errorf("unknown platform %q", raw)
		}
		platforms = append(platforms, pl)
	}
	shells := make([]domain.Shell, 0, len(r.Form["shells"]))
	for _, raw := range r.Form["shells"] {
		sh := domain.Shell(raw)
		if !sh.Valid() {
			return nil, nil, nil, fmt.Errorf("unknown shell %q", raw)
		}
		shells = append(shells, sh)
	}
	return r.Form["groups"], platforms, shells, nil
}

func (a *webapp) handleAliasesPage(w http.ResponseWriter, r *http.Request) {
	rows, blank, err := a.aliasRowsFor(r)
	if err != nil {
		http.Error(w, translate(requestLanguage(r), "error.alias_load"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	view := pageDataFor(r)
	_ = a.tmpl.aliases.ExecuteTemplate(w, "base", aliasesPageData{
		pageData: view, Title: translate(view.Lang, "aliases.title"), Active: "aliases",
		Rows: rows, NewTargeting: blank,
	})
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

	groups, platforms, shells, err := targetingFromForm(r)
	if err != nil {
		a.respondAliasPanel(r, w, http.StatusBadRequest, translate(requestLanguage(r), "error.alias_targeting"))
		return
	}

	al := domain.Alias{
		Name:        r.FormValue("name"),
		Command:     r.FormValue("command"),
		Description: r.FormValue("description"),
		Enabled:     true,
		ProfileIDs:  groups,
		Platforms:   platforms,
		Shells:      shells,
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

	groups, platforms, shells, err := targetingFromForm(r)
	if err != nil {
		a.respondAliasPanelEditing(r, w, http.StatusBadRequest, id, translate(lang, "error.alias_targeting"))
		return
	}

	// Start from the stored alias so Tags and DeviceIDs — the two targeting
	// dimensions this form does not show — survive. Everything the row does
	// show is replaced by what it posted, including an empty selection,
	// which the domain model reads as "every one".
	updated := existing
	updated.Name = r.FormValue("name")
	updated.Command = r.FormValue("command")
	updated.Description = r.FormValue("description")
	updated.ProfileIDs = groups
	updated.Platforms = platforms
	updated.Shells = shells

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
	rows, blank, err := a.aliasRowsFor(r)
	if err != nil {
		http.Error(w, translate(requestLanguage(r), "error.alias_load"), http.StatusInternalServerError)
		return
	}
	a.writePanel(w, r, status, a.tmpl.aliasPanel, "alias_panel", aliasesPageData{
		pageData: pageDataFor(r), Rows: rows, NewTargeting: blank, EditingID: editingID, FormError: formError,
	})
}

// writePanel renders a panel fragment into a buffer before writing a byte of
// it. html/template reports a bad selector only at execute time, so writing
// straight to the ResponseWriter turns that into a silently truncated
// fragment: status 200, half a table, no error anywhere. A collision between
// two embedded structs' field names produced exactly that during this
// screen's development. Buffering makes the failure a 500 instead.
func (a *webapp) writePanel(w http.ResponseWriter, r *http.Request, status int, tmpl *template.Template, name string, data any) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, translate(requestLanguage(r), "error.render"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// writePage is writePanel for a full page: same buffering, same reason. A
// template error halfway through a document is worse than a 500, because the
// browser renders whatever arrived and the operator reads a page that looks
// merely incomplete.
func (a *webapp) writePage(w http.ResponseWriter, r *http.Request, status int, tmpl *template.Template, name string, data any) {
	a.writePanel(w, r, status, tmpl, name, data)
}
