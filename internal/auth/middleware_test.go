package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/store"
)

// seedToken mints a fresh wire token of kind and stores its record in repo,
// returning the wire form a request would present.
func seedToken(t *testing.T, repo *fakeTokenRepo, kind store.TokenKind, mutate func(*store.Token)) string {
	t.Helper()

	minted, err := Mint(kind)
	if err != nil {
		t.Fatalf("Mint(%q): %v", kind, err)
	}
	tok := store.Token{Lookup: minted.Lookup, Kind: kind, SecretHash: minted.SecretHash}
	if mutate != nil {
		mutate(&tok)
	}
	if err := repo.Create(context.Background(), tok); err != nil {
		t.Fatalf("seeding token: %v", err)
	}
	return minted.Wire
}

func fixedNow(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

func newRecordingHandler() (http.Handler, *bool) {
	called := new(bool)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called = true
		if _, ok := SubjectFromContext(r.Context()); !ok {
			http.Error(w, "no subject in context", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}), called
}

func doRequest(t *testing.T, handler http.Handler, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestRequireKindAcceptsAMatchingCurrentToken(t *testing.T) {
	repo := newFakeTokenRepo()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	wire := seedToken(t, repo, store.TokenKindDevice, nil)

	inner, called := newRecordingHandler()
	handler := RequireKind(repo, store.TokenKindDevice, fixedNow(now))(inner)

	rec := doRequest(t, handler, wire)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !*called {
		t.Fatal("the wrapped handler was never invoked for a valid, matching-kind token")
	}
}

// TestRequireKindRefusesWrongKindForTheRoute is the threat-matrix "HTTP
// routing and authentication" case: a device token must be refused on an
// operator (session) route. This is also this task's designated mutation
// target — removing the kind check from the middleware must make this
// test fail.
func TestRequireKindRefusesWrongKindForTheRoute(t *testing.T) {
	repo := newFakeTokenRepo()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	deviceWire := seedToken(t, repo, store.TokenKindDevice, nil)

	inner, called := newRecordingHandler()
	handler := RequireKind(repo, store.TokenKindSession, fixedNow(now))(inner)

	rec := doRequest(t, handler, deviceWire)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a device token on an operator-session route", rec.Code)
	}
	if *called {
		t.Fatal("the wrapped handler was invoked despite the token being the wrong kind for this route")
	}
}

func TestRequireKindRefusesMissingAuthorizationHeader(t *testing.T) {
	repo := newFakeTokenRepo()
	inner, called := newRecordingHandler()
	handler := RequireKind(repo, store.TokenKindDevice, fixedNow(time.Now()))(inner)

	rec := doRequest(t, handler, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 with no Authorization header", rec.Code)
	}
	if *called {
		t.Fatal("the wrapped handler was invoked with no Authorization header at all")
	}
}

func TestRequireKindRefusesMalformedBearerToken(t *testing.T) {
	repo := newFakeTokenRepo()
	inner, called := newRecordingHandler()
	handler := RequireKind(repo, store.TokenKindDevice, fixedNow(time.Now()))(inner)

	rec := doRequest(t, handler, "not-a-wire-token-at-all")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a malformed bearer token", rec.Code)
	}
	if *called {
		t.Fatal("the wrapped handler was invoked with a malformed bearer token")
	}
}

func TestRequireKindRefusesUnknownLookup(t *testing.T) {
	repo := newFakeTokenRepo()
	// A syntactically valid device token that was never seeded into repo.
	minted, err := Mint(store.TokenKindDevice)
	if err != nil {
		t.Fatalf("Mint(): %v", err)
	}

	inner, called := newRecordingHandler()
	handler := RequireKind(repo, store.TokenKindDevice, fixedNow(time.Now()))(inner)

	rec := doRequest(t, handler, minted.Wire)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a well-formed but unknown token", rec.Code)
	}
	if *called {
		t.Fatal("the wrapped handler was invoked for an unknown lookup")
	}
}

func TestRequireKindRefusesWrongSecret(t *testing.T) {
	repo := newFakeTokenRepo()
	minted, err := Mint(store.TokenKindDevice)
	if err != nil {
		t.Fatalf("Mint(): %v", err)
	}
	if err := repo.Create(context.Background(), store.Token{
		Lookup: minted.Lookup, Kind: store.TokenKindDevice, SecretHash: minted.SecretHash,
	}); err != nil {
		t.Fatalf("seeding token: %v", err)
	}

	wrongWire := "add_" + minted.Lookup + ".not-the-real-secret"

	inner, called := newRecordingHandler()
	handler := RequireKind(repo, store.TokenKindDevice, fixedNow(time.Now()))(inner)

	rec := doRequest(t, handler, wrongWire)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a valid lookup with the wrong secret", rec.Code)
	}
	if *called {
		t.Fatal("the wrapped handler was invoked with the wrong secret")
	}
}

// TestRequireKindRefusesExpiredToken is this task's other designated
// mutation target: removing the expiry check must make an expired token
// pass, which this test would then catch.
func TestRequireKindRefusesExpiredToken(t *testing.T) {
	repo := newFakeTokenRepo()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	wire := seedToken(t, repo, store.TokenKindSession, func(tok *store.Token) {
		tok.ExpiresAt = now.Add(-1 * time.Second)
	})

	inner, called := newRecordingHandler()
	handler := RequireKind(repo, store.TokenKindSession, fixedNow(now))(inner)

	rec := doRequest(t, handler, wire)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for an expired session token", rec.Code)
	}
	if *called {
		t.Fatal("the wrapped handler was invoked with an expired token")
	}
}

func TestRequireKindRefusesRevokedToken(t *testing.T) {
	repo := newFakeTokenRepo()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	wire := seedToken(t, repo, store.TokenKindDevice, func(tok *store.Token) {
		tok.RevokedAt = now.Add(-1 * time.Minute)
	})

	inner, called := newRecordingHandler()
	handler := RequireKind(repo, store.TokenKindDevice, fixedNow(now))(inner)

	rec := doRequest(t, handler, wire)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a revoked device token", rec.Code)
	}
	if *called {
		t.Fatal("the wrapped handler was invoked with a revoked token")
	}
}
