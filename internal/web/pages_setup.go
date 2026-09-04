package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/angeltonio/aliasdeck/internal/auth"
)

type setupPageData struct {
	pageData
	Error, Credential, Username, LocalToken string
}

const (
	localSetupCookieName = "aliasdeck_local_setup"
	localSetupTokenBytes = 32
	localSetupTokenTTL   = 10 * time.Minute
	maxLocalSetupTokens  = 64
)

type setupTokenTracker struct {
	mu     sync.Mutex
	tokens map[[sha256.Size]byte]time.Time
}

func newSetupTokenTracker() *setupTokenTracker {
	return &setupTokenTracker{tokens: make(map[[sha256.Size]byte]time.Time)}
}

func (a *webapp) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	credential := r.URL.Query().Get("credential")
	local := a.isCredentiallessSetupRequest(r)
	if (!local && credential == "") || !auth.SetupEnabled(a.setupCredentialPath) {
		http.NotFound(w, r)
		return
	}
	data := setupPageData{Credential: credential}
	if local {
		var err error
		data.LocalToken, err = a.issueLocalSetupToken(w, r)
		if err != nil {
			http.Error(w, translate(requestLanguage(r), "error.setup_start"), http.StatusInternalServerError)
			return
		}
	}
	a.renderSetup(w, r, http.StatusOK, data)
}

func (a *webapp) renderSetup(w http.ResponseWriter, r *http.Request, status int, data setupPageData) {
	data.pageData = pageDataFor(r)
	a.writePage(w, r, status, a.tmpl.setup, "setup.html", data)
}

func (a *webapp) handleSetupSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderSetup(w, r, http.StatusBadRequest, setupPageData{Error: translate(requestLanguage(r), "error.setup_form")})
		return
	}
	data := setupPageData{Credential: r.FormValue("credential"), Username: r.FormValue("username"), LocalToken: r.FormValue("local_token")}
	localCredentialless := a.isCredentiallessSetupRequest(r) && data.Credential == ""
	var err error
	if localCredentialless {
		if !a.consumeLocalSetupToken(r, data.LocalToken) {
			a.renderSetup(w, r, http.StatusNotFound, setupPageData{Error: translate(requestLanguage(r), "error.setup_link")})
			return
		}
		err = auth.CompleteLocalSetup(r.Context(), a.store, a.setupCredentialPath, data.Username, r.FormValue("password"), r.FormValue("confirmation"))
	} else {
		err = auth.CompleteSetup(r.Context(), a.store, a.setupCredentialPath, data.Credential, data.Username, r.FormValue("password"), r.FormValue("confirmation"))
	}
	if err == nil {
		if localCredentialless {
			clearLocalSetupCookie(w)
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	status := http.StatusBadRequest
	if errors.Is(err, auth.ErrSetupDisabled) || errors.Is(err, auth.ErrInvalidSetupCredential) {
		status = http.StatusNotFound
	}
	switch {
	case errors.Is(err, auth.ErrMismatchedSetupPassword):
		data.Error = translate(requestLanguage(r), "error.password_mismatch")
	case errors.Is(err, auth.ErrWeakSetupPassword):
		data.Error = translate(requestLanguage(r), "error.password_weak")
	case status == http.StatusNotFound:
		data.Error = translate(requestLanguage(r), "error.setup_link")
	default:
		data.Error = translate(requestLanguage(r), "error.operator_create")
	}
	if status != http.StatusNotFound && localCredentialless && auth.SetupEnabled(a.setupCredentialPath) {
		data.LocalToken, err = a.issueLocalSetupToken(w, r)
		if err != nil {
			http.Error(w, translate(requestLanguage(r), "error.setup_continue"), http.StatusInternalServerError)
			return
		}
	}
	a.renderSetup(w, r, status, data)
}

func (a *webapp) issueLocalSetupToken(w http.ResponseWriter, r *http.Request) (string, error) {
	if a.setupTokens == nil {
		return "", fmt.Errorf("web: local setup token tracker is unavailable")
	}
	token, err := a.setupTokens.create(a.now())
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     localSetupCookieName,
		Value:    token,
		Path:     "/setup",
		MaxAge:   int(localSetupTokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
	return token, nil
}

func (a *webapp) consumeLocalSetupToken(r *http.Request, formToken string) bool {
	if a.setupTokens == nil || formToken == "" {
		return false
	}
	cookie, err := r.Cookie(localSetupCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	formHash := sha256.Sum256([]byte(formToken))
	cookieHash := sha256.Sum256([]byte(cookie.Value))
	if subtle.ConstantTimeCompare(formHash[:], cookieHash[:]) != 1 {
		return false
	}
	return a.setupTokens.consume(formToken, a.now())
}

func (t *setupTokenTracker) create(now time.Time) (string, error) {
	raw := make([]byte, localSetupTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("web: generating local setup token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))

	t.mu.Lock()
	defer t.mu.Unlock()
	t.removeExpired(now)
	if len(t.tokens) >= maxLocalSetupTokens {
		var oldestHash [sha256.Size]byte
		var oldestExpiry time.Time
		for candidate, expiry := range t.tokens {
			if oldestExpiry.IsZero() || expiry.Before(oldestExpiry) {
				oldestHash, oldestExpiry = candidate, expiry
			}
		}
		delete(t.tokens, oldestHash)
	}
	t.tokens[hash] = now.Add(localSetupTokenTTL)
	return token, nil
}

func (t *setupTokenTracker) consume(token string, now time.Time) bool {
	hash := sha256.Sum256([]byte(token))
	t.mu.Lock()
	defer t.mu.Unlock()
	t.removeExpired(now)
	if _, ok := t.tokens[hash]; !ok {
		return false
	}
	delete(t.tokens, hash)
	return true
}

func (t *setupTokenTracker) removeExpired(now time.Time) {
	for hash, expiry := range t.tokens {
		if !expiry.After(now) {
			delete(t.tokens, hash)
		}
	}
}

func clearLocalSetupCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     localSetupCookieName,
		Path:     "/setup",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// isCredentiallessSetupRequest grants the credentialless setup path to a
// direct loopback peer, or when the operator has explicitly asserted that the
// server's external network boundary is local-only. Proxy metadata always
// makes the request ineligible: application-level headers are never proof of
// locality, and a reverse proxy must use the one-time setup credential.
func (a *webapp) isCredentiallessSetupRequest(r *http.Request) bool {
	for name := range r.Header {
		lower := strings.ToLower(name)
		if lower == "forwarded" || lower == "via" || lower == "x-real-ip" || strings.HasPrefix(lower, "x-forwarded-") {
			return false
		}
	}
	if a.trustLocalSetup {
		return true
	}
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	return err == nil && peer.Addr().IsLoopback()
}
