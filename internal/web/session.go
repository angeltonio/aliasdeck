package web

import (
	"context"
	"net/http"
	"time"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/store"
)

// sessionCookieName is the browser credential. It is HttpOnly and paired
// with the per-session CSRF token derived in csrf.go.
const sessionCookieName = "aliasdeck_session"

// sessionLifetime mirrors internal/api's own fixed 24h session lifetime
// (design decision 8) exactly: the web UI mints the same
// store.TokenKindSession row the JSON API's own login mints, just handed
// back as a cookie instead of a JSON body.
const sessionLifetime = 24 * time.Hour

// subjectContextKey is unexported so no other package can collide with
// this context key by accident (mirrors internal/auth's own pattern).
type subjectContextKey struct{}

// webSubject is the authenticated operator a valid session cookie
// resolves to.
type webSubject struct {
	TokenID    string
	OperatorID string
	CSRFToken  string
}

func withSubject(ctx context.Context, subj webSubject) context.Context {
	return context.WithValue(ctx, subjectContextKey{}, subj)
}

func subjectFromContext(ctx context.Context) (webSubject, bool) {
	subj, ok := ctx.Value(subjectContextKey{}).(webSubject)
	return subj, ok
}

// isSecureRequest uses the explicit public origin behind TLS termination.
// Without one it trusts only direct TLS, never forwarding headers.
func (a *webapp) isSecureRequest(r *http.Request) bool {
	return (a.publicURL != nil && a.publicURL.Scheme == "https") || (a.publicURL == nil && r.TLS != nil)
}

// setSessionCookie hands wire back to the browser as the session cookie,
// expiring it exactly when the underlying store.Token does.
func (a *webapp) setSessionCookie(w http.ResponseWriter, r *http.Request, wire string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    wire,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   a.isSecureRequest(r),
		Expires:  expiresAt,
	})
}

// clearSessionCookie removes the browser's copy of the cookie. It does not,
// by itself, revoke the underlying store.Token — callers that want the
// token dead server-side (handleLogout) must revoke it explicitly first.
func (a *webapp) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   a.isSecureRequest(r),
		MaxAge:   -1,
	})
}

// authenticate resolves r's session cookie to a webSubject, performing the
// exact same checks internal/auth.RequireKind performs for a bearer
// token — parse, look up, verify the secret in constant time, verify kind
// and expiry/revocation — just reading the credential from a cookie
// instead of an Authorization header. Every failure mode (missing
// cookie, malformed token, unknown lookup, wrong secret, wrong kind,
// expired, revoked) is refused identically: the caller only ever learns
// "not authenticated", never which check failed.
func authenticate(r *http.Request, tokens store.TokenRepo, now func() time.Time) (webSubject, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return webSubject{}, false
	}

	parsed, err := auth.Parse(c.Value)
	if err != nil || parsed.Kind != store.TokenKindSession {
		return webSubject{}, false
	}

	tok, err := tokens.ByLookup(r.Context(), parsed.Lookup)
	if err != nil {
		return webSubject{}, false
	}

	if !auth.VerifySecret(parsed.Secret, tok.SecretHash) {
		return webSubject{}, false
	}

	if err := auth.Verify(tok, store.TokenKindSession, now()); err != nil {
		return webSubject{}, false
	}

	return webSubject{TokenID: tok.ID, OperatorID: tok.SubjectID, CSRFToken: sessionCSRF(parsed.Secret, parsed.Lookup)}, true
}
