package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/store/sqlitestore"
)

const (
	localSetupPeer  = "127.0.0.1:43210"
	remoteSetupPeer = "192.0.2.10:43210"
	setupPassword   = "correct horse battery staple"
)

func TestLocalDirectSetupDoesNotRequireCredential(t *testing.T) {
	a, path, credential := newSetupWebapp(t)
	token, cookie, page := beginLocalSetup(t, a)
	if strings.Contains(page.Body.String(), credential) {
		t.Fatal("local setup page exposed the remote setup credential")
	}

	form := setupForm("operator", "")
	form.Set("local_token", token)
	request := newSetupRequest(http.MethodPost, "/setup", localSetupPeer, form)
	request.AddCookie(cookie)
	created := serveSetup(a, request)
	if created.Code != http.StatusSeeOther {
		t.Fatalf("local POST /setup status = %d, want 303", created.Code)
	}
	if location := created.Header().Get("Location"); location != "/login" {
		t.Fatalf("local setup redirect = %q, want /login", location)
	}
	operator, err := a.store.Operators().ByUsername(context.Background(), "operator")
	if err != nil {
		t.Fatalf("load locally created operator: %v", err)
	}
	passwordMatches, err := auth.VerifyPassword(setupPassword, string(operator.PasswordHash))
	if err != nil || !passwordMatches {
		t.Fatalf("verify locally created operator password: matches=%v err=%v", passwordMatches, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("setup credential remains after local success: %v", err)
	}

	replayRequest := newSetupRequest(http.MethodPost, "/setup", localSetupPeer, form)
	replayRequest.AddCookie(cookie)
	replay := serveSetup(a, replayRequest)
	if replay.Code != http.StatusNotFound {
		t.Fatalf("local setup replay status = %d, want 404", replay.Code)
	}
}

func TestLocalSetupRejectsMissingMismatchedAndForgedTokens(t *testing.T) {
	for _, tt := range []struct {
		name       string
		mutateForm func(url.Values, string)
		addCookie  func(*http.Request, *http.Cookie)
	}{
		{
			name:       "missing form token",
			mutateForm: func(_ url.Values, _ string) {},
			addCookie:  func(r *http.Request, cookie *http.Cookie) { r.AddCookie(cookie) },
		},
		{
			name:       "missing cookie",
			mutateForm: func(form url.Values, token string) { form.Set("local_token", token) },
			addCookie:  func(*http.Request, *http.Cookie) {},
		},
		{
			name:       "mismatched token",
			mutateForm: func(form url.Values, _ string) { form.Set("local_token", "different-token") },
			addCookie:  func(r *http.Request, cookie *http.Cookie) { r.AddCookie(cookie) },
		},
		{
			name:       "matching but unissued token",
			mutateForm: func(form url.Values, _ string) { form.Set("local_token", "attacker-token") },
			addCookie: func(r *http.Request, _ *http.Cookie) {
				r.AddCookie(&http.Cookie{Name: localSetupCookieName, Value: "attacker-token"})
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, path, _ := newSetupWebapp(t)
			token, cookie, _ := beginLocalSetup(t, a)
			form := setupForm("operator", "")
			tt.mutateForm(form, token)
			request := newSetupRequest(http.MethodPost, "/setup", localSetupPeer, form)
			tt.addCookie(request, cookie)
			request.Header.Set("Origin", "https://attacker.example")
			if rec := serveSetup(a, request); rec.Code != http.StatusNotFound {
				t.Fatalf("forged local POST /setup status = %d, want 404", rec.Code)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("forged local setup consumed credential: %v", err)
			}
		})
	}
}

func TestLocalSetupTokenIsOneTimeAndRenewedAfterValidationError(t *testing.T) {
	a, path, _ := newSetupWebapp(t)
	token, cookie, _ := beginLocalSetup(t, a)
	weakForm := setupForm("operator", "")
	weakForm.Set("password", "short")
	weakForm.Set("confirmation", "short")
	weakForm.Set("local_token", token)
	weakRequest := newSetupRequest(http.MethodPost, "/setup", localSetupPeer, weakForm)
	weakRequest.AddCookie(cookie)
	weak := serveSetup(a, weakRequest)
	if weak.Code != http.StatusBadRequest {
		t.Fatalf("weak-password local setup status = %d, want 400", weak.Code)
	}
	newToken, newCookie := setupTokenFromResponse(t, weak)
	if newToken == token {
		t.Fatal("validation error reused the consumed local setup token")
	}

	replayForm := setupForm("operator", "")
	replayForm.Set("local_token", token)
	replayRequest := newSetupRequest(http.MethodPost, "/setup", localSetupPeer, replayForm)
	replayRequest.AddCookie(cookie)
	if replay := serveSetup(a, replayRequest); replay.Code != http.StatusNotFound {
		t.Fatalf("consumed local token replay status = %d, want 404", replay.Code)
	}

	validForm := setupForm("operator", "")
	validForm.Set("local_token", newToken)
	validRequest := newSetupRequest(http.MethodPost, "/setup", localSetupPeer, validForm)
	validRequest.AddCookie(newCookie)
	if created := serveSetup(a, validRequest); created.Code != http.StatusSeeOther {
		t.Fatalf("renewed local token setup status = %d, want 303", created.Code)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("setup credential remains after renewed-token success: %v", err)
	}
}

func TestRemoteSetupStillRequiresOneTimeCredential(t *testing.T) {
	a, path, credential := newSetupWebapp(t)

	page := serveSetup(a, newSetupRequest(http.MethodGet, "/setup", remoteSetupPeer, nil))
	if page.Code != http.StatusNotFound {
		t.Fatalf("remote uncredentialed GET /setup status = %d, want 404", page.Code)
	}

	missing := serveSetup(a, newSetupRequest(http.MethodPost, "/setup", remoteSetupPeer, setupForm("operator", "")))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("remote uncredentialed POST /setup status = %d, want 404", missing.Code)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("uncredentialed remote setup consumed credential: %v", err)
	}

	invalid := serveSetup(a, newSetupRequest(http.MethodPost, "/setup", remoteSetupPeer, setupForm("operator", "wrong-credential")))
	if invalid.Code != http.StatusNotFound {
		t.Fatalf("remote invalid-credential POST /setup status = %d, want 404", invalid.Code)
	}

	credentialedPage := serveSetup(a, newSetupRequest(http.MethodGet, "/setup?credential="+url.QueryEscape(credential), remoteSetupPeer, nil))
	if credentialedPage.Code != http.StatusOK {
		t.Fatalf("remote credentialed GET /setup status = %d, want 200", credentialedPage.Code)
	}
	if localTokenPattern.MatchString(credentialedPage.Body.String()) || hasLocalSetupCookie(credentialedPage) {
		t.Fatal("remote setup response minted a local anti-CSRF capability")
	}
	created := serveSetup(a, newSetupRequest(http.MethodPost, "/setup", remoteSetupPeer, setupForm("operator", credential)))
	if created.Code != http.StatusSeeOther {
		t.Fatalf("remote credentialed POST /setup status = %d, want 303", created.Code)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("setup credential remains after remote success: %v", err)
	}
}

func TestCredentialedSetupRemainsUsableThroughLoopbackTunnel(t *testing.T) {
	a, _, credential := newSetupWebapp(t)
	created := serveSetup(a, newSetupRequest(http.MethodPost, "/setup", localSetupPeer, setupForm("operator", credential)))
	if created.Code != http.StatusSeeOther {
		t.Fatalf("credentialed loopback POST /setup status = %d, want 303", created.Code)
	}
}

func TestLoopbackSetupWithProxyMetadataFailsClosed(t *testing.T) {
	for _, tt := range []struct {
		name, header, value string
	}{
		{name: "standard forwarded", header: "Forwarded", value: "for=192.0.2.10"},
		{name: "forwarded for", header: "X-Forwarded-For", value: "192.0.2.10"},
		{name: "forwarded proto", header: "X-Forwarded-Proto", value: "https"},
		{name: "real ip", header: "X-Real-IP", value: "192.0.2.10"},
		{name: "via", header: "Via", value: "1.1 proxy.example"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, path, _ := newSetupWebapp(t)
			get := newSetupRequest(http.MethodGet, "/setup", localSetupPeer, nil)
			get.Header.Set(tt.header, tt.value)
			if rec := serveSetup(a, get); rec.Code != http.StatusNotFound {
				t.Fatalf("proxied local GET /setup status = %d, want 404", rec.Code)
			}

			post := newSetupRequest(http.MethodPost, "/setup", localSetupPeer, setupForm("operator", ""))
			post.Header.Set(tt.header, tt.value)
			if rec := serveSetup(a, post); rec.Code != http.StatusNotFound {
				t.Fatalf("proxied local POST /setup status = %d, want 404", rec.Code)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("proxied setup consumed credential: %v", err)
			}
		})
	}
}

func TestRemoteHeadersCannotClaimLocality(t *testing.T) {
	a, _, _ := newSetupWebapp(t)
	req := newSetupRequest(http.MethodGet, "/setup", remoteSetupPeer, nil)
	req.Host = "localhost"
	req.Header.Set("Origin", "http://localhost")
	req.Header.Set("Referer", "http://localhost/setup")
	if rec := serveSetup(a, req); rec.Code != http.StatusNotFound {
		t.Fatalf("remote GET with local-looking headers status = %d, want 404", rec.Code)
	}
}

func TestConcurrentLocalSetupCreatesOneOperator(t *testing.T) {
	a, path, _ := newSetupWebapp(t)
	tokens := make([]string, 2)
	cookies := make([]*http.Cookie, 2)
	for i := range tokens {
		tokens[i], cookies[i], _ = beginLocalSetup(t, a)
	}
	start := make(chan struct{})
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for i, username := range []string{"operator-one", "operator-two"} {
		wg.Add(1)
		go func(username, token string, cookie *http.Cookie) {
			defer wg.Done()
			<-start
			form := setupForm(username, "")
			form.Set("local_token", token)
			request := newSetupRequest(http.MethodPost, "/setup", localSetupPeer, form)
			request.AddCookie(cookie)
			codes <- serveSetup(a, request).Code
		}(username, tokens[i], cookies[i])
	}
	close(start)
	wg.Wait()
	close(codes)

	got := make([]int, 0, 2)
	for code := range codes {
		got = append(got, code)
	}
	sort.Ints(got)
	want := []int{http.StatusSeeOther, http.StatusNotFound}
	sort.Ints(want)
	if got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("concurrent setup statuses = %v, want %v", got, want)
	}
	count, err := a.store.Operators().Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("operator count = %d, want 1", count)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("setup credential remains after concurrent success: %v", err)
	}
}

func newSetupWebapp(t *testing.T) (*webapp, string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "setup-credential")
	if err := auth.EnsureSetupCredential(path, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlitestore.Open(context.Background(), filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a := &webapp{store: st, now: time.Now, setupTokens: newSetupTokenTracker(), setupCredentialPath: path}
	a.tmpl, err = loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	return a, path, strings.TrimSpace(string(raw))
}

var localTokenPattern = regexp.MustCompile(`name="local_token" value="([A-Za-z0-9_-]+)"`)

func beginLocalSetup(t *testing.T, a *webapp) (string, *http.Cookie, *httptest.ResponseRecorder) {
	t.Helper()
	page := serveSetup(a, newSetupRequest(http.MethodGet, "/setup", localSetupPeer, nil))
	if page.Code != http.StatusOK {
		t.Fatalf("local GET /setup status = %d, want 200", page.Code)
	}
	token, cookie := setupTokenFromResponse(t, page)
	return token, cookie, page
}

func setupTokenFromResponse(t *testing.T, response *httptest.ResponseRecorder) (string, *http.Cookie) {
	t.Helper()
	match := localTokenPattern.FindStringSubmatch(response.Body.String())
	if len(match) != 2 {
		t.Fatal("setup response has no local setup token")
	}
	var setupCookie *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == localSetupCookieName {
			setupCookie = cookie
			break
		}
	}
	if setupCookie == nil {
		t.Fatal("setup response has no local setup cookie")
	}
	if setupCookie.Value != match[1] {
		t.Fatal("local setup cookie and form token differ")
	}
	if !setupCookie.HttpOnly || setupCookie.SameSite != http.SameSiteStrictMode || setupCookie.Path != "/setup" {
		t.Fatalf("local setup cookie attributes = HttpOnly:%v SameSite:%v Path:%q", setupCookie.HttpOnly, setupCookie.SameSite, setupCookie.Path)
	}
	return match[1], setupCookie
}

func hasLocalSetupCookie(response *httptest.ResponseRecorder) bool {
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == localSetupCookieName {
			return true
		}
	}
	return false
}

func setupForm(username, credential string) url.Values {
	return url.Values{
		"credential":   {credential},
		"username":     {username},
		"password":     {setupPassword},
		"confirmation": {setupPassword},
	}
}

func newSetupRequest(method, target, remoteAddr string, form url.Values) *http.Request {
	var body *strings.Reader
	if form == nil {
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	req.RemoteAddr = remoteAddr
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return req
}

func serveSetup(a *webapp, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	if req.Method == http.MethodGet {
		a.handleSetupPage(rec, req)
	} else {
		a.handleSetupSubmit(rec, req)
	}
	return rec
}
