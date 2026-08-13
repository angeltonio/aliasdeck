package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
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
// ever reaching verifyPassword/the login semaphore (handleLogin's
// ByUsername lookup fails first), while a wrong password against a real
// operator pays the full argon2id cost, ~12.8 ms on this project's
// reference hardware (design's Bounded Operations table, "Concurrent
// password verification"). That is a real, measurable timing oracle
// distinguishing "no such operator" from "wrong password" — not a
// property this test can or should claim does not exist.
//
// It is accepted, not fixed, and deliberately not equalized by routing
// every username through verifyPassword regardless of whether it exists:
// doing so would let an attacker exhaust the login semaphore's
// loginConcurrency slots with garbage usernames alone, trading a timing
// oracle that leaks nothing today for a real availability problem
// (design decision 24's own semaphore existing precisely because
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
// 5.14's own RED test: it proves the login semaphore, not luck, is what
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
// loginConcurrency are still held open. Removing the semaphore around
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
		t.Fatalf("a %dth call entered verifyPassword while %d were already held open — the login semaphore did not bound concurrency to %d", loginConcurrency+1, loginConcurrency, loginConcurrency)
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

// TestLoginSemaphoreAcquireObservesContextCancellationAfterUsernameLookup
// is CRITICAL 1's own RED test: a bare `a.loginSem <- struct{}{}` send does
// not observe context cancellation at all, so a request whose client
// disconnects while queued for a slot stays parked until it eventually wins
// one — long after there is anyone left to answer.
//
// The failure window is deliberately narrower than "any disconnected
// client": auth.Operators().ByUsername runs before the semaphore acquire
// and already fails fast on a dead context on its own, so a request
// cancelled before that call reaches the acquire never demonstrates
// anything about the semaphore — it never gets there. The only window that
// says anything about the acquire itself is a client that disconnects
// *after* ByUsername has already succeeded and *while* queued on the send.
// This test builds exactly that window: it fills the semaphore with
// loginConcurrency held "filler" logins, then drives one more login for a
// distinct "target" operator whose ByUsername lookup arms s.byUsernameHook
// — signalling this goroutine at the precise instant handleLogin is about
// to reach the acquire — and only then cancels that request's own context.
//
// This calls (*api).handleLogin directly rather than going through
// NewRouter/http.TimeoutHandler over a real listener. That is a deliberate
// choice, not a shortcut: a real client-side context cancellation only
// reaches the server by the OS actually tearing down a TCP connection and
// the server's own background reader noticing it — an inherently
// asynchronous, best-effort race with no bound this test could assert
// against deterministically. Calling handleLogin with a request already
// carrying our own cancellable context makes r.Context().Done() the exact
// same channel this test's own cancel() closes — synchronous, in-process,
// and unambiguous about what it proves. http.TimeoutHandler's own
// independent ctx.Done() race (it derives its own child context and reacts
// to the same cancellation on its own timeline) would otherwise make the
// outer ServeHTTP call return regardless of whether this package's own
// semaphore acquire ever observed anything — exactly the kind of "passes
// for an unrelated reason" result this correction was warned against.
//
// Distinguishing GREEN from RED: after cancel(), a fixed acquire makes
// handleLogin return immediately (this goroutine closes targetDone) with
// StatusServiceUnavailable, never having called verifyPassword. A bare
// send never returns at all while every slot is held — targetDone would
// not close within this test's own bound, failing loudly rather than
// silently passing.
func TestLoginSemaphoreAcquireObservesContextCancellationAfterUsernameLookup(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, loginConcurrency+1)
	original := verifyPassword
	verifyPassword = func(_, _ string) (bool, error) {
		entered <- struct{}{}
		<-release
		return true, nil
	}
	t.Cleanup(func() { verifyPassword = original })

	s, _ := newFakeStoreWithOperator("filler", "irrelevant-under-the-stub")
	if _, err := s.Operators().Create(t.Context(), store.Operator{
		Username:     "target",
		PasswordHash: []byte("irrelevant-never-reached-if-the-fix-works"),
	}); err != nil {
		t.Fatalf("seeding the target operator: %v", err)
	}

	a := &api{store: s, now: time.Now, loginSem: make(chan struct{}, loginConcurrency)}

	fillerBody := []byte(`{"username":"filler","password":"whatever"}`)
	var wg sync.WaitGroup
	for i := 0; i < loginConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, loginPattern, bytes.NewReader(fillerBody))
			a.handleLogin(rec, req)
		}()
	}
	for i := 0; i < loginConcurrency; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("the filler logins never filled the semaphore")
		}
	}

	armed := make(chan struct{})
	var armOnce sync.Once
	s.byUsernameHook = func(username string) {
		if username != "target" {
			return
		}
		armOnce.Do(func() { close(armed) })
	}

	ctx, cancel := context.WithCancel(context.Background())
	targetReq := httptest.NewRequest(http.MethodPost, loginPattern, bytes.NewReader([]byte(`{"username":"target","password":"whatever"}`))).WithContext(ctx)
	targetRec := httptest.NewRecorder()
	targetDone := make(chan struct{})
	go func() {
		a.handleLogin(targetRec, targetReq)
		close(targetDone)
	}()

	select {
	case <-armed:
	case <-time.After(5 * time.Second):
		t.Fatal("the target request never reached its ByUsername lookup")
	}
	cancel()

	select {
	case <-targetDone:
	case <-time.After(5 * time.Second):
		t.Fatal("handleLogin never returned after its own context was cancelled while queued on the semaphore — it stayed parked on a bare send")
	}
	if targetRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("handleLogin status after cancellation while queued = %d, want %d", targetRec.Code, http.StatusServiceUnavailable)
	}

	// Free the filler slots. If the acquire is a bare send, the (by now
	// impossible, since we already asserted targetDone above) still-parked
	// target request would grab one of these and call verifyPassword; this
	// is the belt-and-suspenders confirmation that a fixed handleLogin
	// truly never reaches verifyPassword at all, not merely that it
	// returned quickly for some other reason.
	close(release)
	wg.Wait()

	select {
	case <-entered:
		t.Fatal("the target request entered verifyPassword despite its context already being cancelled and its handler having already returned on cancellation — the semaphore acquire did not observe context cancellation")
	case <-time.After(200 * time.Millisecond):
		// Expected: the target request returned when its context was
		// cancelled and never reached verifyPassword at all.
	}
}
