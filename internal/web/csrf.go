package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
)

const (
	csrfFormField = "_csrf"
	csrfHeader    = "X-CSRF-Token"
	csrfPurpose   = "aliasdeck-web-csrf-v1"
)

// sessionCSRF derives a browser-form token from the random session secret.
// The one-way HMAC is safe to render; the session secret itself never leaves
// its HttpOnly cookie and naturally rotates at every login.
func sessionCSRF(secret, lookup string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(csrfPurpose))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(lookup))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func csrfFromRequest(r *http.Request) string {
	if token := r.Header.Get(csrfHeader); token != "" {
		return token
	}
	if err := r.ParseForm(); err != nil {
		return ""
	}
	return r.FormValue(csrfFormField)
}

func validCSRF(expected, presented string) bool {
	return expected != "" && presented != "" && hmac.Equal([]byte(expected), []byte(presented))
}
