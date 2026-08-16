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
	local := isDirectLoopbackRequest(r)
	if (!local && credential == "") || !auth.SetupEnabled(a.setupCredentialPath) {
		http.NotFound(w, r)
		return
	}
	data := setupPageData{Credential: credential}
	if local {
		var err error
		data.LocalToken, err = a.issueLocalSetupToken(w, r)
		if err != nil {
			http.Error(w, "could not start local setup", http.StatusInternalServerError)
			return
		}
	}
	a.renderSetup(w, http.StatusOK, data)
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
	data := setupPageData{Credential: r.FormValue("credential"), Username: r.FormValue("username"), LocalToken: r.FormValue("local_token")}
	localCredentialless := isDirectLoopbackRequest(r) && data.Credential == ""
	var err error
	if localCredentialless {
		if !a.consumeLocalSetupToken(r, data.LocalToken) {
			a.renderSetup(w, http.StatusNotFound, setupPageData{Error: "this setup link is invalid or has already been used"})
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
		data.Error = "passwords do not match"
	case errors.Is(err, auth.ErrWeakSetupPassword):
		data.Error = "password must be at least 12 characters"
	case status == http.StatusNotFound:
		data.Error = "this setup link is invalid or has already been used"
	default:
		data.Error = "could not create the operator account"
	}
	if status != http.StatusNotFound && localCredentialless && auth.SetupEnabled(a.setupCredentialPath) {
		data.LocalToken, err = a.issueLocalSetupToken(w, r)
		if err != nil {
			http.Error(w, "could not continue local setup", http.StatusInternalServerError)
			return
		}
	}
	a.renderSetup(w, status, data)
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

// isDirectLoopbackRequest grants the credentialless setup path only when the
// HTTP peer itself is loopback. Application-level headers are never proof of
// locality. Proxy metadata makes even a loopback peer ineligible so a local
// reverse proxy cannot accidentally turn remote traffic into local setup.
func isDirectLoopbackRequest(r *http.Request) bool {
	for name := range r.Header {
		lower := strings.ToLower(name)
		if lower == "forwarded" || lower == "via" || lower == "x-real-ip" || strings.HasPrefix(lower, "x-forwarded-") {
			return false
		}
	}
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	return err == nil && peer.Addr().IsLoopback()
}
