package api

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/store"
)

// handlerTimeout bounds every request this router serves (design's Bounded
// Operations table, "Handler execution"): every handler's context carries
// this deadline, so a slow store call can never pin a connection open
// indefinitely.
const handlerTimeout = 20 * time.Second

// timeoutBody is what http.TimeoutHandler writes when handlerTimeout
// fires. It carries the same {"error":{...}} shape every other response
// in this package uses (errors.go), so a client parses a timeout exactly
// like any other error — never a bare stdlib string.
const timeoutBody = `{"error":{"code":"` + codeTimeout + `","message":"the request took too long to process"}}` + "\n"

// healthMethod and healthPattern name design decision 23's route
// explicitly: GET /api/v1/health, Public, never re-guarded behind a token
// kind. Both routes() and this package's own tests reference these
// constants rather than a repeated literal, so a future edit to the path
// itself only needs to change one place, and a test asserting against
// them is asserting against the same named route routes() declares, not a
// coincidentally-matching string.
const (
	healthMethod  = http.MethodGet
	healthPattern = "/api/v1/health"
)

// route declares one HTTP endpoint: its method, pattern, handler, and
// exactly one authentication requirement. A route MUST set either Public
// or a RequiredKind recognized by auth.RequireKind — never both, never
// neither. Declaring neither is the threat-matrix failure mode this
// package's own registration guards against: a handler added to this
// table and never guarded, silently reachable by anyone (threat matrix,
// "HTTP routing").
type route struct {
	Method       string
	Pattern      string
	Handler      http.HandlerFunc
	RequiredKind store.TokenKind
	Public       bool
}

// validKinds is the closed set of token kinds auth.RequireKind accepts
// (store.TokenKindSession/Enrollment/Device). A RequiredKind outside this
// set — a typo, or a stale value left over from a rename — is exactly as
// unguarded as an empty one, and registration must refuse it identically.
var validKinds = map[store.TokenKind]bool{
	store.TokenKindSession:    true,
	store.TokenKindEnrollment: true,
	store.TokenKindDevice:     true,
}

// routes is the complete, explicit route table this server exposes.
// Design decision 15 (route slice/OpenAPI coverage) and decision 23
// (health route ownership) both name this table directly: healthPattern
// is Public here and every route Phase 5's later tasks add to this slice
// must declare a RequiredKind — there is no other, implicit way for a
// route to become reachable.
func routes() []route {
	return []route{
		{Method: healthMethod, Pattern: healthPattern, Handler: handleHealth, Public: true},
	}
}

// handleHealth reports readiness with a fixed, minimal body — the same
// contract internal/server/handler.go's Phase 4 stub already promises
// (no schema version, no build metadata, no filesystem path, no database
// state). Phase 5's full wiring of this router into internal/server is
// deliberately not part of this batch (5.1-5.6); until it lands, this
// handler exists so routes() and this package's own tests can exercise
// decision 23 end-to-end without depending on internal/server at all.
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, `{"status":"ok"}`+"\n")
}

// NewRouter builds the complete internal/api handler from routes(): every
// route wrapped in auth.RequireKind(tokens, r.RequiredKind, now) unless it
// is Public, every request body bounded by withMaxBytes, the whole mux
// bounded by handlerTimeout. It returns a non-nil error — and a nil
// handler — if any route in the table declares neither Public nor a valid
// RequiredKind, so registration itself fails; there is no path through
// this function that hands back a serving handler built from an invalid
// table.
func NewRouter(tokens auth.TokenLookup, now func() time.Time) (http.Handler, error) {
	return newRouter(routes(), tokens, now)
}

// newRouter is NewRouter's table-injectable core: production code always
// calls it with routes(), and this package's own tests call it directly
// with a synthetic table to exercise the registration guard without
// touching the real route slice.
func newRouter(rs []route, tokens auth.TokenLookup, now func() time.Time) (http.Handler, error) {
	mux := http.NewServeMux()

	for _, r := range rs {
		if err := validateRoute(r); err != nil {
			return nil, err
		}

		var handler http.Handler = r.Handler
		if !r.Public {
			handler = auth.RequireKind(tokens, r.RequiredKind, now)(handler)
		}
		handler = withMaxBytes(handler)

		mux.Handle(r.Method+" "+r.Pattern, handler)
	}

	return http.TimeoutHandler(mux, handlerTimeout, timeoutBody), nil
}

// validateRoute enforces the one rule every route in this table must
// satisfy: Public, or a RequiredKind in validKinds. Exactly one of those
// two must hold; a route satisfying neither is rejected here, which is
// what makes that rejection visible as a registration failure in
// newRouter rather than a silently-accepted, unguarded route.
func validateRoute(r route) error {
	if r.Public {
		return nil
	}
	if !validKinds[r.RequiredKind] {
		return fmt.Errorf("api: route %s %s declares no valid required token kind: every route must set Public or a RequiredKind of session, enrollment, or device", r.Method, r.Pattern)
	}
	return nil
}
