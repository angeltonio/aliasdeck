package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/angeltonio/aliasdeck/internal/store"
)

// subjectContextKey is an unexported type so no other package can collide
// with this context key by accident.
type subjectContextKey struct{}

// Subject is the authenticated caller RequireKind attaches to a request's
// context once its token has passed every check.
type Subject struct {
	TokenID   string
	Kind      store.TokenKind
	SubjectID string
}

// SubjectFromContext returns the Subject RequireKind attached to ctx, and
// whether one was present. A handler reached without going through
// RequireKind (which should not happen once internal/api wires every
// route, per decision 15's coverage test) sees ok == false.
func SubjectFromContext(ctx context.Context) (Subject, bool) {
	subj, ok := ctx.Value(subjectContextKey{}).(Subject)
	return subj, ok
}

// TokenLookup is the narrow slice of store.TokenRepo RequireKind needs —
// exactly store.TokenRepo.ByLookup's shape — so middleware tests construct
// an in-memory fake instead of a real store.Store.
type TokenLookup interface {
	ByLookup(ctx context.Context, lookup string) (store.Token, error)
}

// Refuse writes the response body and status for a request RequireKind
// rejects. internal/auth deliberately has no opinion of its own about what
// shape that response takes: the caller supplies it, so this package never
// has to know whether it fronts a JSON API, a plain-text one, or anything
// else (bounded-review finding, WARNING 2: guarded routes were answering
// 401 in a different shape — text/plain via http.Error — than the rest of
// the API's own {"error":{...}} JSON shape, because this package used to
// hardcode that choice itself). This mirrors decision 24's own reasoning
// about which package owns what: a route's own response-shape opinion
// stays with the caller that owns the route, not the lower-level package
// it merely calls to authenticate one.
type Refuse func(w http.ResponseWriter)

// defaultRefuse is RequireKind's fallback when the caller passes a nil
// Refuse — this package's own previous, zero-dependency behavior,
// preserved as a usable default for any caller with no response shape of
// its own to enforce.
func defaultRefuse(w http.ResponseWriter) {
	http.Error(w, ErrUnauthorized.Error(), http.StatusUnauthorized)
}

// RequireKind returns middleware that accepts only a bearer token of
// exactly kind, current (per now()) and unrevoked. Every other case —
// missing header, malformed token, unknown lookup, wrong secret, wrong
// kind, expired, revoked — is refused identically via refuse (401, and the
// wrapped handler is never invoked), so a device token can never reach an
// operator-only route and vice versa (threat matrix: HTTP routing). A nil
// refuse falls back to defaultRefuse.
//
// now is always the caller's injected clock (server.Run wires time.Now;
// tests wire a fixed instant), so expiry is deterministic and this
// middleware never needs to sleep to be tested.
func RequireKind(tokens TokenLookup, kind store.TokenKind, now func() time.Time, refuse Refuse) func(http.Handler) http.Handler {
	if refuse == nil {
		refuse = defaultRefuse
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wire, ok := bearerToken(r)
			if !ok {
				refuse(w)
				return
			}

			parsed, err := Parse(wire)
			if err != nil {
				refuse(w)
				return
			}

			tok, err := tokens.ByLookup(r.Context(), parsed.Lookup)
			if err != nil {
				refuse(w)
				return
			}

			if !VerifySecret(parsed.Secret, tok.SecretHash) {
				refuse(w)
				return
			}

			if err := Verify(tok, kind, now()); err != nil {
				refuse(w)
				return
			}

			subj := Subject{TokenID: tok.ID, Kind: tok.Kind, SubjectID: tok.SubjectID}
			ctx := context.WithValue(r.Context(), subjectContextKey{}, subj)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// bearerToken extracts the token from a "Bearer <token>" Authorization
// header. It reports false for a missing header, a header without the
// "Bearer " prefix, or an empty token — never panicking on any input.
func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimPrefix(header, prefix)
	return token, token != ""
}
