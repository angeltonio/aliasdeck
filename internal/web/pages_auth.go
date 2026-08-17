package web

import (
	"net/http"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/store"
)

// loginPageData is login.html's own data shape.
type loginPageData struct {
	pageData
	Error          string
	Username       string
	SetupAvailable bool
}

// handleLoginPage serves the login form. An already-authenticated browser
// is sent straight to the alias list instead of being shown the form
// again.
func (a *webapp) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := authenticate(r, a.store.Tokens(), a.now); ok {
		http.Redirect(w, r, "/aliases", http.StatusSeeOther)
		return
	}
	a.renderLogin(w, r, http.StatusOK, loginPageData{SetupAvailable: auth.SetupEnabled(a.setupCredentialPath)})
}

func (a *webapp) renderLogin(w http.ResponseWriter, r *http.Request, status int, data loginPageData) {
	data.pageData = pageDataFor(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = a.tmpl.login.ExecuteTemplate(w, "login.html", data)
}

// handleLoginSubmit exchanges the operator's username/password for a
// session token, exactly the credential check internal/api.handleLogin
// performs (store.Operators().ByUsername + auth.VerifyPassword), and
// hands the resulting store.TokenKindSession token back as a cookie
// instead of a JSON body. The web UI does not reuse handleLogin
// itself — it is an unexported method on a different package's type —
// but performs the identical two checks against the identical store seam,
// per the Milestone 5 proposal's own approach ("renders against the same
// internal/store seam the API uses; does not call /api/v1 over HTTP from
// inside its own process").
// Password verification is protected by the exact limiter instance shared
// with the JSON API, so the two transports cannot multiply Argon2 work.
func (a *webapp) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderLogin(w, r, http.StatusBadRequest, loginPageData{Error: translate(requestLanguage(r), "error.login_form")})
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	op, err := a.store.Operators().ByUsername(r.Context(), username)
	if err != nil {
		a.renderLogin(w, r, http.StatusUnauthorized, loginPageData{Error: translate(requestLanguage(r), "error.login_invalid"), Username: username})
		return
	}

	if !a.loginLimiter.TryAcquire() {
		a.renderLogin(w, r, http.StatusServiceUnavailable, loginPageData{Error: translate(requestLanguage(r), "error.login_busy"), Username: username})
		return
	}
	ok, verr := auth.VerifyPassword(password, string(op.PasswordHash))
	a.loginLimiter.Release()
	if verr != nil || !ok {
		a.renderLogin(w, r, http.StatusUnauthorized, loginPageData{Error: translate(requestLanguage(r), "error.login_invalid"), Username: username})
		return
	}

	minted, err := auth.Mint(store.TokenKindSession)
	if err != nil {
		a.renderLogin(w, r, http.StatusInternalServerError, loginPageData{Error: translate(requestLanguage(r), "error.session"), Username: username})
		return
	}

	now := a.now()
	expiresAt := now.Add(sessionLifetime)
	if err := a.store.Tokens().Create(r.Context(), store.Token{
		Kind:       store.TokenKindSession,
		SubjectID:  op.ID,
		Lookup:     minted.Lookup,
		SecretHash: minted.SecretHash,
		CreatedAt:  now,
		ExpiresAt:  expiresAt,
	}); err != nil {
		a.renderLogin(w, r, http.StatusInternalServerError, loginPageData{Error: translate(requestLanguage(r), "error.session"), Username: username})
		return
	}

	a.setSessionCookie(w, r, minted.Wire, expiresAt)
	http.Redirect(w, r, "/aliases", http.StatusSeeOther)
}

// handleLogout revokes the session's token server-side (so it cannot be
// reused even if the cookie were somehow replayed) and clears the
// browser's copy, mirroring internal/api.handleLogout's own
// "revoke, then respond" ordering.
func (a *webapp) handleLogout(w http.ResponseWriter, r *http.Request) {
	subj, ok := subjectFromContext(r.Context())
	if ok {
		_ = a.store.Tokens().Revoke(r.Context(), subj.TokenID, a.now())
	}
	a.clearSessionCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
