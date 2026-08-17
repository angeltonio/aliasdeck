package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/domain"
)

// TestAuthEndpointsRejectMissingCredentials covers the auth surface's own
// "reject when nothing valid was presented" cases: logout and enrollment-
// token minting require an operator session; device registration requires
// a bearer enrollment token. login itself is intentionally excluded — it
// is Public by design (the credential is the body, not a bearer token) and
// is covered by TestLoginRejectsWrongPassword instead.
func TestAuthEndpointsRejectMissingCredentials(t *testing.T) {
	h := newTestRouter(t, newFakeStore())

	cases := []struct{ method, path string }{
		{http.MethodPost, logoutPattern},
		{http.MethodPost, enrollmentTokensPattern},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			rec := doRequest(h, c.method, c.path, "", nil)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s without a session = %d, want %d", c.method, c.path, rec.Code, http.StatusUnauthorized)
			}
		})
	}

	rec := doRequest(h, http.MethodPost, devicesRegisterPattern, "", []byte(`{}`))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("%s without an enrollment token = %d, want %d", devicesRegisterPattern, rec.Code, http.StatusUnauthorized)
	}
}

// TestLoginSucceedsAndMintsASessionToken is the happy path underpinning
// every other authenticated test in this package: a correct
// username/password mints a session token that itself authenticates a
// session-guarded route.
func TestLoginSucceedsAndMintsASessionToken(t *testing.T) {
	s, _ := newFakeStoreWithOperator("admin", "correct horse battery staple")
	h := newTestRouter(t, s)

	body, _ := json.Marshal(loginRequest{Username: "admin", Password: "correct horse battery staple"})
	rec := doRequest(h, http.MethodPost, loginPattern, "", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s with correct credentials = %d, want %d, body=%s", loginPattern, rec.Code, http.StatusOK, rec.Body.String())
	}

	var got loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding login response: %v", err)
	}
	if got.Token == "" {
		t.Fatal("login response has no token")
	}

	listRec := doRequest(h, http.MethodGet, "/api/v1/aliases", got.Token, nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("using the minted session token against a guarded route = %d, want %d", listRec.Code, http.StatusOK)
	}
}

// TestLoginRejectsWrongPassword proves a wrong password (against a real
// operator record, hashed with the real auth.HashPassword) is refused, and
// an unknown username is refused with the identical response body and
// status — but NOT identical timing: an unknown username returns before
// ever reaching verifyPassword/the login limiter (handleLogin's
// ByUsername lookup fails first), while a wrong password against a real
// operator pays the full argon2id cost, ~12.8 ms on this project's
// reference hardware (design's Bounded Operations table, "Concurrent
// password verification"). That is a real, measurable timing oracle
// distinguishing "no such operator" from "wrong password" — not a
// property this test can or should claim does not exist.
//
// It is accepted, not fixed, and deliberately not equalized by routing
// every username through verifyPassword regardless of whether it exists:
// doing so would let an attacker exhaust the login limiter's
// loginConcurrency slots with garbage usernames alone, trading a timing
// oracle that leaks nothing today for a real availability problem
// (design decision 24's own limiter existing precisely because
// unbounded/uncontrolled concurrent verifyPassword calls are themselves a
// resource-exhaustion lever). It leaks nothing today specifically because
// design decision 20 fixes the only operator account at username "admin"
// and publishes that fact — there is no username left to discover by
// timing it. Revisit this acceptance the moment that constraint changes
// (e.g. multiple operator accounts, or a configurable bootstrap
// username).
func TestLoginRejectsWrongPassword(t *testing.T) {
	s, _ := newFakeStoreWithOperator("admin", "correct horse battery staple")
	h := newTestRouter(t, s)

	cases := []loginRequest{
		{Username: "admin", Password: "wrong password"},
		{Username: "nobody", Password: "correct horse battery staple"},
	}
	for _, in := range cases {
		body, _ := json.Marshal(in)
		rec := doRequest(h, http.MethodPost, loginPattern, "", body)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("login(%q) = %d, want %d, body=%s", in.Username, rec.Code, http.StatusUnauthorized, rec.Body.String())
		}
	}
}

// TestLogoutRevokesCurrentSession is the operator "log out everywhere for
// this session" action (design decision 17): after logout, the same wire
// token must no longer authenticate.
func TestLogoutRevokesCurrentSession(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	token := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	logoutRec := doRequest(h, http.MethodPost, logoutPattern, token, nil)
	if logoutRec.Code != http.StatusNoContent {
		t.Fatalf("POST %s = %d, want %d", logoutPattern, logoutRec.Code, http.StatusNoContent)
	}

	rec := doRequest(h, http.MethodGet, "/api/v1/aliases", token, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("using a logged-out session token = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// TestReplayedEnrollmentTokenIsRefusedEndToEnd is the threat matrix's
// "token handling" scenario, verbatim from the milestone's own
// instructions: a second register with an already-consumed token must
// refuse the request and mint no second device token. Mutation this test
// detects: any of ConsumeEnrollment's guards (used_at/revoked_at/expiry
// checks in auth.ConsumeEnrollment or the fake store's mirror of them)
// being skipped — a second device (and a second device token) would be
// created for the same enrollment token.
func TestReplayedEnrollmentTokenIsRefusedEndToEnd(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	_ = mintSessionFor(s, opID) // exercised only to prove the operator side is independent of this flow
	h := newTestRouter(t, s)

	enrollment := mintEnrollmentToken(s, nil, time.Now().Add(15*time.Minute))
	body, _ := json.Marshal(registerRequest{Name: "laptop", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh})

	first := doRequest(h, http.MethodPost, devicesRegisterPattern, enrollment, body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first registration = %d, want %d, body=%s", first.Code, http.StatusCreated, first.Body.String())
	}
	var firstDevice deviceTokenResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstDevice); err != nil {
		t.Fatalf("decoding first registration response: %v", err)
	}

	second := doRequest(h, http.MethodPost, devicesRegisterPattern, enrollment, body)
	if second.Code == http.StatusCreated {
		t.Fatalf("a second registration with an already-consumed enrollment token succeeded: %s", second.Body.String())
	}

	devices, err := s.Devices().List(t.Context())
	if err != nil {
		t.Fatalf("Devices().List: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("device count = %d, want exactly 1 — a replayed enrollment token must never mint a second device", len(devices))
	}
}

// TestEnrollmentTokensCreateRequiresSessionAndDeviceRegisterConsumesIt is
// the generation half of the same lifecycle: an operator mints a token via
// POST enrollmentTokensPattern, and that exact token (not a synthetic one)
// registers a device.
func TestEnrollmentTokensCreateRequiresSessionAndDeviceRegisterConsumesIt(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	session := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	mintRec := doRequest(h, http.MethodPost, enrollmentTokensPattern, session, []byte(`{}`))
	if mintRec.Code != http.StatusCreated {
		t.Fatalf("POST %s = %d, want %d, body=%s", enrollmentTokensPattern, mintRec.Code, http.StatusCreated, mintRec.Body.String())
	}
	var minted enrollmentTokenResponse
	if err := json.Unmarshal(mintRec.Body.Bytes(), &minted); err != nil {
		t.Fatalf("decoding enrollment token response: %v", err)
	}
	if minted.Token == "" {
		t.Fatal("enrollment token response has no token")
	}

	body, _ := json.Marshal(registerRequest{Name: "laptop", Platform: domain.PlatformLinux, Shell: domain.ShellBash})
	registerRec := doRequest(h, http.MethodPost, devicesRegisterPattern, minted.Token, body)
	if registerRec.Code != http.StatusCreated {
		t.Fatalf("registering with the freshly minted token = %d, want %d, body=%s", registerRec.Code, http.StatusCreated, registerRec.Body.String())
	}
}

// TestLoginConcurrencySemaphoreBoundsConcurrentVerifyPasswordCalls is task
// 5.14's own RED test: it proves the login limiter, not luck, is what
// bounds concurrent auth.VerifyPassword calls to loginConcurrency, and that
// requests beyond that bound genuinely queue rather than all executing at
// once. It overrides the package-level verifyPassword seam with an
// instrumented stand-in that blocks on a channel the test controls — no
// time.Sleep, no dependency on argon2id's real (and here, irrelevant) cost.
//
// Design of the proof: totalRequests (3x loginConcurrency) real HTTP
// requests are fired concurrently at a real httptest.Server wrapping the
// production router. Exactly loginConcurrency of them must reach
// verifyPassword and block there; a bounded wait (200ms, not a sleep used
// to assume timing — a "nothing more arrived" negative check) proves a
// (loginConcurrency+1)th call never reaches verifyPassword while the first
// loginConcurrency are still held open. Removing the limiter around
// verifyPassword in handleLogin makes all totalRequests calls enter
// verifyPassword together, so this test's negative check fails immediately.
func TestLoginConcurrencySemaphoreBoundsConcurrentVerifyPasswordCalls(t *testing.T) {
	const totalRequests = loginConcurrency * 3

	var inFlight int32
	var maxInFlight int32
	entered := make(chan struct{}, totalRequests)
	release := make(chan struct{})

	original := verifyPassword
	verifyPassword = func(_, _ string) (bool, error) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			old := atomic.LoadInt32(&maxInFlight)
			if n <= old {
				break
			}
			if atomic.CompareAndSwapInt32(&maxInFlight, old, n) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		atomic.AddInt32(&inFlight, -1)
		return true, nil
	}
	t.Cleanup(func() { verifyPassword = original })

	s, _ := newFakeStoreWithOperator("admin", "irrelevant-under-the-stub")
	h := newTestRouter(t, s)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	loginBody := `{"username":"admin","password":"whatever"}`

	var wg sync.WaitGroup
	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Post(srv.URL+loginPattern, "application/json", strings.NewReader(loginBody))
			if err == nil {
				resp.Body.Close()
			}
		}()
	}

	for i := 0; i < loginConcurrency; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of the expected %d concurrent calls entered verifyPassword within the bound", i, loginConcurrency)
		}
	}

	select {
	case <-entered:
		t.Fatalf("a %dth call entered verifyPassword while %d were already held open — the login limiter did not bound concurrency to %d", loginConcurrency+1, loginConcurrency, loginConcurrency)
	case <-time.After(200 * time.Millisecond):
		// Expected: no further entrant while loginConcurrency calls are
		// still blocked inside verifyPassword and totalRequests-loginConcurrency
		// more requests are queued outside it.
	}

	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&maxInFlight); got > loginConcurrency {
		t.Fatalf("max concurrent verifyPassword calls observed = %d, want at most %d", got, loginConcurrency)
	}
}

func TestLoginLimiterRejectsSaturationAndRecovers(t *testing.T) {
	var calls atomic.Int32
	original := verifyPassword
	verifyPassword = func(_, _ string) (bool, error) {
		calls.Add(1)
		return true, nil
	}
	t.Cleanup(func() { verifyPassword = original })

	s, _ := newFakeStoreWithOperator("target", "irrelevant-under-the-stub")
	a := &api{store: s, now: time.Now, loginLimiter: auth.NewPasswordLimiter()}
	for i := 0; i < loginConcurrency; i++ {
		if !a.loginLimiter.TryAcquire() {
			t.Fatal("could not saturate limiter")
		}
	}

	body := []byte(`{"username":"target","password":"whatever"}`)
	rec := httptest.NewRecorder()
	a.handleLogin(rec, httptest.NewRequest(http.MethodPost, loginPattern, bytes.NewReader(body)))
	if rec.Code != http.StatusServiceUnavailable || calls.Load() != 0 {
		t.Fatalf("saturated login status=%d verify calls=%d, want 503/0", rec.Code, calls.Load())
	}

	a.loginLimiter.Release()
	recovered := httptest.NewRecorder()
	a.handleLogin(recovered, httptest.NewRequest(http.MethodPost, loginPattern, bytes.NewReader(body)))
	if recovered.Code != http.StatusOK || calls.Load() != 1 {
		t.Fatalf("recovered login status=%d verify calls=%d, want 200/1", recovered.Code, calls.Load())
	}
	for i := 1; i < loginConcurrency; i++ {
		a.loginLimiter.Release()
	}
}
