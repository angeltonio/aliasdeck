package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/angeltonio/aliasdeck/internal/validate"
)

// aliasesPattern and aliasPattern are the two path shapes every alias route
// registers under: the collection (list/create) and one alias by id
// (get/update/delete). {id} is a net/http ServeMux wildcard (Go 1.22+),
// exposed to a handler via r.PathValue("id").
const (
	aliasesPattern = "/api/v1/aliases"
	aliasPattern   = "/api/v1/aliases/{id}"
)

// serverValidationShells is the shell set validate.Name checks a write
// against (design decision 16). Importing renderers.Supported() here would
// be exactly the boundary violation decision 2 forbids — no server package
// may depend on internal/renderers — so this list is a deliberate,
// documented duplicate of what the client actually renders, not a shortcut.
var serverValidationShells = []domain.Shell{domain.ShellZsh, domain.ShellBash, domain.ShellPowerShell}

// aliasResponse wraps a persisted alias with any non-blocking name
// warnings. It is what handleAliasesCreate and handleAliasesUpdate return;
// handleAliasesGet/List return a bare domain.Alias (or slice), since a
// warning is only meaningful at the moment a name is chosen.
type aliasResponse struct {
	domain.Alias
	// NameWarnings lists, per shell in serverValidationShells, why
	// validate.Name objected to this alias's name — omitted entirely when
	// empty. These are informational only: design decision 16 is explicit
	// that a name warning must never block a write, because this server
	// does not know which shells any given device actually runs, and a
	// name illegal on PowerShell but fine on zsh/bash must still be usable
	// by the zsh/bash devices that will actually resolve it.
	NameWarnings []string `json:"nameWarnings,omitempty"`
}

// nameWarnings runs validate.Name for name against every shell in
// serverValidationShells and collects each failure as an informational
// string. It never returns an error and never blocks a write — see
// validateAliasWrite's doc comment for why Name is deliberately absent
// there.
func nameWarnings(name string) []string {
	var warnings []string
	for _, sh := range serverValidationShells {
		if err := validate.Name(name, sh); err != nil {
			warnings = append(warnings, sh.String()+": "+err.Error())
		}
	}
	return warnings
}

// validateAliasWrite enforces design decision 16 for a create or update
// body: validate.Command and validate.Description failures are blocking
// (400 — the same class of problem for every shell, not a per-shell
// concern), but validate.Name is deliberately never called here. Blocking
// on a name warning would refuse a name that is perfectly legal for every
// device that will ever resolve it; the client-side renderer guard is this
// project's last line of defense for names (§12.1), not this endpoint.
func validateAliasWrite(w http.ResponseWriter, a domain.Alias) bool {
	if err := validate.Command(a.Command); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidCommand, err.Error(), nil)
		return false
	}
	if err := validate.Description(a.Description); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidDescription, err.Error(), nil)
		return false
	}
	return true
}

// handleAliasesList returns every alias, targeting intact — the same full
// set AliasRepo.List documents, with no device-side filtering (design
// decision 4 keeps resolution in internal/sync, never here).
func (a *api) handleAliasesList(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.Aliases().List(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleAliasesCreate is the server-api spec's "Authenticated CRUD
// succeeds" scenario's write half: a valid session plus a body passing
// validateAliasWrite persists the alias and returns it (plus any name
// warnings) so a subsequent list reflects it immediately.
func (a *api) handleAliasesCreate(w http.ResponseWriter, r *http.Request) {
	var in domain.Alias
	if !decodeJSON(w, r, &in) {
		return
	}
	if !validateAliasWrite(w, in) {
		return
	}
	warnings := nameWarnings(in.Name)

	out, err := store.CreateAliasWithinLimit(r.Context(), a.store.Aliases(), in, validate.MaxAliases)
	if err != nil {
		if errors.Is(err, store.ErrCapacity) {
			writeError(w, http.StatusBadRequest, codeTooManyAliases,
				fmt.Sprintf("this server already holds %d aliases, the maximum this control plane accepts", validate.MaxAliases), nil)
			return
		}
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, aliasResponse{Alias: out, NameWarnings: warnings})
}

func (a *api) handleAliasesGet(w http.ResponseWriter, r *http.Request) {
	out, err := a.store.Aliases().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) handleAliasesUpdate(w http.ResponseWriter, r *http.Request) {
	var in domain.Alias
	if !decodeJSON(w, r, &in) {
		return
	}
	in.ID = r.PathValue("id")
	if !validateAliasWrite(w, in) {
		return
	}
	warnings := nameWarnings(in.Name)

	out, err := a.store.Aliases().Update(r.Context(), in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, aliasResponse{Alias: out, NameWarnings: warnings})
}

func (a *api) handleAliasesDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.Aliases().Delete(r.Context(), r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
