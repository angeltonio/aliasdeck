package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
)

// loginPattern, logoutPattern and enrollmentTokensPattern are the operator-
// facing auth routes; devicesRegisterPattern is the device-facing one
// (server-auth spec: bootstrap/session/enrollment/device token lifecycle).
const (
	loginPattern            = "/api/v1/auth/login"
	logoutPattern           = "/api/v1/auth/logout"
	enrollmentTokensPattern = "/api/v1/enrollment-tokens"
	devicesRegisterPattern  = "/api/v1/devices/register"
)

// sessionLifetime is design decision 8's fixed, non-sliding session
// lifetime: 24 hours from mint, never extended by activity.
const sessionLifetime = 24 * time.Hour

// defaultEnrollmentTTL is the enrollment token lifetime when a request does
// not specify one (design's token lifetime table: "15 min default (--ttl)").
const defaultEnrollmentTTL = 15 * time.Minute

// loginConcurrency bounds how many concurrent calls to verifyPassword this
// package will make (design decision 24, bounded-review finding "Concurrent
// password verification"): auth.VerifyPassword is measured at ~12.8 ms wall
// time and 64 MiB resident per call, so unbounded concurrency on this
// unauthenticated route is itself a lever — 10 concurrent login attempts
// alone already hold ~640 MiB before any single call is a problem on its
// own. Sized in the low single digits: comfortably more than one operator's
// login is ever queued behind in practice, small enough that the worst case
// (loginConcurrency stacked argon2id blocks) stays a bounded, known cost.
// Excess attempts queue on a.loginSem — but a bare, unconditional channel
// send does not observe context cancellation on its own, so handleLogin's
// acquire below selects on r.Context().Done() as well. That is what
// actually makes the wait bounded by the same deadline http.TimeoutHandler
// (router.go) already attaches to the request context, for both an
// ordinary 20s timeout and an earlier client disconnect; a bare send would
// stay parked past both, blocked on a client that is already gone, until it
// eventually won a slot. (A four-lens correction pass found this package's
// own comment previously asserting that bound already existed, when the
// code did not implement it — corrected here and in design.md decision 24.)
const loginConcurrency = 4

// verifyPassword is auth.VerifyPassword through a package-level seam so
// this package's own concurrency test (auth_test.go) can observe — and
// hold open — exactly how many calls are in flight at once, without
// sleeping and without paying real argon2id's cost on every test run.
// Production code never reassigns it.
var verifyPassword = auth.VerifyPassword

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// handleLogin is the server-auth spec's operator authentication path
// (design decision 17: login authenticates the operator only). It is
// Public at the router level — the credential arrives in the body, not as
// a bearer token RequireKind could check — and authenticates itself here.
//
// The loginSem acquire/release brackets only the verifyPassword call
// (design decision 24): an unknown username is refused before ever
// touching the semaphore or the KDF.
//
// The acquire is a select on r.Context().Done(), not a bare channel send.
// The narrow, real failure window this closes: a client disconnects after
// ByUsername has already succeeded (a dead context fails ByUsername on its
// own, before the acquire is ever reached, so an already-cancelled request
// never demonstrates this at all) while every loginSem slot is held by
// other in-flight logins. A bare send would leave that goroutine parked
// until it eventually won a slot — long after its own client was gone —
// which is exactly the "sixth unbounded operation" this correction exists
// to close. On cancellation this returns before ever calling
// verifyPassword; the write below is a best-effort courtesy to a caller
// that is typically already gone (http.TimeoutHandler's own wrapped
// ResponseWriter silently discards a write arriving after its own timeout
// fired, so this is never a double-response panic either way).
func (a *api) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in loginRequest
	if !decodeJSON(w, r, &in) {
		return
	}

	op, err := a.store.Operators().ByUsername(r.Context(), in.Username)
	if err != nil {
		writeInvalidCredentials(w)
		return
	}

	select {
	case a.loginSem <- struct{}{}:
	case <-r.Context().Done():
		writeError(w, http.StatusServiceUnavailable, codeTimeout, "the request was cancelled while waiting to verify credentials", nil)
		return
	}
	ok, verr := verifyPassword(in.Password, string(op.PasswordHash))
	<-a.loginSem

	if verr != nil || !ok {
		writeInvalidCredentials(w)
		return
	}

	minted, err := auth.Mint(store.TokenKindSession)
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error", nil)
		return
	}

	now := a.now()
	expiresAt := now.Add(sessionLifetime)
	if err := a.store.Tokens().Create(r.Context(), store.Token{
		Kind:       store.TokenKindSession,
		SubjectID:  op.ID,
		Lookup:     minted.Lookup,
		SecretHash: minted.SecretHash,
		CreatedAt:  now,
		ExpiresAt:  expiresAt,
	}); err != nil {
		writeStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{Token: minted.Wire, ExpiresAt: expiresAt})
}

func writeInvalidCredentials(w http.ResponseWriter) {
	writeError(w, http.StatusUnauthorized, codeInvalidCredentials, "invalid username or password", nil)
}

// handleLogout revokes the calling session's own token server-side — the
// operator "log out everywhere for this session" action design decision 17
// deliberately keeps distinct from the CLI's local-only logout (which never
// contacts the server at all). It requires an authenticated session
// (RequiredKind: session in router.go), so auth.SubjectFromContext is
// always populated by the time this handler runs.
func (a *api) handleLogout(w http.ResponseWriter, r *http.Request) {
	subj, ok := auth.SubjectFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "no authenticated session", nil)
		return
	}
	if err := a.store.Tokens().Revoke(r.Context(), subj.TokenID, a.now()); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type enrollmentTokenRequest struct {
	ProfileIDs []string `json:"profileIds,omitempty"`
	TTLSeconds int      `json:"ttlSeconds,omitempty"`
}

type enrollmentTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// handleEnrollmentTokensCreate is the operator action the server-auth
// spec's "Enrollment token registers a device" scenario starts from: it
// requires an authenticated session and mints a single-use enrollment
// token a registering device presents to devicesRegisterPattern.
func (a *api) handleEnrollmentTokensCreate(w http.ResponseWriter, r *http.Request) {
	var in enrollmentTokenRequest
	if !decodeJSON(w, r, &in) {
		return
	}

	ttl := defaultEnrollmentTTL
	if in.TTLSeconds > 0 {
		ttl = time.Duration(in.TTLSeconds) * time.Second
	}

	minted, err := auth.Mint(store.TokenKindEnrollment)
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error", nil)
		return
	}

	now := a.now()
	expiresAt := now.Add(ttl)
	if err := a.store.Tokens().Create(r.Context(), store.Token{
		Kind:       store.TokenKindEnrollment,
		SecretHash: minted.SecretHash,
		Lookup:     minted.Lookup,
		ProfileIDs: in.ProfileIDs,
		CreatedAt:  now,
		ExpiresAt:  expiresAt,
	}); err != nil {
		writeStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, enrollmentTokenResponse{Token: minted.Wire, ExpiresAt: expiresAt})
}

type registerRequest struct {
	Name          string          `json:"name"`
	Platform      domain.Platform `json:"platform"`
	Shell         domain.Shell    `json:"shell"`
	ClientVersion string          `json:"clientVersion,omitempty"`
}

// handleDevicesRegister exchanges a bearer enrollment token for a device
// token (server-auth spec, "Enrollment token registers a device"). It is
// Public at the router level for the same reason handleLogin is: the
// credential is authenticated inside the handler, not by RequireKind,
// because auth.ConsumeEnrollment must both verify AND atomically consume
// the token in one call — RequireKind only verifies.
//
// A replayed (already-consumed) or otherwise invalid enrollment token is
// refused identically to a malformed one: auth.ConsumeEnrollment's own
// composition (Parse, kind check, ByLookup, VerifySecret, then the store's
// atomic consume) is what the threat matrix's "a replayed enrollment token
// is refused end-to-end" scenario exercises, and this handler adds nothing
// of its own on top of it.
func (a *api) handleDevicesRegister(w http.ResponseWriter, r *http.Request) {
	wire, ok := bearerToken(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, codeInvalidToken, "a bearer enrollment token is required", nil)
		return
	}

	var in registerRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if !in.Platform.Valid() {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, fmt.Sprintf("unknown platform %q", in.Platform), nil)
		return
	}
	if !in.Shell.Valid() {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, fmt.Sprintf("unknown shell %q", in.Shell), nil)
		return
	}

	dev := domain.Device{
		Name:          in.Name,
		Platform:      in.Platform,
		Shell:         in.Shell,
		ClientVersion: in.ClientVersion,
	}

	registered, err := auth.ConsumeEnrollment(r.Context(), a.store.Tokens(), wire, dev)
	if err != nil {
		if errors.Is(err, auth.ErrMalformedToken) || errors.Is(err, auth.ErrWrongTokenKind) || errors.Is(err, auth.ErrWrongSecret) {
			writeError(w, http.StatusUnauthorized, codeInvalidToken, "the enrollment token is invalid or already used", nil)
			return
		}
		writeStoreError(w, err)
		return
	}

	minted, err := auth.Mint(store.TokenKindDevice)
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error", nil)
		return
	}
	if err := a.store.Tokens().Create(r.Context(), store.Token{
		Kind:       store.TokenKindDevice,
		SubjectID:  registered.ID,
		Lookup:     minted.Lookup,
		SecretHash: minted.SecretHash,
		CreatedAt:  a.now(),
	}); err != nil {
		// Deliberately accepted, not compensated (bounded-review finding,
		// WARNING 4; design decision 27). auth.ConsumeEnrollment above is
		// atomic (design's Interfaces section), but this Create is a
		// separate write against a different repo: if it fails, the device
		// row already exists — the enrollment token is spent and cannot be
		// replayed to try again — while the operator holding this response
		// only sees a failure. A compensating delete here would be a THIRD
		// unguarded write racing the same failure class it is trying to
		// undo, for a device that is already recoverable without it: an
		// operator who lists devices, finds this one lacking a live token,
		// and calls POST devicesPattern/{id}/token (rotate) mints it a
		// fresh, usable device token with no need to repeat the (single-use,
		// already-consumed) enrollment exchange. Naming the orphaned
		// device's id in the response is what makes that recovery path
		// discoverable from the error itself, not just this comment.
		writeError(w, http.StatusInternalServerError, codeInternal,
			"the device was registered but its token could not be issued; retry by rotating its token", map[string]any{"deviceId": registered.ID})
		return
	}

	writeJSON(w, http.StatusCreated, deviceTokenResponse{DeviceID: registered.ID, DeviceToken: minted.Wire})
}

// bearerToken extracts the token from a "Bearer <token>" Authorization
// header. It mirrors internal/auth/middleware.go's unexported helper of the
// same shape (header parsing, not a security decision) — duplicated rather
// than exported across the package boundary for one line of logic, never
// panicking on any input.
func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimPrefix(header, prefix)
	return token, token != ""
}
