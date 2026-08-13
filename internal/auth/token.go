package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
)

// tokenPrefix is the fixed literal every wire token starts with, followed
// by a one-byte kind marker, an underscore, the plain-text lookup, a dot,
// and the secret (design decision 8: `ad<k>_<lookup>.<secret>`).
const tokenPrefix = "ad"

const (
	lookupByteLength = 16 // 16 random bytes, plain text, UNIQUE-indexed
	secretByteLength = 32 // 32 random bytes, sha256-hashed at rest
)

// kindBytes and byteKinds are the wire encoding of store.TokenKind: one
// byte, so the wire form stays short and a malformed kind byte is
// trivially distinguishable from a valid one without a lookup table scan.
var kindBytes = map[store.TokenKind]byte{
	store.TokenKindSession:    's',
	store.TokenKindEnrollment: 'e',
	store.TokenKindDevice:     'd',
}

var byteKinds = map[byte]store.TokenKind{
	's': store.TokenKindSession,
	'e': store.TokenKindEnrollment,
	'd': store.TokenKindDevice,
}

// MintedToken is the result of minting a new token: Wire is what the
// caller (an operator's browser, a registering device) receives exactly
// once; Lookup and SecretHash are what internal/store persists. The
// plaintext secret itself is never returned separately — it exists only
// inside Wire, and once Wire is handed to the caller this package holds no
// further copy of it.
type MintedToken struct {
	Wire       string
	Lookup     string
	SecretHash []byte
}

// Mint generates a new token of kind, drawing both the lookup and the
// secret from crypto/rand. It returns an error only if kind is not one of
// the three known kinds, or if the system CSPRNG itself fails.
func Mint(kind store.TokenKind) (MintedToken, error) {
	kindByte, ok := kindBytes[kind]
	if !ok {
		return MintedToken{}, fmt.Errorf("auth: unknown token kind %q", kind)
	}

	lookup, err := randomURLSafeString(lookupByteLength)
	if err != nil {
		return MintedToken{}, fmt.Errorf("auth: generating token lookup: %w", err)
	}
	secret, err := randomURLSafeString(secretByteLength)
	if err != nil {
		return MintedToken{}, fmt.Errorf("auth: generating token secret: %w", err)
	}

	return MintedToken{
		Wire:       fmt.Sprintf("%s%c_%s.%s", tokenPrefix, kindByte, lookup, secret),
		Lookup:     lookup,
		SecretHash: hashSecret(secret),
	}, nil
}

// randomURLSafeString returns n random bytes from crypto/rand, encoded as
// base64.RawURLEncoding — an alphabet that never contains "." or "_", so it
// cannot be confused with the wire format's own separators.
func randomURLSafeString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashSecret is the one place the secret half of a token is hashed for
// storage or comparison — sha256, per design decision 8. The 256-bit
// CSPRNG secret gains nothing from a work-factor KDF, and a KDF on every
// authenticated request would be a self-inflicted DoS lever.
func hashSecret(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// ParsedToken is a syntactically well-formed wire token, before any store
// lookup has taken place.
type ParsedToken struct {
	Kind   store.TokenKind
	Lookup string
	Secret string
}

// Parse is a total function over its input: for any string, including
// adversarial input from an HTTP Authorization header, it returns either a
// ParsedToken or ErrMalformedToken, and it never panics. It does not
// distinguish, in what it returns, which specific part of the token was
// wrong (missing prefix, unknown kind, missing separator, empty lookup,
// empty secret) — threat matrix "token handling" requires that a caller
// cannot learn anything about a bad token beyond "it did not parse".
func Parse(wire string) (ParsedToken, error) {
	rest, ok := strings.CutPrefix(wire, tokenPrefix)
	if !ok || rest == "" {
		return ParsedToken{}, ErrMalformedToken
	}

	kindByte := rest[0]
	kind, ok := byteKinds[kindByte]
	if !ok {
		return ParsedToken{}, ErrMalformedToken
	}

	rest, ok = strings.CutPrefix(rest[1:], "_")
	if !ok {
		return ParsedToken{}, ErrMalformedToken
	}

	lookup, secret, ok := strings.Cut(rest, ".")
	if !ok || lookup == "" || secret == "" {
		return ParsedToken{}, ErrMalformedToken
	}

	return ParsedToken{Kind: kind, Lookup: lookup, Secret: secret}, nil
}

// VerifySecret reports whether secret is the plaintext secret that hashes
// to hash. The comparison is constant-time (crypto/subtle), never a
// byte-wise "==" or bytes.Equal, so a mismatch's timing does not leak how
// many leading bytes of the hash matched (threat matrix: token handling).
func VerifySecret(secret string, hash []byte) bool {
	return subtle.ConstantTimeCompare(hashSecret(secret), hash) == 1
}

// Verify checks a token record already fetched from the store (by lookup)
// against the kind a route requires and the current time: it refuses a
// token of the wrong kind, a revoked token, and an expired token. now is
// always the caller's injected clock — Verify never reads the wall clock
// itself, so expiry is deterministic and testable without sleeping.
//
// Verify does not check the secret: callers authenticate a bearer token by
// calling VerifySecret separately once the store has returned the
// candidate row. Keeping the two checks apart means a lookup miss (no such
// token) and a secret mismatch (wrong token for a real lookup) are handled
// identically by a caller — both simply refuse — without Verify itself
// needing to know about ByLookup's error shape.
func Verify(tok store.Token, requiredKind store.TokenKind, now time.Time) error {
	if tok.Kind != requiredKind {
		return ErrWrongTokenKind
	}
	if !tok.RevokedAt.IsZero() {
		return ErrRevokedToken
	}
	if !tok.ExpiresAt.IsZero() && !now.Before(tok.ExpiresAt) {
		return ErrExpiredToken
	}
	return nil
}

// enrollmentTokens is the narrow slice of store.TokenRepo ConsumeEnrollment
// needs, kept as its own interface so auth's tests construct an in-memory
// fake instead of a real store.Store (testing strategy: "N/A — pure unit
// tests").
type enrollmentTokens interface {
	ByLookup(ctx context.Context, lookup string) (store.Token, error)
	ConsumeEnrollment(ctx context.Context, lookup string, dev domain.Device) (domain.Device, error)
}

// ConsumeEnrollment authenticates wire as an enrollment token — parsing
// it, looking up its lookup value, and verifying its secret in constant
// time — and only once that succeeds does it delegate to
// tokens.ConsumeEnrollment, which atomically marks the token used and
// mints dev.
//
// A caller must never call tokens.ConsumeEnrollment directly with an
// unauthenticated lookup value: unlike the secret, the lookup is plain
// text and index-visible, so skipping the secret check here would let
// anyone who merely observes or guesses a lookup register a device. This
// is the composed operation the threat matrix's "a replayed enrollment
// token is refused end-to-end" scenario exercises.
func ConsumeEnrollment(ctx context.Context, tokens enrollmentTokens, wire string, dev domain.Device) (domain.Device, error) {
	parsed, err := Parse(wire)
	if err != nil {
		return domain.Device{}, err
	}
	if parsed.Kind != store.TokenKindEnrollment {
		return domain.Device{}, ErrWrongTokenKind
	}

	tok, err := tokens.ByLookup(ctx, parsed.Lookup)
	if err != nil {
		return domain.Device{}, err
	}
	if !VerifySecret(parsed.Secret, tok.SecretHash) {
		return domain.Device{}, ErrWrongSecret
	}

	return tokens.ConsumeEnrollment(ctx, parsed.Lookup, dev)
}
