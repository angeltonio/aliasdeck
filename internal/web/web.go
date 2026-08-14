package web

import (
	"fmt"
	"net/http"
	"time"

	"github.com/angeltonio/aliasdeck/internal/store"
)

// guard names how a page route is protected — the prototype's own, much
// smaller echo of internal/api/router.go's Public/RequiredKind pair.
// There is only one non-public shape here (a valid session cookie), but
// the discipline is the same one design decision 15 requires of the JSON
// API: a route declaring neither is a bug caught at registration, not a
// silently unguarded page.
type guard int

const (
	guardUndeclared guard = iota
	guardPublic
	guardSession
)

// page declares one UI route: its method, pattern, handler, and exactly
// one guard. This table is never merged into internal/api's own
// (*api).routes() — keeping it a fully separate handler, mounted
// alongside the API mux by internal/server (never inside
// internal/api/openapi_coverage_test.go's comparison), is how these pages
// stay out of the bidirectional docs/openapi.yaml coverage check without
// weakening it: that test only ever sees (*api).routes(), and this
// package adds nothing to it.
type page struct {
	Method  string
	Pattern string
	Handler http.HandlerFunc
	Guard   guard
}

// webapp is the composed set of dependencies every handler in this
// package closes over — mirrors internal/api's own unexported api type.
type webapp struct {
	store store.Store
	now   func() time.Time
	tmpl  *pageTemplates
}

// NewHandler builds the complete prototype UI handler: every page in
// (*webapp).pages() guarded per its declared guard, static assets served
// from the embedded tree. It returns a non-nil error if any page
// declares guardUndeclared, exactly like internal/api.NewRouter refuses
// an unguarded API route.
func NewHandler(st store.Store, now func() time.Time) (http.Handler, error) {
	tmpl, err := loadTemplates()
	if err != nil {
		return nil, fmt.Errorf("web: loading templates: %w", err)
	}
	a := &webapp{store: st, now: now, tmpl: tmpl}
	return newMux(a)
}

func newMux(a *webapp) (http.Handler, error) {
	mux := http.NewServeMux()

	for _, p := range a.pages() {
		if p.Guard == guardUndeclared {
			return nil, fmt.Errorf("web: page %s %s declares no guard: every UI route must be Public or require a session", p.Method, p.Pattern)
		}

		handler := p.Handler
		if p.Guard == guardSession {
			handler = a.requireSession(handler)
		}
		mux.Handle(p.Method+" "+p.Pattern, handler)
	}

	// Static assets (vendored htmx + the hand-written stylesheet) are
	// public by construction: there is nothing operator-specific in
	// either file, and gating them behind a session would just break the
	// login page's own stylesheet.
	mux.Handle("GET /static/", http.FileServerFS(staticFS))

	return mux, nil
}

// requireSession is this package's session-cookie counterpart to
// internal/auth.RequireKind: it resolves the request's cookie via
// authenticate and, on any failure, redirects to /login rather than
// writing a JSON 401 — this is a browser UI, not the API, so its refusal
// is a page, not an error body.
func (a *webapp) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subj, ok := authenticate(r, a.store.Tokens(), a.now)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r.WithContext(withSubject(r.Context(), subj)))
	}
}

// pages is the complete UI route table this prototype exposes. It is
// deliberately small — the six flows the prototype brief asks for and
// nothing else: no profile screens, no device rename/revoke/rotate, no
// first-run setup.
func (a *webapp) pages() []page {
	return []page{
		{Method: http.MethodGet, Pattern: "/{$}", Handler: a.handleRoot, Guard: guardPublic},
		{Method: http.MethodGet, Pattern: "/login", Handler: a.handleLoginPage, Guard: guardPublic},
		{Method: http.MethodPost, Pattern: "/login", Handler: a.handleLoginSubmit, Guard: guardPublic},
		{Method: http.MethodPost, Pattern: "/logout", Handler: a.handleLogout, Guard: guardSession},

		{Method: http.MethodGet, Pattern: "/aliases", Handler: a.handleAliasesPage, Guard: guardSession},
		{Method: http.MethodPost, Pattern: "/aliases", Handler: a.handleAliasesCreate, Guard: guardSession},
		{Method: http.MethodDelete, Pattern: "/aliases/{id}", Handler: a.handleAliasesDelete, Guard: guardSession},

		{Method: http.MethodGet, Pattern: "/devices", Handler: a.handleDevicesPage, Guard: guardSession},
		{Method: http.MethodGet, Pattern: "/devices/add", Handler: a.handleDevicesAddPage, Guard: guardSession},
		{Method: http.MethodPost, Pattern: "/devices/add/token", Handler: a.handleDevicesMintToken, Guard: guardSession},
	}
}

// handleRoot sends a browser at "/" wherever it can actually go: the
// alias list if it already has a live session, the login page otherwise.
func (a *webapp) handleRoot(w http.ResponseWriter, r *http.Request) {
	if _, ok := authenticate(r, a.store.Tokens(), a.now); ok {
		http.Redirect(w, r, "/aliases", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
