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

	// Refuse overrides the response auth.RequireKind writes when this route
	// rejects a request. A nil Refuse falls back to writeUnauthorized —
	// every route had exactly that behavior before Phase 6 (design decision
	// 25's uniform 401 shape). syncPattern is the first route to set this:
	// it has no operator session to fall back on, so its own Refuse
	// (writeUnauthorizedDevice, sync.go) names the one recovery action a
	// device has instead. It changes only the message, never the status
	// code, Content-Type, or the property that every failure mode
	// RequireKind can hit for one route answers identically (threat matrix:
	// token handling) — Refuse is a per-route wording choice, not a
	// per-failure-mode one.
	Refuse auth.Refuse
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

// api is the composed set of dependencies every handler added in this
// package's second half (aliases, profiles, devices, auth) closes over: the
// store, an injectable clock (auth.RequireKind's own now, and every
// timestamp a handler writes), and the shared password-verification limiter (design
// decision 24). It is deliberately unexported: internal/server is the only
// caller outside this package, and it reaches these handlers only through
// NewRouter, never by constructing an api value itself.
type api struct {
	store        store.Store
	now          func() time.Time
	loginLimiter auth.PasswordLimiter
}

// routes is the complete, explicit route table this server exposes.
// Design decision 15 (route slice/OpenAPI coverage) and decision 23
// (health route ownership) both name this table directly: healthPattern
// is Public here and every route declares a RequiredKind — there is no
// other, implicit way for a route to become reachable. This is also the
// exact slice internal/api/openapi_coverage_test.go compares, bidirectionally,
// against docs/openapi.yaml (decision 15).
func (a *api) routes() []route {
	return []route{
		{Method: healthMethod, Pattern: healthPattern, Handler: handleHealth, Public: true},
		{Method: http.MethodGet, Pattern: openapiPattern, Handler: a.handleOpenAPISpec, Public: true},

		// Auth (5.9/5.10): login and the enrollment-token exchange
		// authenticate themselves out of band (a password in the body, an
		// enrollment token as a bearer credential consumed by the handler
		// itself), so both are Public at the router level — RequireKind has
		// no bearer *session*/*device* token to check before the handler
		// even runs. logout and minting a new enrollment token both require
		// an existing operator session.
		{Method: http.MethodPost, Pattern: loginPattern, Handler: a.handleLogin, Public: true},
		{Method: http.MethodPost, Pattern: logoutPattern, Handler: a.handleLogout, RequiredKind: store.TokenKindSession},
		{Method: http.MethodPost, Pattern: enrollmentTokensPattern, Handler: a.handleEnrollmentTokensCreate, RequiredKind: store.TokenKindSession},
		{Method: http.MethodPost, Pattern: devicesRegisterPattern, Handler: a.handleDevicesRegister, Public: true},

		// Aliases (5.7/5.8).
		{Method: http.MethodGet, Pattern: aliasesPattern, Handler: a.handleAliasesList, RequiredKind: store.TokenKindSession},
		{Method: http.MethodPost, Pattern: aliasesPattern, Handler: a.handleAliasesCreate, RequiredKind: store.TokenKindSession},
		{Method: http.MethodGet, Pattern: aliasPattern, Handler: a.handleAliasesGet, RequiredKind: store.TokenKindSession},
		{Method: http.MethodPut, Pattern: aliasPattern, Handler: a.handleAliasesUpdate, RequiredKind: store.TokenKindSession},
		{Method: http.MethodDelete, Pattern: aliasPattern, Handler: a.handleAliasesDelete, RequiredKind: store.TokenKindSession},

		// Profiles (5.7/5.8).
		{Method: http.MethodGet, Pattern: profilesPattern, Handler: a.handleProfilesList, RequiredKind: store.TokenKindSession},
		{Method: http.MethodPost, Pattern: profilesPattern, Handler: a.handleProfilesCreate, RequiredKind: store.TokenKindSession},
		{Method: http.MethodGet, Pattern: profilePattern, Handler: a.handleProfilesGet, RequiredKind: store.TokenKindSession},
		{Method: http.MethodPut, Pattern: profilePattern, Handler: a.handleProfilesUpdate, RequiredKind: store.TokenKindSession},
		{Method: http.MethodDelete, Pattern: profilePattern, Handler: a.handleProfilesDelete, RequiredKind: store.TokenKindSession},

		// Devices (5.7/5.8). No POST /api/v1/devices: a device is born only
		// through devicesRegisterPattern's enrollment-token exchange
		// (design's Interfaces section — DeviceRepo has no Create).
		{Method: http.MethodGet, Pattern: devicesPattern, Handler: a.handleDevicesList, RequiredKind: store.TokenKindSession},
		{Method: http.MethodGet, Pattern: devicePattern, Handler: a.handleDevicesGet, RequiredKind: store.TokenKindSession},
		{Method: http.MethodPut, Pattern: devicePattern, Handler: a.handleDevicesUpdate, RequiredKind: store.TokenKindSession},
		{Method: http.MethodDelete, Pattern: devicePattern, Handler: a.handleDevicesDelete, RequiredKind: store.TokenKindSession},
		{Method: http.MethodPost, Pattern: deviceRevokePattern, Handler: a.handleDevicesRevoke, RequiredKind: store.TokenKindSession},
		{Method: http.MethodPost, Pattern: deviceTokenPattern, Handler: a.handleDevicesRotateToken, RequiredKind: store.TokenKindSession},

		// Device reporting: both routes use the same device-token boundary and
		// actionable refusal. Sync records alias application; heartbeat only
		// records reachability.
		{Method: http.MethodGet, Pattern: syncPattern, Handler: a.handleSync, RequiredKind: store.TokenKindDevice, Refuse: writeUnauthorizedDevice},
		{Method: http.MethodPost, Pattern: heartbeatPattern, Handler: a.handleHeartbeat, RequiredKind: store.TokenKindDevice, Refuse: writeUnauthorizedDevice},
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

// NewRouter builds the complete internal/api handler: every route in
// (*api).routes() wrapped in auth.RequireKind(st.Tokens(), r.RequiredKind,
// now) unless it is Public, every request body bounded by withMaxBytes, the
// whole mux bounded by handlerTimeout. It returns a non-nil error — and a
// nil handler — if any route in the table declares neither Public nor a
// valid RequiredKind, so registration itself fails; there is no path
// through this function that hands back a serving handler built from an
// invalid table.
//
// st is the one store.Store every handler in this package reads and writes
// through; now is the injected clock RequireKind and every handler that
// stamps a timestamp use — production wires time.Now (internal/server.Run),
// tests wire a fixed instant so expiry and token timestamps stay
// deterministic without sleeping.
func NewRouter(st store.Store, now func() time.Time) (http.Handler, error) {
	return NewRouterWithPasswordLimiter(st, now, auth.NewPasswordLimiter())
}

// NewRouterWithPasswordLimiter builds the API with the same process-wide
// expensive-password-work bound used by the browser UI.
func NewRouterWithPasswordLimiter(st store.Store, now func() time.Time, limiter auth.PasswordLimiter) (http.Handler, error) {
	if limiter == nil {
		return nil, fmt.Errorf("api: password limiter is required")
	}
	a := &api{store: st, now: now, loginLimiter: limiter}
	return newRouter(a.routes(), st.Tokens(), now)
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
			refuse := r.Refuse
			if refuse == nil {
				refuse = writeUnauthorized
			}
			handler = auth.RequireKind(tokens, r.RequiredKind, now, refuse)(handler)
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
