package auth

import "errors"

// ErrMalformedToken is returned by Parse for any input that is not exactly
// a well-formed ad<k>_<lookup>.<secret> wire token. It intentionally does
// not distinguish which part was wrong (threat matrix: token handling).
var ErrMalformedToken = errors.New("auth: malformed token")

// ErrWrongSecret is returned when a token's lookup resolves to a real
// stored token but the presented secret does not match it.
var ErrWrongSecret = errors.New("auth: wrong secret")

// ErrWrongTokenKind is returned when a token authenticates successfully
// but is not the kind a route requires — a device token presented on an
// operator-only route, for example (threat matrix: HTTP routing).
var ErrWrongTokenKind = errors.New("auth: wrong token kind for this route")

// ErrExpiredToken is returned by Verify when now is at or past the
// token's ExpiresAt.
var ErrExpiredToken = errors.New("auth: token expired")

// ErrRevokedToken is returned by Verify when the token has a non-zero
// RevokedAt.
var ErrRevokedToken = errors.New("auth: token revoked")

// ErrPasswordMismatch is returned by VerifyPassword when the password does
// not match the stored hash.
var ErrPasswordMismatch = errors.New("auth: password mismatch")

// ErrMalformedPasswordHash is returned by VerifyPassword when the stored
// encoded hash is not in the format HashPassword produces.
var ErrMalformedPasswordHash = errors.New("auth: malformed password hash")

// ErrWeakAdminPassword is returned by Bootstrap when ALIASDECK_ADMIN_PASSWORD
// is set but is empty, all whitespace, or shorter than
// minAdminPasswordLength.
var ErrWeakAdminPassword = errors.New("auth: admin password too weak")

// ErrUnauthorized is returned by RequireKind's caller-visible surface (via
// http.StatusUnauthorized) whenever authentication fails for any reason —
// missing header, malformed token, unknown lookup, wrong secret, wrong
// kind, expired, or revoked. It exists as a single sentinel so tests and
// future callers can assert on "authentication failed" without caring
// which of the above caused it.
var ErrUnauthorized = errors.New("auth: unauthorized")
