package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/angeltonio/aliasdeck/internal/store/sqlitestore"
)

func TestAuthenticatedMutationsRequireSessionBoundCSRF(t *testing.T) {
	a, h, st := newSecurityWebapp(t, nil)
	cookieA, csrfA := issueWebSession(t, a, st, "operator-a")
	cookieB, csrfB := issueWebSession(t, a, st, "operator-b")
	if csrfA == csrfB {
		t.Fatal("distinct sessions received the same CSRF token")
	}

	page := doWebRequest(h, http.MethodGet, "/aliases", nil, cookieA, "")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `name="csrf-token" content="`+csrfA+`"`) {
		t.Fatalf("authenticated page omitted its derived CSRF token: status=%d body=%q", page.Code, page.Body.String())
	}

	for _, tt := range []struct {
		name, token string
	}{
		{name: "missing"},
		{name: "invalid", token: "invalid-token"},
		{name: "other session", token: csrfB},
	} {
		t.Run(tt.name, func(t *testing.T) {
			form := url.Values{"name": {"safe"}, "command": {"printf safe"}}
			if tt.token != "" {
				form.Set(csrfFormField, tt.token)
			}
			rec := doWebRequest(h, http.MethodPost, "/aliases", form, cookieA, "")
			if rec.Code != http.StatusForbidden {
				t.Fatalf("POST /aliases status = %d, want 403", rec.Code)
			}
		})
	}

	created := doWebRequest(h, http.MethodPost, "/aliases", url.Values{
		csrfFormField: {csrfA}, "name": {"safe"}, "command": {"printf safe"},
	}, cookieA, "")
	if created.Code != http.StatusOK {
		t.Fatalf("valid alias create status = %d body=%q", created.Code, created.Body.String())
	}
	aliases, err := st.Aliases().List(context.Background())
	if err != nil || len(aliases) != 1 {
		t.Fatalf("persisted aliases = %v, err=%v", aliases, err)
	}

	// The edit route is a mutation like any other, so it has to be inside
	// this test rather than only in the handler's own: a route reachable
	// with a session but no CSRF token would let another origin rewrite an
	// operator's aliases.
	editPath := "/aliases/" + aliases[0].ID
	editForm := url.Values{"name": {"renamed"}, "command": {"printf renamed"}}
	if rec := doWebRequest(h, http.MethodPut, editPath, editForm, cookieA, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("HTMX edit without token status = %d, want 403", rec.Code)
	}
	if rec := doWebRequest(h, http.MethodPut, editPath, editForm, cookieA, csrfB); rec.Code != http.StatusForbidden {
		t.Fatalf("HTMX edit with another session's token status = %d, want 403", rec.Code)
	}
	accepted := url.Values{csrfFormField: {csrfA}, "name": {"renamed"}, "command": {"printf renamed"}}
	if rec := doWebRequest(h, http.MethodPut, editPath, accepted, cookieA, ""); rec.Code != http.StatusOK {
		t.Fatalf("HTMX edit with form token status = %d body=%q", rec.Code, rec.Body.String())
	}
	edited, err := st.Aliases().Get(context.Background(), aliases[0].ID)
	if err != nil || edited.Name != "renamed" {
		t.Fatalf("edited alias = %+v, err=%v; want the rename applied", edited, err)
	}

	// The group screen's mutations are new routes, and a route reachable
	// with a session but no CSRF token is exactly what this test exists to
	// catch — so every one of them is checked here, not only in its own
	// handler test.
	if rec := doWebRequest(h, http.MethodPost, "/profiles", url.Values{"name": {"servers"}}, cookieA, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("group create without token status = %d, want 403", rec.Code)
	}
	group := doWebRequest(h, http.MethodPost, "/profiles", url.Values{csrfFormField: {csrfA}, "name": {"servers"}}, cookieA, "")
	if group.Code != http.StatusOK {
		t.Fatalf("group create with token status = %d body=%q", group.Code, group.Body.String())
	}
	groups, err := st.Profiles().List(context.Background())
	if err != nil || len(groups) != 1 {
		t.Fatalf("persisted groups = %v, err=%v", groups, err)
	}
	groupPath := "/profiles/" + groups[0].ID
	if rec := doWebRequest(h, http.MethodPut, groupPath, url.Values{"name": {"renamed"}}, cookieA, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("group edit without token status = %d, want 403", rec.Code)
	}
	if rec := doWebRequest(h, http.MethodDelete, groupPath, nil, cookieA, csrfB); rec.Code != http.StatusForbidden {
		t.Fatalf("group delete with another session's token status = %d, want 403", rec.Code)
	}
	if rec := doWebRequest(h, http.MethodDelete, groupPath, nil, cookieA, csrfA); rec.Code != http.StatusOK {
		t.Fatalf("group delete with header token status = %d body=%q", rec.Code, rec.Body.String())
	}

	deletePath := "/aliases/" + aliases[0].ID
	if rec := doWebRequest(h, http.MethodDelete, deletePath, nil, cookieA, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("HTMX delete without token status = %d, want 403", rec.Code)
	}
	if rec := doWebRequest(h, http.MethodDelete, deletePath, nil, cookieA, csrfA); rec.Code != http.StatusOK {
		t.Fatalf("HTMX delete with header token status = %d body=%q", rec.Code, rec.Body.String())
	}

	// Device membership editing is a mutation too, and it is the one that
	// decides which machines receive which aliases — so it belongs here.
	if rec := doWebRequest(h, http.MethodPut, "/devices/some-id", url.Values{"name": {"x"}}, cookieA, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("device edit without token status = %d, want 403", rec.Code)
	}
	if rec := doWebRequest(h, http.MethodPut, "/devices/some-id", url.Values{"name": {"x"}}, cookieA, csrfB); rec.Code != http.StatusForbidden {
		t.Fatalf("device edit with another session's token status = %d, want 403", rec.Code)
	}

	mint := doWebRequest(h, http.MethodPost, "/devices/add/token", url.Values{
		csrfFormField: {csrfA}, "autoSync": {"true"},
	}, cookieA, csrfA)
	if mint.Code != http.StatusOK || !strings.Contains(mint.Body.String(), "aliasdeck register") {
		t.Fatalf("HTMX mint with CSRF status=%d body=%q", mint.Code, mint.Body.String())
	}

	if rec := doWebRequest(h, http.MethodPost, "/logout", nil, cookieB, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("logout without CSRF status = %d, want 403", rec.Code)
	}
	if _, ok := authenticate(requestWithCookie(cookieB), st.Tokens(), a.now); !ok {
		t.Fatal("rejected logout revoked the session")
	}
	loggedOut := doWebRequest(h, http.MethodPost, "/logout", url.Values{csrfFormField: {csrfB}}, cookieB, "")
	if loggedOut.Code != http.StatusSeeOther {
		t.Fatalf("valid logout status = %d, want 303", loggedOut.Code)
	}
	if _, ok := authenticate(requestWithCookie(cookieB), st.Tokens(), a.now); ok {
		t.Fatal("valid logout did not revoke the session")
	}

	_, rotated := issueWebSession(t, a, st, "operator-a")
	if rotated == csrfA {
		t.Fatal("new login/session did not rotate the CSRF token")
	}
}

func TestExplicitHTTPSPublicURLControlsCookiesAndEnrollmentCommands(t *testing.T) {
	public, _ := url.Parse("https://aliases.example:8443")
	_, h, st := newSecurityWebapp(t, public)
	hash, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Operators().Create(context.Background(), store.Operator{Username: "admin", PasswordHash: []byte(hash)}); err != nil {
		t.Fatal(err)
	}

	login := doWebRequest(h, http.MethodPost, "/login", url.Values{
		"username": {"admin"}, "password": {"correct horse battery staple"},
	}, nil, "")
	if login.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d body=%q", login.Code, login.Body.String())
	}
	var session *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == sessionCookieName {
			session = c
		}
	}
	if session == nil || !session.Secure {
		t.Fatalf("session cookie behind explicit HTTPS origin = %#v, want Secure", session)
	}
	parsed, err := auth.Parse(session.Value)
	if err != nil {
		t.Fatal(err)
	}
	csrf := sessionCSRF(parsed.Secret, parsed.Lookup)
	mint := doWebRequest(h, http.MethodPost, "/devices/add/token", url.Values{csrfFormField: {csrf}}, session, csrf)
	if mint.Code != http.StatusOK {
		t.Fatalf("mint status = %d body=%q", mint.Code, mint.Body.String())
	}
	if !strings.Contains(mint.Body.String(), "https://aliases.example:8443") || strings.Contains(mint.Body.String(), "http://internal.test") {
		t.Fatalf("enrollment command did not use explicit public HTTPS origin: %q", mint.Body.String())
	}
}

func TestWebLoginSharesLimiterAndRecoversAfterSaturation(t *testing.T) {
	a, h, st := newSecurityWebapp(t, nil)
	hash, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Operators().Create(context.Background(), store.Operator{Username: "admin", PasswordHash: []byte(hash)}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < auth.PasswordVerificationConcurrency; i++ {
		if !a.loginLimiter.TryAcquire() {
			t.Fatal("could not saturate password limiter")
		}
	}
	form := url.Values{"username": {"admin"}, "password": {"correct horse battery staple"}}
	overloaded := doWebRequest(h, http.MethodPost, "/login", form, nil, "")
	if overloaded.Code != http.StatusServiceUnavailable || !strings.Contains(overloaded.Body.String(), "too many password checks") {
		t.Fatalf("overloaded login status=%d body=%q", overloaded.Code, overloaded.Body.String())
	}
	for i := 0; i < auth.PasswordVerificationConcurrency; i++ {
		a.loginLimiter.Release()
	}
	recovered := doWebRequest(h, http.MethodPost, "/login", form, nil, "")
	if recovered.Code != http.StatusSeeOther {
		t.Fatalf("login after limiter recovery status=%d body=%q", recovered.Code, recovered.Body.String())
	}
}

func TestWebAliasCreateLocalizesConflictAndCapacity(t *testing.T) {
	a, h, st := newSecurityWebapp(t, nil)
	cookie, csrf := issueWebSession(t, a, st, "operator")
	form := url.Values{csrfFormField: {csrf}, "name": {"dup"}, "command": {"true"}}
	if rec := doWebRequest(h, http.MethodPost, "/aliases", form, cookie, ""); rec.Code != http.StatusOK {
		t.Fatalf("first create status=%d body=%q", rec.Code, rec.Body.String())
	}
	duplicate := doWebRequest(h, http.MethodPost, "/aliases", form, cookie, "")
	if duplicate.Code != http.StatusConflict || !strings.Contains(duplicate.Body.String(), "already exists") {
		t.Fatalf("duplicate create status=%d body=%q", duplicate.Code, duplicate.Body.String())
	}

	a.store = aliasOverrideStore{Store: st, aliases: capacityAliasRepo{AliasRepo: st.Aliases()}}
	spanish := url.Values{csrfFormField: {csrf}, "name": {"otro"}, "command": {"true"}}
	req := httptest.NewRequest(http.MethodPost, "/aliases", strings.NewReader(spanish.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept-Language", "es")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "máximo de 5000 alias") {
		t.Fatalf("capacity status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func newSecurityWebapp(t *testing.T, publicURL *url.URL) (*webapp, http.Handler, *sqlitestore.SQLiteStore) {
	t.Helper()
	st, err := sqlitestore.Open(context.Background(), filepath.Join(t.TempDir(), "aliasdeck.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	tmpl, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	a := &webapp{store: st, now: func() time.Time { return now }, tmpl: tmpl, enrollments: newEnrollmentTracker(), setupTokens: newSetupTokenTracker(), publicURL: publicURL, loginLimiter: auth.NewPasswordLimiter()}
	h, err := newMux(a)
	if err != nil {
		t.Fatal(err)
	}
	return a, h, st
}

func issueWebSession(t *testing.T, a *webapp, st store.Store, operatorID string) (*http.Cookie, string) {
	t.Helper()
	minted, err := auth.Mint(store.TokenKindSession)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Tokens().Create(context.Background(), store.Token{Kind: store.TokenKindSession, SubjectID: operatorID, Lookup: minted.Lookup, SecretHash: minted.SecretHash, CreatedAt: a.now(), ExpiresAt: a.now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	parsed, _ := auth.Parse(minted.Wire)
	return &http.Cookie{Name: sessionCookieName, Value: minted.Wire}, sessionCSRF(parsed.Secret, parsed.Lookup)
}

func doWebRequest(h http.Handler, method, path string, form url.Values, cookie *http.Cookie, csrfHeaderValue string) *httptest.ResponseRecorder {
	var body *strings.Reader
	if form == nil {
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	req.Host = "internal.test"
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrfHeaderValue != "" {
		req.Header.Set(csrfHeader, csrfHeaderValue)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func requestWithCookie(cookie *http.Cookie) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(cookie)
	return r
}

type aliasOverrideStore struct {
	store.Store
	aliases store.AliasRepo
}

func (s aliasOverrideStore) Aliases() store.AliasRepo { return s.aliases }

type capacityAliasRepo struct{ store.AliasRepo }

func (capacityAliasRepo) CreateWithinLimit(context.Context, domain.Alias, int) (domain.Alias, error) {
	return domain.Alias{}, store.ErrCapacity
}
