package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/store"
)

// fakeTokenLookup is the narrow auth.TokenLookup fake every router test
// needs — none of these tests exercise a real token, so ByLookup always
// fails, which is enough to prove a guarded route refuses an unauthenticated
// request.
type fakeTokenLookup struct{}

func (fakeTokenLookup) ByLookup(_ context.Context, _ string) (store.Token, error) {
	return store.Token{}, store.ErrNotFound
}

// TestNewRouterFailsRegistrationForRouteMissingRequiredKindDeclaration is
// the threat-matrix RED test for "HTTP routing": a handler added to the
// route table without declaring who may call it (neither Public nor a
// valid RequiredKind) must fail registration itself, not merely make some
// unrelated helper report an error nobody reads. newRouter IS the
// registration path NewRouter calls — asserting its own return value here
// is asserting that registration failed, not asserting that a validator
// function was capable of failing in isolation.
func TestNewRouterFailsRegistrationForRouteMissingRequiredKindDeclaration(t *testing.T) {
	rs := []route{
		{
			Method:  http.MethodGet,
			Pattern: "/api/v1/unsafe",
			Handler: func(http.ResponseWriter, *http.Request) {},
			// Deliberately neither Public nor RequiredKind — the exact
			// mistake this test exists to catch: a handler added and
			// never guarded.
		},
	}

	h, err := newRouter(rs, fakeTokenLookup{}, time.Now)
	if err == nil {
		t.Fatal("newRouter(...) = nil error, want a non-nil error: a route declaring neither Public nor a valid RequiredKind must fail registration")
	}
	if h != nil {
		t.Fatal("newRouter(...) returned a non-nil handler alongside a registration error — a failed registration must not also hand back a usable router")
	}
}

// TestNewRouterFailsRegistrationForUnknownRequiredKind proves that a typo'd
// or otherwise unrecognized RequiredKind is exactly as unguarded as leaving
// it empty — it must not silently register as if it were valid.
func TestNewRouterFailsRegistrationForUnknownRequiredKind(t *testing.T) {
	rs := []route{
		{
			Method:       http.MethodGet,
			Pattern:      "/api/v1/unsafe",
			Handler:      func(http.ResponseWriter, *http.Request) {},
			RequiredKind: store.TokenKind("bogus"),
		},
	}

	if _, err := newRouter(rs, fakeTokenLookup{}, time.Now); err == nil {
		t.Fatal("newRouter(...) = nil error, want a non-nil error: an unrecognized RequiredKind must fail registration exactly like a missing one")
	}
}

// TestNewRouterAcceptsAWellFormedRouteTable is the GREEN-path counterpart:
// a table where every route is either Public or declares a valid
// RequiredKind must register cleanly.
func TestNewRouterAcceptsAWellFormedRouteTable(t *testing.T) {
	rs := []route{
		{Method: http.MethodGet, Pattern: "/api/v1/public", Handler: func(http.ResponseWriter, *http.Request) {}, Public: true},
		{Method: http.MethodGet, Pattern: "/api/v1/guarded", Handler: func(http.ResponseWriter, *http.Request) {}, RequiredKind: store.TokenKindSession},
	}

	h, err := newRouter(rs, fakeTokenLookup{}, time.Now)
	if err != nil {
		t.Fatalf("newRouter(...) = %v, want nil", err)
	}
	if h == nil {
		t.Fatal("newRouter(...) returned a nil handler alongside a nil error")
	}
}

// TestNewRouterAppliesRequireKindToGuardedRoutes proves the general
// mechanism actually guards a non-Public route: an unauthenticated request
// against a route declaring RequiredKind must be refused, never reaching
// the wrapped handler.
func TestNewRouterAppliesRequireKindToGuardedRoutes(t *testing.T) {
	reached := false
	rs := []route{
		{
			Method:       http.MethodGet,
			Pattern:      "/api/v1/guarded",
			Handler:      func(http.ResponseWriter, *http.Request) { reached = true },
			RequiredKind: store.TokenKindSession,
		},
	}

	h, err := newRouter(rs, fakeTokenLookup{}, time.Now)
	if err != nil {
		t.Fatalf("newRouter(...) = %v, want nil", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/guarded", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if reached {
		t.Fatal("guarded handler was reached without any Authorization header — RequireKind must refuse it first")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/guarded without auth = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// TestHealthRouteIsReachableWithoutAuthentication is design decision 23's
// own RED test, run against the real, unmodified production route table
// (routes()), not a synthetic one: GET healthPattern must succeed with no
// Authorization header at all. Naming it by the same healthMethod/
// healthPattern constants routes() itself uses is what makes this "assert
// that specific route is reachable unauthenticated, by name" rather than a
// generic "some public route exists" check — re-guarding healthPattern
// behind a token kind in routes() fails this exact test.
func TestHealthRouteIsReachableWithoutAuthentication(t *testing.T) {
	h, err := NewRouter(newFakeStore(), time.Now)
	if err != nil {
		t.Fatalf("NewRouter(...) = %v, want nil", err)
	}

	req := httptest.NewRequest(healthMethod, healthPattern, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s without an Authorization header = %d, want %d — decision 23 requires this route stay reachable without a token", healthMethod, healthPattern, rec.Code, http.StatusOK)
	}
}

// TestNewRouterRefusesAHealthRouteMissingItsPublicDeclaration is the direct
// mutation-detection test for "re-guard /api/v1/health behind a token
// kind": if a caller ever declares the health path with a RequiredKind
// instead of Public, registration must not silently accept it as
// unauthenticated — it becomes an ordinary guarded route like any other,
// and TestHealthRouteIsReachableWithoutAuthentication (against the real
// routes()) is what actually catches that regression in production code.
// This test instead proves the router applies no special-casing by path
// string: a route at healthPattern with RequiredKind set is guarded like
// any other pattern.
func TestNewRouterRefusesAHealthRouteMissingItsPublicDeclaration(t *testing.T) {
	rs := []route{
		{Method: healthMethod, Pattern: healthPattern, Handler: handleHealth, RequiredKind: store.TokenKindSession},
	}

	h, err := newRouter(rs, fakeTokenLookup{}, time.Now)
	if err != nil {
		t.Fatalf("newRouter(...) = %v, want nil", err)
	}

	req := httptest.NewRequest(healthMethod, healthPattern, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("%s %s guarded by RequiredKind without auth = %d, want %d — the router itself must not treat this path as special", healthMethod, healthPattern, rec.Code, http.StatusUnauthorized)
	}
}
